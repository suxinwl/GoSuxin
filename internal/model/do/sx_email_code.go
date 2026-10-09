// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// SxEmailCode is the golang structure of table sx_email_code for DAO operations like Where/Data.
type SxEmailCode struct {
	g.Meta  `orm:"table:sx_email_code, do:true"`
	Id      any //
	Email   any //
	Code    any //
	Type    any //
	Expire  any //
	Used    any //
	Created any //
}
