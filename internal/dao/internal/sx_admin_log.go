// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// SxAdminLogDao is the data access object for the table sx_admin_log.
type SxAdminLogDao struct {
	table    string             // table is the underlying table name of the DAO.
	group    string             // group is the database configuration group name of the current DAO.
	columns  SxAdminLogColumns  // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler // handlers for customized model modification.
}

// SxAdminLogColumns defines and stores column names for the table sx_admin_log.
type SxAdminLogColumns struct {
	Id      string //
	AdminId string //
	Action  string //
	Ip      string //
	Created string //
}

// sxAdminLogColumns holds the columns for the table sx_admin_log.
var sxAdminLogColumns = SxAdminLogColumns{
	Id:      "id",
	AdminId: "admin_id",
	Action:  "action",
	Ip:      "ip",
	Created: "created",
}

// NewSxAdminLogDao creates and returns a new DAO object for table data access.
func NewSxAdminLogDao(handlers ...gdb.ModelHandler) *SxAdminLogDao {
	return &SxAdminLogDao{
		group:    "default",
		table:    "sx_admin_log",
		columns:  sxAdminLogColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *SxAdminLogDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *SxAdminLogDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *SxAdminLogDao) Columns() SxAdminLogColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *SxAdminLogDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *SxAdminLogDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *SxAdminLogDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
