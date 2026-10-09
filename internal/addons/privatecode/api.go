package privatecode

import (
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/utility/gf"
)

type Result struct{ *gf.R }
type ListReq struct {
	g.Meta   `path:"privatecode/content/list" method:"get" tags:"privatecode"`
	Page     int    `p:"page" d:"1" v:"min:1"`
	PageSize int    `p:"pageSize" d:"20" v:"between:1,100"`
	Keyword  string `p:"keyword" v:"max-length:120"`
	Cid      int64  `p:"cid" v:"min:0"`
	Status   string `p:"status" v:"in:0,1"`
}
type DetailReq struct {
	g.Meta `path:"privatecode/content/detail" method:"get" tags:"privatecode"`
	Id     int64 `p:"id" v:"required|min:1"`
}
type SaveReq struct {
	g.Meta  `path:"privatecode/content/save" method:"post" tags:"privatecode"`
	Id      int64  `p:"id" v:"min:0"`
	Cid     int64  `p:"cid" v:"required|min:1"`
	Title   string `p:"title" v:"required|max-length:120"`
	Name    string `p:"name" v:"required|regex:^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$"`
	Des     string `p:"des" v:"max-length:1000"`
	Content string `p:"content" v:"max-length:20000"`
	Author  string `p:"author" v:"max-length:80"`
	Status  int    `p:"status" v:"in:0,1"`
}
type DeleteReq struct {
	g.Meta `path:"privatecode/content/delete" method:"post" tags:"privatecode"`
	Id     int64 `p:"id" v:"required|min:1"`
}
type CateListReq struct {
	g.Meta `path:"privatecode/cate/list" method:"get" tags:"privatecode"`
}
type CateSaveReq struct {
	g.Meta `path:"privatecode/cate/save" method:"post" tags:"privatecode"`
	Id     int64  `p:"id" v:"min:0"`
	Name   string `p:"name" v:"required|max-length:80"`
	Remark string `p:"remark" v:"max-length:500"`
	Weigh  int    `p:"weigh"`
}
type CateDeleteReq struct {
	g.Meta `path:"privatecode/cate/delete" method:"post" tags:"privatecode"`
	Id     int64 `p:"id" v:"required|min:1"`
}
type UploadReq struct {
	g.Meta  `path:"privatecode/release/upload" method:"post" tags:"privatecode"`
	Id      int64             `p:"id" v:"required|min:1"`
	Version string            `p:"version" v:"required|regex:^[A-Za-z0-9][A-Za-z0-9._-]{0,31}$"`
	Note    string            `p:"note" v:"max-length:1000"`
	Publish bool              `p:"publish"`
	File    *ghttp.UploadFile `p:"file" type:"file" v:"required"`
}
type DownloadReq struct {
	g.Meta `path:"privatecode/release/download" method:"get" tags:"privatecode"`
	Id     int64 `p:"id" v:"required|min:1"`
}
