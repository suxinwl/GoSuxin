package album

import (
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/utility/gf"
)

type StorageGetReq struct {
	g.Meta `path:"album/storage/get" method:"get" tags:"album"`
}
type StorageGetRes struct{ *gf.R }
type StorageSaveReq struct {
	g.Meta          `path:"album/storage/save" method:"post" tags:"album"`
	ID              string `p:"id" v:"max-length:64"`
	Name            string `p:"name" v:"max-length:100"`
	ClientID        string `p:"clientID" v:"max-length:200"`
	ClientSecret    string `p:"clientSecret" v:"max-length:500"`
	ParentID        int64  `p:"parentId" v:"min:0"`
	EnglishParentID *int64 `p:"englishParentId" v:"min:0"`
	CDNKey          string `p:"cdnKey" v:"max-length:500"`
	URLAuth         *bool  `p:"urlAuth"`
}
type StorageSaveRes struct{ *gf.R }
type StorageTestReq struct {
	g.Meta `path:"album/storage/test" method:"post" tags:"album"`
	ID     string `p:"id" v:"required|max-length:64"`
}
type StorageTestRes struct{ *gf.R }
type StorageActivateReq struct {
	g.Meta `path:"album/storage/activate" method:"post" tags:"album"`
	ID     string `p:"id" v:"required|max-length:64"`
}
type StorageActivateRes struct{ *gf.R }
