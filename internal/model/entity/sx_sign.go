// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

// SxSign is the golang structure for table sx_sign.
type SxSign struct {
	Id     uint `json:"id"     orm:"id"      description:""`
	UserId uint `json:"userId" orm:"user_id" description:""`
	Day    uint `json:"day"    orm:"day"     description:""`
	Points int  `json:"points" orm:"points"  description:""`
}
