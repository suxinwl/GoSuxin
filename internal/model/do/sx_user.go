// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// SxUser is the golang structure of table sx_user for DAO operations like Where/Data.
type SxUser struct {
	g.Meta        `orm:"table:sx_user, do:true"`
	Id            any //
	Email         any //
	Name          any //
	Pwd           any //
	Points        any //
	VipExpire     any //
	Avatar        any //
	Status        any //
	RegIp         any //
	RegTime       any //
	EmailVerified any //
	LastLoginTime any //
	LastLoginIp   any //
	SignDay       any //
}
