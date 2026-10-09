package suxinvideo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"github.com/suxinwl/GoSuxin/utility/gf"
)

type LogsReq struct {
	g.Meta `path:"/logs" method:"get"`
	Page   int `p:"page"`
}
type LogsRes struct{}
type ClearLogsReq struct {
	g.Meta `path:"/logs/clear" method:"post"`
}
type ClearLogsRes struct{}

func cmsAudit(r *ghttp.Request) {
	r.Middleware.Next()
	if r.Method != "POST" || r.Response.Status >= 400 || r.GetCtxVar("cms_denied").Bool() {
		return
	}
	uid := gconv.Int64(r.GetCtxVar("uid"))
	if uid < 1 {
		return
	}
	action := strings.TrimPrefix(r.URL.Path, "/admin/suxinvideo/")
	if action == "" {
		return
	}
	if action == "save" || action == "delete" || action == "deleteBatch" {
		table := r.Get("table").String()
		if _, ok := editableTables[table]; ok {
			action = table + "/" + action
		}
	}
	if action == "save" || action == "delete" || strings.HasSuffix(action, "/save") || strings.HasSuffix(action, "/delete") {
		if id := r.Get("id").Int64(); id > 0 {
			action += fmt.Sprintf("#%d", id)
		}
	}
	if err := execSQL(r.Context(), "INSERT INTO sx_admin_log(admin_id,action,ip,created) VALUES(?,?,?,?)", uid, cutRunes(action, 200), cmsClientIP(r.Context(), r), time.Now().Unix()); err != nil {
		g.Log().Warning(r.Context(), "CMS audit:", err)
	}
}
func (*Admin) Logs(ctx context.Context, req *LogsReq) (*LogsRes, error) {
	page := clampPage(req.Page)
	count, err := one(ctx, "SELECT COUNT(*) n FROM sx_admin_log")
	if err != nil {
		return nil, err
	}
	rows, err := all(ctx, "SELECT l.id,l.admin_id,l.action,l.ip,l.created,a.username FROM sx_admin_log l LEFT JOIN gf_admin a ON a.id=l.admin_id ORDER BY l.id DESC LIMIT 30 OFFSET ?", (page-1)*30)
	if err != nil {
		return nil, err
	}
	g.RequestFromCtx(ctx).Response.WriteJson(gf.Success().SetData(map[string]any{"list": rows, "total": count["n"], "page": page}))
	return &LogsRes{}, nil
}
func (*Admin) ClearLogs(ctx context.Context, _ *ClearLogsReq) (*ClearLogsRes, error) {
	if err := execSQL(ctx, "DELETE FROM sx_admin_log"); err != nil {
		return nil, fmt.Errorf("清空 CMS 日志: %w", err)
	}
	g.RequestFromCtx(ctx).Response.WriteJson(gf.Success().SetData(true))
	return &ClearLogsRes{}, nil
}
