package suxinvideo

import (
	"context"
	"strings"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"github.com/suxinwl/GoSuxin/utility/gf"
)

type CapabilitiesReq struct {
	g.Meta `path:"/capabilities" method:"get" noAuth:"1"`
}
type CapabilitiesRes struct{}

// Host RBAC authorizes paths. These endpoints share paths between CMS tables,
// so a second check is required to keep table-specific grants separate.
func resourceAllowed(ctx context.Context, table, action string) (bool, error) {
	uid := gconv.Int64(ctx.Value("uid"))
	if uid < 1 {
		return false, nil
	}
	roles, err := all(ctx, "SELECT r.rules,r.btns FROM gf_auth_role r JOIN gf_auth_role_access a ON a.role_id=r.id WHERE a.uid=?", uid)
	if err != nil {
		return false, err
	}
	if len(roles) == 0 {
		return false, nil
	}
	for _, role := range roles {
		if gconv.String(role["rules"]) == "*" {
			return true, nil
		}
	}
	ids := make([]string, 0)
	for _, role := range roles {
		for _, part := range strings.Split(gconv.String(role["btns"]), ",") {
			if id := gconv.Int64(strings.TrimSpace(part)); id > 0 {
				ids = append(ids, gconv.String(id))
			}
		}
	}
	if len(ids) == 0 {
		return false, nil
	}
	path := "/admin/suxinvideo/" + action
	if table == "user" && (action == "save" || action == "delete") {
		path = "/admin/suxinvideo/user/" + action
	}
	query := "SELECT COUNT(*) n FROM gf_auth_rule WHERE status=0 AND type=2 AND id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ") AND path=? AND (permission=? OR permission='')"
	args := make([]any, 0, len(ids)+2)
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, path, "suxinvideo:"+table+":"+action)
	entry, err := one(ctx, query, args...)
	return err == nil && gconv.Int(entry["n"]) > 0, err
}

func requireResource(ctx context.Context, r *ghttp.Request, table, action string) (bool, error) {
	allowed, err := resourceAllowed(ctx, table, action)
	if err != nil {
		return false, err
	}
	if !allowed {
		r.SetCtxVar("cms_denied", true)
		r.Response.WriteJson(gf.Failed().SetMsg("无权操作此管理页面"))
	}
	return allowed, nil
}

func (*Admin) Capabilities(ctx context.Context, _ *CapabilitiesReq) (*CapabilitiesRes, error) {
	caps := map[string]map[string]bool{}
	for table := range editableTables {
		caps[table] = map[string]bool{}
		for _, action := range []string{"list", "save", "delete", "deleteBatch", "cleanup"} {
			ok, err := resourceAllowed(ctx, table, action)
			if err != nil {
				return nil, err
			}
			caps[table][action] = ok
		}
	}
	pages := map[string]bool{}
	views := map[string][2]string{
		"main/dashboard": {"", "dashboard"},
		"content/vod":    {"vod", "list"}, "content/type": {"type", "list"},
		"content/slide": {"slide", "list"}, "content/article": {"article", "list"},
		"content/link": {"link", "list"}, "content/collect": {"collect_api", "list"},
		"content/filmreqs": {"film_request", "list"}, "content/player": {"player", "list"},
		"content/live": {"", "live/channels"},
		"user/user":    {"user", "list"}, "user/goods": {"goods", "list"},
		"user/order": {"order", "list"}, "user/comment": {"comment", "list"},
		"system/market": {"", "config"}, "system/setting": {"", "config"},
		"system/images": {"", "images"}, "system/log": {"", "logs"}, "system/clients": {"", "clients/releases"},
	}
	for view, target := range views {
		ok, err := resourceAllowed(ctx, target[0], target[1])
		if err != nil {
			return nil, err
		}
		pages[view] = ok
	}
	pages["content/collect"] = pages["content/collect"] && pages["system/setting"]
	pages["system/market"] = pages["system/market"] && caps["plugin"]["list"]
	actions := map[string]bool{}
	for _, action := range []string{"config", "saveConfig", "clearCache", "collect", "collect/auto", "collect/job/start", "collect/job/status", "collect/job/cancel", "collect/classes", "collect/preview", "collect/duplicates", "collect/merge", "user/unlock", "images/delete", "images/clean", "logs/clear", "mailTest", "adminPassword", "installTheme", "uninstallTheme", "clients/releases", "clients/publish", "clients/status", "live/groups", "live/channels", "live/streams", "live/subscriptions", "live/save", "live/delete", "live/import", "live/refresh", "live/job", "live/probe"} {
		ok, err := resourceAllowed(ctx, "", action)
		if err != nil {
			return nil, err
		}
		actions[action] = ok
	}
	for action := range liveProviderAdminActions {
		ok, err := resourceAllowed(ctx, "", "live/"+action)
		if err != nil {
			return nil, err
		}
		actions["live/"+action] = ok
	}
	g.RequestFromCtx(ctx).Response.WriteJson(gf.Success().SetData(map[string]any{"resources": caps, "pages": pages, "actions": actions}))
	return &CapabilitiesRes{}, nil
}
