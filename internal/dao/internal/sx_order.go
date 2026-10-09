// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// SxOrderDao is the data access object for the table sx_order.
type SxOrderDao struct {
	table    string             // table is the underlying table name of the DAO.
	group    string             // group is the database configuration group name of the current DAO.
	columns  SxOrderColumns     // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler // handlers for customized model modification.
}

// SxOrderColumns defines and stores column names for the table sx_order.
type SxOrderColumns struct {
	Id         string //
	OrderNo    string //
	UserId     string //
	GoodsId    string //
	Type       string //
	Title      string //
	Amount     string //
	UsdtAmount string //
	PayType    string //
	Status     string //
	TradeNo    string //
	Created    string //
	PaidTime   string //
}

// sxOrderColumns holds the columns for the table sx_order.
var sxOrderColumns = SxOrderColumns{
	Id:         "id",
	OrderNo:    "order_no",
	UserId:     "user_id",
	GoodsId:    "goods_id",
	Type:       "type",
	Title:      "title",
	Amount:     "amount",
	UsdtAmount: "usdt_amount",
	PayType:    "pay_type",
	Status:     "status",
	TradeNo:    "trade_no",
	Created:    "created",
	PaidTime:   "paid_time",
}

// NewSxOrderDao creates and returns a new DAO object for table data access.
func NewSxOrderDao(handlers ...gdb.ModelHandler) *SxOrderDao {
	return &SxOrderDao{
		group:    "default",
		table:    "sx_order",
		columns:  sxOrderColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *SxOrderDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *SxOrderDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *SxOrderDao) Columns() SxOrderColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *SxOrderDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *SxOrderDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *SxOrderDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
