package adminsystem

import (
	"context"
	"fmt"
	"github.com/suxinwl/GoSuxin/api/admin/system"
	"github.com/suxinwl/GoSuxin/internal/dao"
	"github.com/suxinwl/GoSuxin/internal/extend/clogic"
	"github.com/suxinwl/GoSuxin/utility/gf"

	"github.com/suxinwl/GoSuxin/framework/container/gmap"
	"github.com/suxinwl/GoSuxin/framework/container/gvar"
	"github.com/suxinwl/GoSuxin/framework/crypto/gmd5"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"github.com/suxinwl/GoSuxin/framework/util/grand"
)

// 获取账号数据
func (s *sAdminsystem) AccountList(ctx context.Context, req *system.AccountListReq) (res *gf.R) {
	//组合搜索条件
	whereMap := gmap.New()
	account_id, filter := clogic.GetDataAuthor(ctx)
	if filter { //需要权限过滤
		whereMap.Set("account_id IN(?)", account_id)
	}
	if req.Name != "" {
		whereMap.Set("name like ?", "%"+gconv.String(req.Name)+"%")
	}
	if !g.IsEmpty(req.Status) {
		whereMap.Set("status =?", req.Status)
	}
	if times := gf.FindTimeCondition(req.Createtime); times != nil {
		whereMap.Set("createtime between ? and ?", times)
	}
	MDB := dao.Admin.Ctx(ctx).Where(whereMap)
	//whereOr条件
	if filter {
		MDB = MDB.WhereOr("id", ctx.Value("uid"))
	}
	totalCount, _ := MDB.Clone().Count()
	list, err := MDB.Fields("id,name,nickname,username,avatar,tel,mobile,email,dept_id,remark,status,createtime").Page(req.Page, req.PageSize).Order("id desc").All()
	if err != nil {
		res = gf.Failed().SetMsg("获取账号数据失败").SetData(err)
		return
	}
	for _, val := range list {
		roleid, _ := dao.AuthRoleAccess.Ctx(ctx).Where("uid", val["id"]).Array("role_id")
		rolename, _ := dao.AuthRole.Ctx(ctx).WhereIn("id", roleid).Array("name")
		val["rolename"] = gvar.New(rolename)
		val["roleid"] = gvar.New(roleid)
		depname, _ := dao.AuthDept.Ctx(ctx).Where("id", val["dept_id"]).Value("name")
		val["depname"] = depname
	}
	res = gf.Success().SetMsg("获取账号数据列表").SetData(gf.Map{
		"page":     req.Page,
		"pageSize": req.PageSize,
		"total":    totalCount,
		"items":    list})
	return
}

// 判断账号是否存在
func (s *sAdminsystem) Isaccountexist(ctx context.Context, req *system.IsaccountexistReq) (res *gf.R) {
	if req.Id != gf.ZeroValue {
		res1, err := dao.Admin.Ctx(ctx).Where("id !=", req.Id).Where("username", req.Username).Value("id")
		if err != nil {
			res = gf.Failed().SetMsg("验证失败").SetData(err)
		} else if res1 != nil {
			res = gf.Failed().SetMsg("账号已存在").SetData(err)
		} else {
			res = gf.Success().SetMsg("验证通过").SetData(res1)
		}
	} else {
		res2, err := dao.Admin.Ctx(ctx).Where("username", req.Username).Value("id")
		if err != nil {
			res = gf.Failed().SetMsg("验证失败").SetData(err)
		} else if res2 != nil {
			res = gf.Failed().SetMsg("账号已存在").SetData(err)
		} else {
			res = gf.Success().SetMsg("验证通过").SetData(res2)
		}
	}
	return
}

// 添加、编辑账号数据
func (s *sAdminsystem) AccountSave(ctx context.Context, req *system.AccountSaveReq) (res *gf.R) {
	var roleid []interface{}
	if !g.IsEmpty(req.Roleid) {
		roleid = req.Roleid.([]interface{})
	}
	if !g.IsEmpty(req.Password) {
		salt := grand.Str("123456789", 6)
		mdpass := fmt.Sprintf("%v%v", req.Password, salt)
		req.Password = gmd5.MustEncryptString(mdpass)
		req.Salt = salt
	}
	if g.IsEmpty(req.Avatar) {
		req.Avatar = "/resource/static/brand/logo.png?v=suxin1"
	}
	if req.Id == gf.ZeroValue {
		req.AccountId = ctx.Value("uid") //当前用户ID
		addId, err := dao.Admin.Ctx(ctx).Data(req).InsertAndGetId()
		if err != nil {
			res = gf.Failed().SetMsg("添加失败").SetData(err)
		} else {
			//添加角色-多个
			clogic.AppRoleAccess(ctx, roleid, addId)
			res = gf.Success().SetMsg("添加成功！").SetData(addId)
		}
	} else {
		paramMap := gconv.Map(req)
		//当密码为空这不修复密码
		if g.IsEmpty(paramMap["password"]) {
			delete(paramMap, "password")
			delete(paramMap, "salt")
		}
		_, err := dao.Admin.Ctx(ctx).Data(paramMap).Where("id", req.Id).Update()
		if err != nil {
			res = gf.Failed().SetMsg("更新失败").SetData(err)
		} else {
			//添加角色-多个
			if roleid != nil {
				clogic.AppRoleAccess(ctx, roleid, req.Id)
			}
			res = gf.Success().SetMsg("更新成功！").SetData(paramMap)
		}
	}
	return
}

// 更新账号状态
func (s *sAdminsystem) AccountStatus(ctx context.Context, req *system.AccountStatusReq) (res *gf.R) {
	result, err := dao.Admin.Ctx(ctx).Where("id", req.Id).Update(gf.Map{"status": req.Status})
	if err != nil {
		res = gf.Failed().SetMsg("更新失败").SetData(err)
		return
	}
	msg := "更新成功！"
	if result == nil {
		msg = "暂无数据更新"
	}
	res = gf.Success().SetMsg(msg)
	return
}

// 删除账号数据
func (s *sAdminsystem) AccountDel(ctx context.Context, req *system.AccountDelReq) (res *gf.R) {
	result, err := dao.Admin.Ctx(ctx).WhereIn("id", req.Ids).Delete()
	if err != nil {
		res = gf.Failed().SetMsg("删除账号失败").SetData(err)
		return
	}
	res = gf.Success().SetMsg("删除账号成功").SetData(result)
	return
}
