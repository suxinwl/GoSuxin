package suxinvideo

import (
	"context"
	"strings"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// The host auth_rule schema requires these UI strings even for hidden API
// buttons. Keep timestamps with GoFrame's model handling and retain existing
// route IDs; the business install SQL never owns the host schema.
func authRuleDefaults() row {
	return row{"des": "", "locale": "", "icon": "", "component": "", "redirect": ""}
}

func upgradeClientPermissions(ctx context.Context) error {
	parent, err := g.Model("auth_rule").Ctx(ctx).Where("routename", "suxinvideo_admin").One()
	if err != nil || parent == nil {
		return err
	}
	for action, title := range map[string]string{"clients/releases": "查看客户端发布", "clients/publish": "发布签名客户端", "clients/status": "上下架客户端"} {
		name := "suxinvideo_admin_" + strings.ReplaceAll(action, "/", "_")
		entry, e := g.Model("auth_rule").Ctx(ctx).Where("routename", name).One()
		if e != nil {
			return e
		}
		values := row{"pid": parent["id"].Int64(), "title": title, "path": "/admin/suxinvideo/" + action, "routepath": strings.ReplaceAll(action, "/", "_"), "routename": name, "permission": "", "type": 2, "status": 0, "requiresauth": 1, "hideinmenu": 1}
		if entry == nil {
			for field, value := range authRuleDefaults() {
				values[field] = value
			}
			_, e = g.Model("auth_rule").Ctx(ctx).Data(values).Insert()
		} else {
			_, e = g.Model("auth_rule").Ctx(ctx).Where("id", entry["id"]).Data(values).Update()
		}
		if e != nil {
			return e
		}
	}
	// Preserve effective grants: settings readers may inspect releases and
	// settings editors may publish. Other role permissions remain untouched.
	rules, err := g.Model("auth_rule").Ctx(ctx).Fields("id,path").WhereIn("path", []string{"/admin/suxinvideo/config", "/admin/suxinvideo/saveConfig", "/admin/suxinvideo/clients/releases", "/admin/suxinvideo/clients/publish", "/admin/suxinvideo/clients/status"}).Where("status", 0).All()
	if err != nil {
		return err
	}
	ids := map[string]int64{}
	for _, rule := range rules {
		ids[rule["path"].String()] = rule["id"].Int64()
	}
	extras := map[int64][]int64{ids["/admin/suxinvideo/config"]: {ids["/admin/suxinvideo/clients/releases"]}, ids["/admin/suxinvideo/saveConfig"]: {ids["/admin/suxinvideo/clients/releases"], ids["/admin/suxinvideo/clients/publish"], ids["/admin/suxinvideo/clients/status"]}}
	roles, err := g.Model("auth_role").Ctx(ctx).Fields("id,rules,btns").All()
	if err != nil {
		return err
	}
	for _, role := range roles {
		if role["rules"].String() == "*" {
			continue
		}
		changes := row{}
		for _, field := range []string{"rules", "btns"} {
			value, changed := expandRoleButtons(role[field].String(), nil, extras)
			if changed {
				changes[field] = value
			}
		}
		if len(changes) > 0 {
			if _, err = g.Model("auth_role").Ctx(ctx).Where("id", role["id"]).Data(changes).Update(); err != nil {
				return err
			}
		}
	}
	return nil
}
