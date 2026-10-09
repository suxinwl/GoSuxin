// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

// SxVod is the golang structure for table sx_vod.
type SxVod struct {
	Id         uint    `json:"id"         orm:"id"         description:""`
	TypeId     uint    `json:"typeId"     orm:"type_id"    description:""`
	ApiId      uint    `json:"apiId"      orm:"api_id"     description:""`
	ApiVid     string  `json:"apiVid"     orm:"api_vid"    description:""`
	Name       string  `json:"name"       orm:"name"       description:""`
	NameNorm   string  `json:"nameNorm"   orm:"name_norm"  description:""`
	Sub        string  `json:"sub"        orm:"sub"        description:""`
	Class      string  `json:"class"      orm:"class"      description:""`
	Year       string  `json:"year"       orm:"year"       description:""`
	Area       string  `json:"area"       orm:"area"       description:""`
	Lang       string  `json:"lang"       orm:"lang"       description:""`
	Remarks    string  `json:"remarks"    orm:"remarks"    description:""`
	Score      float64 `json:"score"      orm:"score"      description:""`
	Director   string  `json:"director"   orm:"director"   description:""`
	Actor      string  `json:"actor"      orm:"actor"      description:""`
	Content    string  `json:"content"    orm:"content"    description:""`
	Pic        string  `json:"pic"        orm:"pic"        description:""`
	PlayFrom   string  `json:"playFrom"   orm:"play_from"  description:""`
	PlayUrl    string  `json:"playUrl"    orm:"play_url"   description:""`
	Vip        int     `json:"vip"        orm:"vip"        description:""`
	Points     int     `json:"points"     orm:"points"     description:""`
	TotalHits  uint    `json:"totalHits"  orm:"total_hits" description:""`
	Status     int     `json:"status"     orm:"status"     description:""`
	Addtime    uint    `json:"addtime"    orm:"addtime"    description:""`
	Updatetime uint    `json:"updatetime" orm:"updatetime" description:""`
}
