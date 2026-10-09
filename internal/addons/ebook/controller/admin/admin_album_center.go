package admin

import (
	"context"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	adminapi "github.com/suxinwl/GoSuxin/internal/addons/ebook/api/admin"
	albumlogic "github.com/suxinwl/GoSuxin/internal/addons/ebook/logic"
	"github.com/suxinwl/GoSuxin/utility/gf"
)

func (c *ControllerAlbum) CategoryList(ctx context.Context, req *adminapi.CategoryListReq) (*adminapi.CategoryListRes, error) {
	rows, err := albumlogic.Categories(ctx)
	if err != nil {
		return &adminapi.CategoryListRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	albumlogic.PresentCategories(rows, "")
	return &adminapi.CategoryListRes{R: gf.Success().SetData(rows)}, nil
}
func (c *ControllerAlbum) CategorySave(ctx context.Context, req *adminapi.CategorySaveReq) (*adminapi.CategorySaveRes, error) {
	id, err := albumlogic.SaveCategory(ctx, req.Id, req.Name, req.EnglishTitle, req.Description, req.SortOrder, req.EnglishDescription)
	if err != nil {
		return &adminapi.CategorySaveRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	return &adminapi.CategorySaveRes{R: gf.Success().SetData(g.Map{"id": id})}, nil
}
func (c *ControllerAlbum) CategoryCover(ctx context.Context, req *adminapi.CategoryCoverReq) (*adminapi.CategoryCoverRes, error) {
	if err := albumlogic.SetCategoryCover(ctx, req.Id, req.Language, req.File, req.Remove); err != nil {
		return &adminapi.CategoryCoverRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	return &adminapi.CategoryCoverRes{R: gf.Success()}, nil
}
func (c *ControllerAlbum) CategoryDelete(ctx context.Context, req *adminapi.CategoryDeleteReq) (*adminapi.CategoryDeleteRes, error) {
	err := albumlogic.DeleteCategory(ctx, req.Id)
	if err != nil {
		return &adminapi.CategoryDeleteRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	return &adminapi.CategoryDeleteRes{R: gf.Success().SetData(nil)}, nil
}
func (c *ControllerAlbum) Detail(ctx context.Context, req *adminapi.DetailReq) (*adminapi.DetailRes, error) {
	row, pages, err := albumlogic.Pages(ctx, req.Id)
	if err != nil {
		return &adminapi.DetailRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	return &adminapi.DetailRes{R: gf.Success().SetData(g.Map{"album": row, "pages": pages})}, nil
}
func (c *ControllerAlbum) PageList(ctx context.Context, req *adminapi.PageListReq) (*adminapi.PageListRes, error) {
	row, pages, err := albumlogic.Pages(ctx, req.AlbumId)
	if err != nil {
		return &adminapi.PageListRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	return &adminapi.PageListRes{R: gf.Success().SetData(g.Map{"album": row, "pages": pages})}, nil
}
func (c *ControllerAlbum) PageReorder(ctx context.Context, req *adminapi.PageReorderReq) (*adminapi.PageReorderRes, error) {
	err := albumlogic.EditPages(ctx, req.AlbumId, "reorder", req.Ids, nil)
	if err != nil {
		return &adminapi.PageReorderRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	return &adminapi.PageReorderRes{R: gf.Success().SetData(nil)}, nil
}
func (c *ControllerAlbum) PageDelete(ctx context.Context, req *adminapi.PageDeleteReq) (*adminapi.PageDeleteRes, error) {
	err := albumlogic.EditPages(ctx, req.AlbumId, "delete", req.Ids, nil)
	if err != nil {
		return &adminapi.PageDeleteRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	return &adminapi.PageDeleteRes{R: gf.Success().SetData(nil)}, nil
}
func (c *ControllerAlbum) PageCover(ctx context.Context, req *adminapi.PageCoverReq) (*adminapi.PageCoverRes, error) {
	err := albumlogic.EditPages(ctx, req.AlbumId, "cover", []int64{req.PageId}, nil)
	if err != nil {
		return &adminapi.PageCoverRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	return &adminapi.PageCoverRes{R: gf.Success().SetData(nil)}, nil
}
func (c *ControllerAlbum) PageReplace(ctx context.Context, req *adminapi.PageReplaceReq) (*adminapi.PageReplaceRes, error) {
	err := albumlogic.EditPages(ctx, req.AlbumId, "replace", []int64{req.PageId}, req.File)
	if err != nil {
		return &adminapi.PageReplaceRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	return &adminapi.PageReplaceRes{R: gf.Success().SetData(nil)}, nil
}
func (c *ControllerAlbum) ShareList(ctx context.Context, req *adminapi.ShareListReq) (*adminapi.ShareListRes, error) {
	rows, err := albumlogic.ShareList(ctx, req.AlbumId)
	if err != nil {
		return &adminapi.ShareListRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	return &adminapi.ShareListRes{R: gf.Success().SetData(rows)}, nil
}
func (c *ControllerAlbum) ShareUpdate(ctx context.Context, req *adminapi.ShareUpdateReq) (*adminapi.ShareUpdateRes, error) {
	plain, err := albumlogic.UpdateShare(ctx, req.AlbumId, req.Id, req.PasswordMode, req.Password, req.ExpiresAt, req.Permanent, req.Enabled)
	if err != nil {
		return &adminapi.ShareUpdateRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	return &adminapi.ShareUpdateRes{R: gf.Success().SetData(g.Map{"password": plain})}, nil
}

func (c *ControllerAlbum) Permissions(ctx context.Context, req *adminapi.PermissionsReq) (*adminapi.PermissionsRes, error) {
	if albumlogic.CanManageAll(ctx) {
		return &adminapi.PermissionsRes{R: gf.Success().SetData([]string{"*"})}, nil
	}
	roles, err := g.Model("auth_role_access").Ctx(ctx).Where("uid", albumlogic.OwnerID(ctx)).Array("role_id")
	if err != nil {
		return nil, err
	}
	if len(roles) == 0 {
		return &adminapi.PermissionsRes{R: gf.Success().SetData([]string{})}, nil
	}
	buttons, err := g.Model("auth_role").Ctx(ctx).WhereIn("id", roles).Array("btns")
	if err != nil {
		return nil, err
	}
	ids := gf.ArrayMerge(buttons)
	if len(ids) == 0 {
		return &adminapi.PermissionsRes{R: gf.Success().SetData([]string{})}, nil
	}
	paths, err := g.Model("auth_rule").Ctx(ctx).WhereIn("id", ids).Where("type", 2).Where("status", 0).WhereLike("path", "/admin/album/%").Array("path")
	if err != nil {
		return nil, err
	}
	return &adminapi.PermissionsRes{R: gf.Success().SetData(paths)}, nil
}

func (c *ControllerAlbum) PageCleanupRetry(ctx context.Context, req *adminapi.PageCleanupRetryReq) (*adminapi.PageCleanupRetryRes, error) {
	data, err := albumlogic.RetryAssetDeletes(ctx, req.AlbumId)
	if err != nil {
		return &adminapi.PageCleanupRetryRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	return &adminapi.PageCleanupRetryRes{R: gf.Success().SetData(data)}, nil
}
