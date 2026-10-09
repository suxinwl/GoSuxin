package admin

import (
	"context"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	api "github.com/suxinwl/GoSuxin/internal/addons/ebook/api/admin"
	album "github.com/suxinwl/GoSuxin/internal/addons/ebook/logic"
	"github.com/suxinwl/GoSuxin/utility/gf"
)

func (c *ControllerAlbum) SiteGet(ctx context.Context, req *api.SiteGetReq) (*api.SiteGetRes, error) {
	data, err := album.SiteSettings()
	if err != nil {
		return &api.SiteGetRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	return &api.SiteGetRes{R: gf.Success().SetData(data)}, nil
}
func (c *ControllerAlbum) SiteSave(ctx context.Context, req *api.SiteSaveReq) (*api.SiteSaveRes, error) {
	if err := album.SaveSiteSettings(req.SiteConfig); err != nil {
		return &api.SiteSaveRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	return &api.SiteSaveRes{R: gf.Success().SetMsg("站点设置已保存，刷新画册页面后生效")}, nil
}
func (c *ControllerAlbum) SiteLogo(ctx context.Context, req *api.SiteLogoReq) (*api.SiteLogoRes, error) {
	logo, err := album.SaveSiteLogo(req.File)
	if err != nil {
		return &api.SiteLogoRes{R: gf.Failed().SetMsg(err.Error())}, nil
	}
	return &api.SiteLogoRes{R: gf.Success().SetData(g.Map{"url": logo})}, nil
}
