package album

import (
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/utility/gf"
)

type ListReq struct {
	g.Meta     `path:"album/list" method:"get" noValApi:"1" tags:"public-album" summary:"公开画册书架"`
	Category   string `p:"category"`
	CategoryId int64  `p:"categoryId"`
	Page       int    `p:"page" d:"1"`
	PageSize   int    `p:"pageSize" d:"24"`
}
type ListRes struct{ *gf.R }
type DetailReq struct {
	g.Meta `path:"album/detail" method:"get" noValApi:"1" tags:"public-album" summary:"公开画册详情"`
	Id     int64 `p:"id" v:"required"`
}
type DetailRes struct{ *gf.R }
type ShareReq struct {
	g.Meta   `path:"album/share" method:"post" noValApi:"1" tags:"public-album" summary:"读取私密分享画册"`
	Key      string `p:"key" v:"required"`
	Password string `p:"password"`
}
type ShareRes struct{ *gf.R }

type CategoriesReq struct {
	g.Meta `path:"album/categories" method:"get" noValApi:"1" tags:"public-album"`
}
type CategoriesRes struct{ *gf.R }

type SiteReq struct {
	g.Meta `path:"album/site" method:"get" noValApi:"1" tags:"public-album"`
}
type SiteRes struct{ *gf.R }
