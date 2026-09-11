// ========================
// 用于验证后台管理接口的RBAC权限验证
// ========================
package middleware

import (
	"github.com/suxinwl/GoSuxin/internal/dao"
	"github.com/suxinwl/GoSuxin/utility/gf"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
)

func Auth(r *ghttp.Request) {
	if r.GetServeHandler().Handler.GetMetaTag("noAuth") == "1" { //忽略不需要权限验证的接口
		r.Middleware.Next()
	} else {
		haseauth := checkAuth(r)
		if haseauth {
			r.Middleware.Next()
		} else {
			r.Response.WriteJsonExit(gf.Failed().SetMsg(gf.I18n(r, "sys_auth_permission")))
			r.ExitAll()
		}
	}
}

// 检查接口权限
func checkAuth(r *ghttp.Request) bool {
	uid := r.Context().Value("uid")
	ctx := r.Context()
	role_id, acerr := dao.AuthRoleAccess.Ctx(ctx).Where("uid", uid).Array("role_id")
	if acerr != nil || role_id == nil {
		return false
	}
	//1.判断是否有超级角色
	super_role, rerr := dao.AuthRole.Ctx(ctx).WhereIn("id", role_id).Where("rules", "*").Count()
	if rerr != nil {
		return false
	}
	if super_role != 0 { //超级角色
		superRoleAuth, _ := g.Cfg("app").Get(ctx, "app.superRoleAuth")
		if superRoleAuth.Bool() {
			hasepath, ruerr := dao.AuthRule.Ctx(ctx).Where("status", 0).Where("type", 2).Where("path", r.URL.Path).Count()
			if ruerr == nil && hasepath != 0 {
				return true
			}
		} else {
			return true
		}
	} else { //普通角色
		btns_ids, rerr := dao.AuthRole.Ctx(ctx).WhereIn("id", role_id).Array("btns") //数据权限
		if rerr != nil {
			return false
		}
		hasepath, ruerr := dao.AuthRule.Ctx(ctx).Where("status", 0).Where("type", 2).WhereIn("id", gf.ArrayMerge(btns_ids)).Where("path", r.URL.Path).Count()
		if ruerr == nil && hasepath != 0 {
			return true
		}
	}
	return false
}
