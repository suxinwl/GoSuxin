// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

// SxLoginFail is the golang structure for table sx_login_fail.
type SxLoginFail struct {
	Id        uint   `json:"id"        orm:"id"         description:""`
	Type      string `json:"type"      orm:"type"       description:""`
	Account   string `json:"account"   orm:"account"    description:""`
	Ip        string `json:"ip"        orm:"ip"         description:""`
	Fails     int    `json:"fails"     orm:"fails"      description:""`
	BanUntil  uint   `json:"banUntil"  orm:"ban_until"  description:""`
	UpdatedAt uint   `json:"updatedAt" orm:"updated_at" description:""`
}
