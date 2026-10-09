// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// SxLoginFail is the golang structure of table sx_login_fail for DAO operations like Where/Data.
type SxLoginFail struct {
	g.Meta    `orm:"table:sx_login_fail, do:true"`
	Id        any //
	Type      any //
	Account   any //
	Ip        any //
	Fails     any //
	BanUntil  any //
	UpdatedAt any //
}
