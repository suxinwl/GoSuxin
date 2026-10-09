// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// SxUserDao is the data access object for the table sx_user.
type SxUserDao struct {
	table    string             // table is the underlying table name of the DAO.
	group    string             // group is the database configuration group name of the current DAO.
	columns  SxUserColumns      // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler // handlers for customized model modification.
}

// SxUserColumns defines and stores column names for the table sx_user.
type SxUserColumns struct {
	Id            string //
	Email         string //
	Name          string //
	Pwd           string //
	Points        string //
	VipExpire     string //
	Avatar        string //
	Status        string //
	RegIp         string //
	RegTime       string //
	EmailVerified string //
	LastLoginTime string //
	LastLoginIp   string //
	SignDay       string //
}

// sxUserColumns holds the columns for the table sx_user.
var sxUserColumns = SxUserColumns{
	Id:            "id",
	Email:         "email",
	Name:          "name",
	Pwd:           "pwd",
	Points:        "points",
	VipExpire:     "vip_expire",
	Avatar:        "avatar",
	Status:        "status",
	RegIp:         "reg_ip",
	RegTime:       "reg_time",
	EmailVerified: "email_verified",
	LastLoginTime: "last_login_time",
	LastLoginIp:   "last_login_ip",
	SignDay:       "sign_day",
}

// NewSxUserDao creates and returns a new DAO object for table data access.
func NewSxUserDao(handlers ...gdb.ModelHandler) *SxUserDao {
	return &SxUserDao{
		group:    "default",
		table:    "sx_user",
		columns:  sxUserColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *SxUserDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *SxUserDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *SxUserDao) Columns() SxUserColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *SxUserDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *SxUserDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *SxUserDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
