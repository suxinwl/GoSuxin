// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// SxSignDao is the data access object for the table sx_sign.
type SxSignDao struct {
	table    string             // table is the underlying table name of the DAO.
	group    string             // group is the database configuration group name of the current DAO.
	columns  SxSignColumns      // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler // handlers for customized model modification.
}

// SxSignColumns defines and stores column names for the table sx_sign.
type SxSignColumns struct {
	Id     string //
	UserId string //
	Day    string //
	Points string //
}

// sxSignColumns holds the columns for the table sx_sign.
var sxSignColumns = SxSignColumns{
	Id:     "id",
	UserId: "user_id",
	Day:    "day",
	Points: "points",
}

// NewSxSignDao creates and returns a new DAO object for table data access.
func NewSxSignDao(handlers ...gdb.ModelHandler) *SxSignDao {
	return &SxSignDao{
		group:    "default",
		table:    "sx_sign",
		columns:  sxSignColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *SxSignDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *SxSignDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *SxSignDao) Columns() SxSignColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *SxSignDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *SxSignDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *SxSignDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
