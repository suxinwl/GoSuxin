// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

// SxEmailCode is the golang structure for table sx_email_code.
type SxEmailCode struct {
	Id      uint   `json:"id"      orm:"id"      description:""`
	Email   string `json:"email"   orm:"email"   description:""`
	Code    string `json:"code"    orm:"code"    description:""`
	Type    string `json:"type"    orm:"type"    description:""`
	Expire  uint   `json:"expire"  orm:"expire"  description:""`
	Used    int    `json:"used"    orm:"used"    description:""`
	Created uint   `json:"created" orm:"created" description:""`
}
