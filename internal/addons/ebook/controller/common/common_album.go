package common

import (
	"context"
	"crypto/sha256"
	"fmt"

	publicapi "github.com/suxinwl/GoSuxin/internal/addons/ebook/api/common"
	albumlogic "github.com/suxinwl/GoSuxin/internal/addons/ebook/logic"
	"github.com/suxinwl/GoSuxin/utility/gf"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/os/gtime"
	"golang.org/x/crypto/bcrypt"
)

type ControllerAlbumPublic struct{}

func NewAlbumPublic() *ControllerAlbumPublic { return &ControllerAlbumPublic{} }

func (c *ControllerAlbumPublic) List(ctx context.Context, req *publicapi.ListReq) (*publicapi.ListRes, error) {
	page, size := req.Page, req.PageSize
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 50 {
		size = 24
	}
	model := g.Model("album").Ctx(ctx).Where("visibility", "public").Where("status", "published")
	model = model.Where("language", albumlogic.SiteLanguage(ctx))
	if req.CategoryId > 0 {
		model = model.Where("category_id", req.CategoryId)
	} else if req.Category != "" {
		model = model.Where("category", req.Category)
	}
	total, err := model.Count()
	if err != nil {
		return &publicapi.ListRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	items, err := model.Fields("id,title,description,category,category_id,language,cover_url,page_count,published_at").Order("sort_order DESC,published_at DESC").Page(page, size).All()
	if err != nil {
		return &publicapi.ListRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	for _, item := range items {
		albumlogic.LocalizeAlbum(ctx, item)
		albumlogic.PresentAlbum(item, nil, "")
	}
	return &publicapi.ListRes{R: gf.Success().SetData(g.Map{"items": items, "page": page, "pageSize": size, "total": total})}, nil
}

func (c *ControllerAlbumPublic) Detail(ctx context.Context, req *publicapi.DetailReq) (*publicapi.DetailRes, error) {
	album, err := albumlogic.PublicAlbum(ctx, req.Id)
	if err != nil || album.IsEmpty() {
		return &publicapi.DetailRes{R: gf.Failed().SetMsg("公开画册不存在")}, nil
	}
	pages, err := g.Model("album_page").Ctx(ctx).Where("album_id", req.Id).Order("page_no ASC").All()
	if err != nil {
		return &publicapi.DetailRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	albumlogic.PresentAlbum(album, pages, "")
	albumlogic.LocalizeAlbum(ctx, album)
	return &publicapi.DetailRes{R: gf.Success().SetData(g.Map{"album": album, "pages": pages})}, nil
}

func (c *ControllerAlbumPublic) Share(ctx context.Context, req *publicapi.ShareReq) (*publicapi.ShareRes, error) {
	album, share, err := albumlogic.AccessibleShare(ctx, req.Key)
	if err != nil {
		return &publicapi.ShareRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	hash := share["password_hash"].String()
	if hash != "" && req.Password == "" {
		return &publicapi.ShareRes{R: gf.Failed().SetCode(4101).SetMsg("请输入分享密码")}, nil
	}
	if hash != "" && bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		return &publicapi.ShareRes{R: gf.Failed().SetMsg("分享密码错误")}, nil
	}
	pages, err := g.Model("album_page").Ctx(ctx).Where("album_id", album["id"]).Order("page_no ASC").All()
	if err != nil {
		return &publicapi.ShareRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	r := g.RequestFromCtx(ctx)
	ipHash := sha256.Sum256([]byte(r.GetClientIp()))
	_, _ = g.Model("album_visit").Ctx(ctx).Data(g.Map{"album_id": album["id"], "share_id": share["id"], "ip_hash": fmt.Sprintf("%x", ipHash[:]), "user_agent": r.Header.Get("User-Agent"), "createtime": gtime.Now()}).Insert()
	_, _ = g.Model("album_share").Ctx(ctx).Where("id", share["id"]).Increment("visit_count", 1)
	albumlogic.PresentAlbum(album, pages, albumlogic.NewFileGrant(album["id"].Int64(), 0, share["id"].Int64(), share["auth_version"].Int64()))
	albumlogic.LocalizeAlbum(ctx, album)
	return &publicapi.ShareRes{R: gf.Success().SetData(g.Map{"album": album, "pages": pages})}, nil
}

func (c *ControllerAlbumPublic) Categories(ctx context.Context, req *publicapi.CategoriesReq) (*publicapi.CategoriesRes, error) {
	rows, err := albumlogic.Categories(ctx)
	if err != nil {
		return &publicapi.CategoriesRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	albumlogic.PresentCategories(rows, albumlogic.SiteLanguage(ctx))
	return &publicapi.CategoriesRes{R: gf.Success().SetData(rows)}, nil
}

func (c *ControllerAlbumPublic) Site(ctx context.Context, req *publicapi.SiteReq) (*publicapi.SiteRes, error) {
	g.RequestFromCtx(ctx).Response.Header().Set("Cache-Control", "no-store")
	data, err := albumlogic.PublicSite(ctx)
	if err != nil {
		return &publicapi.SiteRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	return &publicapi.SiteRes{R: gf.Success().SetData(data)}, nil
}
