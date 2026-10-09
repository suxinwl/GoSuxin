package album

import (
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	album "github.com/suxinwl/GoSuxin/internal/addons/ebook/logic"
	"github.com/suxinwl/GoSuxin/utility/gf"
)

type SiteGetReq struct {
	g.Meta `path:"album/site/get" method:"get" tags:"album"`
}
type SiteGetRes struct{ *gf.R }
type SiteSaveReq struct {
	g.Meta `path:"album/site/save" method:"post" tags:"album"`
	album.SiteConfig
}
type SiteSaveRes struct{ *gf.R }
type SiteLogoReq struct {
	g.Meta `path:"album/site/logo" method:"post" tags:"album"`
	File   *ghttp.UploadFile `p:"file" type:"file" v:"required"`
}
type SiteLogoRes struct{ *gf.R }
