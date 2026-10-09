// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

// SxCollectApi is the golang structure for table sx_collect_api.
type SxCollectApi struct {
	Id           uint   `json:"id"           orm:"id"            description:""`
	Name         string `json:"name"         orm:"name"          description:""`
	ApiUrl       string `json:"apiUrl"       orm:"api_url"       description:""`
	Remark       string `json:"remark"       orm:"remark"        description:""`
	Status       int    `json:"status"       orm:"status"        description:""`
	CollectAuto  int    `json:"collectAuto"  orm:"collect_auto"  description:""`
	CollectHours int    `json:"collectHours" orm:"collect_hours" description:""`
	Addtime      uint   `json:"addtime"      orm:"addtime"       description:""`
}
