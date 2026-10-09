// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

// SxComment is the golang structure for table sx_comment.
type SxComment struct {
	Id      uint   `json:"id"      orm:"id"      description:""`
	UserId  uint   `json:"userId"  orm:"user_id" description:""`
	VodId   uint   `json:"vodId"   orm:"vod_id"  description:""`
	Content string `json:"content" orm:"content" description:""`
	Status  int    `json:"status"  orm:"status"  description:""`
	Created uint   `json:"created" orm:"created" description:""`
}
