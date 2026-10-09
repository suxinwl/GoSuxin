package admin

import (
	"context"
	"fmt"
	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"strings"

	adminapi "github.com/suxinwl/GoSuxin/internal/addons/ebook/api/admin"
	albumlogic "github.com/suxinwl/GoSuxin/internal/addons/ebook/logic"
	"github.com/suxinwl/GoSuxin/utility/gf"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/os/gtime"
)

type ControllerAlbum struct{}

func NewAlbum() *ControllerAlbum { return &ControllerAlbum{} }

func (c *ControllerAlbum) List(ctx context.Context, req *adminapi.ListReq) (*adminapi.ListRes, error) {
	model := g.Model("album").Ctx(ctx)
	if !albumlogic.CanManageAll(ctx) {
		model = model.Where("owner_id", albumlogic.OwnerID(ctx))
	}
	if req.Status != "" {
		model = model.Where("status", req.Status)
	}
	if req.Language != "" {
		model = model.Where("language", req.Language)
	}
	if req.CategoryId > 0 {
		model = model.Where("category_id", req.CategoryId)
	} else if req.Category != "" {
		model = model.Where("category", req.Category)
	}
	if req.Keyword != "" {
		model = model.WhereLike("title", "%"+req.Keyword+"%")
	}
	if req.Visibility != "" {
		model = model.Where("visibility", req.Visibility)
	}
	page, size := req.Page, req.PageSize
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	items, err := model.Order("sort_order DESC,id DESC").Page(page, size).All()
	if err != nil {
		return &adminapi.ListRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	total, err := model.Count()
	if err != nil {
		return &adminapi.ListRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	for _, item := range items {
		albumlogic.PresentAlbum(item, nil, albumlogic.NewFileGrant(item["id"].Int64(), albumlogic.OwnerID(ctx), 0))
	}
	return &adminapi.ListRes{R: gf.Success().SetData(g.Map{"items": items, "page": page, "pageSize": size, "total": total})}, nil
}

func (c *ControllerAlbum) Save(ctx context.Context, req *adminapi.SaveReq) (*adminapi.SaveRes, error) {
	if strings.TrimSpace(req.Title) == "" {
		return &adminapi.SaveRes{R: gf.Failed().SetMsg("请输入画册标题")}, nil
	}
	visibility := req.Visibility
	if visibility != "public" && visibility != "private" {
		visibility = "private"
	}
	id := req.Id
	err := g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		catModel := tx.Model("album_category")
		if req.CategoryId > 0 {
			catModel = catModel.Where("id", req.CategoryId)
		} else {
			catModel = catModel.Where("name", req.Category)
		}
		cat, err := catModel.LockUpdate().One()
		if err != nil {
			return err
		}
		if cat.IsEmpty() {
			return fmt.Errorf("请选择有效分类")
		}
		data := g.Map{"title": strings.TrimSpace(req.Title), "description": strings.TrimSpace(req.Description), "category": cat["name"], "category_id": cat["id"], "visibility": visibility, "sort_order": req.SortOrder, "updatetime": gtime.Now()}
		if id == 0 {
			data["language"] = albumlogic.NormalizeLanguage(req.Language)
			data["owner_id"] = albumlogic.OwnerID(ctx)
			data["source_type"] = "images"
			data["status"] = "draft"
			data["createtime"] = gtime.Now()
			id, err = tx.Model("album").Data(data).InsertAndGetId()
			return err
		}
		row, err := albumlogic.OwnAlbumModel(ctx, id).TX(tx).LockUpdate().One()
		if err != nil {
			return err
		}
		if row.IsEmpty() {
			return fmt.Errorf("画册不存在或无权修改")
		}
		if req.Language != "" && req.Language != row["language"].String() {
			if row["status"].String() == "published" || row["status"].String() == "processing" {
				return fmt.Errorf("请先下架并等待转换完成，再修改画册语言")
			}
			data["language"] = req.Language
			data["edit_version"] = gdb.Raw("edit_version+1")
		}
		_, err = tx.Model("album").Where("id", id).Data(data).Update()
		return err
	})
	if err != nil {
		return &adminapi.SaveRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	return &adminapi.SaveRes{R: gf.Success().SetData(g.Map{"id": id})}, nil
}

func (c *ControllerAlbum) AddPage(ctx context.Context, req *adminapi.AddPageReq) (*adminapi.AddPageRes, error) {
	page, err := albumlogic.AddImage(ctx, req.AlbumId, req.File, req.PageNo)
	if err != nil {
		return &adminapi.AddPageRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	return &adminapi.AddPageRes{R: gf.Success().SetData(g.Map{"pageNo": page})}, nil
}

func (c *ControllerAlbum) UploadPdf(ctx context.Context, req *adminapi.UploadPdfReq) (*adminapi.UploadPdfRes, error) {
	if err := albumlogic.SubmitPDF(ctx, req.AlbumId, req.File, false); err != nil {
		return &adminapi.UploadPdfRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	return &adminapi.UploadPdfRes{R: gf.Success().SetData(g.Map{"albumId": req.AlbumId, "status": "processing"})}, nil
}

func (c *ControllerAlbum) RetryPdf(ctx context.Context, req *adminapi.RetryPdfReq) (*adminapi.RetryPdfRes, error) {
	if err := albumlogic.SubmitPDF(ctx, req.AlbumId, nil, true); err != nil {
		return &adminapi.RetryPdfRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	return &adminapi.RetryPdfRes{R: gf.Success().SetData(g.Map{"albumId": req.AlbumId, "status": "processing"})}, nil
}

func (c *ControllerAlbum) Publish(ctx context.Context, req *adminapi.PublishReq) (*adminapi.PublishRes, error) {
	if err := albumlogic.SetPublished(ctx, req.Id, req.Publish); err != nil {
		return &adminapi.PublishRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	status := "offline"
	if req.Publish {
		status = "published"
	}
	return &adminapi.PublishRes{R: gf.Success().SetData(g.Map{"status": status})}, nil
}

func (c *ControllerAlbum) CreateShare(ctx context.Context, req *adminapi.CreateShareReq) (*adminapi.CreateShareRes, error) {
	exists, _ := albumlogic.OwnAlbumModel(ctx, req.AlbumId).Where("status", "published").One()
	if exists.IsEmpty() {
		return &adminapi.CreateShareRes{R: gf.Failed().SetMsg("仅可分享已发布的本人画册")}, nil
	}
	expires, err := albumlogic.ShareExpiry(req.ExpiresAt, req.Permanent)
	if err != nil {
		return &adminapi.CreateShareRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	mode := req.PasswordMode
	if req.Password != "" && mode == "random" {
		mode = "custom"
	}
	hash, plain, err := albumlogic.SharePassword(mode, req.Password)
	if err != nil {
		return &adminapi.CreateShareRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	share, err := albumlogic.CreateShare(ctx, req.AlbumId, hash, expires)
	if err != nil {
		return &adminapi.CreateShareRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	return &adminapi.CreateShareRes{R: gf.Success().SetData(g.Map{"password": plain, "expires_at": expires, "key": share["share_key"], "url": fmt.Sprintf("/share/%s", share["share_key"].String())})}, nil
}
