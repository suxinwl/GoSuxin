package album

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/os/gtime"
)

const assetPrefix = "/_album/asset/"

func assetRecord(ctx context.Context, location string) (gdb.Record, error) {
	if !strings.HasPrefix(location, assetPrefix) {
		return nil, errors.New("不是资源引用")
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(location, assetPrefix), 10, 64)
	if err != nil || id <= 0 {
		return nil, errors.New("无效资源引用")
	}
	row, err := g.Model("album_asset").Ctx(ctx).Where("id", id).One()
	if err != nil {
		return nil, err
	}
	if row.IsEmpty() {
		return nil, errors.New("文件资源不存在")
	}
	return row, nil
}

// PutAsset completes all remote IO before any album mutation transaction starts.
func PutAsset(ctx context.Context, albumID int64, location string, target StorageProfile) (string, error) {
	return putAsset(ctx, albumID, location, target, strconv.FormatInt(albumID, 10))
}
func putAsset(ctx context.Context, albumID int64, location string, target StorageProfile, folder string) (string, error) {
	path, err := StoredFile(location)
	if err != nil {
		return "", err
	}
	checksum, size, err := fileChecksum(path)
	if err != nil {
		return "", err
	}
	data := g.Map{"album_id": albumID, "provider": "local", "local_path": location, "checksum": checksum, "size_bytes": size, "createtime": gtime.Now()}
	if target.ID != "" {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		defer cancel()
		key, err := NewKey()
		if err != nil {
			return "", err
		}
		client := clientFor(target)
		id, err := client.upload(ctx, path, folder+"/"+key+"-"+filepath.Base(path), target.ParentID)
		if err != nil {
			return "", err
		}
		if _, err = client.direct(ctx, id); err != nil {
			return "", err
		}
		data["provider"] = "pan123"
		data["profile_id"] = target.ID
		data["file_id"] = id
		data["local_path"] = ""
	}
	id, err := g.Model("album_asset").Ctx(ctx).Data(data).InsertAndGetId()
	if err != nil {
		return "", err
	}
	return assetPrefix + strconv.FormatInt(id, 10), nil
}
func cleanupTemporary(location string) {
	if path, err := StoredFile(location); err == nil {
		_ = os.Remove(path)
	}
}
func stageImage(ctx context.Context, id int64, fileLocation string, target StorageProfile) (string, error) {
	return PutAsset(ctx, id, fileLocation, target)
}

func cloudURL(ctx context.Context, asset gdb.Record, expires time.Time) (string, error) {
	p, err := storageProfile(asset["profile_id"].String())
	if err != nil {
		return "", err
	}
	raw, err := clientFor(p).direct(ctx, asset["file_id"].Int64())
	if err != nil {
		return "", err
	}
	return profileCDNURL(raw, p, expires)
}
func cloudExpiry(ctx context.Context, row gdb.Record, token string) (time.Time, error) {
	expiry := time.Now().Add(60 * time.Second)
	if token != "" {
		grant, err := parseFileGrant(token, row["id"].Int64())
		if err != nil {
			return expiry, err
		}
		if t := time.Unix(grant.Expires, 0); t.Before(expiry) {
			expiry = t
		}
		if grant.Share > 0 {
			s, err := g.Model("album_share").Ctx(ctx).Where("id", grant.Share).One()
			if err != nil || s.IsEmpty() {
				return expiry, errors.New("分享不可用")
			}
			if !s["expires_at"].IsNil() {
				t := s["expires_at"].Time()
				if t.Before(expiry) {
					expiry = t
				}
			}
		}
	}
	if !expiry.After(time.Now()) {
		return expiry, errors.New("阅读授权已过期")
	}
	return expiry, nil
}

// Original PDFs are never served to guests; remote originals are materialized only for conversion.
func materializeAsset(ctx context.Context, location string) (string, func(), error) {
	if !strings.HasPrefix(location, assetPrefix) {
		p, e := StoredFile(location)
		return p, func() {}, e
	}
	a, e := assetRecord(ctx, location)
	if e != nil {
		return "", func() {}, e
	}
	if a["provider"].String() == "local" {
		p, e := StoredFile(a["local_path"].String())
		return p, func() {}, e
	}
	address, e := cloudURL(ctx, a, time.Now().Add(60*time.Second))
	if e != nil {
		return "", func() {}, e
	}
	req, e := http.NewRequestWithContext(ctx, "GET", address, nil)
	if e != nil {
		return "", func() {}, e
	}
	res, e := panCDNClient(panHTTP).Do(req)
	if e != nil {
		return "", func() {}, errors.New("下载原 PDF 失败")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", func() {}, errors.New("云端原 PDF 不可读取")
	}
	if e = os.MkdirAll("storage/temp", 0700); e != nil {
		return "", func() {}, e
	}
	f, e := os.CreateTemp("storage/temp", "pdf-*.pdf")
	if e != nil {
		return "", func() {}, e
	}
	cleanup := func() { _ = os.Remove(f.Name()) }
	_, e = io.Copy(f, io.LimitReader(res.Body, a["size_bytes"].Int64()+1))
	f.Close()
	if e != nil {
		cleanup()
		return "", func() {}, e
	}
	md5, size, e := fileChecksum(f.Name())
	if e != nil || size != a["size_bytes"].Int64() || md5 != a["checksum"].String() {
		cleanup()
		return "", func() {}, errors.New("原 PDF 下载完整性校验失败")
	}
	return f.Name(), cleanup, nil
}
func CheckStorage(ctx context.Context, id string) error {
	p, err := storageProfile(id)
	if err != nil {
		return err
	}
	if p.ClientID == "" || p.ClientSecret == "" {
		return errors.New("请填写开发者 clientID 和 clientSecret")
	}
	if p.URLAuthEnabled() && p.CDNKey == "" {
		return errors.New("开启 URL 鉴权时，请填写 CDN 鉴权密钥并与 123 控制台保持一致")
	}
	if p.EnglishParentID == nil || *p.EnglishParentID == p.ParentID {
		return errors.New("请填写不同的中文、英文目标目录 ID，再测试连接")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	c := clientFor(p)
	user, err := c.request(ctx, "GET", "https://open-api.123pan.com/api/v1/user/info", nil, true)
	if err != nil {
		return err
	}
	uid := panNumber(user, "uid")
	if uid <= 0 {
		return errors.New("账号 UID 无效")
	}
	if p.UID != 0 && p.UID != uid {
		return errors.New("账号与原存储连接不一致")
	}
	for _, parent := range []int64{p.ParentID, *p.EnglishParentID} {
		if parent == 0 {
			continue
		}
		d, e := c.detail(ctx, parent)
		if e != nil {
			return e
		}
		if panNumber(d, "type") != 1 || panNumber(d, "trashed") != 0 {
			return errors.New("目标目录不存在或在回收站")
		}
	}
	if err = os.MkdirAll("storage/temp", 0700); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("storage/temp", "probe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aZ1sAAAAASUVORK5CYII=")
	key, err := NewKey()
	if err != nil {
		return err
	}
	for _, parent := range []int64{p.ParentID, *p.EnglishParentID} {
		for name, body := range map[string][]byte{"probe.png": png, "probe.pdf": storageProbePDF()} {
			path := filepath.Join(dir, name)
			if err = os.WriteFile(path, body, 0600); err != nil {
				return err
			}
			fileID, e := c.upload(ctx, path, "_album_connection_test/"+key+"/"+name, parent)
			if e != nil {
				return e
			}
			raw, e := c.direct(ctx, fileID)
			if e != nil {
				return e
			}
			if !p.URLAuthEnabled() {
				if e = probeCDN(ctx, c.http, raw, true); e != nil {
					return fmt.Errorf("无签名直链访问失败：请确认 123 控制台 URL 鉴权已关闭，并检查直链权限：%w", e)
				}
				continue
			}
			good, e := signPanURL(raw, uid, p.CDNKey, time.Now().Add(60*time.Second))
			if e != nil {
				return e
			}
			bad, _ := signPanURL(raw, uid, p.CDNKey+"invalid", time.Now().Add(60*time.Second))
			for _, check := range []struct {
				url   string
				valid bool
				label string
			}{{raw, false, "无签名拒绝测试"}, {bad, false, "错误签名拒绝测试"}, {good, true, "有效签名访问测试"}} {
				if e = probeCDN(ctx, c.http, check.url, check.valid); e != nil {
					return fmt.Errorf("%s：%w", check.label, e)
				}
			}
			expires := time.Now().Add(5 * time.Second)
			short, _ := signPanURL(raw, uid, p.CDNKey, expires)
			if e = probeCDN(ctx, c.http, short, true); e != nil {
				return fmt.Errorf("短签名访问测试：%w", e)
			}
			if e = pause(ctx, time.Until(expires)+time.Second); e != nil {
				return e
			}
			if e = probeCDN(ctx, c.http, short, false); e != nil {
				return fmt.Errorf("过期签名拒绝测试：%w", e)
			}
		}
	}
	return markStorageVerified(p, uid)
}
func probeCDN(ctx context.Context, client *http.Client, address string, valid bool) error {
	req, e := http.NewRequestWithContext(ctx, "GET", address, nil)
	if e != nil {
		return e
	}
	req.Header.Set("Range", "bytes=0-0")
	res, e := panCDNClient(client).Do(req)
	if e != nil {
		return errors.New("CDN 测试连接失败")
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1024))
	if valid && (res.StatusCode == 200 || res.StatusCode == 206) {
		return nil
	}
	if !valid && (res.StatusCode == 401 || res.StatusCode == 403) {
		return nil
	}
	return fmt.Errorf("CDN 鉴权验证失败（HTTP %d）：请检查 URL 鉴权开关、密钥与目录直链权限", res.StatusCode)
}
func storageProbePDF() []byte {
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << >> >>"}
	offsets := []int{0}
	for i, o := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	b.WriteString("xref\n0 4\n0000000000 65535 f \n")
	for _, o := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)
	return []byte(b.String())
}
