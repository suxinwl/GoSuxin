package privatecode

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/os/gtime"
	"github.com/suxinwl/GoSuxin/internal/addons/support"
	"github.com/suxinwl/GoSuxin/utility/gf"
)

type Controller struct{}

func gtimeNow() *gtime.Time { return gtime.Now() }
func answer(data interface{}, err error) (*Result, error) {
	if err != nil {
		return &Result{gf.Failed().SetMsg(err.Error())}, nil
	}
	return &Result{gf.Success().SetData(data)}, nil
}
func contentModel(ctx context.Context, edit bool) *gdb.Model {
	model := g.Model("privatecode_content").Ctx(ctx)
	if !support.CanManageAll(ctx) {
		if edit {
			model = model.Where("owner_id", support.UID(ctx))
		} else {
			model = model.Where("(owner_id=? OR status=1)", support.UID(ctx))
		}
	}
	return model
}
func (c *Controller) List(ctx context.Context, req *ListReq) (*Result, error) {
	model := contentModel(ctx, false)
	if req.Keyword != "" {
		model = model.Where("(title LIKE ? OR name LIKE ?)", "%"+req.Keyword+"%", "%"+req.Keyword+"%")
	}
	if req.Cid > 0 {
		model = model.Where("cid", req.Cid)
	}
	if req.Status != "" {
		model = model.Where("status", req.Status)
	}
	total, err := model.Clone().Count()
	if err != nil {
		return answer(nil, err)
	}
	items, err := model.Fields("id,owner_id,cid,title,name,des,author,version,status,download,createtime,updatetime").Order("id DESC").Page(req.Page, req.PageSize).All()
	if err != nil {
		return answer(nil, err)
	}
	all := support.CanManageAll(ctx)
	for _, item := range items {
		item["can_edit"] = g.NewVar(all || item["owner_id"].Int64() == support.UID(ctx))
	}
	return answer(g.Map{"items": items, "total": total, "page": req.Page, "pageSize": req.PageSize}, nil)
}
func (c *Controller) Detail(ctx context.Context, req *DetailReq) (*Result, error) {
	row, err := contentModel(ctx, false).Where("id", req.Id).One()
	if err != nil {
		return answer(nil, err)
	}
	if row.IsEmpty() {
		return answer(nil, fmt.Errorf("代码资料不存在或无权访问"))
	}
	releases, err := g.Model("privatecode_release").Ctx(ctx).Where("content_id", req.Id).Fields("id,version,filename,sha256,size,note,download,createtime").Order("id DESC").All()
	row["can_edit"] = g.NewVar(support.CanManageAll(ctx) || row["owner_id"].Int64() == support.UID(ctx))
	return answer(g.Map{"item": row, "releases": releases}, err)
}
func (c *Controller) Save(ctx context.Context, req *SaveReq) (*Result, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return answer(nil, fmt.Errorf("请输入标题"))
	}
	id := req.Id
	err := g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		cat, e := tx.Model("privatecode_cate").Where("id", req.Cid).LockUpdate().One()
		if e != nil {
			return e
		}
		if cat.IsEmpty() {
			return fmt.Errorf("请选择有效分类")
		}
		data := g.Map{"cid": req.Cid, "title": title, "name": req.Name, "des": strings.TrimSpace(req.Des), "content": req.Content, "author": strings.TrimSpace(req.Author), "status": req.Status, "updatetime": gtime.Now()}
		if id == 0 {
			if req.Status != 0 {
				return fmt.Errorf("请先保存资料并上传源码包，再发布")
			}
			data["owner_id"] = support.UID(ctx)
			data["createtime"] = gtime.Now()
			id, e = tx.Model("privatecode_content").Data(data).InsertAndGetId()
			return e
		}
		row, e := contentModel(ctx, true).TX(tx).Where("id", id).LockUpdate().One()
		if e != nil {
			return e
		}
		if row.IsEmpty() {
			return fmt.Errorf("代码资料不存在或无权修改")
		}
		if req.Status == 1 {
			n, e := tx.Model("privatecode_release").Where("content_id", id).Count()
			if e != nil {
				return e
			}
			if n == 0 {
				return fmt.Errorf("请先上传源码包，再发布")
			}
		}
		_, e = tx.Model("privatecode_content").Where("id", id).Data(data).Update()
		return e
	})
	return answer(g.Map{"id": id}, err)
}
func (c *Controller) Delete(ctx context.Context, req *DeleteReq) (*Result, error) {
	var keys []string
	err := g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		row, e := contentModel(ctx, true).TX(tx).Where("id", req.Id).LockUpdate().One()
		if e != nil {
			return e
		}
		if row.IsEmpty() {
			return fmt.Errorf("代码资料不存在或无权删除")
		}
		files, e := tx.Model("privatecode_release").Where("content_id", req.Id).Array("file_key")
		if e != nil {
			return e
		}
		for _, v := range files {
			keys = append(keys, v.String())
		}
		if _, e = tx.Model("privatecode_release").Where("content_id", req.Id).Delete(); e != nil {
			return e
		}
		_, e = tx.Model("privatecode_content").Where("id", req.Id).Delete()
		return e
	})
	if err == nil {
		for _, key := range keys {
			if path, e := packagePath(key); e == nil {
				if e = os.Remove(path); e != nil && !os.IsNotExist(e) {
					g.Log().Warning(ctx, "私有仓文件清理失败", e)
				}
			}
		}
	}
	return answer(nil, err)
}
func (c *Controller) Categories(ctx context.Context, _ *CateListReq) (*Result, error) {
	rows, err := g.Model("privatecode_cate").Ctx(ctx).Order("weigh ASC,id ASC").All()
	return answer(rows, err)
}
func (c *Controller) SaveCategory(ctx context.Context, req *CateSaveReq) (*Result, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return answer(nil, fmt.Errorf("请输入分类名称"))
	}
	data := g.Map{"name": name, "remark": strings.TrimSpace(req.Remark), "weigh": req.Weigh}
	id := req.Id
	var err error
	if id == 0 {
		data["createtime"] = gtime.Now()
		id, err = g.Model("privatecode_cate").Ctx(ctx).Data(data).InsertAndGetId()
	} else {
		result, e := g.Model("privatecode_cate").Ctx(ctx).Where("id", id).Data(data).Update()
		err = e
		if e == nil {
			n, e := g.Model("privatecode_cate").Ctx(ctx).Where("id", id).Count()
			if e != nil {
				err = e
			} else if n == 0 {
				err = fmt.Errorf("分类不存在")
			}
		}
		_ = result
	}
	return answer(g.Map{"id": id}, err)
}
func (c *Controller) DeleteCategory(ctx context.Context, req *CateDeleteReq) (*Result, error) {
	err := g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		row, e := tx.Model("privatecode_cate").Where("id", req.Id).LockUpdate().One()
		if e != nil {
			return e
		}
		if row.IsEmpty() {
			return fmt.Errorf("分类不存在")
		}
		count, e := tx.Model("privatecode_content").Where("cid", req.Id).Count()
		if e != nil {
			return e
		}
		if count > 0 {
			return fmt.Errorf("分类下仍有代码资料，请先调整分类")
		}
		_, e = tx.Model("privatecode_cate").Where("id", req.Id).Delete()
		return e
	})
	return answer(nil, err)
}

var fileKeyPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func packagePath(key string) (string, error) {
	if !fileKeyPattern.MatchString(key) {
		return "", fmt.Errorf("无效源码包标识")
	}
	return filepath.Abs(filepath.Join("storage", "privatecode", "packages", key+".zip"))
}
func (c *Controller) Upload(ctx context.Context, req *UploadReq) (*Result, error) {
	if req.File == nil || req.File.Size < 1 || req.File.Size > maxPackageSize || !strings.EqualFold(filepath.Ext(req.File.Filename), ".zip") {
		return answer(nil, fmt.Errorf("请选择不超过 128 MB 的 ZIP 源码包"))
	}
	row, err := contentModel(ctx, true).Where("id", req.Id).One()
	if err != nil {
		return answer(nil, err)
	}
	if row.IsEmpty() {
		return answer(nil, fmt.Errorf("代码资料不存在或无权修改"))
	}
	dir := filepath.Join("storage", "privatecode", "packages")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return answer(nil, err)
	}
	temp, err := os.CreateTemp(dir, "upload-*.tmp")
	if err != nil {
		return answer(nil, err)
	}
	defer func() { _ = temp.Close(); _ = os.Remove(temp.Name()) }()
	in, err := req.File.Open()
	if err != nil {
		return answer(nil, err)
	}
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(temp, hash), io.LimitReader(in, maxPackageSize+1))
	_ = in.Close()
	if err != nil {
		return answer(nil, err)
	}
	if err = ValidateArchive(temp, size); err != nil {
		return answer(nil, err)
	}
	if err = temp.Sync(); err != nil {
		return answer(nil, err)
	}
	if err = temp.Close(); err != nil {
		return answer(nil, err)
	}
	entropy := make([]byte, 32)
	if _, err = rand.Read(entropy); err != nil {
		return answer(nil, err)
	}
	key := hex.EncodeToString(entropy)
	dest, err := packagePath(key)
	if err != nil {
		return answer(nil, err)
	}
	if err = os.Rename(temp.Name(), dest); err != nil {
		return answer(nil, err)
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	var releaseID int64
	err = g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		row, e := contentModel(ctx, true).TX(tx).Where("id", req.Id).LockUpdate().One()
		if e != nil {
			return e
		}
		if row.IsEmpty() {
			return fmt.Errorf("代码资料不存在或无权修改")
		}
		existing, e := tx.Model("privatecode_release").Where("content_id", req.Id).Where("version", req.Version).Count()
		if e != nil {
			return e
		}
		if existing > 0 {
			return fmt.Errorf("此版本已存在，请使用新的版本号")
		}
		releaseID, e = tx.Model("privatecode_release").Data(g.Map{"content_id": req.Id, "version": req.Version, "filename": row["name"].String() + "-" + req.Version + ".zip", "file_key": key, "sha256": digest, "size": size, "note": req.Note, "createtime": gtime.Now()}).InsertAndGetId()
		if e != nil {
			return e
		}
		data := g.Map{"version": req.Version, "updatetime": gtime.Now()}
		if req.Publish {
			data["status"] = 1
		}
		_, e = tx.Model("privatecode_content").Where("id", req.Id).Data(data).Update()
		return e
	})
	if err != nil {
		_ = os.Remove(dest)
	}
	return answer(g.Map{"id": releaseID, "sha256": digest, "size": size}, err)
}
func (c *Controller) Download(ctx context.Context, req *DownloadReq) (*Result, error) {
	release, err := g.Model("privatecode_release").Ctx(ctx).Where("id", req.Id).One()
	if err != nil {
		return answer(nil, err)
	}
	if release.IsEmpty() {
		return answer(nil, fmt.Errorf("源码版本不存在"))
	}
	row, err := contentModel(ctx, false).Where("id", release["content_id"]).One()
	if err != nil {
		return answer(nil, err)
	}
	if row.IsEmpty() {
		return answer(nil, fmt.Errorf("无权下载此源码包"))
	}
	file, err := packagePath(release["file_key"].String())
	if err != nil {
		return answer(nil, err)
	}
	in, err := os.Open(file)
	if err != nil {
		return answer(nil, fmt.Errorf("源码包文件不存在"))
	}
	defer in.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, in)
	if err != nil {
		return answer(nil, err)
	}
	if n != release["size"].Int64() || hex.EncodeToString(hash.Sum(nil)) != release["sha256"].String() {
		return answer(nil, fmt.Errorf("源码包校验失败，请检查文件完整性"))
	}
	if _, err = g.Model("privatecode_release").Ctx(ctx).Where("id", req.Id).Increment("download", 1); err != nil {
		return answer(nil, err)
	}
	if _, err = g.Model("privatecode_content").Ctx(ctx).Where("id", row["id"]).Increment("download", 1); err != nil {
		return answer(nil, err)
	}
	r := g.RequestFromCtx(ctx)
	r.Response.Header().Set("Cache-Control", "private, no-store")
	r.Response.ServeFileDownload(file, release["filename"].String())
	r.ExitAll()
	return nil, nil
}
