// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

// SxOrder is the golang structure for table sx_order.
type SxOrder struct {
	Id         uint    `json:"id"         orm:"id"          description:""`
	OrderNo    string  `json:"orderNo"    orm:"order_no"    description:""`
	UserId     uint    `json:"userId"     orm:"user_id"     description:""`
	GoodsId    uint    `json:"goodsId"    orm:"goods_id"    description:""`
	Type       string  `json:"type"       orm:"type"        description:""`
	Title      string  `json:"title"      orm:"title"       description:""`
	Amount     float64 `json:"amount"     orm:"amount"      description:""`
	UsdtAmount float64 `json:"usdtAmount" orm:"usdt_amount" description:""`
	PayType    string  `json:"payType"    orm:"pay_type"    description:""`
	Status     int     `json:"status"     orm:"status"      description:""`
	TradeNo    string  `json:"tradeNo"    orm:"trade_no"    description:""`
	Created    uint    `json:"created"    orm:"created"     description:""`
	PaidTime   uint    `json:"paidTime"   orm:"paid_time"   description:""`
}
