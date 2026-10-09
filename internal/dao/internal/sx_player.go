// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// SxPlayerDao is the data access object for the table sx_player.
type SxPlayerDao struct {
	table    string             // table is the underlying table name of the DAO.
	group    string             // group is the database configuration group name of the current DAO.
	columns  SxPlayerColumns    // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler // handlers for customized model modification.
}

// SxPlayerColumns defines and stores column names for the table sx_player.
type SxPlayerColumns struct {
	Id     string //
	Code   string //
	Name   string //
	Parse  string //
	Status string //
}

// sxPlayerColumns holds the columns for the table sx_player.
var sxPlayerColumns = SxPlayerColumns{
	Id:     "id",
	Code:   "code",
	Name:   "name",
	Parse:  "parse",
	Status: "status",
}

// NewSxPlayerDao creates and returns a new DAO object for table data access.
func NewSxPlayerDao(handlers ...gdb.ModelHandler) *SxPlayerDao {
	return &SxPlayerDao{
		group:    "default",
		table:    "sx_player",
		columns:  sxPlayerColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *SxPlayerDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *SxPlayerDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *SxPlayerDao) Columns() SxPlayerColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *SxPlayerDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *SxPlayerDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *SxPlayerDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
