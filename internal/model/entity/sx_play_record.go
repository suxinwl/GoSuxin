// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

// SxPlayRecord is the golang structure for table sx_play_record.
type SxPlayRecord struct {
	Id       uint `json:"id"       orm:"id"       description:""`
	UserId   uint `json:"userId"   orm:"user_id"  description:""`
	VodId    uint `json:"vodId"    orm:"vod_id"   description:""`
	Episode  int  `json:"episode"  orm:"episode"  description:""`
	Position uint `json:"position" orm:"position" description:""`
	Updated  uint `json:"updated"  orm:"updated"  description:""`
}
