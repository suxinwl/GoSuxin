// The upstream analysis 1.0.0 plugin supplies chart layouts only.
// This adapter replaces all fixed sample figures with scoped host database queries.
package analysis

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/internal/addons/support"
	"github.com/suxinwl/GoSuxin/internal/plugins"
	"github.com/suxinwl/GoSuxin/utility/gf"
)

func init() {
	plugins.Register(plugins.Plugin{Name: "analysis", Title: "统计分析", Version: "1.0.0", Description: "真实操作趋势、登录地域与终端，以及影视、画册和私有仓统计。", Entry: "/analysis", MenuRoots: []string{"analysis"}, Admin: []interface{}{&Controller{}}, Start: start})
}
func start(ctx context.Context, _ *ghttp.Server) (func(), error) {
	ready, err := support.Ready()
	if err != nil || !ready {
		return nil, err
	}
	err = support.SeedMenu(ctx, "analysis", "统计分析", "/analysis/index", "icon-bar-chart", 3, []support.Action{{Path: "overview", Title: "查看统计数据"}})
	return func() {}, err
}

type OverviewReq struct {
	g.Meta `path:"analysis/overview" method:"get" tags:"analysis"`
	Days   int `p:"days" d:"30" v:"in:7,30,90"`
}
type OverviewRes struct{ *gf.R }
type Controller struct{}
type Point struct {
	Name  string `json:"name"`
	Value int64  `json:"value"`
}
type Day struct {
	Name        string `json:"name"`
	Operations  int64  `json:"operations"`
	Logins      int64  `json:"logins"`
	AlbumVisits int64  `json:"albumVisits"`
}

func Period(now time.Time, days int) (time.Time, time.Time, error) {
	if days != 7 && days != 30 && days != 90 {
		return time.Time{}, time.Time{}, fmt.Errorf("统计时段仅支持 7、30、90 天")
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return today.AddDate(0, 0, -days+1), today.AddDate(0, 0, 1), nil
}
func fillTrend(start time.Time, days int, operations, logins, visits map[string]int64) []Day {
	result := make([]Day, 0, days)
	for i := 0; i < days; i++ {
		label := start.AddDate(0, 0, i).Format("2006-01-02")
		result = append(result, Day{Name: label, Operations: operations[label], Logins: logins[label], AlbumVisits: visits[label]})
	}
	return result
}

func (c *Controller) Overview(ctx context.Context, req *OverviewReq) (*OverviewRes, error) {
	result, err := Snapshot(ctx, req.Days, time.Now())
	if err != nil {
		return &OverviewRes{gf.Failed().SetMsg("统计数据读取失败：" + err.Error())}, nil
	}
	return &OverviewRes{gf.Success().SetData(result)}, nil
}

func Snapshot(ctx context.Context, days int, now time.Time) (g.Map, error) {
	start, end, err := Period(now, days)
	if err != nil {
		return nil, err
	}
	all := support.CanManageAll(ctx)
	uid := support.UID(ctx)
	dbZone, err := databaseZone()
	if err != nil {
		return nil, err
	}
	_, localOffset := now.Zone()
	_, dbOffset := now.In(dbZone).Zone()
	shift := (localOffset - dbOffset) / 60
	expression := func(column string) string {
		if shift == 0 {
			return column
		}
		return fmt.Sprintf("DATE_ADD(%s, INTERVAL %d MINUTE)", column, shift)
	}
	from, to := start.In(dbZone).Format("2006-01-02 15:04:05"), end.In(dbZone).Format("2006-01-02 15:04:05")
	opWhere := "createtime>=? AND createtime<?"
	opArgs := []interface{}{from, to}
	loginWhere := opWhere + " AND status=0"
	loginArgs := []interface{}{from, to}
	visitWhere := "v.createtime>=? AND v.createtime<?"
	visitArgs := []interface{}{from, to}
	if !all {
		opWhere += " AND uid=?"
		opArgs = append(opArgs, uid)
		loginWhere += " AND uid=?"
		loginArgs = append(loginArgs, uid)
		visitWhere += " AND a.owner_id=?"
		visitArgs = append(visitArgs, uid)
	}
	query := func(sql string, args []interface{}) (gdb.Result, error) { return g.DB().GetAll(ctx, sql, args...) }
	scalar := func(sql string, args []interface{}) (int64, error) {
		v, e := g.DB().GetValue(ctx, sql, args...)
		if e != nil {
			return 0, e
		}
		return v.Int64(), nil
	}
	series := func(table, where string, args []interface{}) (map[string]int64, error) {
		rows, e := query("SELECT DATE_FORMAT("+expression("createtime")+",'%Y-%m-%d') AS name,COUNT(*) AS value FROM "+table+" WHERE "+where+" GROUP BY name", args)
		data := map[string]int64{}
		for _, r := range rows {
			data[r["name"].String()] = r["value"].Int64()
		}
		return data, e
	}
	points := func(sql string, args []interface{}) ([]Point, error) {
		rows, e := query(sql, args)
		data := make([]Point, 0, len(rows))
		for _, r := range rows {
			data = append(data, Point{Name: r["name"].String(), Value: r["value"].Int64()})
		}
		return data, e
	}
	summary := g.Map{}
	for _, q := range []struct {
		key, sql string
		args     []interface{}
	}{
		{"operations", "SELECT COUNT(*) FROM gf_operation_log WHERE " + opWhere, opArgs},
		{"uniqueIPs", "SELECT COUNT(DISTINCT NULLIF(ip,'')) FROM gf_operation_log WHERE " + opWhere, opArgs},
		{"logins", "SELECT COUNT(*) FROM gf_login_log WHERE " + loginWhere, loginArgs},
	} {
		n, e := scalar(q.sql, q.args)
		if e != nil {
			return nil, e
		}
		summary[q.key] = n
	}
	operations, err := series("gf_operation_log", opWhere, opArgs)
	if err != nil {
		return nil, err
	}
	logins, err := series("gf_login_log", loginWhere, loginArgs)
	if err != nil {
		return nil, err
	}
	// Optional business modules may be uninstalled or not present in another deployment.
	tables, err := query("SELECT table_name AS name FROM information_schema.tables WHERE table_schema=DATABASE()", nil)
	if err != nil {
		return nil, err
	}
	present := map[string]bool{}
	for _, r := range tables {
		present[r["name"].String()] = true
	}
	visits := map[string]int64{}
	summary["albumVisits"] = int64(0)
	if present["gf_album"] && present["gf_album_visit"] {
		n, e := scalar("SELECT COUNT(*) FROM gf_album_visit v JOIN gf_album a ON a.id=v.album_id WHERE "+visitWhere, visitArgs)
		if e != nil {
			return nil, e
		}
		summary["albumVisits"] = n
		rows, e := query("SELECT DATE_FORMAT("+expression("v.createtime")+",'%Y-%m-%d') AS name,COUNT(*) AS value FROM gf_album_visit v JOIN gf_album a ON a.id=v.album_id WHERE "+visitWhere+" GROUP BY name", visitArgs)
		if e != nil {
			return nil, e
		}
		for _, r := range rows {
			visits[r["name"].String()] = r["value"].Int64()
		}
	}
	for _, q := range []struct {
		key, table string
		owner      bool
	}{
		{"albums", "gf_album", true}, {"privatePackages", "gf_privatecode_content", true}, {"films", "sx_vod", false}, {"members", "sx_user", false}, {"orders", "sx_order", false},
	} {
		n := int64(0)
		if present[q.table] && (all || q.owner) {
			sql := "SELECT COUNT(*) FROM " + q.table
			args := []interface{}{}
			if !all {
				sql += " WHERE owner_id=?"
				args = append(args, uid)
			}
			n, err = scalar(sql, args)
			if err != nil {
				return nil, err
			}
		}
		summary[q.key] = n
	}
	browser, err := points("SELECT COALESCE(NULLIF(browser,''),'未识别') AS name,COUNT(*) AS value FROM gf_login_log WHERE "+loginWhere+" GROUP BY name ORDER BY value DESC LIMIT 10", loginArgs)
	if err != nil {
		return nil, err
	}
	os, err := points("SELECT COALESCE(NULLIF(os,''),'未识别') AS name,COUNT(*) AS value FROM gf_login_log WHERE "+loginWhere+" GROUP BY name ORDER BY value DESC LIMIT 10", loginArgs)
	if err != nil {
		return nil, err
	}
	geo, err := points("SELECT COALESCE(NULLIF(address,''),'未解析地域') AS name,COUNT(*) AS value FROM gf_login_log WHERE "+loginWhere+" GROUP BY name ORDER BY value DESC LIMIT 10", loginArgs)
	if err != nil {
		return nil, err
	}
	modules, err := points("SELECT SUBSTRING_INDEX(TRIM(LEADING '/' FROM url),'/',2) AS name,COUNT(*) AS value FROM gf_operation_log WHERE "+opWhere+" GROUP BY name ORDER BY value DESC LIMIT 10", opArgs)
	if err != nil {
		return nil, err
	}
	for i := range modules {
		modules[i].Name = moduleTitle(modules[i].Name)
	}
	rows, err := query("SELECT FLOOR(HOUR("+expression("createtime")+")/2)*2 AS hour,COUNT(*) AS value FROM gf_operation_log WHERE "+opWhere+" GROUP BY hour", opArgs)
	if err != nil {
		return nil, err
	}
	hourCounts := map[int]int64{}
	for _, r := range rows {
		hourCounts[r["hour"].Int()] = r["value"].Int64()
	}
	timeslots := make([]Point, 0, 12)
	for h := 0; h < 24; h += 2 {
		timeslots = append(timeslots, Point{Name: fmt.Sprintf("%02d:00", h), Value: hourCounts[h]})
	}
	scope := "全部数据"
	if !all {
		scope = "本人日志、画册及私有仓资料"
	}
	return g.Map{"days": days, "from": start.Format("2006-01-02 15:04:05"), "through": end.AddDate(0, 0, -1).Format("2006-01-02"), "updatedAt": now.Format("2006-01-02 15:04:05"), "scope": scope, "canManageAll": all, "summary": summary, "trend": fillTrend(start, days, operations, logins, visits), "browser": browser, "os": os, "geo": geo, "modules": modules, "timeslots": timeslots}, nil
}

// MySQL serializes time.Time using its configured loc (UTC if omitted).
func databaseZone() (*time.Location, error) {
	cfg := g.DB().GetConfig()
	name := cfg.Timezone
	if name == "" {
		values, err := url.ParseQuery(cfg.Extra)
		if err != nil {
			return nil, err
		}
		name = values.Get("loc")
	}
	if name == "" {
		name = "UTC"
	}
	return time.LoadLocation(name)
}
func moduleTitle(name string) string {
	names := map[string]string{"admin/user": "账号与菜单", "admin/system": "系统管理", "admin/developer": "开发与插件", "admin/datacenter": "数据与文件", "admin/dashboard": "工作台", "admin/album": "电子画册", "admin/suxinvideo": "影视 CMS", "admin/privatecode": "私有插件仓", "admin/analysis": "统计分析"}
	if title, ok := names[name]; ok {
		return title
	}
	return name
}
