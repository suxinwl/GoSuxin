// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// SxAdminLog is the golang structure of table sx_admin_log for DAO operations like Where/Data.
type SxAdminLog struct {
	g.Meta  `orm:"table:sx_admin_log, do:true"`
	Id      any //
	AdminId any //
	Action  any //
	Ip      any //
	Created any //
}
