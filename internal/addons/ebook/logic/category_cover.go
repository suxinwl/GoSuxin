package album

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"

	"github.com/suxinwl/GoSuxin/framework/container/gvar"
	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/os/gtime"
)

func categoryCoverField(language string) string {
	if language == "en" {
		return "english_cover_url"
	}
	return "cover_url"
}
func PresentCategories(rows gdb.Result, language string) {
	for _, row := range rows {
		for _, lang := range []string{"zh", "en"} {
			field := categoryCoverField(lang)
			if !row[field].IsEmpty() {
				version := fmt.Sprintf("%x", sha256.Sum256([]byte(row[field].String())))[:12]
				row[field] = gvar.New(fmt.Sprintf("/common/album/category-cover?id=%d&language=%s&v=%s", row["id"].Int64(), lang, version))
			}
		}
		if language == "en" {
			name := row["english_title"].String()
			if name == "" {
				name = row["stable_key"].String()
			}
			row["name"] = gvar.New(name)
			row["description"] = gvar.New(row["english_description"].String())
			row["cover_url"] = gvar.New(row["english_cover_url"].String())
		}
	}
}
func LocalizeAlbum(ctx context.Context, row gdb.Record) {
	if SiteLanguage(ctx) != "en" {
		return
	}
	c, err := g.Model("album_category").Ctx(ctx).Where("id", row["category_id"]).One()
	name := "Catalogue"
	if err == nil && !c.IsEmpty() {
		name = c["english_title"].String()
		if name == "" {
			name = c["stable_key"].String()
		}
	}
	row["category"] = gvar.New(name)
}

// Covers are public category artwork, never a reference to a private album page.
// The API accepts uploaded image bytes, not arbitrary asset IDs or URLs.
func SetCategoryCover(ctx context.Context, id int64, language string, file *ghttp.UploadFile, remove bool) error {
	if language != "zh" && language != "en" {
		return errors.New("无效语言")
	}
	assetLifecycle.RLock()
	defer func() { assetLifecycle.RUnlock(); flushAssetDeletesSoon(0) }()
	field := categoryCoverField(language)
	row, err := g.Model("album_category").Ctx(ctx).Where("id", id).One()
	if err != nil {
		return err
	}
	if row.IsEmpty() {
		return errors.New("分类不存在")
	}
	previous := row[field].String()
	location := ""
	if !remove {
		if file == nil || file.FileHeader == nil || file.Size > 20*1024*1024 {
			return errors.New("请选择不超过 20 MB 的分类封面图片")
		}
		target, e := CurrentStorageTarget()
		if e != nil {
			return e
		}
		target, e = targetForLanguage(target, language)
		if e != nil {
			return e
		}
		temporary, e := savePrivateUpload(0, file, false)
		if e != nil {
			return e
		}
		location, err = putAsset(ctx, 0, temporary, target, fmt.Sprintf("_categories/%d", id))
		if target.ID != "" || err != nil {
			cleanupTemporary(temporary)
		}
		if err != nil {
			return err
		}
	}
	return g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		fresh, e := tx.Model("album_category").Where("id", id).LockUpdate().One()
		if e != nil {
			return e
		}
		if fresh.IsEmpty() || fresh[field].String() != previous {
			return errors.New("分类封面已变化，请刷新后重试")
		}
		if _, e = tx.Model("album_category").Where("id", id).Data(g.Map{field: location, "updatetime": gtime.Now()}).Update(); e != nil {
			return e
		}
		return queueAssetDeletes(ctx, tx, 0, []string{previous})
	})
}

func ServeCategoryCover(r *ghttp.Request) {
	r.Response.Header().Set("Cache-Control", "private, no-store")
	r.Response.Header().Set("X-Content-Type-Options", "nosniff")
	field := categoryCoverField(r.Get("language", SiteLanguage(r.Context())).String())
	row, err := g.Model("album_category").Ctx(r.Context()).Where("id", r.Get("id").Int64()).One()
	if err != nil || row.IsEmpty() || row[field].IsEmpty() {
		r.Response.WriteStatus(http.StatusNotFound)
		return
	}
	location := row[field].String()
	a, err := assetRecord(r.Context(), location)
	if err != nil || a["album_id"].Int64() != 0 {
		r.Response.WriteStatus(http.StatusNotFound)
		return
	}
	if a["provider"].String() == "pan123" {
		file, e := cachedCloudImage(r.Context(), a)
		if e != nil {
			r.Response.WriteStatus(http.StatusBadGateway)
			return
		}
		defer file.Close()
		// The cover may have been replaced or its category deleted during CDN IO.
		n, e := g.Model("album_category").Ctx(r.Context()).Where("id", row["id"]).Where(field, location).Count()
		if e != nil || n == 0 {
			r.Response.WriteStatus(http.StatusNotFound)
			return
		}
		serveImageContent(r, file, imageCacheKey(a))
		return
	}
	path, err := StoredFile(location)
	if err != nil {
		r.Response.WriteStatus(http.StatusNotFound)
		return
	}
	serveLocalImage(r, path)
}
