// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// SxPlayRecord is the golang structure of table sx_play_record for DAO operations like Where/Data.
type SxPlayRecord struct {
	g.Meta   `orm:"table:sx_play_record, do:true"`
	Id       any //
	UserId   any //
	VodId    any //
	Episode  any //
	Position any //
	Updated  any //
}
