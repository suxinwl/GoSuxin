// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

// SxLink is the golang structure for table sx_link.
type SxLink struct {
	Id     uint   `json:"id"     orm:"id"     description:""`
	Name   string `json:"name"   orm:"name"   description:""`
	Url    string `json:"url"    orm:"url"    description:""`
	Sort   int    `json:"sort"   orm:"sort"   description:""`
	Status int    `json:"status" orm:"status" description:""`
}
