// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// SxVodDao is the data access object for the table sx_vod.
type SxVodDao struct {
	table    string             // table is the underlying table name of the DAO.
	group    string             // group is the database configuration group name of the current DAO.
	columns  SxVodColumns       // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler // handlers for customized model modification.
}

// SxVodColumns defines and stores column names for the table sx_vod.
type SxVodColumns struct {
	Id         string //
	TypeId     string //
	ApiId      string //
	ApiVid     string //
	Name       string //
	NameNorm   string //
	Sub        string //
	Class      string //
	Year       string //
	Area       string //
	Lang       string //
	Remarks    string //
	Score      string //
	Director   string //
	Actor      string //
	Content    string //
	Pic        string //
	PlayFrom   string //
	PlayUrl    string //
	Vip        string //
	Points     string //
	TotalHits  string //
	Status     string //
	Addtime    string //
	Updatetime string //
}

// sxVodColumns holds the columns for the table sx_vod.
var sxVodColumns = SxVodColumns{
	Id:         "id",
	TypeId:     "type_id",
	ApiId:      "api_id",
	ApiVid:     "api_vid",
	Name:       "name",
	NameNorm:   "name_norm",
	Sub:        "sub",
	Class:      "class",
	Year:       "year",
	Area:       "area",
	Lang:       "lang",
	Remarks:    "remarks",
	Score:      "score",
	Director:   "director",
	Actor:      "actor",
	Content:    "content",
	Pic:        "pic",
	PlayFrom:   "play_from",
	PlayUrl:    "play_url",
	Vip:        "vip",
	Points:     "points",
	TotalHits:  "total_hits",
	Status:     "status",
	Addtime:    "addtime",
	Updatetime: "updatetime",
}

// NewSxVodDao creates and returns a new DAO object for table data access.
func NewSxVodDao(handlers ...gdb.ModelHandler) *SxVodDao {
	return &SxVodDao{
		group:    "default",
		table:    "sx_vod",
		columns:  sxVodColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *SxVodDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *SxVodDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *SxVodDao) Columns() SxVodColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *SxVodDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *SxVodDao) Ctx(ctx context.Context) *gdb.Model {
	model := dao.DB().Model(dao.table)
	for _, handler := range dao.handlers {
		model = handler(model)
	}
	return model.Safe().Ctx(ctx)
}

// Transaction wraps the transaction logic using function f.
// It rolls back the transaction and returns the error if function f returns a non-nil error.
// It commits the transaction and returns nil if function f returns nil.
//
// Note: Do not commit or roll back the transaction in function f,
// as it is automatically handled by this function.
func (dao *SxVodDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
