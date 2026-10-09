// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

// SxUserVod is the golang structure for table sx_user_vod.
type SxUserVod struct {
	Id      uint `json:"id"      orm:"id"      description:""`
	UserId  uint `json:"userId"  orm:"user_id" description:""`
	VodId   uint `json:"vodId"   orm:"vod_id"  description:""`
	Points  int  `json:"points"  orm:"points"  description:""`
	Created uint `json:"created" orm:"created" description:""`
}
