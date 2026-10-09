// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// SxComment is the golang structure of table sx_comment for DAO operations like Where/Data.
type SxComment struct {
	g.Meta  `orm:"table:sx_comment, do:true"`
	Id      any //
	UserId  any //
	VodId   any //
	Content any //
	Status  any //
	Created any //
}
