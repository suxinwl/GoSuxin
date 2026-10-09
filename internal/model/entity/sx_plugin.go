// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

// SxPlugin is the golang structure for table sx_plugin.
type SxPlugin struct {
	Id      uint   `json:"id"      orm:"id"      description:""`
	Code    string `json:"code"    orm:"code"    description:""`
	Name    string `json:"name"    orm:"name"    description:""`
	Type    string `json:"type"    orm:"type"    description:""`
	Version string `json:"version" orm:"version" description:""`
	Author  string `json:"author"  orm:"author"  description:""`
	Status  int    `json:"status"  orm:"status"  description:""`
	Expire  uint   `json:"expire"  orm:"expire"  description:""`
}
