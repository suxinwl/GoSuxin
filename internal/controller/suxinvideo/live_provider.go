package suxinvideo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type LiveViewer struct {
	MemberID, AppSessionID, SubscriptionID, ProfileID int64
	WebSession                                        bool
	SubscriptionHash                                  string
}
type liveViewerKey struct{}

func liveContextWithViewer(ctx context.Context, v LiveViewer) context.Context {
	return context.WithValue(ctx, liveViewerKey{}, v)
}
func liveViewerFromContext(ctx context.Context) LiveViewer {
	v, _ := ctx.Value(liveViewerKey{}).(LiveViewer)
	return v
}
func liveOptionalAuth(r *ghttp.Request) error {
	if err := AppAuthenticateRequest(r); err != nil {
		return err
	}
	user, err := currentUser(r.Context())
	if err != nil {
		return err
	}
	v := LiveViewer{}
	if user != nil {
		v.MemberID = gconv.Int64(user["id"])
		if p, ok := AppPrincipalFromContext(r.Context()); ok {
			v.AppSessionID = p.SessionID
		} else {
			v.WebSession = true
		}
	}
	r.SetCtx(liveContextWithViewer(r.Context(), v))
	return nil
}

// Called for every protected media request, including subscriptions and renewals.
func liveValidateViewer(ctx context.Context, v LiveViewer) error {
	if v.MemberID > 0 {
		r, err := one(ctx, "SELECT id FROM sx_user WHERE id=? AND status=1", v.MemberID)
		if err != nil {
			return err
		}
		if r == nil {
			return appError(403, "直播会员授权已撤销")
		}
	}
	if v.AppSessionID > 0 {
		r, err := one(ctx, "SELECT id FROM sx_app_session WHERE id=? AND user_id=? AND revoked=0 AND access_expire>?", v.AppSessionID, v.MemberID, time.Now().Unix())
		if err != nil {
			return err
		}
		if r == nil {
			return appError(401, "直播登录会话已失效")
		}
	}
	if v.WebSession {
		r := g.RequestFromCtx(ctx)
		if r == nil {
			return appError(401, "请先登录")
		}
		id, _ := r.Session.Get("sx_user_id")
		if id == nil || id.Int64() != v.MemberID {
			return appError(401, "直播登录会话已失效")
		}
	}
	if v.SubscriptionID > 0 {
		r, err := one(ctx, "SELECT id FROM sx_live_distribution WHERE id=? AND member_id=? AND profile_id=? AND token_hash=? AND enabled=1 AND (expires_at=0 OR expires_at>?)", v.SubscriptionID, v.MemberID, v.ProfileID, v.SubscriptionHash, time.Now().Unix())
		if err != nil {
			return err
		}
		if r == nil {
			return appError(403, "直播订阅已撤销或过期")
		}
	}
	if v.ProfileID > 0 {
		r, err := one(ctx, "SELECT id FROM sx_live_profile WHERE id=? AND enabled=1", v.ProfileID)
		if err != nil {
			return err
		}
		if r == nil {
			return appError(403, "直播分发配置已停用")
		}
	}
	return nil
}

func liveStreamAccessWhere(ctx context.Context) (string, []any, error) {
	v := liveViewerFromContext(ctx)
	condition := `(s.source_kind<>'provider' OR (EXISTS(SELECT 1 FROM sx_live_module lm WHERE lm.provider_key=s.provider_key AND lm.module_key=s.module_key AND lm.enabled=1)
 AND ((s.provider_key='public' AND s.access_level='public') OR (s.provider_key='member' AND ? > 0 AND EXISTS(SELECT 1 FROM sx_live_member_access la JOIN sx_user lu ON lu.id=la.member_id WHERE la.member_id=? AND la.module_key=s.module_key AND la.enabled=1 AND lu.status=1)))))`
	args := []any{v.MemberID, v.MemberID}
	if v.ProfileID > 0 {
		p, err := one(ctx, "SELECT modules_json,channel_ids_json FROM sx_live_profile WHERE id=? AND enabled=1", v.ProfileID)
		if err != nil {
			return "", nil, err
		}
		if p == nil {
			return "", nil, appError(403, "直播分发配置已停用")
		}
		var modules []string
		if err = json.Unmarshal([]byte(gconv.String(p["modules_json"])), &modules); err != nil {
			return "", nil, errors.New("直播分发配置无效")
		}
		if len(modules) > 0 {
			condition += " AND (s.source_kind='provider' AND CONCAT(s.provider_key,':',s.module_key) IN (" + strings.TrimSuffix(strings.Repeat("?,", len(modules)), ",") + ")"
			for _, m := range modules {
				args = append(args, m)
			}
			condition += ")"
		}
		var channelIDs []int64
		raw := gconv.String(p["channel_ids_json"])
		if raw != "" {
			if err = json.Unmarshal([]byte(raw), &channelIDs); err != nil {
				return "", nil, errors.New("直播频道配置无效")
			}
		}
		if len(channelIDs) > 0 {
			condition += " AND s.channel_id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(channelIDs)), ",") + ")"
			for _, id := range channelIDs {
				args = append(args, id)
			}
		}
	}
	return condition, args, nil
}

func liveStreamAccess(ctx context.Context, s LiveStream, v LiveViewer) error {
	if err := liveValidateViewer(ctx, v); err != nil {
		return err
	}
	clause, args, err := liveStreamAccessWhere(liveContextWithViewer(ctx, v))
	if err != nil {
		return err
	}
	args = append([]any{s.ID}, args...)
	r, err := one(ctx, "SELECT s.id FROM sx_live_stream s WHERE s.id=? AND s.enabled=1 AND "+clause, args...)
	if err != nil {
		return err
	}
	if r == nil {
		if v.MemberID == 0 && (s.AccessLevel == "member" || s.ProviderKey == "member") {
			return appError(401, "此直播线路需要登录并由管理员授权")
		}
		return appError(403, "此直播线路未获授权或已停用")
	}
	return nil
}

type liveProviderChannel struct {
	ModuleKey       string  `json:"provider_key"`
	Ref             string  `json:"provider_ref"`
	Name            string  `json:"name"`
	Group           string  `json:"group"`
	Logo            string  `json:"logo"`
	TVGID           string  `json:"tvg_id"`
	EpgID           string  `json:"epg_id"`
	Format          string  `json:"format"`
	Catchup         bool    `json:"catchup"`
	AccountRequired bool    `json:"account_required"`
	Mode            string  `json:"mode"`
	Kind            string  `json:"kind"`
	EventID         any     `json:"programme_id"`
	Title           string  `json:"title"`
	Start           string  `json:"start"`
	End             string  `json:"end"`
	DurationSeconds float64 `json:"duration_seconds"`
}
type liveProviderCatalog struct {
	OK        bool                  `json:"ok"`
	Profile   string                `json:"profile"`
	UpdatedAt any                   `json:"updated_at"`
	Channels  []liveProviderChannel `json:"channels"`
}
type liveProviderResolution struct {
	OK              bool              `json:"ok"`
	URL             string            `json:"url"`
	Headers         map[string]string `json:"headers"`
	Format          string            `json:"format"`
	Mime            string            `json:"mime"`
	Expires         string            `json:"expires_at"`
	Catchup         bool              `json:"catchup"`
	Mode            string            `json:"mode"`
	EventID         any               `json:"programme_id"`
	DurationSeconds float64           `json:"duration_seconds"`
}

func liveProviderValidKey(key string) bool { return key == "public" || key == "member" }

var liveProviderModulePattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,100}$`)

var liveProviderTransport http.RoundTripper = &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 4 * time.Second}).DialContext, ResponseHeaderTimeout: 30 * time.Second}

func liveProviderCall(ctx context.Context, key, method, path string, body any, out any) error {
	data, err := liveProviderRead(ctx, key, method, path, body, 8<<20)
	if err != nil {
		return err
	}
	if json.Unmarshal(data, out) != nil {
		return errors.New("直播引擎响应无效")
	}
	return nil
}
func liveProviderRead(ctx context.Context, key, method, path string, body any, limit int64) ([]byte, error) {
	if !liveProviderValidKey(key) {
		return nil, errors.New("直播引擎标识无效")
	}
	allowed := map[string]string{"/internal/health": "GET", "/internal/catalog": "GET", "/internal/modules": "GET", "/internal/epg": "GET", "/internal/resolve": "POST", "/internal/sync": "POST", "/internal/modules/config": "POST", "/internal/modules/login": "POST"}
	if allowed[path] != method {
		return nil, errors.New("直播引擎接口无效")
	}
	origin := liveProviderOrigin(key)
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() || u.User != nil || u.Path != "" {
		return nil, errors.New("直播引擎必须使用固定本机地址")
	}
	secret := liveEngineSecret()
	if len(secret) < 32 {
		return nil, errors.New("直播引擎内部授权未初始化")
	}
	var payload []byte
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, origin+path, bytes.NewReader(payload))
	if err != nil {
		return nil, errors.New("直播引擎请求无效")
	}
	req.Header.Set("X-IPTV-Secret", secret)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 35 * time.Second, Transport: liveProviderTransport, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("内部接口不允许重定向") }}
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("直播引擎尚未就绪或连接失败")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("直播引擎响应过大或读取失败")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("直播引擎操作失败（HTTP %d）", response.StatusCode)
	}
	return data, nil
}

func liveMaterializeStream(ctx context.Context, s LiveStream, start, end int64) (LiveStream, error) {
	if s.SourceMode == "event_replay" {
		start, end = 0, 0
	}
	if s.SourceKind != "provider" {
		if s.MediaType == "" {
			s.MediaType = "hls"
		}
		return s, nil
	}
	if !liveProviderValidKey(s.ProviderKey) || !liveProviderModulePattern.MatchString(s.ModuleKey) || s.ProviderRef == "" {
		return s, errors.New("直播动态来源配置无效")
	}
	payload := row{"provider_key": s.ModuleKey, "provider_ref": s.ProviderRef}
	if s.MediaType != "" {
		payload["format"] = s.MediaType
	}
	if start > 0 {
		if end <= start || end > time.Now().Unix() {
			return s, appError(400, "回看节目时间无效")
		}
		payload["start"] = time.Unix(start, 0).UTC().Format(time.RFC3339)
		payload["end"] = time.Unix(end, 0).UTC().Format(time.RFC3339)
	}
	var resolution liveProviderResolution
	if err := liveProviderCall(ctx, s.ProviderKey, "POST", "/internal/resolve", payload, &resolution); err != nil {
		return s, err
	}
	u, err := url.Parse(resolution.URL)
	origin, _ := url.Parse(liveProviderOrigin(s.ProviderKey))
	if err != nil || u.Scheme != origin.Scheme || u.Host != origin.Host || u.User != nil || !strings.HasPrefix(u.Path, "/internal/media/") {
		return s, errors.New("直播引擎返回了未授权媒体地址")
	}
	if start > 0 && !resolution.Catchup {
		return s, appError(404, "此线路未提供该节目回看")
	}
	if resolution.Format != "hls" && resolution.Format != "flv" && resolution.Format != "mp4" {
		return s, errors.New("直播引擎媒体格式不支持")
	}
	s.URL = resolution.URL
	s.MediaType = resolution.Format
	if resolution.Mode == "event_replay" {
		s.SourceMode = resolution.Mode
	}
	if resolution.DurationSeconds > 0 && resolution.DurationSeconds <= 86400 {
		s.DurationMS = int64(resolution.DurationSeconds * 1000)
	}
	s.ExpiresAt = time.Now().Add(15 * time.Minute).Unix()
	if expiry, e := time.Parse(time.RFC3339, resolution.Expires); e == nil {
		s.ExpiresAt = expiry.Unix()
	}
	if s.ExpiresAt <= time.Now().Unix() {
		return s, errors.New("直播媒体地址已过期")
	}
	// Runtime engine headers never replace administrator settings in the database.
	for k, v := range resolution.Headers {
		if strings.ContainsAny(k+v, "\r\n") {
			return s, errors.New("直播媒体请求头无效")
		}
	}
	return s, nil
}

func liveProviderAccountConfigured(ctx context.Context) bool {
	r, e := one(ctx, "SELECT COUNT(*) n FROM sx_live_module WHERE provider_key='member' AND enabled=1")
	return e == nil && gconv.Int(r["n"]) > 0
}

func liveCanonicalProviderID(tvgID, name string) string {
	tvgID = liveNormalizeTVGID(tvgID)
	if _, ok := liveOfficialChineseNames[strings.ToLower(tvgID)]; ok {
		return tvgID
	}
	if regexp.MustCompile(`(?i)^CCTV[-\s]?4(?:[^0-9].*)?(欧洲|美洲|亚洲|EUROPE|AMERICA|ASIA)`).MatchString(name) {
		return ""
	}
	key := liveImportNameKey(name)
	for id, n := range liveOfficialChineseNames {
		if liveImportNameKey(n) == key {
			return id
		}
	}
	aliases := map[string]string{"凤凰中文": "PhoenixChineseChannel.hk", "凤凰中文台": "PhoenixChineseChannel.hk", "凤凰资讯": "PhoenixInfoNewsChannel.hk", "凤凰资讯台": "PhoenixInfoNewsChannel.hk", "凤凰香港": "PhoenixHongKongChannel.hk", "凤凰香港台": "PhoenixHongKongChannel.hk", "翡翠台": "Jade.hk"}
	if id := aliases[key]; id != "" {
		return id
	}
	m := regexp.MustCompile(`(?i)^CCTV[-\s]?(\d{1,2})(\+|PLUS)?(?:[\s-]|综合|财经|综艺|体育|电影|军事|电视剧|纪录|科教|戏曲|社会|新闻|少儿|音乐|奥林匹克|农业|高清|HD|$)`).FindStringSubmatch(name)
	if len(m) > 0 {
		suffix := ""
		if m[2] != "" {
			suffix = "plus"
		}
		id := "CCTV" + m[1] + suffix + ".cn"
		if _, ok := liveOfficialChineseNames[strings.ToLower(id)]; ok {
			return id
		}
	}
	// Preserve the original EPG identifier separately. Unknown IDs must not prevent
	// exact names and administrator aliases from finding an existing station.
	return ""
}

func liveSyncProvider(ctx context.Context, p *liveProgress, key string) error {
	return liveSyncProviderData(ctx, p, key, true)
}

func liveSyncProviderData(ctx context.Context, p *liveProgress, key string, probe bool) (syncErr error) {
	defer func() {
		if syncErr != nil {
			_ = execSQL(context.Background(), "UPDATE sx_live_module SET status='error',last_error='来源同步失败；已有目录和节目单保留',updated=? WHERE provider_key=? AND enabled=1", time.Now().Unix(), key)
		}
	}()
	var catalog liveProviderCatalog
	if err := liveProviderCall(ctx, key, "GET", "/internal/catalog", nil, &catalog); err != nil {
		return err
	}
	if !catalog.OK {
		return errors.New("直播目录尚未就绪")
	}
	p.Total += len(catalog.Channels)
	_ = p.persist(ctx)
	modules, err := all(ctx, "SELECT module_key,enabled,source_revision FROM sx_live_module WHERE provider_key=?", key)
	if err != nil {
		return err
	}
	enabled := map[string]row{}
	for _, m := range modules {
		if gconv.Bool(m["enabled"]) {
			enabled[gconv.String(m["module_key"])] = m
		}
	}
	ids := []int64{}
	for _, c := range catalog.Channels {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		p.Processed++
		module := enabled[c.ModuleKey]
		if module == nil {
			continue
		}
		if c.Ref == "" || len(c.Ref) > 500 || !liveProviderModulePattern.MatchString(c.ModuleKey) {
			p.Failed++
			continue
		}
		identity := liveIdentity("provider:" + key + ":" + c.ModuleKey + ":" + c.Ref)
		// Use the immutable reference as the import identity, never the signed URL.
		binding, _ := one(ctx, "SELECT channel_id FROM sx_live_provider_binding WHERE provider_key=? AND module_key=? AND provider_ref=?", key, c.ModuleKey, c.Ref)
		importItem := liveImportItem{TVGID: liveCanonicalProviderID(c.TVGID, c.Name), Name: c.Name, Logo: liveProviderPublicLogo(c.Logo), Group: c.Group, URL: "provider:" + identity, Headers: map[string]string{}}
		if binding != nil {
			bound, e := one(ctx, "SELECT tvg_id,name FROM sx_live_channel WHERE id=?", binding["channel_id"])
			if e != nil {
				return e
			}
			if bound != nil {
				importItem.TVGID = gconv.String(bound["tvg_id"])
				importItem.Name = gconv.String(bound["name"])
			}
		}
		var id int64
		var created bool
		var e error
		if binding != nil {
			id, created, e = liveUpsertBoundProviderStream(ctx, gconv.Int64(binding["channel_id"]), key, c, identity)
		} else {
			id, created, e = liveUpsertImport(ctx, importItem, 0)
		}
		if e != nil {
			p.Failed++
			continue
		}
		access := "public"
		if key == "member" || c.AccountRequired {
			access = "member"
		}
		checked := int64(0)
		if probe {
			checked = time.Now().Unix()
		}
		e = execSQL(ctx, `UPDATE sx_live_stream SET source_kind='provider',provider_key=?,module_key=?,provider_ref=?,access_level=?,source_revision=?,media_type=?,epg_id=?,catchup_enabled=?,url='',last_checked=IF(last_checked=0,?,last_checked),updated=? WHERE id=?`, key, c.ModuleKey, c.Ref, access, module["source_revision"], c.Format, c.EpgID, c.Catchup, checked, time.Now().Unix(), id)
		if e != nil {
			p.Failed++
			continue
		}
		ids = append(ids, id)
		mode := "live"
		if c.Mode == "event_replay" {
			mode = "event_replay"
		}
		duration := int64(0)
		if c.DurationSeconds > 0 && c.DurationSeconds <= 86400 {
			duration = int64(c.DurationSeconds * 1000)
		}
		if e = execSQL(ctx, "UPDATE sx_live_stream SET source_mode=?,event_id=?,duration_ms=? WHERE id=?", mode, cutRunes(gconv.String(c.EventID), 200), duration, id); e != nil {
			return e
		}
		if mode == "event_replay" {
			if e = liveUpsertEventProgramme(ctx, key, c, id); e != nil {
				return e
			}
		}
		if created {
			p.Added++
		} else {
			p.Updated++
		}
		p.Message = fmt.Sprintf("同步直播目录：%d / %d", p.Processed, p.Total)
		_ = p.persist(ctx)
	}
	if err = liveImportProviderEPG(ctx, key); err != nil {
		p.Message = "频道目录已同步，节目单暂未更新"
		return errors.New("频道目录已保存；节目单同步失败，已有节目单保留")
	}
	if err = execSQL(ctx, "UPDATE sx_live_module SET last_sync=?,status='ready',last_error='',updated=? WHERE provider_key=? AND enabled=1", time.Now().Unix(), time.Now().Unix(), key); err != nil {
		return err
	}
	if probe {
		p.Total += len(ids)
		return liveProbeIDs(ctx, p, ids)
	}
	return nil
}

// Pull the engine's current catalogue into Go independently of the Node refresh
// timer. A failed attempt keeps last_sync and retries after a short cooldown.
func liveDueProviders(ctx context.Context, now int64) ([]row, error) {
	return all(ctx, `SELECT provider_key,MIN(last_sync) last_sync FROM sx_live_module WHERE enabled=1 AND provider_key IN ('public','member') AND last_sync<=? AND (last_error='' OR updated<=?) GROUP BY provider_key ORDER BY last_sync,provider_key`, now-int64((6*time.Hour).Seconds()), now-int64((15*time.Minute).Seconds()))
}

func liveProviderPull(ctx context.Context, p *liveProgress, key string) error {
	var health row
	if err := liveProviderCall(ctx, key, "GET", "/internal/health", nil, &health); err != nil {
		_ = execSQL(context.Background(), "UPDATE sx_live_module SET status='error',last_error='直播引擎暂未就绪；已有目录和节目单保留',updated=? WHERE provider_key=? AND enabled=1", time.Now().Unix(), key)
		return err
	}
	if !gconv.Bool(health["ready"]) || gconv.Bool(health["refreshing"]) {
		_ = execSQL(context.Background(), "UPDATE sx_live_module SET last_error='直播引擎正在刷新；已有目录和节目单保留',updated=? WHERE provider_key=? AND enabled=1", time.Now().Unix(), key)
		return errors.New("直播引擎正在刷新；稍后重试目录同步")
	}
	// Media health checks use the existing four-worker sweep. The catalogue pull
	// itself remains short and does not rewrite account settings or refresh rates.
	return liveSyncProviderData(ctx, p, key, false)
}

func liveScheduleProviderSync(ctx context.Context) (bool, error) {
	providers, err := liveDueProviders(ctx, time.Now().Unix())
	if err != nil || len(providers) == 0 {
		return false, err
	}
	key := gconv.String(providers[0]["provider_key"])
	_, err = liveStartJob(ctx, "provider_catalog_sync", func(ctx context.Context, p *liveProgress) error { return liveProviderPull(ctx, p, key) })
	return err == nil, err
}

func liveProviderDefaultModule(key, module string) bool {
	return key == "public" && (module == "migu" || module == "yangshipin" || module == "fengshows")
}

func liveUpsertBoundProviderStream(ctx context.Context, channelID int64, key string, c liveProviderChannel, identity string) (int64, bool, error) {
	hash := liveIdentity("provider:" + identity)
	existing, e := one(ctx, "SELECT id FROM sx_live_stream WHERE url_hash=? ORDER BY id LIMIT 1", hash)
	if e != nil {
		return 0, false, e
	}
	now := time.Now().Unix()
	if existing != nil {
		id := gconv.Int64(existing["id"])
		e = execSQL(ctx, "UPDATE sx_live_stream SET channel_id=?,updated=? WHERE id=?", channelID, now, id)
		return id, false, e
	}
	e = execSQL(ctx, "INSERT INTO sx_live_stream(channel_id,name,url,url_hash,headers_json,created,updated) VALUES(?,?,'',?,'{}',?,?)", channelID, c.Name, hash, now, now)
	if e != nil {
		return 0, false, e
	}
	r, e := one(ctx, "SELECT id FROM sx_live_stream WHERE channel_id=? AND url_hash=?", channelID, hash)
	if e != nil {
		return 0, false, e
	}
	return gconv.Int64(r["id"]), true, nil
}
func liveDiscoverProviderModules(ctx context.Context, key string) error {
	var native row
	if err := liveProviderCall(ctx, key, "GET", "/internal/modules", nil, &native); err != nil {
		return err
	}
	for _, m := range gconv.Maps(native["modules"]) {
		module := gconv.String(m["id"])
		if !liveProviderModulePattern.MatchString(module) {
			continue
		}
		now := time.Now().Unix()
		if err := execSQL(ctx, `INSERT IGNORE INTO sx_live_module(provider_key,module_key,name,enabled,config_json,secrets_cipher,created,updated) VALUES(?,?,?,?,'{}','',?,?)`, key, module, m["name"], gconv.Bool(m["enabled"]) && key == "public", now, now); err != nil {
			return err
		}
	}
	return nil
}
func liveProviderRefresh(ctx context.Context, p *liveProgress, key string) error {
	if key == "member" && !liveProviderAccountConfigured(ctx) {
		return appError(409, "账号来源尚未启用，请先配置或登录一个会员来源模块")
	}
	if err := liveWaitProvider(ctx, key, 20*time.Second); err != nil {
		return err
	}
	if err := liveApplyProviderConfig(ctx, key); err != nil {
		return err
	}
	var response row
	if err := liveProviderCall(ctx, key, "POST", "/internal/sync", row{}, &response); err != nil {
		return err
	}
	for i := 0; i < 120; i++ {
		var health row
		if err := liveProviderCall(ctx, key, "GET", "/internal/health", nil, &health); err != nil {
			return err
		}
		if !gconv.Bool(health["refreshing"]) {
			return liveSyncProvider(ctx, p, key)
		}
		p.Message = "直播引擎正在刷新频道目录"
		_ = p.persist(ctx)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return errors.New("直播引擎刷新超时；现有频道保留")
}
func liveInitializeProvider(ctx context.Context, key string) error {
	if err := liveDiscoverProviderModules(ctx, key); err != nil {
		return err
	}
	_, err := liveStartJob(ctx, "provider_sync", func(ctx context.Context, p *liveProgress) error { return liveProviderRefresh(ctx, p, key) })
	return err
}

func liveProviderPublicLogo(raw string) string {
	u, e := url.Parse(raw)
	if e != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || net.ParseIP(u.Hostname()) != nil {
		return ""
	}
	return raw
}
