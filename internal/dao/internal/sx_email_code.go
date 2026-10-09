// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// SxEmailCodeDao is the data access object for the table sx_email_code.
type SxEmailCodeDao struct {
	table    string             // table is the underlying table name of the DAO.
	group    string             // group is the database configuration group name of the current DAO.
	columns  SxEmailCodeColumns // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler // handlers for customized model modification.
}

// SxEmailCodeColumns defines and stores column names for the table sx_email_code.
type SxEmailCodeColumns struct {
	Id      string //
	Email   string //
	Code    string //
	Type    string //
	Expire  string //
	Used    string //
	Created string //
}

// sxEmailCodeColumns holds the columns for the table sx_email_code.
var sxEmailCodeColumns = SxEmailCodeColumns{
	Id:      "id",
	Email:   "email",
	Code:    "code",
	Type:    "type",
	Expire:  "expire",
	Used:    "used",
	Created: "created",
}

// NewSxEmailCodeDao creates and returns a new DAO object for table data access.
func NewSxEmailCodeDao(handlers ...gdb.ModelHandler) *SxEmailCodeDao {
	return &SxEmailCodeDao{
		group:    "default",
		table:    "sx_email_code",
		columns:  sxEmailCodeColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *SxEmailCodeDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *SxEmailCodeDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *SxEmailCodeDao) Columns() SxEmailCodeColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *SxEmailCodeDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *SxEmailCodeDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *SxEmailCodeDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
