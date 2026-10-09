// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

// SxArticle is the golang structure for table sx_article.
type SxArticle struct {
	Id      uint   `json:"id"      orm:"id"      description:""`
	Title   string `json:"title"   orm:"title"   description:""`
	Content string `json:"content" orm:"content" description:""`
	Status  int    `json:"status"  orm:"status"  description:""`
	Addtime uint   `json:"addtime" orm:"addtime" description:""`
}
