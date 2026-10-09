// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// SxSlideDao is the data access object for the table sx_slide.
type SxSlideDao struct {
	table    string             // table is the underlying table name of the DAO.
	group    string             // group is the database configuration group name of the current DAO.
	columns  SxSlideColumns     // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler // handlers for customized model modification.
}

// SxSlideColumns defines and stores column names for the table sx_slide.
type SxSlideColumns struct {
	Id     string //
	Name   string //
	Pic    string //
	Url    string //
	Pos    string //
	Sort   string //
	Status string //
}

// sxSlideColumns holds the columns for the table sx_slide.
var sxSlideColumns = SxSlideColumns{
	Id:     "id",
	Name:   "name",
	Pic:    "pic",
	Url:    "url",
	Pos:    "pos",
	Sort:   "sort",
	Status: "status",
}

// NewSxSlideDao creates and returns a new DAO object for table data access.
func NewSxSlideDao(handlers ...gdb.ModelHandler) *SxSlideDao {
	return &SxSlideDao{
		group:    "default",
		table:    "sx_slide",
		columns:  sxSlideColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *SxSlideDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *SxSlideDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *SxSlideDao) Columns() SxSlideColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *SxSlideDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *SxSlideDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *SxSlideDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
