package suxinvideo

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	hostgf "github.com/suxinwl/GoSuxin/utility/gf"
)

type Admin struct{}
type AdminListReq struct {
	g.Meta  `path:"/list" method:"get"`
	Table   string `p:"table"`
	Page    int    `p:"page"`
	Keyword string `p:"keyword"`
	TypeID  int64  `p:"type_id"`
	Status  string `p:"status"`
	ID      int64  `p:"id"`
}
type AdminListRes struct{}
type AdminSaveReq struct {
	g.Meta `path:"/save" method:"post"`
	Table  string         `p:"table"`
	ID     int64          `p:"id"`
	Data   map[string]any `p:"data"`
}
type AdminSaveRes struct{}
type AdminDeleteReq struct {
	g.Meta `path:"/delete" method:"post"`
	Table  string `p:"table"`
	ID     int64  `p:"id"`
}
type AdminDeleteRes struct{}
type AdminConfigReq struct {
	g.Meta `path:"/config" method:"get"`
}
type AdminConfigRes struct{}
type AdminSaveConfigReq struct {
	g.Meta `path:"/saveConfig" method:"post"`
	Values map[string]string `p:"values"`
}
type AdminSaveConfigRes struct{}
type AdminStatsReq struct {
	g.Meta `path:"/stats" method:"get"`
}
type AdminStatsRes struct{}

var editableTables = map[string]map[string]bool{
	"vod":          fields("type_id api_id api_vid name name_norm sub class year area lang remarks score director actor content pic play_from play_url vip points status addtime updatetime"),
	"type":         fields("pid name sort status show_home icon image"),
	"article":      fields("title content status addtime"),
	"topic":        fields("name pic description content status addtime"),
	"slide":        fields("name pic url pos sort status"),
	"link":         fields("name url sort status"),
	"player":       fields("code name parse status"),
	"goods":        fields("name price points days sort status"),
	"user":         fields("name points vip_expire avatar status email_verified"),
	"order":        fields("title"),
	"comment":      fields("content status"),
	"film_request": fields("status note"),
	"collect_api":  fields("name api_url remark status collect_auto collect_hours addtime"),
	"plugin":       fields("name status expire"),
}
var searchable = map[string]string{"vod": "name", "type": "name", "article": "title", "topic": "name", "user": "email", "collect_api": "name", "goods": "name", "player": "name", "film_request": "title"}
var secretSettings = fields("geetest_key turnstile_secret smtp_pass codepay_key epay_key usdt_trongrid_key wxpay_key alipay_private_key baidu_push_token proxy_secret")
var adminSettings = fields(`site_name site_logo site_favicon user_default_avatar site_icp site_keywords site_description site_mode site_template player_parse player_autoplay player_ad_filter player_ad_domains
	member_enable register_enable comment_audit comment_enable points_sign points_register points_pay_rate img_auto_clean_enable
	kp_icon_search kp_icon_history kp_icon_user kp_slide_enable kp_slide_count kp_slide_source rewrite_enable browser_check_enable home_slide_enable
	captcha_provider geetest_id geetest_key turnstile_site_key turnstile_secret smtp_host smtp_port smtp_secure smtp_user smtp_pass smtp_from_name
	codepay_gateway codepay_pid codepay_key codepay_channel epay_gateway epay_pid epay_key epay_channel usdt_address usdt_rate usdt_trongrid_key
	wxpay_appid wxpay_mchid wxpay_key alipay_appid alipay_private_key alipay_public_key cdn_mode
	ad_home_enable ad_home_code ad_playtop_enable ad_playtop_code ad_playbottom_enable ad_playbottom_code ad_footer_enable ad_footer_code
	debug admin_remark search_limit_enable search_limit_times search_limit_window baidu_push_site baidu_push_token
	collect_auto_enable collect_auto_interval collect_dedup_title collect_img_local collect_speed collect_source_concurrency source_discovery_enable source_discovery_interval yqk_bootstrap_url yqk_navigation_enable home_recommend_source home_hero_source
	content_block_enable content_block_keywords content_block_categories`)
var booleanSettings = fields(`player_autoplay player_ad_filter member_enable register_enable comment_audit comment_enable img_auto_clean_enable
	kp_icon_search kp_icon_history kp_icon_user kp_slide_enable rewrite_enable browser_check_enable home_slide_enable
	cdn_mode ad_home_enable ad_playtop_enable ad_playbottom_enable ad_footer_enable debug search_limit_enable
	collect_auto_enable collect_dedup_title collect_img_local source_discovery_enable yqk_navigation_enable content_block_enable`)

func fields(s string) map[string]bool {
	m := map[string]bool{}
	for _, f := range strings.Fields(s) {
		m[f] = true
	}
	return m
}
func adminTable(name string) (string, bool) { _, ok := editableTables[name]; return "sx_" + name, ok }

func (*Admin) AdminList(ctx context.Context, req *AdminListReq) (*AdminListRes, error) {
	table, ok := adminTable(req.Table)
	r := g.RequestFromCtx(ctx)
	if !ok {
		badRequest(r, "无效数据表")
		return &AdminListRes{}, nil
	}
	allowed, err := requireResource(ctx, r, req.Table, "list")
	if err != nil {
		return nil, err
	}
	if !allowed {
		return &AdminListRes{}, nil
	}
	page := clampPage(req.Page)
	from, selectFields, sortColumn := table, "*", "id"
	if req.Table == "vod" {
		from, selectFields = "sx_vod v LEFT JOIN sx_type t ON t.id=v.type_id", "v.*,t.name tname"
		sortColumn = "v.id"
	} else if req.Table == "type" {
		selectFields, sortColumn = "sx_type.*,(SELECT COUNT(*) FROM sx_vod v WHERE v.type_id=sx_type.id) c", "sort ASC,id ASC"
	} else if req.Table == "slide" || req.Table == "link" {
		sortColumn = "sort ASC,id"
	} else if req.Table == "goods" {
		sortColumn = "sort ASC,price ASC"
	} else if req.Table == "order" {
		from, selectFields, sortColumn = "sx_order o LEFT JOIN sx_user u ON u.id=o.user_id", "o.*,u.name uname,u.email", "o.id"
	} else if req.Table == "comment" {
		from, selectFields, sortColumn = "sx_comment c LEFT JOIN sx_user u ON u.id=c.user_id LEFT JOIN sx_vod v ON v.id=c.vod_id", "c.*,u.name uname,v.name vname", "c.id"
	}
	where := ""
	args := []any{}
	if col := searchable[req.Table]; col != "" && strings.TrimSpace(req.Keyword) != "" {
		if req.Table == "user" {
			where = " WHERE (email LIKE ? OR name LIKE ?)"
			args = append(args, "%"+strings.TrimSpace(req.Keyword)+"%", "%"+strings.TrimSpace(req.Keyword)+"%")
		} else {
			if req.Table == "vod" {
				col = "v." + col
			}
			where = " WHERE " + col + " LIKE ?"
			args = append(args, "%"+strings.TrimSpace(req.Keyword)+"%")
		}
	}
	if req.Table == "vod" && req.TypeID > 0 {
		if where == "" {
			where = " WHERE "
		} else {
			where += " AND "
		}
		where += "v.type_id=?"
		args = append(args, req.TypeID)
	}
	if req.ID > 0 && (req.Table == "vod" || req.Table == "article") {
		if where == "" {
			where = " WHERE "
		} else {
			where += " AND "
		}
		if req.Table == "vod" {
			where += "v.id=?"
		} else {
			where += "id=?"
		}
		args = append(args, req.ID)
	}
	if req.Status != "" && req.Status != "-1" && (req.Table == "order" || req.Table == "comment") {
		status := gconv.Int(req.Status)
		if req.Status != gconv.String(status) || status < 0 || status > 2 || (req.Table == "comment" && status > 1) {
			badRequest(r, "状态筛选无效")
			return &AdminListRes{}, nil
		}
		if where == "" {
			where = " WHERE "
		} else {
			where += " AND "
		}
		prefix := "o"
		if req.Table == "comment" {
			prefix = "c"
		}
		where += prefix + ".status=?"
		args = append(args, status)
	}
	count, err := one(ctx, "SELECT COUNT(*) AS n FROM "+from+where, args...)
	if err != nil {
		return nil, err
	}
	orderDirection := " DESC"
	if req.Table == "type" || req.Table == "goods" {
		orderDirection = ""
	}
	pageSize := 20
	paginated := req.Table == "vod" || req.Table == "article" || req.Table == "user" || req.Table == "order" || req.Table == "comment"
	query := "SELECT " + selectFields + " FROM " + from + where + " ORDER BY " + sortColumn + orderDirection
	if paginated {
		query += " LIMIT ? OFFSET ?"
		args = append(args, pageSize, (page-1)*pageSize)
	} else if req.Table == "film_request" {
		query += " LIMIT 200"
	}
	rows, err := all(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	if req.Table == "user" {
		for _, item := range rows {
			delete(item, "pwd")
		}
	}
	result := map[string]any{"total": gconv.Int(count["n"]), "page": page, "pageSize": pageSize, "paginated": paginated, "list": rows}
	if req.Table == "vod" {
		types, err := all(ctx, "SELECT id,name,pid FROM sx_type WHERE status=1 ORDER BY sort,id LIMIT 200")
		if err != nil {
			return nil, err
		}
		result["types"] = types
	}
	r.Response.WriteJson(hostgf.Success().SetData(result))
	return &AdminListRes{}, nil
}
func (*Admin) AdminSave(ctx context.Context, req *AdminSaveReq) (*AdminSaveRes, error) {
	table, ok := adminTable(req.Table)
	r := g.RequestFromCtx(ctx)
	if !ok || len(req.Data) == 0 {
		badRequest(r, "参数无效")
		return &AdminSaveRes{}, nil
	}
	allowed, err := requireResource(ctx, r, req.Table, "save")
	if err != nil {
		return nil, err
	}
	if !allowed {
		return &AdminSaveRes{}, nil
	}
	if req.Table == "vod" {
		playURL := gconv.String(req.Data["play_url"])
		picture := gconv.String(req.Data["pic"])
		lower := strings.ToLower(playURL + picture)
		if len([]rune(playURL)) > 1000000 || strings.Contains(lower, "<?php") || strings.Contains(lower, "<?=") || strings.Contains(lower, "<script") || gconv.Int(req.Data["points"]) < 0 || gconv.Float64(req.Data["score"]) < 0 || gconv.Float64(req.Data["score"]) > 10 {
			badRequest(r, "影片内容或播放地址无效")
			return &AdminSaveRes{}, nil
		}
		if raw, ok := req.Data["name"]; ok {
			name := strings.TrimSpace(gconv.String(raw))
			if name == "" || len([]rune(name)) > 120 {
				badRequest(r, "片名无效")
				return &AdminSaveRes{}, nil
			}
			req.Data["name"] = name
			req.Data["name_norm"] = normalizeVodName(name)
		} else if req.ID == 0 {
			badRequest(r, "片名不能为空")
			return &AdminSaveRes{}, nil
		}
		req.Data["updatetime"] = time.Now().Unix()
		if req.ID == 0 {
			req.Data["addtime"] = time.Now().Unix()
		}
	}
	if req.Table == "article" && req.ID == 0 {
		if strings.TrimSpace(gconv.String(req.Data["title"])) == "" {
			badRequest(r, "文章标题不能为空")
			return &AdminSaveRes{}, nil
		}
		req.Data["addtime"] = time.Now().Unix()
	}
	if req.Table == "collect_api" && req.ID == 0 {
		req.Data["addtime"] = time.Now().Unix()
	}
	if req.Table == "type" {
		name := strings.TrimSpace(gconv.String(req.Data["name"]))
		if name == "" || len([]rune(name)) > 30 || gconv.Int64(req.Data["pid"]) == req.ID && req.ID > 0 {
			badRequest(r, "分类名称或父级无效")
			return &AdminSaveRes{}, nil
		}
		req.Data["name"] = name
		if parentID := gconv.Int64(req.Data["pid"]); parentID > 0 {
			parent, e := one(ctx, "SELECT id,pid FROM sx_type WHERE id=?", parentID)
			if e != nil {
				return nil, e
			}
			if parent == nil || gconv.Int64(parent["pid"]) == req.ID && req.ID > 0 {
				badRequest(r, "父级分类不存在或形成循环")
				return &AdminSaveRes{}, nil
			}
		}
	}
	if req.Table == "slide" {
		if strings.TrimSpace(gconv.String(req.Data["pic"])) == "" {
			badRequest(r, "图片地址不能为空")
			return &AdminSaveRes{}, nil
		}
		link := strings.TrimSpace(gconv.String(req.Data["url"]))
		if link != "" && !strings.HasPrefix(link, "/") && !strings.HasPrefix(strings.ToLower(link), "https://") && !strings.HasPrefix(strings.ToLower(link), "http://") {
			badRequest(r, "跳转链接仅允许 http(s) 或站内地址")
			return &AdminSaveRes{}, nil
		}
		if req.Data["pos"] != "movie" {
			req.Data["pos"] = "top"
		}
	}
	if req.Table == "player" {
		parse := strings.TrimSpace(gconv.String(req.Data["parse"]))
		if parse != "" && !strings.Contains(parse, "{url}") {
			badRequest(r, "解析地址必须包含 {url}")
			return &AdminSaveRes{}, nil
		}
	}
	if req.Table == "goods" {
		if req.ID == 0 && gconv.Int(req.Data["points"]) <= 0 && gconv.Int(req.Data["days"]) <= 0 && gconv.Float64(req.Data["price"]) > 0 {
			rate := gconv.Float64(setting(ctx, "points_pay_rate", "10"))
			if rate > 0 && rate <= 100000 {
				req.Data["points"] = int64(gconv.Float64(req.Data["price"])*rate + 0.5)
			}
		}
		if strings.TrimSpace(gconv.String(req.Data["name"])) == "" || gconv.Float64(req.Data["price"]) < 0.01 || (gconv.Int(req.Data["points"]) <= 0 && gconv.Int(req.Data["days"]) <= 0) {
			badRequest(r, "套餐名称、价格和权益无效")
			return &AdminSaveRes{}, nil
		}
	}
	if req.Table == "link" && req.Data["url"] != nil {
		u, e := url.Parse(gconv.String(req.Data["url"]))
		if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
			badRequest(r, "链接地址无效")
			return &AdminSaveRes{}, nil
		}
	}
	allowedFields := editableTables[req.Table]
	keys := make([]string, 0, len(req.Data))
	for col := range req.Data {
		if !allowedFields[col] || checkIdentifier(col) != nil {
			badRequest(r, "包含不可编辑字段")
			return &AdminSaveRes{}, nil
		}
		keys = append(keys, col)
	}
	sort.Strings(keys)
	args := make([]any, 0, len(keys)+1)
	marks := make([]string, 0, len(keys))
	assign := make([]string, 0, len(keys))
	for _, col := range keys {
		marks = append(marks, "?")
		assign = append(assign, "`"+col+"`=?")
		args = append(args, req.Data[col])
	}
	err = nil
	if req.ID > 0 {
		err = execSQL(ctx, "UPDATE "+table+" SET "+strings.Join(assign, ",")+" WHERE id=?", append(args, req.ID)...)
	} else {
		if req.Table == "user" || req.Table == "order" || req.Table == "comment" || req.Table == "film_request" || req.Table == "plugin" {
			badRequest(r, "此类数据不允许后台直接创建")
			return &AdminSaveRes{}, nil
		}
		quoted := make([]string, len(keys))
		for i, col := range keys {
			quoted[i] = "`" + col + "`"
		}
		err = execSQL(ctx, "INSERT INTO "+table+" ("+strings.Join(quoted, ",")+") VALUES ("+strings.Join(marks, ",")+")", args...)
	}
	if err != nil {
		return nil, err
	}
	if req.Table == "type" {
		invalidateContentPolicyCache()
	}
	if req.Table == "vod" {
		if err := invalidateAppFilmCatalog(ctx); err != nil {
			return nil, err
		}
	}
	r.Response.WriteJson(hostgf.Success().SetData(true))
	return &AdminSaveRes{}, nil
}
func (*Admin) AdminDelete(ctx context.Context, req *AdminDeleteReq) (*AdminDeleteRes, error) {
	table, ok := adminTable(req.Table)
	r := g.RequestFromCtx(ctx)
	if !ok || req.ID < 1 {
		badRequest(r, "参数无效")
		return &AdminDeleteRes{}, nil
	}
	allowed, err := requireResource(ctx, r, req.Table, "delete")
	if err != nil {
		return nil, err
	}
	if !allowed {
		return &AdminDeleteRes{}, nil
	}
	if req.Table == "user" {
		badRequest(r, "会员请使用安全删除操作")
		return &AdminDeleteRes{}, nil
	}
	if req.Table == "type" {
		count, e := one(ctx, "SELECT COUNT(*) n FROM sx_vod WHERE type_id=?", req.ID)
		if e != nil {
			return nil, e
		}
		if gconv.Int(count["n"]) > 0 {
			badRequest(r, "该分类下有影片，请先转移")
			return &AdminDeleteRes{}, nil
		}
	}
	if err := execSQL(ctx, "DELETE FROM "+table+" WHERE id=?", req.ID); err != nil {
		return nil, err
	}
	if req.Table == "type" {
		invalidateContentPolicyCache()
	}
	if req.Table == "vod" {
		if err := invalidateAppFilmCatalog(ctx); err != nil {
			return nil, err
		}
	}
	r.Response.WriteJson(hostgf.Success().SetData(true))
	return &AdminDeleteRes{}, nil
}
func (*Admin) AdminConfig(ctx context.Context, _ *AdminConfigReq) (*AdminConfigRes, error) {
	rows, err := all(ctx, "SELECT `key`,`value` FROM sx_config ORDER BY `key`")
	if err != nil {
		return nil, err
	}
	values := map[string]any{}
	for _, row := range rows {
		key := gconv.String(row["key"])
		if secretSettings[key] {
			values[key] = ""
		} else {
			values[key] = row["value"]
		}
	}
	for _, key := range []string{"site_logo", "site_favicon", "user_default_avatar"} {
		values[key] = brandSetting(ctx, key)
	}
	themes, err := all(ctx, "SELECT code,name FROM sx_plugin WHERE type='template' AND status=1 ORDER BY id")
	if err != nil {
		return nil, err
	}
	values["__themes"] = themes
	g.RequestFromCtx(ctx).Response.WriteJson(hostgf.Success().SetData(values))
	return &AdminConfigRes{}, nil
}
func (*Admin) AdminSaveConfig(ctx context.Context, req *AdminSaveConfigReq) (*AdminSaveConfigRes, error) {
	r := g.RequestFromCtx(ctx)
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		address, kind, err := saveBrandUpload(ctx)
		if err != nil {
			badRequest(r, err.Error())
			return &AdminSaveConfigRes{}, nil
		}
		r.Response.WriteJson(hostgf.Success().SetData(map[string]string{"url": address, "kind": kind}))
		return &AdminSaveConfigRes{}, nil
	}
	for key, value := range req.Values {
		if !adminSettings[key] || len(value) > 10000 {
			badRequest(r, "配置项无效")
			return &AdminSaveConfigRes{}, nil
		}
		if booleanSettings[key] && value != "0" && value != "1" {
			badRequest(r, "开关配置值无效")
			return &AdminSaveConfigRes{}, nil
		}
		if key == "player_ad_domains" {
			normalized, err := normalizeMediaAdDomains(value)
			if err != nil {
				badRequest(r, err.Error())
				return &AdminSaveConfigRes{}, nil
			}
			req.Values[key] = normalized
		}
		if key == "content_block_keywords" || key == "content_block_categories" {
			if err := validateContentRuleText(value); err != nil {
				badRequest(r, err.Error())
				return &AdminSaveConfigRes{}, nil
			}
		}
		if (key == "site_logo" || key == "site_favicon" || key == "user_default_avatar") && !validBrandURL(value) {
			badRequest(r, "网站图标地址须为已上传图片或 HTTPS 地址")
			return &AdminSaveConfigRes{}, nil
		}
		if key == "site_mode" && value != "cms" && value != "waterfall" && value != "movie" {
			badRequest(r, "影视站模式无效")
			return &AdminSaveConfigRes{}, nil
		}
		if key == "home_recommend_source" && value != "yqk" && value != "local" {
			badRequest(r, "首页推荐数据源无效")
			return &AdminSaveConfigRes{}, nil
		}
		if key == "home_hero_source" && value != "yqk" && value != "managed" {
			badRequest(r, "首页轮播数据源无效")
			return &AdminSaveConfigRes{}, nil
		}
		if key == "kp_slide_source" && value != "new" && value != "hot" && value != "slide" {
			badRequest(r, "幻灯数据源无效")
			return &AdminSaveConfigRes{}, nil
		}
		if key == "kp_slide_count" && (gconv.Int(value) < 2 || gconv.Int(value) > 10) {
			badRequest(r, "幻灯数量须为2至10")
			return &AdminSaveConfigRes{}, nil
		}
		if key == "collect_auto_interval" && (gconv.Int(value) < 5 || gconv.Int(value) > 10080) {
			badRequest(r, "采集间隔须为5至10080分钟")
			return &AdminSaveConfigRes{}, nil
		}
		if key == "collect_source_concurrency" {
			workers, err := strconv.Atoi(value)
			if err != nil || workers < 1 || workers > 6 {
				badRequest(r, "并发采集源数量须为1至6")
				return &AdminSaveConfigRes{}, nil
			}
			req.Values[key] = strconv.Itoa(workers)
		}
		if key == "collect_speed" && value != "gentle" && value != "slow" && value != "normal" && value != "fast" {
			badRequest(r, "采集速度须为温和、慢速、标准或快速")
			return &AdminSaveConfigRes{}, nil
		}
		if key == "source_discovery_interval" {
			interval, err := strconv.Atoi(value)
			if err != nil || interval < 15 || interval > 1440 {
				badRequest(r, "补源检测间隔须为15至1440分钟")
				return &AdminSaveConfigRes{}, nil
			}
		}
		if key == "yqk_bootstrap_url" {
			entry, parseErr := url.Parse(strings.TrimSpace(value))
			if parseErr != nil || entry.Scheme != "https" || entry.Hostname() == "" || entry.User != nil || entry.Fragment != "" || len(value) > 2048 {
				badRequest(r, "小柒入口须为有效的 HTTPS JSON 地址")
				return &AdminSaveConfigRes{}, nil
			}
		}
		if key == "points_pay_rate" && (gconv.Int(value) < 1 || gconv.Int(value) > 100000) {
			badRequest(r, "充值积分比例无效")
			return &AdminSaveConfigRes{}, nil
		}
		if key == "player_parse" && value != "" && !strings.Contains(value, "{url}") {
			badRequest(r, "全局解析地址必须包含 {url}")
			return &AdminSaveConfigRes{}, nil
		}
		if key == "captcha_provider" {
			if value != "graph" && value != "geetest" && value != "turnstile" {
				badRequest(r, "验证码方式无效")
				return &AdminSaveConfigRes{}, nil
			}
			needed := []string{}
			if value == "geetest" {
				needed = []string{"geetest_id", "geetest_key"}
			}
			if value == "turnstile" {
				needed = []string{"turnstile_site_key", "turnstile_secret"}
			}
			for _, field := range needed {
				candidate := req.Values[field]
				if candidate == "" {
					candidate = setting(ctx, field, "")
				}
				if candidate == "" {
					badRequest(r, "请先配置验证码服务的站点密钥")
					return &AdminSaveConfigRes{}, nil
				}
			}
		}
		if key == "site_template" {
			theme := cleanTheme(value)
			if theme != value {
				badRequest(r, "主题代码无效")
				return &AdminSaveConfigRes{}, nil
			}
			record, err := one(ctx, "SELECT id FROM sx_plugin WHERE code=? AND type='template' AND status=1", theme)
			if err != nil {
				return nil, err
			}
			if record == nil {
				badRequest(r, "主题尚未安装")
				return &AdminSaveConfigRes{}, nil
			}
		}
	}
	err := g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		for key, value := range req.Values {
			if secretSettings[key] && value == "" {
				continue
			}
			if _, err := tx.Exec("INSERT INTO sx_config(`key`,`value`) VALUES(?,?) ON DUPLICATE KEY UPDATE `value`=VALUES(`value`)", key, value); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	invalidateContentPolicyCache()
	r.Response.WriteJson(hostgf.Success().SetData(true))
	return &AdminSaveConfigRes{}, nil
}
func (*Admin) AdminStats(ctx context.Context, _ *AdminStatsReq) (*AdminStatsRes, error) {
	stats := map[string]int{}
	for _, name := range []string{"vod", "type", "user", "order", "article", "comment", "film_request"} {
		row, err := one(ctx, "SELECT COUNT(*) AS n FROM sx_"+name)
		if err != nil {
			return nil, fmt.Errorf("统计 %s: %w", name, err)
		}
		stats[name] = gconv.Int(row["n"])
	}
	g.RequestFromCtx(ctx).Response.WriteJson(hostgf.Success().SetData(stats))
	return &AdminStatsRes{}, nil
}
