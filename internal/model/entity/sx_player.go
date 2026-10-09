// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

// SxPlayer is the golang structure for table sx_player.
type SxPlayer struct {
	Id     uint   `json:"id"     orm:"id"     description:""`
	Code   string `json:"code"   orm:"code"   description:""`
	Name   string `json:"name"   orm:"name"   description:""`
	Parse  string `json:"parse"  orm:"parse"  description:""`
	Status int    `json:"status" orm:"status" description:""`
}
