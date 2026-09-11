package common

import (
	"context"

	"github.com/suxinwl/GoSuxin/api/common/basetool"
	"github.com/suxinwl/GoSuxin/utility/gf"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// 数据表排序
func (c *ControllerBasetool) Weigh(ctx context.Context, req *basetool.WeighReq) (res *basetool.WeighRes, err error) {
	var resData = gf.Failed()
	_, err = g.Model(req.Tableanme).Where("pid", req.Pid).Save(req.WeighList)
	if err != nil {
		resData = gf.Failed().SetMsg("排序更新失败！").SetData(err)
	} else {
		resData = gf.Success().SetMsg("排序更新成功！").SetData(res)
	}
	res = &basetool.WeighRes{
		R: resData,
	}
	return
}

// 获取数据表
func (c *ControllerBasetool) GetTables(ctx context.Context, req *basetool.GetTablesReq) (res *basetool.GetTablesRes, err error) {
	var resData = gf.Failed()
	dbname, err := g.Cfg().Get(ctx, "database.default.name")
	if err != nil {
		resData = gf.Failed().SetMsg("获取数据库配置失败！").SetData(err)
	} else {
		tablelist, err := g.DB().Query(ctx, "select TABLE_NAME,TABLE_COMMENT,ENGINE,TABLE_COLLATION from information_schema.tables where table_schema = '"+dbname.String()+"'")
		if err != nil {
			resData = gf.Failed().SetMsg("获取数据表失败！").SetData(err)
		} else {
			var talbe_list []interface{}
			for _, Val := range tablelist {
				talbe_list = append(talbe_list, gf.Map{"name": Val["TABLE_NAME"], "title": Val["TABLE_COMMENT"], "engine": Val["ENGINE"], "collation": Val["TABLE_COLLATION"]})
			}
			resData = gf.Success().SetMsg("获取数据表").SetData(talbe_list)
		}
	}
	res = &basetool.GetTablesRes{
		R: resData,
	}
	return
}
