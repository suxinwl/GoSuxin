package adminsystem

import (
	"context"
	"github.com/suxinwl/GoSuxin/api/admin/system"
	"github.com/suxinwl/GoSuxin/internal/dao"
	"github.com/suxinwl/GoSuxin/utility/gf"

	"github.com/suxinwl/GoSuxin/framework/container/gmap"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

// 获取部门数据
func (s *sAdminsystem) DeptList(ctx context.Context, req *system.DeptListReq) (res *gf.R) {
	//组合搜索条件
	whereMap := gmap.New()
	if req.Name != "" {
		whereMap.Set("name like ?", "%"+gconv.String(req.Name)+"%")
	}
	if !g.IsEmpty(req.Status) {
		whereMap.Set("status =?", req.Status)
	}
	if times := gf.FindTimeCondition(req.Createtime); times != nil {
		whereMap.Set("createtime between ? and ?", times)
	}
	list, err := dao.AuthDept.Ctx(ctx).Where(whereMap).Order("weigh asc").All()
	if err != nil {
		res = gf.Failed().SetMsg("获取部门数据失败").SetData(err)
		return
	}
	if len(list) > 0 {
		list = gf.GetTreeArray(list, 0, "")
	}
	res = gf.Success().SetMsg("获取部门数据列表").SetData(list)
	return
}

// 获取选项列表
func (s *sAdminsystem) DeptParent(ctx context.Context, req *system.DeptParentReq) (res *gf.R) {
	list, err := dao.AuthDept.Ctx(ctx).Where("status", 0).Where("id !=", req.Id).Fields("id,pid,name,remark").Order("weigh asc").All()
	if err != nil {
		res = gf.Failed().SetMsg("获取父级数据失败").SetData(err)
		return
	}
	if list == nil {
		list = make(gf.OrmResult, 0)
	}
	list = gf.GetMenuChildrenArray(list, 0, "pid")
	res = gf.Success().SetMsg("获取选项列表(父级)").SetData(list)
	return
}

// 保存、编辑部门
func (s *sAdminsystem) DeptSave(ctx context.Context, req *system.DeptSaveReq) (res *gf.R) {
	if req.Id == gf.ZeroValue {
		addId, err := dao.AuthDept.Ctx(ctx).Data(req).InsertAndGetId()
		if err != nil {
			res = gf.Failed().SetMsg("添加部门失败").SetData(err)
		} else {
			if addId != 0 {
				dao.AuthDept.Ctx(ctx).Where("id", addId).Update(map[string]interface{}{"weigh": addId})
			}
			res = gf.Success().SetMsg("添加成功！")
		}
	} else {
		result, err := dao.AuthDept.Ctx(ctx).Data(req).Where("id", req.Id).Update()
		if err != nil {
			res = gf.Failed().SetMsg("更新部门失败").SetData(err)
		} else {
			res = gf.Success().SetMsg("更新成功！").SetData(result)
		}
	}
	return
}

// 更新状态
func (s *sAdminsystem) DeptStatus(ctx context.Context, req *system.DeptStatusReq) (res *gf.R) {
	result, err := dao.AuthDept.Ctx(ctx).Where("id", req.Id).Update(gf.Map{"status": req.Status})
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

// 删除部门
func (s *sAdminsystem) DeptDel(ctx context.Context, req *system.DeptDelReq) (res *gf.R) {
	result, err := dao.AuthDept.Ctx(ctx).WhereIn("id", req.Ids).Delete()
	if err != nil {
		res = gf.Failed().SetMsg("删除部门失败").SetData(err)
		return
	}
	//删除子类数据
	dao.AuthDept.Ctx(ctx).WhereIn("pid", req.Ids).Delete()
	res = gf.Success().SetMsg("删除部门成功").SetData(result)
	return
}

// 移动部门
func (s *sAdminsystem) DragDept(ctx context.Context, req *system.DragDeptReq) (res *gf.R) {
	if req.Id == req.Pid {
		res = gf.Failed().SetMsg("同一部门无法绑定")
		return
	}
	_, err := dao.AuthDept.Ctx(ctx).Where("id", req.Id).Update(gf.Map{"pid": req.Pid})
	if err != nil {
		res = gf.Failed().SetMsg("移动部门失败").SetData(err)
		return
	}
	res = gf.Success().SetMsg("移动部门成功")
	return
}
