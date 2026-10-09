// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

// SxUser is the golang structure for table sx_user.
type SxUser struct {
	Id            uint   `json:"id"            orm:"id"              description:""`
	Email         string `json:"email"         orm:"email"           description:""`
	Name          string `json:"name"          orm:"name"            description:""`
	Pwd           string `json:"pwd"           orm:"pwd"             description:""`
	Points        int    `json:"points"        orm:"points"          description:""`
	VipExpire     uint   `json:"vipExpire"     orm:"vip_expire"      description:""`
	Avatar        string `json:"avatar"        orm:"avatar"          description:""`
	Status        int    `json:"status"        orm:"status"          description:""`
	RegIp         string `json:"regIp"         orm:"reg_ip"          description:""`
	RegTime       uint   `json:"regTime"       orm:"reg_time"        description:""`
	EmailVerified int    `json:"emailVerified" orm:"email_verified"  description:""`
	LastLoginTime uint   `json:"lastLoginTime" orm:"last_login_time" description:""`
	LastLoginIp   string `json:"lastLoginIp"   orm:"last_login_ip"   description:""`
	SignDay       uint   `json:"signDay"       orm:"sign_day"        description:""`
}
