// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// SxLoginFailDao is the data access object for the table sx_login_fail.
type SxLoginFailDao struct {
	table    string             // table is the underlying table name of the DAO.
	group    string             // group is the database configuration group name of the current DAO.
	columns  SxLoginFailColumns // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler // handlers for customized model modification.
}

// SxLoginFailColumns defines and stores column names for the table sx_login_fail.
type SxLoginFailColumns struct {
	Id        string //
	Type      string //
	Account   string //
	Ip        string //
	Fails     string //
	BanUntil  string //
	UpdatedAt string //
}

// sxLoginFailColumns holds the columns for the table sx_login_fail.
var sxLoginFailColumns = SxLoginFailColumns{
	Id:        "id",
	Type:      "type",
	Account:   "account",
	Ip:        "ip",
	Fails:     "fails",
	BanUntil:  "ban_until",
	UpdatedAt: "updated_at",
}

// NewSxLoginFailDao creates and returns a new DAO object for table data access.
func NewSxLoginFailDao(handlers ...gdb.ModelHandler) *SxLoginFailDao {
	return &SxLoginFailDao{
		group:    "default",
		table:    "sx_login_fail",
		columns:  sxLoginFailColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *SxLoginFailDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *SxLoginFailDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *SxLoginFailDao) Columns() SxLoginFailColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *SxLoginFailDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *SxLoginFailDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *SxLoginFailDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
