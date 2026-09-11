package wxapp

import (
	"context"

	v1 "github.com/suxinwl/GoSuxin/api/wxapp/v1"
	"github.com/suxinwl/GoSuxin/utility/auth"
	"github.com/suxinwl/GoSuxin/utility/gf"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

func (c *ControllerV1) Hello(ctx context.Context, req *v1.HelloReq) (res *v1.HelloRes, err error) {
	var rr = gf.Failed()
	token, err := auth.GenerateToken(ctx, "123", g.Map{"uid": 1, "username": "zhangsan"})
	if err != nil {
		rr = gf.Success().SetData(err).SetMsg("登录失败")
	} else {
		rr = gf.Success().SetMsg("添加成功rule").SetData(token)
	}
	res = &v1.HelloRes{
		R: rr,
	}
	return
}
