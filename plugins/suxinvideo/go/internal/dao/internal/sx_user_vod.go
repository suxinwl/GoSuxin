// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// SxUserVodDao is the data access object for the table sx_user_vod.
type SxUserVodDao struct {
	table    string             // table is the underlying table name of the DAO.
	group    string             // group is the database configuration group name of the current DAO.
	columns  SxUserVodColumns   // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler // handlers for customized model modification.
}

// SxUserVodColumns defines and stores column names for the table sx_user_vod.
type SxUserVodColumns struct {
	Id      string //
	UserId  string //
	VodId   string //
	Points  string //
	Created string //
}

// sxUserVodColumns holds the columns for the table sx_user_vod.
var sxUserVodColumns = SxUserVodColumns{
	Id:      "id",
	UserId:  "user_id",
	VodId:   "vod_id",
	Points:  "points",
	Created: "created",
}

// NewSxUserVodDao creates and returns a new DAO object for table data access.
func NewSxUserVodDao(handlers ...gdb.ModelHandler) *SxUserVodDao {
	return &SxUserVodDao{
		group:    "default",
		table:    "sx_user_vod",
		columns:  sxUserVodColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *SxUserVodDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *SxUserVodDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *SxUserVodDao) Columns() SxUserVodColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *SxUserVodDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *SxUserVodDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *SxUserVodDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
