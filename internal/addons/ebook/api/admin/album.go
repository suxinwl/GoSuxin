package album

import (
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/os/gtime"
	"github.com/suxinwl/GoSuxin/utility/gf"
)

type ListReq struct {
	g.Meta     `path:"album/list" method:"get" tags:"album" summary:"画册列表"`
	Page       int    `p:"page" d:"1"`
	PageSize   int    `p:"pageSize" d:"20"`
	Status     string `p:"status"`
	Category   string `p:"category"`
	CategoryId int64  `p:"categoryId"`
	Keyword    string `p:"keyword"`
	Visibility string `p:"visibility"`
	Language   string `p:"language" v:"in:zh,en"`
}
type ListRes struct{ *gf.R }
type SaveReq struct {
	g.Meta      `path:"album/save" method:"post" tags:"album" summary:"创建或编辑画册"`
	Id          int64  `p:"id"`
	Title       string `p:"title" v:"required"`
	Description string `p:"description"`
	Category    string `p:"category"`
	CategoryId  int64  `p:"categoryId"`
	Visibility  string `p:"visibility" d:"private"`
	SortOrder   int    `p:"sortOrder"`
	Language    string `p:"language" v:"in:zh,en"`
}
type SaveRes struct{ *gf.R }
type PublishReq struct {
	g.Meta  `path:"album/publish" method:"post" tags:"album" summary:"发布或下架画册"`
	Id      int64 `p:"id" v:"required"`
	Publish bool  `p:"publish"`
}
type PublishRes struct{ *gf.R }
type AddPageReq struct {
	g.Meta  `path:"album/page/add" method:"post" tags:"album" summary:"上传画册页面"`
	AlbumId int64             `p:"albumId" v:"required"`
	File    *ghttp.UploadFile `p:"file" type:"file" v:"required"`
	PageNo  int               `p:"pageNo" d:"0"`
}
type AddPageRes struct{ *gf.R }
type UploadPdfReq struct {
	g.Meta  `path:"album/pdf/upload" method:"post" tags:"album" summary:"上传 PDF 并异步转换"`
	AlbumId int64             `p:"albumId" v:"required"`
	File    *ghttp.UploadFile `p:"file" type:"file" v:"required"`
}
type UploadPdfRes struct{ *gf.R }
type RetryPdfReq struct {
	g.Meta  `path:"album/pdf/retry" method:"post" tags:"album" summary:"重试 PDF 转换"`
	AlbumId int64 `p:"albumId" v:"required"`
}
type RetryPdfRes struct{ *gf.R }
type CreateShareReq struct {
	g.Meta       `path:"album/share/create" method:"post" tags:"album" summary:"创建分享链接"`
	AlbumId      int64       `p:"albumId" v:"required"`
	Password     string      `p:"password"`
	PasswordMode string      `p:"passwordMode" d:"random"`
	Permanent    bool        `p:"permanent"`
	ExpiresAt    *gtime.Time `p:"expiresAt"`
}
type CreateShareRes struct{ *gf.R }

type CategoryListReq struct {
	g.Meta `path:"album/category/list" method:"get" tags:"album"`
}
type CategoryListRes struct{ *gf.R }
type CategorySaveReq struct {
	g.Meta             `path:"album/category/save" method:"post" tags:"album"`
	Id                 int64  `p:"id"`
	Name               string `p:"name" v:"required|max-length:80"`
	EnglishTitle       string `p:"englishTitle" v:"max-length:120"`
	Description        string `p:"description" v:"max-length:500"`
	EnglishDescription string `p:"englishDescription" v:"max-length:500"`
	SortOrder          int    `p:"sortOrder"`
}
type CategorySaveRes struct{ *gf.R }
type CategoryCoverReq struct {
	g.Meta   `path:"album/category/cover" method:"post" tags:"album"`
	Id       int64             `p:"id" v:"required|min:1"`
	Language string            `p:"language" d:"zh" v:"in:zh,en"`
	Remove   bool              `p:"remove"`
	File     *ghttp.UploadFile `p:"file" type:"file"`
}
type CategoryCoverRes struct{ *gf.R }
type CategoryDeleteReq struct {
	g.Meta `path:"album/category/delete" method:"post" tags:"album"`
	Id     int64 `p:"id" v:"required"`
}
type CategoryDeleteRes struct{ *gf.R }
type DetailReq struct {
	g.Meta `path:"album/detail" method:"get" tags:"album"`
	Id     int64 `p:"id" v:"required"`
}
type DetailRes struct{ *gf.R }
type PageListReq struct {
	g.Meta  `path:"album/page/list" method:"get" tags:"album"`
	AlbumId int64 `p:"albumId" v:"required"`
}
type PageListRes struct{ *gf.R }
type PageReorderReq struct {
	g.Meta  `path:"album/page/reorder" method:"post" tags:"album"`
	AlbumId int64   `p:"albumId" v:"required"`
	Ids     []int64 `p:"ids" v:"required"`
}
type PageReorderRes struct{ *gf.R }
type PageDeleteReq struct {
	g.Meta  `path:"album/page/delete" method:"post" tags:"album"`
	AlbumId int64   `p:"albumId" v:"required"`
	Ids     []int64 `p:"ids" v:"required"`
}
type PageDeleteRes struct{ *gf.R }
type PageCoverReq struct {
	g.Meta  `path:"album/page/cover" method:"post" tags:"album"`
	AlbumId int64 `p:"albumId" v:"required"`
	PageId  int64 `p:"pageId" v:"required"`
}
type PageCoverRes struct{ *gf.R }
type PageReplaceReq struct {
	g.Meta  `path:"album/page/replace" method:"post" tags:"album"`
	AlbumId int64             `p:"albumId" v:"required"`
	PageId  int64             `p:"pageId" v:"required"`
	File    *ghttp.UploadFile `p:"file" type:"file" v:"required"`
}
type PageReplaceRes struct{ *gf.R }
type ShareListReq struct {
	g.Meta  `path:"album/share/list" method:"get" tags:"album"`
	AlbumId int64 `p:"albumId" v:"required"`
}
type ShareListRes struct{ *gf.R }
type ShareUpdateReq struct {
	g.Meta       `path:"album/share/update" method:"post" tags:"album"`
	AlbumId      int64       `p:"albumId" v:"required"`
	Id           int64       `p:"id" v:"required"`
	PasswordMode string      `p:"passwordMode" d:"keep"`
	Password     string      `p:"password"`
	ExpiresAt    *gtime.Time `p:"expiresAt"`
	Permanent    bool        `p:"permanent"`
	Enabled      bool        `p:"enabled"`
}
type ShareUpdateRes struct{ *gf.R }

type PermissionsReq struct {
	g.Meta `path:"album/permissions" method:"get" noAuth:"1" tags:"album"`
}
type PermissionsRes struct{ *gf.R }

type PageCleanupRetryReq struct {
	g.Meta  `path:"album/page/cleanup/retry" method:"post" tags:"album"`
	AlbumId int64 `p:"albumId" v:"required"`
}
type PageCleanupRetryRes struct{ *gf.R }
