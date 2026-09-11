package adminsystem

import (
	"context"
	"fmt"
	"github.com/suxinwl/GoSuxin/api/admin/system"
	"github.com/suxinwl/GoSuxin/internal/dao"
	"github.com/suxinwl/GoSuxin/internal/extend/clogic"
	"github.com/suxinwl/GoSuxin/utility/gf"
	"strings"

	"github.com/suxinwl/GoSuxin/framework/container/gmap"
	"github.com/suxinwl/GoSuxin/framework/container/gvar"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

// 获取角色数据
func (s *sAdminsystem) RoleList(ctx context.Context, req *system.RoleListReq) (res *gf.R) {
	uid := ctx.Value("uid") //当前用户ID
	user_role_ids, _ := dao.AuthRoleAccess.Ctx(ctx).Where("uid", uid).Array("role_id")
	role_chil_ids := clogic.GetAllChilIds("auth_role", user_role_ids) //批量获取子节点id
	all_role_id := gf.MergeArr(user_role_ids, role_chil_ids)
	//获取账号权限
	account_id, _ := clogic.GetDataAuthor(ctx)
	account_id = append(account_id, 0)
	//获取自己权限组-显示自己所在的权限组
	my_role_account_id, _ := dao.AuthRole.Ctx(ctx).WhereIn("id", user_role_ids).Value("account_id")
	account_id = append(account_id, my_role_account_id)
	//组合搜索条件
	whereMap := gmap.New()
	whereMap.Set("id IN(?)", all_role_id) //in 查询
	whereMap.Set("account_id IN(?)", account_id)
	if req.Name != "" {
		whereMap.Set("name like ?", "%"+gconv.String(req.Name)+"%")
	}
	if !g.IsEmpty(req.Status) {
		whereMap.Set("status =?", req.Status)
	}
	if times := gf.FindTimeCondition(req.Createtime); times != nil {
		whereMap.Set("createtime between ? and ?", times)
	}
	roleList, _ := dao.AuthRole.Ctx(ctx).Where(whereMap).Order("weigh asc").All()
	//获取最大一级的pid
	max_role_id, _ := dao.AuthRole.Ctx(ctx).Where(whereMap).Order("id asc").Value("pid")
	roleList = gf.GetTreeArray(roleList, gf.Int64(max_role_id), "")
	if roleList == nil {
		roleList = make(gf.OrmResult, 0)
	}
	res = gf.Success().SetMsg("获取拥有角色列表").SetData(gf.Map{"list": roleList, "max_pid": max_role_id})
	return
}

// 获取选项列表
func (s *sAdminsystem) RoleParent(ctx context.Context, req *system.RoleParentReq) (res *gf.R) {
	uid := ctx.Value("uid")
	user_role_ids, _ := dao.AuthRoleAccess.Ctx(ctx).Where("uid", uid).Array("role_id")
	role_chil_ids := clogic.GetAllChilIds("auth_role", user_role_ids) //批量获取子节点id
	all_role_id := gf.MergeArr(user_role_ids, role_chil_ids)
	//获取数据权限

	account_id, _ := clogic.GetDataAuthor(ctx)
	account_id = append(account_id, 0)
	//获取自己权限组-显示自己所在的权限组
	my_role_account_id, _ := dao.AuthRole.Ctx(ctx).WhereIn("id", user_role_ids).Value("account_id")
	account_id = append(account_id, my_role_account_id)
	//查找条件
	whereMap := gmap.New()
	whereMap.Set("id IN(?)", all_role_id)        //in 查询
	whereMap.Set("account_id IN(?)", account_id) //账号数据权限
	if req.Id != gf.ZeroValue {
		whereMap.Set("id !=?", req.Id)
	}
	roleList, _ := dao.AuthRole.Ctx(ctx).Where(whereMap).Fields("id,pid,name").Order("weigh asc").All()
	//获取最大一级的pid
	max_role_id, _ := dao.AuthRole.Ctx(ctx).Where(whereMap).Order("id asc").Value("pid")
	roleList = gf.GetTreeArray(roleList, gf.Int64(max_role_id), "")
	if roleList == nil {
		roleList = make(gf.OrmResult, 0)
	}
	res = gf.Success().SetMsg("获取选项列表(父级)").SetData(roleList)
	return
}

// 编辑角色表单获取菜单
func (s *sAdminsystem) RoleMenuList(ctx context.Context, req *system.RoleMenuListReq) (res *gf.R) {
	var rule_ids []interface{}
	pid := req.Pid
	MDB := dao.AuthRule.Ctx(ctx).Where("status", 0).WhereIn("type", []interface{}{0, 1})
	if pid == gf.ZeroValue { //获取本账号所拥有的权限
		user_id := ctx.Value("uid")
		role_id, _ := dao.AuthRoleAccess.Ctx(ctx).Where("uid", user_id).Array("role_id")
		menu_id, _ := dao.AuthRole.Ctx(ctx).WhereIn("id", role_id).Array("rules")
		//获取超级角色
		super_role, _ := dao.AuthRole.Ctx(ctx).WhereIn("id", role_id).Where("rules", "*").Value("id")
		if super_role == nil { //不是超级权限-过滤菜单权限
			getmenus := gf.ArrayMerge(menu_id)
			MDB = MDB.WhereIn("id", getmenus)
			rule_ids = getmenus
		}
	} else {
		//获取用户权限
		menu_id_str, _ := dao.AuthRole.Ctx(ctx).Where("id", pid).Value("rules")
		if !strings.Contains(menu_id_str.String(), "*") { //不是超级权限-过滤菜单权限
			getmenus := gf.Axplode(menu_id_str.String())
			MDB = MDB.WhereIn("id", getmenus)
			rule_ids = getmenus
		}
	}
	menuList, _ := MDB.Fields("id,pid,title,locale").Order("weigh asc").All()
	for _, val := range menuList {
		if val["title"].String() == "" {
			val["title"] = val["locale"]
		}
		delete(val, "locale")
		//获取按钮
		whereMap := gmap.New()
		if rule_ids != nil {
			whereMap.Set("id IN(?)", rule_ids)
		}
		btn_rules, _ := dao.AuthRule.Ctx(ctx).Where("status", 0).Where("type", 2).Where("pid", val["id"]).Where(whereMap).Fields("id,pid,title,des,locale").Order("weigh asc").All()
		if len(btn_rules) > 0 {
			item := gf.Map{
				"title":     "按钮权限",
				"id":        btn_rules[0]["id"],
				"pid":       val["id"],
				"checkable": false,
				"btn_rules": btn_rules,
			}
			var valitem []gf.Map
			valitem = append(valitem, item)
			val["children"] = gvar.New(valitem)
			var btnids []interface{}
			for _, btnid := range btn_rules {
				btnids = append(btnids, btnid["id"])
			}
			val["btnids"] = gvar.New(btnids)
		} else if val["pid"].Int() == 0 {
			//一级菜单获取子级菜单按钮
			sub_rule_ids, _ := dao.AuthRule.Ctx(ctx).Where("pid", val["id"]).Where("status", 0).Where("type !=", 2).Array("id")
			btn_rule_ids, _ := dao.AuthRule.Ctx(ctx).Where("status", 0).Where("type", 2).WhereIn("pid", sub_rule_ids).Array("id")
			val["btnids"] = gvar.New(btn_rule_ids)
		}
		val["checkable"] = gvar.New(true)
	}
	menuList = gf.GetMenuChildrenArray(menuList, 0, "pid")
	if rule_ids == nil {
		btn_idsdata, _ := dao.AuthRule.Ctx(ctx).Where("status", 0).Where("type", 2).Array("id")
		res = gf.Success().SetMsg("获取超级权限菜单数据").SetData(gf.Map{"list": menuList, "btn_rule_ids": btn_idsdata})
	} else {
		btn_idsdata, _ := dao.AuthRule.Ctx(ctx).Where("status", 0).Where("type", 2).WhereIn("id", rule_ids).Array("id")
		res = gf.Success().SetMsg("获取子级菜单数据").SetData(gf.Map{"list": menuList, "btn_rule_ids": btn_idsdata})
	}
	return
}

// 保存、编辑角色
func (s *sAdminsystem) RoleSave(ctx context.Context, req *system.RoleSaveReq) (res *gf.R) {
	//处理菜单数据
	if !g.IsNil(req.Menu) && req.Menu != "*" {
		rules := clogic.GetRulesID("auth_rule", "pid", req.Menu) //获取子菜单包含的父级ID
		rudata := rules.([]interface{})
		var rulesStr []string
		for _, v := range rudata {
			str := fmt.Sprintf("%v", v) //interface{}强转string
			rulesStr = append(rulesStr, str)
		}
		//按钮权限
		for _, bv := range req.Bbtns.([]interface{}) {
			str := fmt.Sprintf("%v", bv) //interface{}强转string
			rulesStr = append(rulesStr, str)
		}
		req.Rules = strings.Join(rulesStr, ",")
	} else {
		req.Rules = ""
	}
	if req.Id == gf.ZeroValue {
		req.AccountId = gf.Int64(ctx.Value("uid"))
		addId, err := dao.AuthRole.Ctx(ctx).Data(req).InsertAndGetId()
		if err != nil {
			res = gf.Failed().SetMsg("添加角色失败").SetData(err)
		} else {
			if addId != 0 {
				dao.AuthRole.Ctx(ctx).Where("id", addId).Update(map[string]interface{}{"weigh": addId})
			}
			res = gf.Success().SetMsg("添加成功！")
		}
	} else {
		result, err := dao.AuthRole.Ctx(ctx).Data(req).Where("id", req.Id).Update()
		if err != nil {
			res = gf.Failed().SetMsg("更新角色失败").SetData(err)
		} else {
			res = gf.Success().SetMsg("更新成功！").SetData(result)
		}
	}
	return
}

// 更新状态
func (s *sAdminsystem) RoleStatus(ctx context.Context, req *system.RoleStatusReq) (res *gf.R) {
	result, err := dao.AuthRole.Ctx(ctx).Where("id", req.Id).Update(gf.Map{"status": req.Status})
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

// 删除菜单
func (s *sAdminsystem) RoleDel(ctx context.Context, req *system.RoleDelReq) (res *gf.R) {
	result, err := dao.AuthRole.Ctx(ctx).WhereIn("id", req.Ids).Delete()
	if err != nil {
		res = gf.Failed().SetMsg("删除菜单失败").SetData(err)
		return
	}
	//删除子类数据
	dao.AuthRole.Ctx(ctx).WhereIn("pid", req.Ids).Delete()
	res = gf.Success().SetMsg("删除菜单成功").SetData(result)
	return
}
