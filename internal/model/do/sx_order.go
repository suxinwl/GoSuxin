// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// SxOrder is the golang structure of table sx_order for DAO operations like Where/Data.
type SxOrder struct {
	g.Meta     `orm:"table:sx_order, do:true"`
	Id         any //
	OrderNo    any //
	UserId     any //
	GoodsId    any //
	Type       any //
	Title      any //
	Amount     any //
	UsdtAmount any //
	PayType    any //
	Status     any //
	TradeNo    any //
	Created    any //
	PaidTime   any //
}
