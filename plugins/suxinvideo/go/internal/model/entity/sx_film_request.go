// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

// SxFilmRequest is the golang structure for table sx_film_request.
type SxFilmRequest struct {
	Id      uint   `json:"id"      orm:"id"      description:""`
	UserId  uint   `json:"userId"  orm:"user_id" description:""`
	Title   string `json:"title"   orm:"title"   description:""`
	Note    string `json:"note"    orm:"note"    description:""`
	Status  int    `json:"status"  orm:"status"  description:""`
	Created uint   `json:"created" orm:"created" description:""`
}
