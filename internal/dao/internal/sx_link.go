// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// SxLinkDao is the data access object for the table sx_link.
type SxLinkDao struct {
	table    string             // table is the underlying table name of the DAO.
	group    string             // group is the database configuration group name of the current DAO.
	columns  SxLinkColumns      // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler // handlers for customized model modification.
}

// SxLinkColumns defines and stores column names for the table sx_link.
type SxLinkColumns struct {
	Id     string //
	Name   string //
	Url    string //
	Sort   string //
	Status string //
}

// sxLinkColumns holds the columns for the table sx_link.
var sxLinkColumns = SxLinkColumns{
	Id:     "id",
	Name:   "name",
	Url:    "url",
	Sort:   "sort",
	Status: "status",
}

// NewSxLinkDao creates and returns a new DAO object for table data access.
func NewSxLinkDao(handlers ...gdb.ModelHandler) *SxLinkDao {
	return &SxLinkDao{
		group:    "default",
		table:    "sx_link",
		columns:  sxLinkColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *SxLinkDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *SxLinkDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *SxLinkDao) Columns() SxLinkColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *SxLinkDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *SxLinkDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *SxLinkDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
