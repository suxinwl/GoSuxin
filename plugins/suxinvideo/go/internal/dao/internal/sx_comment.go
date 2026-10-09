// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// SxCommentDao is the data access object for the table sx_comment.
type SxCommentDao struct {
	table    string             // table is the underlying table name of the DAO.
	group    string             // group is the database configuration group name of the current DAO.
	columns  SxCommentColumns   // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler // handlers for customized model modification.
}

// SxCommentColumns defines and stores column names for the table sx_comment.
type SxCommentColumns struct {
	Id      string //
	UserId  string //
	VodId   string //
	Content string //
	Status  string //
	Created string //
}

// sxCommentColumns holds the columns for the table sx_comment.
var sxCommentColumns = SxCommentColumns{
	Id:      "id",
	UserId:  "user_id",
	VodId:   "vod_id",
	Content: "content",
	Status:  "status",
	Created: "created",
}

// NewSxCommentDao creates and returns a new DAO object for table data access.
func NewSxCommentDao(handlers ...gdb.ModelHandler) *SxCommentDao {
	return &SxCommentDao{
		group:    "default",
		table:    "sx_comment",
		columns:  sxCommentColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *SxCommentDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *SxCommentDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *SxCommentDao) Columns() SxCommentColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *SxCommentDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *SxCommentDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *SxCommentDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
