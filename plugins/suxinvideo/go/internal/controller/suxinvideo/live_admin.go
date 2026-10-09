package suxinvideo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"github.com/suxinwl/GoSuxin/utility/gf"
)

type LiveGroupsReq struct {
	g.Meta `path:"/live/groups" method:"get"`
}
type LiveChannelsReq struct {
	g.Meta  `path:"/live/channels" method:"get"`
	GroupID int64  `p:"group_id"`
	Q       string `p:"q"`
	Page    int    `p:"page"`
	Size    int    `p:"size"`
}
type LiveStreamsReq struct {
	g.Meta    `path:"/live/streams" method:"get"`
	ChannelID int64 `p:"channel_id"`
}
type LiveSubscriptionsReq struct {
	g.Meta `path:"/live/subscriptions" method:"get"`
}
type LiveSaveReq struct {
	g.Meta `path:"/live/save" method:"post"`
	Kind   string         `p:"kind"`
	ID     int64          `p:"id"`
	Data   map[string]any `p:"data"`
}
type LiveDeleteReq struct {
	g.Meta `path:"/live/delete" method:"post"`
	Kind   string `p:"kind"`
	ID     int64  `p:"id"`
}
type LiveImportReq struct {
	g.Meta  `path:"/live/import" method:"post"`
	Content string `p:"content"`
	URL     string `p:"url"`
	Preview bool   `p:"preview"`
}
type LiveRefreshReq struct {
	g.Meta         `path:"/live/refresh" method:"post"`
	SubscriptionID int64 `p:"subscription_id"`
}
type LiveJobReq struct {
	g.Meta `path:"/live/job" method:"get"`
	ID     int64 `p:"id"`
}
type LiveProbeReq struct {
	g.Meta   `path:"/live/probe" method:"post"`
	StreamID int64 `p:"stream_id"`
}
type LiveAdminRes struct{}

func liveAdminAllowed(ctx context.Context, action string) (bool, error) {
	return requireResource(ctx, g.RequestFromCtx(ctx), "", "live/"+action)
}
func liveAdminWrite(ctx context.Context, data any, err error) (*LiveAdminRes, error) {
	r := g.RequestFromCtx(ctx)
	if err != nil {
		r.SetCtxVar("cms_denied", true)
		r.Response.WriteJson(gf.Failed().SetMsg(err.Error()))
	} else {
		r.Response.WriteJson(gf.Success().SetData(data))
	}
	return &LiveAdminRes{}, nil
}
func (*Admin) LiveGroups(ctx context.Context, _ *LiveGroupsReq) (*LiveAdminRes, error) {
	if ok, err := liveAdminAllowed(ctx, "groups"); !ok || err != nil {
		return &LiveAdminRes{}, err
	}
	rows, err := all(ctx, "SELECT id,name,sort,enabled,manual_edited FROM sx_live_group ORDER BY sort,id")
	return liveAdminWrite(ctx, row{"list": rows}, err)
}
func (*Admin) LiveChannels(ctx context.Context, req *LiveChannelsReq) (*LiveAdminRes, error) {
	if ok, err := liveAdminAllowed(ctx, "channels"); !ok || err != nil {
		return &LiveAdminRes{}, err
	}
	page, size := req.Page, req.Size
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 30
	}
	if size > 100 {
		size = 100
	}
	where := " WHERE 1=1"
	args := []any{}
	if req.GroupID > 0 {
		where += " AND c.group_id=?"
		args = append(args, req.GroupID)
	}
	if q := strings.TrimSpace(req.Q); q != "" {
		where += " AND (c.name LIKE ? OR c.tvg_id LIKE ?)"
		args = append(args, "%"+q+"%", "%"+q+"%")
	}
	n, err := one(ctx, "SELECT COUNT(*) n FROM sx_live_channel c"+where, args...)
	if err != nil {
		return nil, err
	}
	args = append(args, size, (page-1)*size)
	rows, err := all(ctx, `SELECT c.*,g.name group_name,(SELECT COUNT(*) FROM sx_live_stream s WHERE s.channel_id=c.id) stream_count,
 (SELECT COUNT(*) FROM sx_live_stream s WHERE s.channel_id=c.id AND s.enabled=1 AND s.health='healthy') healthy_count
 FROM sx_live_channel c LEFT JOIN sx_live_group g ON g.id=c.group_id`+where+" ORDER BY c.sort,c.id LIMIT ? OFFSET ?", args...)
	for _, r := range rows {
		aliases := []string{}
		_ = json.Unmarshal([]byte(gconv.String(r["aliases_json"])), &aliases)
		r["aliases"] = aliases
		delete(r, "aliases_json")
	}
	return liveAdminWrite(ctx, row{"list": rows, "total": gconv.Int(n["n"]), "page": page, "size": size}, err)
}
func (*Admin) LiveStreams(ctx context.Context, req *LiveStreamsReq) (*LiveAdminRes, error) {
	if ok, err := liveAdminAllowed(ctx, "streams"); !ok || err != nil {
		return &LiveAdminRes{}, err
	}
	rows, err := all(ctx, "SELECT s.*,(SELECT COUNT(*) FROM sx_live_stream ranked WHERE ranked.channel_id=s.channel_id AND ranked.id<=s.id) line_no FROM sx_live_stream s WHERE s.channel_id=? ORDER BY s.priority DESC,s.id", req.ChannelID)
	for _, r := range rows {
		r["name"] = liveStreamDisplayName(r)
		delete(r, "line_no")
		headers := map[string]string{}
		_ = json.Unmarshal([]byte(gconv.String(r["headers_json"])), &headers)
		r["headers"] = liveRedactHeaders(headers)
		delete(r, "headers_json")
		delete(r, "url_hash")
	}
	return liveAdminWrite(ctx, row{"list": rows}, err)
}
func (*Admin) LiveSubscriptions(ctx context.Context, _ *LiveSubscriptionsReq) (*LiveAdminRes, error) {
	if ok, err := liveAdminAllowed(ctx, "subscriptions"); !ok || err != nil {
		return &LiveAdminRes{}, err
	}
	rows, err := all(ctx, "SELECT id,name,url,enabled,last_refresh,last_error FROM sx_live_subscription ORDER BY id")
	for _, r := range rows {
		r["interval_hours"] = 6
	}
	return liveAdminWrite(ctx, row{"list": rows}, err)
}

func liveSaveData(ctx context.Context, kind string, id int64, input map[string]any) (int64, error) {
	allowed := map[string]map[string]bool{
		"group": fields("name sort enabled"), "channel": fields("tvg_id name logo group_id sort enabled aliases"),
		"stream": fields("channel_id name url headers priority quality enabled"), "subscription": fields("name url enabled"),
	}
	if id < 0 || allowed[kind] == nil {
		return 0, errors.New("直播管理参数无效")
	}
	table := "sx_live_" + kind
	var existing row
	var err error
	if id > 0 {
		existing, err = one(ctx, "SELECT * FROM "+table+" WHERE id=?", id)
		if err != nil {
			return 0, err
		}
		if existing == nil {
			return 0, errors.New("直播记录不存在")
		}
	}
	data := row{}
	for k, v := range input {
		if allowed[kind][k] {
			data[k] = v
		}
	}
	if existing != nil {
		for k := range allowed[kind] {
			if _, exists := data[k]; !exists {
				data[k] = existing[k]
			}
		}
	}
	name := strings.TrimSpace(gconv.String(data["name"]))
	limit := 120
	if kind == "channel" || kind == "stream" {
		limit = 200
	}
	if name == "" || len([]rune(name)) > limit {
		return 0, errors.New("名称为空或过长")
	}
	data["name"] = name
	if _, ok := data["enabled"]; !ok {
		data["enabled"] = 1
	} else {
		data["enabled"] = gconv.Int(gconv.Bool(data["enabled"]))
	}
	if kind == "channel" {
		groupID := gconv.Int64(data["group_id"])
		group, err := one(ctx, "SELECT id FROM sx_live_group WHERE id=?", groupID)
		if err != nil {
			return 0, err
		}
		if group == nil {
			return 0, errors.New("请选择直播分组")
		}
		tvgID := liveNormalizeTVGID(gconv.String(data["tvg_id"]))
		if len(tvgID) > 200 {
			return 0, errors.New("节目表标识过长")
		}
		data["tvg_id"] = tvgID
		logo := strings.TrimSpace(gconv.String(data["logo"]))
		if logo != "" && liveCheckImportURL(logo) != nil {
			return 0, errors.New("图标须为 HTTP/HTTPS 图片地址")
		}
		data["logo"] = logo
		aliases := []string{}
		if value, exists := input["aliases"]; exists {
			aliases = gconv.Strings(value)
		} else if existing != nil {
			_ = json.Unmarshal([]byte(gconv.String(existing["aliases_json"])), &aliases)
		}
		if len(aliases) > 20 {
			return 0, errors.New("频道别名最多 20 个")
		}
		for _, alias := range aliases {
			if len([]rune(alias)) > 120 {
				return 0, errors.New("频道别名过长")
			}
		}
		encoded, _ := json.Marshal(aliases)
		data["aliases_json"] = string(encoded)
		delete(data, "aliases")
		if id == 0 {
			data["identity_key"] = liveChannelIdentity(liveImportItem{TVGID: tvgID, Name: name})
		}
	}
	if kind == "group" && id == 0 {
		data["identity_key"] = liveGroupIdentity(name)
	}
	if kind == "stream" || kind == "subscription" {
		address := strings.TrimSpace(gconv.String(data["url"]))
		if err := liveCheckImportURL(address); err != nil {
			return 0, err
		}
		validate := safeCollectorURL
		if kind == "subscription" {
			validate = validateLiveSubscriptionURL
		}
		if err := validate(ctx, address); err != nil {
			return 0, errors.New("仅允许公网 HTTP/HTTPS 地址")
		}
		data["url"] = address
		data["url_hash"] = liveIdentity(address)
	}
	if kind == "stream" {
		channel, err := one(ctx, "SELECT id FROM sx_live_channel WHERE id=?", gconv.Int64(data["channel_id"]))
		if err != nil {
			return 0, err
		}
		if channel == nil {
			return 0, errors.New("请选择频道")
		}
		oldHeaders := map[string]string{}
		if existing != nil {
			_ = json.Unmarshal([]byte(gconv.String(existing["headers_json"])), &oldHeaders)
		}
		incoming := map[string]string{}
		if input["headers"] != nil {
			raw, _ := json.Marshal(input["headers"])
			if json.Unmarshal(raw, &incoming) != nil {
				return 0, errors.New("请求头必须是字符串对象")
			}
		}
		merged, err := liveMergeHeaders(oldHeaders, incoming)
		if err != nil {
			return 0, err
		}
		encoded, _ := json.Marshal(merged)
		data["headers_json"] = string(encoded)
		delete(data, "headers")
		priority := gconv.Int(data["priority"])
		if priority < 0 || priority > 10000 {
			return 0, errors.New("直播源优先级须为 0 至 10000")
		}
		data["priority"] = priority
		if id == 0 || gconv.String(existing["url"]) != gconv.String(data["url"]) || gconv.String(existing["headers_json"]) != string(encoded) {
			data["health"] = "pending"
			data["last_checked"] = 0
			data["last_error"] = ""
		}
		if len(gconv.String(data["quality"])) > 100 {
			return 0, errors.New("清晰度说明过长")
		}
	}
	if kind == "group" || kind == "channel" {
		sort := gconv.Int(data["sort"])
		if sort < -10000 || sort > 10000 {
			return 0, errors.New("排序数值无效")
		}
		data["sort"] = sort
	}
	if kind != "subscription" {
		data["manual_edited"] = 1
	}
	data["updated"] = time.Now().Unix()
	if id == 0 {
		data["created"] = time.Now().Unix()
	}
	return liveWriteRecord(ctx, table, id, data)
}

// The host model layer prefixes table names with gf_. CMS tables are explicit
// sx_* tables, so use parameterized SQL just as the existing CMS CRUD does.
func liveWriteRecord(ctx context.Context, table string, id int64, data row) (int64, error) {
	keys := make([]string, 0, len(data))
	for key := range data {
		if checkIdentifier(key) != nil {
			return 0, errors.New("直播配置字段无效")
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	args := []any{}
	columns := []string{}
	marks := []string{}
	assignments := []string{}
	for _, key := range keys {
		args = append(args, data[key])
		columns = append(columns, "`"+key+"`")
		marks = append(marks, "?")
		assignments = append(assignments, "`"+key+"`=?")
	}
	if id > 0 {
		if err := execSQL(ctx, "UPDATE "+table+" SET "+strings.Join(assignments, ",")+" WHERE id=?", append(args, id)...); err != nil {
			return 0, errors.New("保存失败，频道标识或直播地址可能重复")
		}
		return id, nil
	}
	result, err := g.DB().Exec(ctx, "INSERT INTO "+table+" ("+strings.Join(columns, ",")+") VALUES ("+strings.Join(marks, ",")+")", args...)
	if err != nil {
		return 0, errors.New("保存失败，频道标识或直播地址可能重复")
	}
	return result.LastInsertId()
}
func liveMergeHeaders(existing, input map[string]string) (map[string]string, error) {
	merged := map[string]string{}
	for k, v := range existing {
		merged[k] = v
	}
	for k, v := range input {
		k = canonicalLiveHeader(k)
		if liveSensitiveHeader(k) && (v == "" || strings.Contains(v, "••••")) {
			continue
		}
		merged[k] = v
	}
	return liveCleanHeaders(merged)
}
func canonicalLiveHeader(k string) string { return http.CanonicalHeaderKey(k) }

func (*Admin) LiveSave(ctx context.Context, req *LiveSaveReq) (*LiveAdminRes, error) {
	if ok, err := liveAdminAllowed(ctx, "save"); !ok || err != nil {
		return &LiveAdminRes{}, err
	}
	id, err := liveSaveData(ctx, req.Kind, req.ID, req.Data)
	if err != nil {
		return liveAdminWrite(ctx, nil, err)
	}
	data := row{"id": id}
	if req.Kind == "stream" {
		jobID, e := liveStartJob(ctx, "probe", func(ctx context.Context, p *liveProgress) error {
			p.Total = 1
			return liveProbeIDs(ctx, p, []int64{id})
		})
		if e == nil {
			data["job_id"] = jobID
		} else {
			data["message"] = "已保存；现有刷新任务结束后可点击检测，检测完成前不会公开新源"
		}
	}
	return liveAdminWrite(ctx, data, nil)
}
func (*Admin) LiveDelete(ctx context.Context, req *LiveDeleteReq) (*LiveAdminRes, error) {
	if ok, err := liveAdminAllowed(ctx, "delete"); !ok || err != nil {
		return &LiveAdminRes{}, err
	}
	if req.ID < 1 || !(req.Kind == "group" || req.Kind == "channel" || req.Kind == "stream" || req.Kind == "subscription") {
		return liveAdminWrite(ctx, nil, errors.New("删除参数无效"))
	}
	extra := ""
	if req.Kind != "subscription" {
		extra = ",manual_edited=1"
	}
	err := execSQL(ctx, "UPDATE sx_live_"+req.Kind+" SET enabled=0"+extra+",updated=? WHERE id=?", time.Now().Unix(), req.ID)
	return liveAdminWrite(ctx, row{"disabled": true, "message": "已停用并保留记录，订阅刷新不会恢复手动停用内容"}, err)
}
func (*Admin) LiveImport(ctx context.Context, req *LiveImportReq) (*LiveAdminRes, error) {
	if ok, err := liveAdminAllowed(ctx, "import"); !ok || err != nil {
		return &LiveAdminRes{}, err
	}
	if len(req.Content) > liveImportLimit {
		return liveAdminWrite(ctx, nil, errors.New("直播列表最多 16 MB"))
	}
	if req.Content == "" && req.URL == "" {
		return liveAdminWrite(ctx, nil, errors.New("请粘贴列表或填写订阅地址"))
	}
	if req.Preview {
		content := req.Content
		var err error
		if content == "" {
			content, err = liveFetchSubscription(ctx, req.URL)
		}
		if err != nil {
			return liveAdminWrite(ctx, nil, err)
		}
		_, preview, err := liveParseImport(content, "")
		return liveAdminWrite(ctx, preview, err)
	}
	id, err := liveStartJob(ctx, "import", func(ctx context.Context, p *liveProgress) error {
		content := req.Content
		var err error
		if content == "" {
			content, err = liveFetchSubscription(ctx, req.URL)
		}
		if err != nil {
			return err
		}
		items, _, err := liveParseImport(content, "")
		if err != nil {
			return err
		}
		return liveImportItems(ctx, p, items, 0)
	})
	return liveAdminWrite(ctx, row{"job_id": id, "status": "pending"}, err)
}
func (*Admin) LiveRefresh(ctx context.Context, req *LiveRefreshReq) (*LiveAdminRes, error) {
	if ok, err := liveAdminAllowed(ctx, "refresh"); !ok || err != nil {
		return &LiveAdminRes{}, err
	}
	id, err := liveStartJob(ctx, "refresh", func(ctx context.Context, p *liveProgress) error {
		return liveRefreshSubscriptions(ctx, p, req.SubscriptionID, true)
	})
	return liveAdminWrite(ctx, row{"job_id": id, "status": "pending"}, err)
}
func (*Admin) LiveJob(ctx context.Context, req *LiveJobReq) (*LiveAdminRes, error) {
	if ok, err := liveAdminAllowed(ctx, "job"); !ok || err != nil {
		return &LiveAdminRes{}, err
	}
	query := "SELECT id,status,kind,total,processed,added,updated_count AS updated,failed,message,created,updated AS updated_at FROM sx_live_job"
	args := []any{}
	if req.ID > 0 {
		query += " WHERE id=?"
		args = append(args, req.ID)
	}
	record, err := one(ctx, query+" ORDER BY id DESC LIMIT 1", args...)
	return liveAdminWrite(ctx, record, err)
}
func (*Admin) LiveProbe(ctx context.Context, req *LiveProbeReq) (*LiveAdminRes, error) {
	if ok, err := liveAdminAllowed(ctx, "probe"); !ok || err != nil {
		return &LiveAdminRes{}, err
	}
	stream, err := liveStream(ctx, req.StreamID)
	if err != nil {
		return liveAdminWrite(ctx, nil, err)
	}
	checkCtx, cancel := context.WithTimeout(ctx, 24*time.Second)
	defer cancel()
	probe, err := liveProbeStream(checkCtx, stream)
	message := ""
	if err != nil {
		message = "当前无法确认该源可播放，已保留供后续重试"
	}
	_ = liveSetHealth(ctx, stream.ID, err == nil, message)
	return liveAdminWrite(ctx, row{"healthy": err == nil, "latency_ms": probe.LatencyMS, "quality": probe.Quality, "message": message}, nil)
}
