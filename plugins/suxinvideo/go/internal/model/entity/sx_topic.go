// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

// SxTopic is the golang structure for table sx_topic.
type SxTopic struct {
	Id          uint   `json:"id"          orm:"id"          description:""`
	Name        string `json:"name"        orm:"name"        description:""`
	Pic         string `json:"pic"         orm:"pic"         description:""`
	Description string `json:"description" orm:"description" description:""`
	Content     string `json:"content"     orm:"content"     description:""`
	Status      int    `json:"status"      orm:"status"      description:""`
	Addtime     uint   `json:"addtime"     orm:"addtime"     description:""`
}
