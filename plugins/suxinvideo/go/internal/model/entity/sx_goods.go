// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

// SxGoods is the golang structure for table sx_goods.
type SxGoods struct {
	Id     uint    `json:"id"     orm:"id"     description:""`
	Name   string  `json:"name"   orm:"name"   description:""`
	Price  float64 `json:"price"  orm:"price"  description:""`
	Points int     `json:"points" orm:"points" description:""`
	Days   int     `json:"days"   orm:"days"   description:""`
	Sort   int     `json:"sort"   orm:"sort"   description:""`
	Status int     `json:"status" orm:"status" description:""`
}
