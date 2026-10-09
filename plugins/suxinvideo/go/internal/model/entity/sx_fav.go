// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

// SxFav is the golang structure for table sx_fav.
type SxFav struct {
	Id      uint `json:"id"      orm:"id"      description:""`
	UserId  uint `json:"userId"  orm:"user_id" description:""`
	VodId   uint `json:"vodId"   orm:"vod_id"  description:""`
	Created uint `json:"created" orm:"created" description:""`
}
