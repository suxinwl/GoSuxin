// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

// SxType is the golang structure for table sx_type.
type SxType struct {
	Id       uint   `json:"id"       orm:"id"        description:""`
	Pid      uint   `json:"pid"      orm:"pid"       description:""`
	Name     string `json:"name"     orm:"name"      description:""`
	Sort     int    `json:"sort"     orm:"sort"      description:""`
	Status   int    `json:"status"   orm:"status"    description:""`
	ShowHome int    `json:"showHome" orm:"show_home" description:""`
	Icon     string `json:"icon"     orm:"icon"      description:""`
	Image    string `json:"image"    orm:"image"     description:""`
}
