// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// SxGoodsDao is the data access object for the table sx_goods.
type SxGoodsDao struct {
	table    string             // table is the underlying table name of the DAO.
	group    string             // group is the database configuration group name of the current DAO.
	columns  SxGoodsColumns     // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler // handlers for customized model modification.
}

// SxGoodsColumns defines and stores column names for the table sx_goods.
type SxGoodsColumns struct {
	Id     string //
	Name   string //
	Price  string //
	Points string //
	Days   string //
	Sort   string //
	Status string //
}

// sxGoodsColumns holds the columns for the table sx_goods.
var sxGoodsColumns = SxGoodsColumns{
	Id:     "id",
	Name:   "name",
	Price:  "price",
	Points: "points",
	Days:   "days",
	Sort:   "sort",
	Status: "status",
}

// NewSxGoodsDao creates and returns a new DAO object for table data access.
func NewSxGoodsDao(handlers ...gdb.ModelHandler) *SxGoodsDao {
	return &SxGoodsDao{
		group:    "default",
		table:    "sx_goods",
		columns:  sxGoodsColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *SxGoodsDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *SxGoodsDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *SxGoodsDao) Columns() SxGoodsColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *SxGoodsDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *SxGoodsDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *SxGoodsDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
