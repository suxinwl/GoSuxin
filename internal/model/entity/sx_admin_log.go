// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

// SxAdminLog is the golang structure for table sx_admin_log.
type SxAdminLog struct {
	Id      uint64 `json:"id"      orm:"id"       description:""`
	AdminId uint64 `json:"adminId" orm:"admin_id" description:""`
	Action  string `json:"action"  orm:"action"   description:""`
	Ip      string `json:"ip"      orm:"ip"       description:""`
	Created uint   `json:"created" orm:"created"  description:""`
}
