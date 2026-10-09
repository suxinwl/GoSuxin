// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// SxTypeDao is the data access object for the table sx_type.
type SxTypeDao struct {
	table    string             // table is the underlying table name of the DAO.
	group    string             // group is the database configuration group name of the current DAO.
	columns  SxTypeColumns      // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler // handlers for customized model modification.
}

// SxTypeColumns defines and stores column names for the table sx_type.
type SxTypeColumns struct {
	Id       string //
	Pid      string //
	Name     string //
	Sort     string //
	Status   string //
	ShowHome string //
	Icon     string //
	Image    string //
}

// sxTypeColumns holds the columns for the table sx_type.
var sxTypeColumns = SxTypeColumns{
	Id:       "id",
	Pid:      "pid",
	Name:     "name",
	Sort:     "sort",
	Status:   "status",
	ShowHome: "show_home",
	Icon:     "icon",
	Image:    "image",
}

// NewSxTypeDao creates and returns a new DAO object for table data access.
func NewSxTypeDao(handlers ...gdb.ModelHandler) *SxTypeDao {
	return &SxTypeDao{
		group:    "default",
		table:    "sx_type",
		columns:  sxTypeColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *SxTypeDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *SxTypeDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *SxTypeDao) Columns() SxTypeColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *SxTypeDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *SxTypeDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *SxTypeDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
