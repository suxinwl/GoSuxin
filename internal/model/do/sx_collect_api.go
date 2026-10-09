// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// SxCollectApi is the golang structure of table sx_collect_api for DAO operations like Where/Data.
type SxCollectApi struct {
	g.Meta       `orm:"table:sx_collect_api, do:true"`
	Id           any //
	Name         any //
	ApiUrl       any //
	Remark       any //
	Status       any //
	CollectAuto  any //
	CollectHours any //
	Addtime      any //
}
