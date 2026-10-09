// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// SxPluginDao is the data access object for the table sx_plugin.
type SxPluginDao struct {
	table    string             // table is the underlying table name of the DAO.
	group    string             // group is the database configuration group name of the current DAO.
	columns  SxPluginColumns    // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler // handlers for customized model modification.
}

// SxPluginColumns defines and stores column names for the table sx_plugin.
type SxPluginColumns struct {
	Id      string //
	Code    string //
	Name    string //
	Type    string //
	Version string //
	Author  string //
	Status  string //
	Expire  string //
}

// sxPluginColumns holds the columns for the table sx_plugin.
var sxPluginColumns = SxPluginColumns{
	Id:      "id",
	Code:    "code",
	Name:    "name",
	Type:    "type",
	Version: "version",
	Author:  "author",
	Status:  "status",
	Expire:  "expire",
}

// NewSxPluginDao creates and returns a new DAO object for table data access.
func NewSxPluginDao(handlers ...gdb.ModelHandler) *SxPluginDao {
	return &SxPluginDao{
		group:    "default",
		table:    "sx_plugin",
		columns:  sxPluginColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *SxPluginDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *SxPluginDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *SxPluginDao) Columns() SxPluginColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *SxPluginDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *SxPluginDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *SxPluginDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
