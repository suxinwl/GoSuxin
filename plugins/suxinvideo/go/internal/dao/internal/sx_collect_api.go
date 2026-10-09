// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// SxCollectApiDao is the data access object for the table sx_collect_api.
type SxCollectApiDao struct {
	table    string              // table is the underlying table name of the DAO.
	group    string              // group is the database configuration group name of the current DAO.
	columns  SxCollectApiColumns // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler  // handlers for customized model modification.
}

// SxCollectApiColumns defines and stores column names for the table sx_collect_api.
type SxCollectApiColumns struct {
	Id           string //
	Name         string //
	ApiUrl       string //
	Remark       string //
	Status       string //
	CollectAuto  string //
	CollectHours string //
	Addtime      string //
}

// sxCollectApiColumns holds the columns for the table sx_collect_api.
var sxCollectApiColumns = SxCollectApiColumns{
	Id:           "id",
	Name:         "name",
	ApiUrl:       "api_url",
	Remark:       "remark",
	Status:       "status",
	CollectAuto:  "collect_auto",
	CollectHours: "collect_hours",
	Addtime:      "addtime",
}

// NewSxCollectApiDao creates and returns a new DAO object for table data access.
func NewSxCollectApiDao(handlers ...gdb.ModelHandler) *SxCollectApiDao {
	return &SxCollectApiDao{
		group:    "default",
		table:    "sx_collect_api",
		columns:  sxCollectApiColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *SxCollectApiDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *SxCollectApiDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *SxCollectApiDao) Columns() SxCollectApiColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *SxCollectApiDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *SxCollectApiDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *SxCollectApiDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
