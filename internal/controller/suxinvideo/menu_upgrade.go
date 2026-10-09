package suxinvideo

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

// Keep the bundled entry identifiable when the host sidebar is collapsed.
func ensureCMSHostEntry(ctx context.Context) error {
	menu, err := g.Model("auth_rule").Ctx(ctx).Where("routename", "suxinvideo_admin").Where("status", 0).One()
	if err != nil || menu.IsEmpty() {
		return err
	}
	changes := g.Map{}
	if menu["icon"].String() == "" {
		changes["icon"] = "icon-video-camera"
	}
	if menu["title"].String() == "速信影视CMS后台" {
		changes["title"] = "影视CMS"
	}
	if menu["weigh"].Int() == 0 {
		changes["weigh"] = 2
	}
	if len(changes) > 0 {
		if _, err := g.Model("auth_rule").Ctx(ctx).Where("id", menu["id"]).Data(changes).Update(); err != nil {
			return err
		}
	}
	table, err := g.DB().GetValue(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?", g.DB().GetConfig().Prefix+"home_quickop")
	if err != nil || table.Int() == 0 {
		return err
	}
	count, err := g.Model("home_quickop").Ctx(ctx).Where("path_url", "suxinvideo_admin").Count()
	if err != nil || count > 0 {
		return err
	}
	_, err = g.Model("home_quickop").Ctx(ctx).Data(g.Map{
		"uid": 1, "business_id": 1, "is_common": 0, "type": 0,
		"name": "影视CMS后台", "path_url": "suxinvideo_admin", "icon": "icon-video-camera", "weigh": 2,
	}).Insert()
	return err
}

// The host installer inserts new routes but never updates or removes prior menu rows.
// Reconcile only this plugin's former tree after the new standalone route exists.
func upgradeCMSMenu(ctx context.Context) error {
	old, err := g.Model("auth_rule").Ctx(ctx).Where("routename", "suxinvideo").One()
	if err != nil || old == nil {
		return err
	}
	current, err := g.Model("auth_rule").Ctx(ctx).Where("routename", "suxinvideo_admin").One()
	if err != nil || current == nil {
		return err
	}
	oldID, newID := old["id"].Int64(), current["id"].Int64()
	previous, err := g.Model("auth_rule").Ctx(ctx).Where("pid", oldID).All()
	if err != nil {
		return err
	}
	next, err := g.Model("auth_rule").Ctx(ctx).Where("pid", newID).All()
	if err != nil {
		return err
	}
	byPath := map[string]int64{}
	byPathExtras := map[string][]int64{}
	for _, item := range next {
		path := item["path"].String()
		if path != "" && item["permission"].String() == "" {
			byPath[path] = item["id"].Int64()
		} else if path != "" && strings.HasPrefix(item["permission"].String(), "suxinvideo:") {
			byPathExtras[path] = append(byPathExtras[path], item["id"].Int64())
		}
	}
	menuMap := map[int64]int64{oldID: newID}
	buttonMap := map[int64]int64{}
	buttonExtras := map[int64][]int64{}
	oldIDs := []int64{oldID}
	for _, item := range previous {
		id := item["id"].Int64()
		oldIDs = append(oldIDs, id)
		if item["type"].Int() == 1 {
			menuMap[id] = newID
		}
		if mapped := byPath[item["path"].String()]; mapped > 0 {
			buttonMap[id] = mapped
			buttonExtras[id] = byPathExtras[item["path"].String()]
			buttonExtras[mapped] = byPathExtras[item["path"].String()]
		}
	}
	roles, err := g.Model("auth_role").Ctx(ctx).Fields("id,rules,menu,btns").All()
	if err != nil {
		return err
	}
	for _, role := range roles {
		if role["rules"].String() == "*" {
			continue
		}
		changes := map[string]any{}
		for _, field := range []string{"rules", "menu", "btns"} {
			mapping := buttonMap
			if field != "btns" {
				mapping = menuMap
			}
			value, changed := replaceRoleIDs(role[field].String(), mapping)
			if field == "btns" {
				value, changed = expandRoleButtons(role[field].String(), buttonMap, buttonExtras)
			}
			if field == "rules" {
				value2, c2 := expandRoleButtons(value, buttonMap, buttonExtras)
				value = value2
				changed = changed || c2
			}
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
	_, err = g.Model("auth_rule").Ctx(ctx).WhereIn("id", oldIDs).Data(map[string]any{"status": 1}).Update()
	return err
}

func expandRoleButtons(value string, mapping map[int64]int64, extras map[int64][]int64) (string, bool) {
	if value == "" || value == "*" {
		return value, false
	}
	seen := map[int64]bool{}
	ids := []int64{}
	changed := false
	add := func(id int64) {
		if id > 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, part := range strings.Split(value, ",") {
		id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || id < 1 {
			continue
		}
		if replacement := mapping[id]; replacement > 0 {
			add(replacement)
			changed = true
		} else {
			add(id)
		}
		if extra := extras[id]; len(extra) > 0 {
			changed = true
			for _, extraID := range extra {
				add(extraID)
			}
		}
	}
	if !changed {
		return value, false
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ","), true
}

func replaceRoleIDs(value string, mapping map[int64]int64) (string, bool) {
	if value == "" || value == "*" {
		return value, false
	}
	seen := map[int64]bool{}
	ids := []int64{}
	changed := false
	for _, part := range strings.Split(value, ",") {
		id, e := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if e != nil || id < 1 {
			continue
		}
		if replacement := mapping[id]; replacement > 0 {
			if replacement != id {
				changed = true
			}
			id = replacement
		}
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	if !changed {
		return value, false
	}
	sort.SliceStable(ids, func(i, j int) bool { return ids[i] < ids[j] })
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = gconv.String(id)
	}
	return strings.Join(parts, ","), true
}

// Preserve an existing collector's grants when the async job routes are added.
func upgradeCollectJobPermissions(ctx context.Context) error {
	rules, err := g.Model("auth_rule").Ctx(ctx).Fields("id,path").WhereIn("path", []string{
		"/admin/suxinvideo/collect", "/admin/suxinvideo/collect/auto",
		"/admin/suxinvideo/collect/classes", "/admin/suxinvideo/collect/preview",
		"/admin/suxinvideo/collect/job/start", "/admin/suxinvideo/collect/job/status",
		"/admin/suxinvideo/collect/job/cancel",
	}).Where("status", 0).WhereNull("deletetime").All()
	if err != nil {
		return err
	}
	byPath := map[string]int64{}
	for _, rule := range rules {
		byPath[rule["path"].String()] = rule["id"].Int64()
	}
	start, status, cancel := byPath["/admin/suxinvideo/collect/job/start"], byPath["/admin/suxinvideo/collect/job/status"], byPath["/admin/suxinvideo/collect/job/cancel"]
	if start == 0 || status == 0 || cancel == 0 {
		return nil
	}
	roles, err := g.Model("auth_role").Ctx(ctx).Fields("id,rules,btns").All()
	if err != nil {
		return err
	}
	for _, role := range roles {
		if role["rules"].String() == "*" {
			continue
		}
		changes := map[string]any{}
		for _, field := range []string{"rules", "btns"} {
			value := role[field].String()
			ids := map[int64]bool{}
			for _, part := range strings.Split(value, ",") {
				if id, e := strconv.ParseInt(strings.TrimSpace(part), 10, 64); e == nil && id > 0 {
					ids[id] = true
				}
			}
			hasCollect := ids[byPath["/admin/suxinvideo/collect"]]
			hasAuto := ids[byPath["/admin/suxinvideo/collect/auto"]]
			hasPreview := ids[byPath["/admin/suxinvideo/collect/preview"]]
			if !hasCollect && !hasAuto && !hasPreview {
				continue
			}
			if hasCollect || hasAuto {
				ids[status], ids[cancel] = true, true
			}
			if hasCollect {
				ids[start] = true
			}
			if hasCollect || hasPreview {
				for _, path := range []string{"/admin/suxinvideo/collect/classes", "/admin/suxinvideo/collect/preview"} {
					if id := byPath[path]; id > 0 {
						ids[id] = true
					}
				}
			}
			numbers := make([]int64, 0, len(ids))
			for id := range ids {
				numbers = append(numbers, id)
			}
			sort.Slice(numbers, func(i, j int) bool { return numbers[i] < numbers[j] })
			parts := make([]string, len(numbers))
			for i, id := range numbers {
				parts[i] = strconv.FormatInt(id, 10)
			}
			next := strings.Join(parts, ",")
			if next != value {
				changes[field] = next
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

// New live permissions keep stable IDs and do not widen an existing restricted
// role's access. Administrators assign each live action using the host RBAC UI.
func upgradeLivePermissions(ctx context.Context) error {
	parent, err := g.Model("auth_rule").Ctx(ctx).Where("routename", "suxinvideo_admin").One()
	if err != nil || parent == nil {
		return err
	}
	for action, title := range map[string]string{
		"groups": "直播分组", "channels": "直播频道", "streams": "直播线路", "subscriptions": "直播订阅",
		"save": "保存直播配置", "delete": "停用直播配置", "import": "导入直播列表", "refresh": "刷新直播订阅", "job": "直播导入进度", "probe": "检测直播线路",
	} {
		name := "suxinvideo_admin_live_" + action
		entry, err := g.Model("auth_rule").Ctx(ctx).Where("routename", name).One()
		if err != nil {
			return err
		}
		values := row{"pid": parent["id"].Int64(), "title": title, "path": "/admin/suxinvideo/live/" + action, "routepath": "live_" + action, "routename": name, "permission": "", "type": 2, "status": 0, "requiresauth": 1, "hideinmenu": 1}
		if entry == nil {
			for k, v := range authRuleDefaults() {
				values[k] = v
			}
			_, err = g.Model("auth_rule").Ctx(ctx).Data(values).Insert()
		} else {
			_, err = g.Model("auth_rule").Ctx(ctx).Where("id", entry["id"]).Data(values).Update()
		}
		if err != nil {
			return err
		}
	}
	return nil
}
