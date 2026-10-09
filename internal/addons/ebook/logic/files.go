package album

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
)

// Grants are short-lived, album-scoped capabilities. Restarting invalidates them;
// the persistent share link can always obtain a fresh grant after verification.
var fileKey = func() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}
	return key
}()

type fileGrant struct {
	Album   int64 `json:"a"`
	Owner   int64 `json:"o,omitempty"`
	Share   int64 `json:"s,omitempty"`
	Expires int64 `json:"e"`
	Version int64 `json:"v"`
}

func NewFileGrant(albumID, ownerID, shareID int64, versions ...int64) string {
	version := int64(1)
	if len(versions) > 0 {
		version = versions[0]
	}
	return newFileGrantAt(albumID, ownerID, shareID, version, time.Now())
}

func newFileGrantAt(albumID, ownerID, shareID, version int64, now time.Time) string {
	// Reuse one URL within each half-hour window; lifetime remains 30–60 minutes.
	expires := now.Truncate(30 * time.Minute).Add(time.Hour).Unix()
	b, _ := json.Marshal(fileGrant{albumID, ownerID, shareID, expires, version})
	payload := base64.RawURLEncoding.EncodeToString(b)
	mac := hmac.New(sha256.New, fileKey)
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func parseFileGrant(token string, albumID int64) (fileGrant, error) {
	var grant fileGrant
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return grant, errors.New("invalid grant")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return grant, err
	}
	mac := hmac.New(sha256.New, fileKey)
	mac.Write([]byte(parts[0]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return grant, errors.New("invalid signature")
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return grant, err
	}
	if err = json.Unmarshal(b, &grant); err != nil {
		return grant, err
	}
	if grant.Album != albumID || grant.Expires <= time.Now().Unix() {
		return grant, errors.New("expired grant")
	}
	return grant, nil
}

func FileURL(albumID int64, kind string, pageNo int, grant string) string {
	v := url.Values{"albumId": {fmt.Sprint(albumID)}, "kind": {kind}}
	if pageNo > 0 {
		v.Set("pageNo", fmt.Sprint(pageNo))
	}
	if grant != "" {
		v.Set("grant", grant)
	}
	return "/common/album/file?" + v.Encode()
}

// Stable page identity avoids reusing a page-number URL for different bytes after reorder.
// Legacy links without pageId remain readable for existing clients.
func pageFileURL(albumID, pageID int64, pageNo int, grant string) string {
	if pageID <= 0 {
		return FileURL(albumID, "page", pageNo, grant)
	}
	return FileURL(albumID, "page", 0, grant) + "&pageId=" + fmt.Sprint(pageID)
}

func imageRevision(location string) string {
	digest := sha256.Sum256([]byte(location))
	return "&v=" + fmt.Sprintf("%x", digest[:12])
}

// Never expose source locations in API responses. Files are resolved from the DB,
// not from client-supplied paths, and authorization is checked on every request.
func PresentAlbum(row gdb.Record, pages gdb.Result, grant string) {
	siteSuffix := ""
	if grant == "" {
		if config, err := SiteSettings(); err == nil {
			site := config.ChineseURL
			if row["language"].String() == "en" {
				site = config.EnglishURL
			}
			u, _ := url.Parse(site)
			if u != nil {
				siteSuffix = "&site=" + url.QueryEscape(u.Path)
			}
		}
	}

	id := row["id"].Int64()
	if !row["cover_url"].IsEmpty() {
		row["cover_url"].Set(FileURL(id, "cover", 0, grant) + siteSuffix + imageRevision(row["cover_url"].String()))
	}
	delete(row, "original_url")
	delete(row, "source_key")
	delete(row, "pending_pdf")
	delete(row, "storage_profile_id")
	delete(row, "storage_parent_id")
	for _, page := range pages {
		page["image_url"].Set(pageFileURL(id, page["id"].Int64(), page["page_no"].Int(), grant) + siteSuffix + imageRevision(page["image_url"].String()))
		page["thumbnail_url"] = page["image_url"]
	}
}

// Confine both new private storage and legacy files, including Windows ADS,
// backslashes, dot segments, and symlinks/junctions escaping the storage root.
func confinedFile(root, relative string) (string, error) {
	if relative == "" || strings.ContainsAny(relative, "\\:~\x00") {
		return "", errors.New("invalid file path")
	}
	for _, part := range strings.Split(relative, "/") {
		if part == "" || part == "." || part == ".." || strings.TrimRight(part, " .") != part {
			return "", errors.New("invalid file path")
		}
	}
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	current := base
	for _, part := range strings.Split(relative, "/") {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("storage links are not allowed")
		}
	}
	target, err := filepath.EvalSymlinks(filepath.Join(base, filepath.FromSlash(relative)))
	if err != nil {
		return "", err
	}
	base, err = filepath.EvalSymlinks(base)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errors.New("file outside storage")
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("not a regular file")
	}
	return target, nil
}

func StoredFile(location string) (string, error) {
	if strings.HasPrefix(location, assetPrefix) {
		asset, err := assetRecord(context.Background(), location)
		if err != nil {
			return "", err
		}
		if asset["provider"].String() != "local" {
			return "", errors.New("文件存储在云盘")
		}
		location = asset["local_path"].String()
		if strings.HasPrefix(location, assetPrefix) {
			return "", errors.New("无效资源引用")
		}
	}
	if strings.HasPrefix(location, "/_album/") {
		return confinedFile("storage/albums", strings.TrimPrefix(location, "/_album/"))
	}
	if strings.HasPrefix(location, "/resource/uploads/") {
		return confinedFile("resource/uploads", strings.TrimPrefix(location, "/resource/uploads/"))
	}
	return "", errors.New("画册文件必须使用本地受保护存储")
}

func mayReadFile(ctx context.Context, row gdb.Record, token string) bool {
	if IsPublic(row) && token == "" {
		return NormalizeLanguage(row["language"].String()) == SiteLanguage(ctx)
	}
	grant, err := parseFileGrant(token, row["id"].Int64())
	if err != nil {
		return false
	}
	if grant.Owner > 0 {
		return canManageOwner(ctx, grant.Owner, row["owner_id"].Int64())
	}
	if grant.Share == 0 || row["status"].String() != "published" {
		return false
	}
	share, err := g.Model("album_share").Ctx(ctx).Where("id", grant.Share).Where("album_id", grant.Album).Where("enabled", 1).One()
	return err == nil && !share.IsEmpty() && share["auth_version"].Int64() == grant.Version && (share["expires_at"].IsNil() || share["expires_at"].Time().After(time.Now()))
}

func canManageOwner(ctx context.Context, uid, ownerID int64) bool {
	// Do not keep a capability valid after the administrator account is removed.
	count, err := g.Model("admin").Ctx(ctx).Where("id", uid).Where("status", 0).Count()
	if err != nil || count == 0 {
		return false
	}
	if uid == ownerID {
		return true
	}
	roles, err := g.Model("auth_role_access").Ctx(ctx).Where("uid", uid).Array("role_id")
	if err != nil || len(roles) == 0 {
		return false
	}
	count, err = g.Model("auth_role").Ctx(ctx).WhereIn("id", roles).Where("rules", "*").Count()
	return err == nil && count > 0
}

func ServeAlbumFile(r *ghttp.Request) {
	r.Response.Header().Set("Cache-Control", "private, no-store")
	r.Response.Header().Set("X-Content-Type-Options", "nosniff")
	row, err := g.Model("album").Ctx(r.Context()).Where("id", r.Get("albumId").Int64()).One()
	if err != nil || row.IsEmpty() || !mayReadFile(r.Context(), row, r.Get("grant").String()) {
		r.Response.WriteStatus(http.StatusForbidden)
		return
	}
	pageSelector := g.Map{"album_id": row["id"].Int64()}
	if r.Get("pageId").String() != "" {
		pageSelector["id"] = r.Get("pageId").Int64()
	} else {
		pageSelector["page_no"] = r.Get("pageNo").Int()
	}
	location := ""
	switch r.Get("kind").String() {
	case "cover":
		location = row["cover_url"].String()
	case "page":
		page, e := g.Model("album_page").Ctx(r.Context()).Where(pageSelector).One()
		if e == nil && !page.IsEmpty() {
			location = page["image_url"].String()
		}
	default:
		r.Response.WriteStatus(http.StatusNotFound)
		return
	}
	if strings.HasPrefix(location, assetPrefix) {
		asset, e := assetRecord(r.Context(), location)
		if e != nil || asset["album_id"].Int64() != row["id"].Int64() {
			r.Response.WriteStatus(http.StatusNotFound)
			return
		}
		if asset["provider"].String() == "pan123" {
			file, e := cachedCloudImage(r.Context(), asset)
			if e != nil {
				r.Response.WriteStatus(http.StatusBadGateway)
				return
			}
			defer file.Close()
			// Check again after network IO: a share may have been revoked while resolving its URL.
			fresh, e := g.Model("album").Ctx(r.Context()).Where("id", row["id"]).One()
			if e != nil || fresh.IsEmpty() || !mayReadFile(r.Context(), fresh, r.Get("grant").String()) {
				r.Response.WriteStatus(http.StatusForbidden)
				return
			}
			var referenced bool
			if r.Get("kind").String() == "cover" {
				referenced = fresh["cover_url"].String() == location
			} else {
				n, e := g.Model("album_page").Ctx(r.Context()).Where(pageSelector).Where("image_url", location).Count()
				referenced = e == nil && n > 0
			}
			if !referenced {
				r.Response.WriteStatus(http.StatusNotFound)
				return
			}
			serveImageContent(r, file, imageCacheKey(asset))
			return
		}
	}
	file, err := StoredFile(location)
	if err != nil {
		r.Response.WriteStatus(http.StatusNotFound)
		return
	}
	serveLocalImage(r, file)
}

// Legacy direct URLs must never bypass album authorization. Other application
// attachments remain available. New album uploads never enter this public tree.
func ServeLegacyUpload(r *ghttp.Request) {
	r.Response.Header().Set("Cache-Control", "private, no-store")
	relative := strings.TrimPrefix(r.URL.Path, "/resource/uploads/")
	lower := strings.ToLower(relative)
	if strings.HasPrefix(lower, "album-pdf/") || strings.HasPrefix(lower, "puty/") || strings.HasPrefix(lower, "album/") {
		r.Response.WriteStatus(403)
		return
	}
	location := "/resource/uploads/" + relative
	count, err := g.Model("album").Ctx(r.Context()).Where("cover_url", location).WhereOr("original_url", location).Count()
	if err != nil || count > 0 {
		r.Response.WriteStatus(403)
		return
	}
	count, err = g.Model("album_page").Ctx(r.Context()).Where("image_url", location).WhereOr("thumbnail_url", location).Count()
	if err != nil || count > 0 {
		r.Response.WriteStatus(403)
		return
	}
	file, err := confinedFile("resource/uploads", relative)
	if err != nil {
		r.Response.WriteStatus(404)
		return
	}
	r.Response.ServeFile(file)
}
