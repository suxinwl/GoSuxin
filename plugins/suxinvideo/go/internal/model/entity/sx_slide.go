// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

// SxSlide is the golang structure for table sx_slide.
type SxSlide struct {
	Id     uint   `json:"id"     orm:"id"     description:""`
	Name   string `json:"name"   orm:"name"   description:""`
	Pic    string `json:"pic"    orm:"pic"    description:""`
	Url    string `json:"url"    orm:"url"    description:""`
	Pos    string `json:"pos"    orm:"pos"    description:""`
	Sort   int    `json:"sort"   orm:"sort"   description:""`
	Status int    `json:"status" orm:"status" description:""`
}
