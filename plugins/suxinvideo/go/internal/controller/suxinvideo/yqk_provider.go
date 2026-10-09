package suxinvideo

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	xq "github.com/suxinwl/GoSuxin/internal/xiaoqiapp"
	"github.com/suxinwl/GoSuxin/internal/yqksign"
)

const yqkSourceURL = "yqk://app"
const yqkDefaultBootstrapURL = "https://59.36.165.33:8976/down/l7e07KwFjWE7.json"
const yqkStatePath = "data/suxinvideo-provider/yqk-runtime.json"

func isYQKSource(raw string) bool      { return strings.EqualFold(strings.TrimSpace(raw), yqkSourceURL) }
func yqkPlayerCode(kind string) string { return "yqk_" + kind }

func yqkMarkerParts(marker string) (vodID, kind, epID string, err error) {
	u, e := url.Parse(strings.TrimSpace(marker))
	if e != nil || u.Scheme != "yqk" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Port() != "" {
		return "", "", "", errors.New("小柒播放标识无效")
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) != 2 || !yqkPositiveID(u.Host) || !yqkPositiveID(parts[0]) || !yqkPositiveID(parts[1]) {
		return "", "", "", errors.New("小柒播放标识无效")
	}
	return u.Host, parts[0], parts[1], nil
}

func yqkPositiveID(value string) bool {
	if value == "" || len(value) > 19 || value[0] == '0' {
		return false
	}
	n, e := strconv.ParseInt(value, 10, 64)
	return e == nil && n > 0 && strconv.FormatInt(n, 10) == value
}

type yqkCursor struct {
	Value string `json:"value"`
	Until int64  `json:"until"`
}
type yqkDiskState struct {
	AnonymousID string               `json:"anonymous_id"`
	Bootstrap   string               `json:"bootstrap"`
	SDK         yqksign.ClientState  `json:"sdk"`
	Cursors     map[string]yqkCursor `json:"cursors,omitempty"`
}
type yqkCacheEntry struct {
	Data  json.RawMessage
	Until time.Time
}

var yqkRuntime = struct {
	sync.Mutex
	loaded      bool
	disk        yqkDiskState
	configURL   string
	configList  []string
	configUntil time.Time
	cache       map[string]yqkCacheEntry
}{cache: map[string]yqkCacheEntry{}}

// These independent client leases prevent collection from holding the SDK's
// serial client lock while viewers resolve episodes. Acquisition is cancellable.
var yqkCollectSlots = make(chan struct{}, 2)
var yqkPlaybackSlots = make(chan struct{}, 3)

func yqkLoadLocked() error {
	if yqkRuntime.loaded {
		return nil
	}
	if b, e := os.ReadFile(yqkStatePath); e == nil {
		_ = json.Unmarshal(b, &yqkRuntime.disk)
	}
	if len(yqkRuntime.disk.AnonymousID) != 36 || !strings.HasPrefix(yqkRuntime.disk.AnonymousID, "cms-") {
		var random [16]byte
		if _, e := rand.Read(random[:]); e != nil {
			return errors.New("小柒匿名访问标识创建失败")
		}
		yqkRuntime.disk.AnonymousID = "cms-" + hex.EncodeToString(random[:])
	}
	if yqkRuntime.disk.Cursors == nil {
		yqkRuntime.disk.Cursors = map[string]yqkCursor{}
	}
	if e := yqkSaveLocked(); e != nil {
		return e
	}
	yqkRuntime.loaded = true
	return nil
}

func yqkSaveLocked() error {
	if e := os.MkdirAll(filepath.Dir(yqkStatePath), 0700); e != nil {
		return errors.New("小柒缓存目录不可写")
	}
	for key, value := range yqkRuntime.disk.Cursors {
		if value.Until <= time.Now().Unix() {
			delete(yqkRuntime.disk.Cursors, key)
		}
	}
	b, e := json.Marshal(yqkRuntime.disk)
	if e != nil {
		return errors.New("小柒运行状态保存失败")
	}
	// Distinct temp names also avoid a diagnostics process truncating an
	// active service's write. Runtime identity belongs to the service install.
	temp, e := os.CreateTemp(filepath.Dir(yqkStatePath), "yqk-runtime-*.tmp")
	if e != nil {
		return errors.New("小柒运行状态保存失败")
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	_, writeErr := temp.Write(b)
	closeErr := temp.Close()
	if writeErr != nil || closeErr != nil || os.Rename(tempPath, yqkStatePath) != nil {
		return errors.New("小柒运行状态保存失败")
	}
	return nil
}

func yqkAnonymousCommon(anonymous string) map[string]any {
	return map[string]any{"appId": yqksign.AppID, "bundlerId": "com.novorixalabs.ts", "cus1tom": "cus3tom", "deviceInfo": "GoCMS", "osInfo": "13", "otherParam": "0", "patchNumber": 0, "source": "1001_default", "udid": anonymous, "version": "1.3.64", "versionCode": 1107}
}

// Validate initial and redirected requests at the transport boundary too. SDK
// Restore/config documents may introduce new origins; none may reach private IPs.
type yqkGuardTransport struct{ base http.RoundTripper }

func (t yqkGuardTransport) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}
func (t yqkGuardTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" || safeCollectorURL(req.Context(), req.URL.String()) != nil {
		return nil, errors.New("小柒接口地址不安全或不可达")
	}
	return t.base.RoundTrip(req)
}

func yqkHTTPClient() *http.Client {
	client := safeCollectorHTTPClient(12 * time.Second)
	client.Transport = yqkGuardTransport{base: client.Transport}
	return client
}

func yqkBootstrap(ctx context.Context, address string, client *http.Client) ([]string, error) {
	yqkRuntime.Lock()
	if yqkRuntime.configURL == address && time.Now().Before(yqkRuntime.configUntil) {
		out := append([]string(nil), yqkRuntime.configList...)
		yqkRuntime.Unlock()
		return out, nil
	}
	yqkRuntime.Unlock()
	u, e := url.Parse(address)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("小柒配置入口必须是有效 HTTPS 地址")
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if e != nil {
		return nil, errors.New("小柒配置入口无效")
	}
	req.Header.Set("Accept", "application/json")
	response, e := client.Do(req)
	if e != nil {
		return nil, errors.New("小柒配置入口连接失败或超时")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("小柒配置入口返回 HTTP %d", response.StatusCode)
	}
	body, e := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if e != nil || len(body) > 64<<10 {
		return nil, errors.New("小柒配置入口响应过大或读取失败")
	}
	var entries []string
	if json.Unmarshal(body, &entries) != nil || len(entries) == 0 || len(entries) > 16 {
		return nil, errors.New("小柒配置入口须返回 HTTPS 地址数组")
	}
	for _, entry := range entries {
		u, e := url.Parse(entry)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
			return nil, errors.New("小柒配置入口包含无效地址")
		}
	}
	yqkRuntime.Lock()
	yqkRuntime.configURL, yqkRuntime.configList, yqkRuntime.configUntil = address, append([]string(nil), entries...), time.Now().Add(15*time.Minute)
	yqkRuntime.Unlock()
	return entries, nil
}

func withYQK(ctx context.Context, playback bool, work func(context.Context, *yqksign.Client, string) error) error {
	slots, limit := yqkCollectSlots, 55*time.Second
	if playback {
		slots, limit = yqkPlaybackSlots, 35*time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	select {
	case slots <- struct{}{}:
	case <-ctx.Done():
		return errors.New("小柒请求繁忙或已取消")
	}
	defer func() { <-slots }()
	address := strings.TrimSpace(setting(ctx, "yqk_bootstrap_url", yqkDefaultBootstrapURL))
	client := yqkHTTPClient()
	defer client.CloseIdleConnections()
	yqkRuntime.Lock()
	e := yqkLoadLocked()
	identity, state := yqkRuntime.disk.AnonymousID, yqkRuntime.disk.SDK
	if yqkRuntime.disk.Bootstrap != address {
		state = yqksign.ClientState{}
	}
	yqkRuntime.Unlock()
	if e != nil {
		return e
	}
	entries, e := yqkBootstrap(ctx, address, client)
	if e != nil {
		// A temporary outer-document outage must not discard a previously
		// validated dynamic configuration. Every restored request still passes
		// the same guarded transport and SDK discovery/failover limits.
		if len(state.ConfigURLs) == 0 {
			return e
		}
		entries = append([]string(nil), state.ConfigURLs...)
	}
	sdk, e := yqksign.NewClient(yqksign.ClientOptions{BootstrapConfigURLs: entries, Common: yqkAnonymousCommon(identity), HTTPClient: client, MaxAttempts: 8, MaxResponseBytes: 4 << 20})
	if e != nil {
		return errors.New("小柒接口客户端初始化失败")
	}
	if state.ActiveBackend != "" {
		_ = sdk.Restore(state)
	}
	e = work(ctx, sdk, address)
	state = sdk.Snapshot()
	if state.ActiveBackend != "" {
		yqkRuntime.Lock()
		yqkRuntime.disk.SDK, yqkRuntime.disk.Bootstrap = state, address
		if saveErr := yqkSaveLocked(); saveErr != nil {
			g.Log().Warning(ctx, "CMS YQK runtime state: ", saveErr)
		}
		yqkRuntime.Unlock()
	}
	return e
}

func yqkAPIError(err error) error {
	if err == nil {
		return nil
	}
	var business *yqksign.BusinessError
	if errors.As(err, &business) {
		message := strings.ToLower(business.Response.Msg)
		if strings.Contains(message, "sign") {
			return errors.New("小柒接口签名校验失败，请更新接口适配")
		}
		if strings.Contains(message, "登录") || strings.Contains(message, "login") || strings.Contains(message, "vip") {
			return errors.New("小柒该线路需要上游授权，游客暂不可用")
		}
		return errors.New("小柒接口暂未提供有效数据")
	}
	return errors.New("小柒接口连接失败、超时或数据格式变化")
}

func yqkCachedCall(ctx context.Context, client *yqksign.Client, cacheKey, path string, parameters map[string]any, ttl time.Duration) (json.RawMessage, error) {
	yqkRuntime.Lock()
	entry, ok := yqkRuntime.cache[cacheKey]
	yqkRuntime.Unlock()
	if ok && time.Now().Before(entry.Until) {
		return append(json.RawMessage(nil), entry.Data...), nil
	}
	r, e := client.Call(ctx, path, parameters)
	if e != nil {
		return nil, yqkAPIError(e)
	}
	yqkRuntime.Lock()
	if len(yqkRuntime.cache) >= 128 {
		for key := range yqkRuntime.cache {
			delete(yqkRuntime.cache, key)
			break
		}
	}
	yqkRuntime.cache[cacheKey] = yqkCacheEntry{Data: append(json.RawMessage(nil), r.Data...), Until: time.Now().Add(ttl)}
	yqkRuntime.Unlock()
	return r.Data, nil
}

func yqkDecode(raw []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if decoder.Decode(target) != nil {
		return errors.New("小柒接口数据格式变化")
	}
	return nil
}

func yqkDetail(ctx context.Context, client *yqksign.Client, address, vodID string) (map[string]any, error) {
	if !yqkPositiveID(vodID) {
		return nil, errors.New("小柒影片标识无效")
	}
	raw, e := yqkCachedCall(ctx, client, address+":vod:"+vodID, "/v2/api/vodInfo/index", map[string]any{"vodId": gconv.Int64(vodID)}, 3*time.Minute)
	if e != nil {
		return nil, e
	}
	var item map[string]any
	if e = yqkDecode(raw, &item); e != nil {
		return nil, e
	}
	if gconv.String(item["vodId"]) != vodID {
		return nil, errors.New("小柒影片身份返回不匹配")
	}
	return item, nil
}

func yqkExcludedPlayer(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, word := range []string{"豆瓣", "黄豆", "黄果", "douban", "huangdou", "huangguo"} {
		if strings.Contains(name, word) {
			return true
		}
	}
	return name == "db"
}

// The detail endpoint has no category field. The search/list response supplies
// its actual year / subcategory / region in flags; retain this by remote ID.
func yqkListMetadata(item map[string]any) (year, kind, area string) {
	parts := strings.Split(gconv.String(item["flags"]), "/")
	if len(parts) >= 3 {
		year, kind, area = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2])
		if len(year) != 4 || gconv.Int(year) < 1900 || gconv.Int(year) > 2200 {
			year = ""
		}
	}
	return
}

func yqkMetadataMissing(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "0", "未知", "未知年份", "未知地区", "不详", "unknown", "null", "n/a":
		return true
	}
	return false
}

// List flags belong only to the same remote film. A conflicting valid detail
// field, or malformed nonempty metadata, must not be replaced by a candidate.
func yqkVerifiedDetailMetadata(detail, listed map[string]any) (year, kind, area string, ok bool) {
	id := gconv.String(detail["vodId"])
	name := discoveryNormalizeTitle(gconv.String(detail["vodName"]))
	if !yqkPositiveID(id) || id != gconv.String(listed["vodId"]) || name == "" || name != discoveryNormalizeTitle(gconv.String(listed["vodName"])) {
		return "", "", "", false
	}
	year, kind, area = yqkListMetadata(listed)
	if year == "" || yqkMetadataMissing(kind) || yqkMetadataMissing(area) || discoveryRegion(area) == "" {
		return "", "", "", false
	}
	knownYear := strings.TrimSpace(gconv.String(detail["year"]))
	knownArea := strings.TrimSpace(gconv.String(detail["areaName"]))
	if !yqkMetadataMissing(knownYear) && knownYear != year {
		return "", "", "", false
	}
	if !yqkMetadataMissing(knownArea) && discoveryRegion(knownArea) != discoveryRegion(area) {
		return "", "", "", false
	}
	return year, kind, area, true
}

func yqkApplyDetailMetadata(detail, listed map[string]any) bool {
	year, _, area, ok := yqkVerifiedDetailMetadata(detail, listed)
	if !ok {
		return false
	}
	if yqkMetadataMissing(gconv.String(detail["year"])) {
		detail["year"] = year
	}
	if yqkMetadataMissing(gconv.String(detail["areaName"])) {
		detail["areaName"] = area
	}
	detail["flags"] = listed["flags"]
	return true
}

func yqkRememberList(address string, item map[string]any) {
	id := gconv.String(item["vodId"])
	if !yqkPositiveID(id) {
		return
	}
	b, e := json.Marshal(item)
	if e != nil {
		return
	}
	yqkRuntime.Lock()
	if len(yqkRuntime.cache) >= 128 {
		for key := range yqkRuntime.cache {
			delete(yqkRuntime.cache, key)
			break
		}
	}
	yqkRuntime.cache[address+":metadata:"+id] = yqkCacheEntry{Data: b, Until: time.Now().Add(30 * time.Minute)}
	yqkRuntime.Unlock()
}

func yqkDetailMetadata(ctx context.Context, client *yqksign.Client, address string, detail map[string]any) {
	id, name := gconv.String(detail["vodId"]), gconv.String(detail["vodName"])
	yqkRuntime.Lock()
	cached, ok := yqkRuntime.cache[address+":metadata:"+id]
	yqkRuntime.Unlock()
	var item map[string]any
	if ok && time.Now().Before(cached.Until) {
		_ = yqkDecode(cached.Data, &item)
	}
	if item == nil {
		// One exact-identity search fills missing type metadata for direct-ID
		// collection. No unrelated result is ever substituted for the detail.
		response, err := client.Search(ctx, name, "")
		if err == nil {
			var data struct {
				Items []map[string]any `json:"items"`
			}
			if response.DecodeData(&data) == nil {
				for _, candidate := range data.Items {
					yqkRememberList(address, candidate)
					if gconv.String(candidate["vodId"]) == id {
						item = candidate
					}
				}
			}
		}
	}
	yqkApplyDetailMetadata(detail, item)
}

func yqkListItem(vod map[string]any, channelID int, channelName string) map[string]any {
	year, kind, area := yqkListMetadata(vod)
	if kind == "" {
		kind = channelName
	}
	return map[string]any{"__yqk": true, "vod_id": gconv.String(vod["vodId"]), "vod_name": gconv.String(vod["vodName"]), "vod_pic": gconv.String(vod["coverImg"]), "vod_year": year, "vod_area": area, "vod_content": gconv.String(vod["intro"]), "vod_remarks": gconv.String(vod["remark"]), "vod_douban_score": gconv.String(vod["score"]), "type_id": channelID, "type_name": kind, "vod_play_from": "", "vod_play_url": ""}
}

func yqkVodItem(vod map[string]any, channelID int, channelName string) map[string]any {
	id := gconv.String(vod["vodId"])
	year, area := gconv.String(vod["year"]), gconv.String(vod["areaName"])
	if flagYear, actualKind, flagArea, ok := yqkVerifiedDetailMetadata(vod, vod); ok {
		channelName = actualKind
		if yqkMetadataMissing(year) {
			year = flagYear
		}
		if yqkMetadataMissing(area) {
			area = flagArea
		}
	}
	result := map[string]any{"__yqk": true, "vod_id": id, "vod_name": gconv.String(vod["vodName"]), "vod_pic": gconv.String(vod["coverImg"]), "vod_year": year, "vod_area": area, "vod_content": gconv.String(vod["intro"]), "vod_remarks": gconv.String(vod["updateRemark"]), "vod_douban_score": gconv.String(vod["score"]), "vod_class": strings.Join(gconv.Strings(vod["tagList"]), ","), "type_id": channelID, "type_name": channelName}
	for _, mapping := range [][2]string{{"directorList", "vod_director"}, {"actorList", "vod_actor"}} {
		var names []string
		for _, person := range gconv.Maps(vod[mapping[0]]) {
			if name := strings.TrimSpace(gconv.String(person["vodWorkerName"])); name != "" {
				names = append(names, name)
			}
		}
		result[mapping[1]] = strings.Join(names, ",")
	}
	var sources []source
	for _, player := range gconv.Maps(vod["playerList"]) {
		kind, name := gconv.String(player["vodPlayerKind"]), gconv.String(player["playerName"])
		if _, allowed := yqkPlayers[kind]; !allowed || yqkExcludedPlayer(name) {
			continue
		}
		src := source{Code: yqkPlayerCode(kind), Name: yqkPlayers[kind]}
		for _, ep := range gconv.Maps(player["epList"]) {
			epID, label := gconv.String(ep["epId"]), strings.TrimSpace(gconv.String(ep["epName"]))
			if !yqkPositiveID(epID) || label == "" || strings.ContainsAny(label, "$#\r\n") {
				continue
			}
			src.Episodes = append(src.Episodes, episode{Name: label, URL: "yqk://" + id + "/" + kind + "/" + epID})
		}
		src = discoveryNormalizeSource(src)
		if len(src.Episodes) > 0 {
			sources = append(sources, src)
		}
	}
	// Tags and episode counts do not establish the type of a film: a new
	// animation series can have just one published episode. Keep unknown
	// metadata unknown so strict cross-provider matching cannot misclassify it.
	result["vod_play_from"], result["vod_play_url"] = serializeDiscoverySources(sources)
	return result
}

func yqkClasses(ctx context.Context, client *yqksign.Client, address string) ([]map[string]any, error) {
	raw, e := yqkCachedCall(ctx, client, address+":classes", "/v1/api/search/init", nil, 15*time.Minute)
	if e != nil {
		return nil, e
	}
	var data map[string]any
	if e = yqkDecode(raw, &data); e != nil {
		return nil, e
	}
	var classes []map[string]any
	for _, ch := range gconv.Maps(data["hotChannelList"]) {
		id, name := gconv.Int(ch["channelId"]), strings.TrimSpace(gconv.String(ch["channelName"]))
		if id > 0 && name != "" && !yqkExcludedPlayer(name) {
			classes = append(classes, map[string]any{"type_id": id, "type_pid": 0, "type_name": name})
		}
	}
	if len(classes) == 0 {
		return nil, errors.New("小柒未返回有效分类")
	}
	return classes, nil
}

func yqkCursorKey(address, keyword string, category, page int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d|%d", address, keyword, category, page)))
	return hex.EncodeToString(sum[:])
}

func fetchYQK(ctx context.Context, params url.Values) (payload macPayload, err error) {
	switch params.Get("ac") {
	case "list", "videolist", "detail":
	default:
		return payload, errors.New("小柒不支持此采集操作")
	}
	pageNumber, category := 1, 0
	if raw := params.Get("pg"); raw != "" {
		pageNumber, err = strconv.Atoi(raw)
		if err != nil || pageNumber < 1 || pageNumber > 100000 || strconv.Itoa(pageNumber) != raw {
			return payload, errors.New("小柒页码必须是1至100000的整数")
		}
	}
	if raw := params.Get("t"); raw != "" {
		category, err = strconv.Atoi(raw)
		if err != nil || category < 0 || category > 100000 || strconv.Itoa(category) != raw {
			return payload, errors.New("小柒分类编号无效")
		}
	}
	if params.Get("ac") == "detail" && strings.TrimSpace(params.Get("ids")) == "" {
		return payload, errors.New("小柒详情查询缺少影片编号")
	}
	err = withYQK(ctx, false, func(ctx context.Context, client *yqksign.Client, address string) error {
		payload.Code, payload.Page = 1, pageNumber
		classes, e := yqkClasses(ctx, client, address)
		if e != nil {
			return e
		}
		payload.Class = classes
		if params.Get("ac") == "list" {
			return nil
		}
		categoryName := ""
		for _, item := range classes {
			if gconv.Int(item["type_id"]) == category {
				categoryName = gconv.String(item["type_name"])
				break
			}
		}
		if category > 0 && categoryName == "" {
			return errors.New("小柒分类不存在")
		}
		if ids := strings.TrimSpace(params.Get("ids")); ids != "" {
			parts := strings.Split(ids, ",")
			if len(parts) > 20 {
				return errors.New("小柒单次最多查询20部影片")
			}
			for _, id := range parts {
				detail, e := yqkDetail(ctx, client, address, strings.TrimSpace(id))
				if e != nil {
					return e
				}
				yqkDetailMetadata(ctx, client, address, detail)
				payload.List = append(payload.List, yqkVodItem(detail, category, categoryName))
			}
			payload.Total, payload.PageCount = len(payload.List), 1
			return nil
		}
		keyword := strings.TrimSpace(params.Get("wd"))
		cursor := ""
		if payload.Page > 1 {
			yqkRuntime.Lock()
			stored, ok := yqkRuntime.disk.Cursors[yqkCursorKey(address, keyword, category, payload.Page)]
			yqkRuntime.Unlock()
			if !ok || stored.Until <= time.Now().Unix() {
				return errors.New("小柒分页游标已失效，请从第1页连续采集")
			}
			cursor = stored.Value
		}
		var response *yqksign.Response
		if keyword != "" {
			response, e = client.Search(ctx, keyword, cursor)
		} else if category > 0 {
			response, e = client.Call(ctx, "/v2/api/channel/getVodList", map[string]any{"channelId": category, "nextCount": 18, "nextVal": cursor})
		} else {
			response, e = client.Call(ctx, "/v1/api/search/queryNow", map[string]any{"nextCount": 18, "nextVal": cursor, "queryValueJson": "[]", "sortType": ""})
		}
		if e != nil {
			return yqkAPIError(e)
		}
		var page struct {
			Items   []map[string]any `json:"items"`
			HasNext bool             `json:"hasNext"`
			NextVal string           `json:"nextVal"`
		}
		if response.DecodeData(&page) != nil {
			return errors.New("小柒分页数据格式变化")
		}
		if len(page.Items) > 30 {
			return errors.New("小柒单页数据超出限制")
		}
		for _, item := range page.Items {
			if !yqkPositiveID(gconv.String(item["vodId"])) {
				continue
			}
			yqkRememberList(address, item)
			payload.List = append(payload.List, yqkListItem(item, category, categoryName))
		}
		payload.PageCount = payload.Page
		payload.Total = (payload.Page-1)*18 + len(payload.List)
		if page.HasNext {
			if page.NextVal == "" || page.NextVal == cursor {
				return errors.New("小柒未返回有效的下一页游标")
			}
			payload.PageCount++
			payload.Total++
			yqkRuntime.Lock()
			if len(yqkRuntime.disk.Cursors) > 512 {
				for k := range yqkRuntime.disk.Cursors {
					delete(yqkRuntime.disk.Cursors, k)
					break
				}
			}
			yqkRuntime.disk.Cursors[yqkCursorKey(address, keyword, category, payload.Page+1)] = yqkCursor{Value: page.NextVal, Until: time.Now().Add(6 * time.Hour).Unix()}
			e = yqkSaveLocked()
			yqkRuntime.Unlock()
			if e != nil {
				return e
			}
		}
		return nil
	})
	return
}

// Aggregation must not bring back a disabled legacy Douban CDN under an opaque
// APP line name (e.g. MT). BD/Baidu is distinct and is intentionally retained.
func yqkMediaPermitted(raw string, collectors []row) bool {
	if !legacyDoubanMediaHost(playbackHost(raw)) {
		return true
	}
	for _, collector := range collectors {
		name, host := gconv.String(collector["name"]), playbackHost(gconv.String(collector["api_url"]))
		if (strings.Contains(name, "豆瓣") || hostIs(host, "dbzy5.com")) && gconv.Int(collector["status"]) != 1 {
			return false
		}
	}
	return true
}

// Once the default (or first usable fallback) URL succeeds, additional quality
// lookups share one short budget. A slow optional quality never discards the
// usable default. No temporary playback URL is persisted or cached here.
func yqkResolveQualityURLs(ctx context.Context, qualities []map[string]any, extraBudget time.Duration, resolve func(context.Context, map[string]any) (xq.CMSMediaVariant, error)) []xq.CMSMediaVariant {
	var variants []xq.CMSMediaVariant
	workCtx := ctx
	cancel := func() {}
	defer func() { cancel() }()
	for _, quality := range qualities {
		if workCtx.Err() != nil {
			break
		}
		if !gconv.Bool(quality["canPlay"]) || gconv.Int(quality["vodResolution"]) < 1 {
			continue
		}
		variant, err := resolve(workCtx, quality)
		if err != nil || variant.URL == "" {
			continue
		}
		variants = append(variants, variant)
		if len(variants) == 1 {
			workCtx, cancel = context.WithTimeout(ctx, extraBudget)
		}
	}
	return variants
}

func resolveYQK(ctx context.Context, marker string) (media xq.CMSMedia, err error) {
	vodID, kind, epID, e := yqkMarkerParts(marker)
	if e != nil {
		return media, e
	}
	if _, ok := yqkPlayers[kind]; !ok {
		return media, errors.New("小柒该线路未启用")
	}
	err = withYQK(ctx, true, func(ctx context.Context, client *yqksign.Client, address string) error {
		detail, e := yqkDetail(ctx, client, address, vodID)
		if e != nil {
			return e
		}
		owned := false
		for _, player := range gconv.Maps(detail["playerList"]) {
			if gconv.String(player["vodPlayerKind"]) != kind || yqkExcludedPlayer(gconv.String(player["playerName"])) {
				continue
			}
			for _, ep := range gconv.Maps(player["epList"]) {
				if gconv.String(ep["epId"]) == epID {
					owned = true
					break
				}
			}
		}
		if !owned {
			return errors.New("小柒该影片线路中不存在此集，请重新采集")
		}
		raw, e := yqkCachedCall(ctx, client, address+":ep:"+epID, "/v2/api/vodInfo/epDetail", map[string]any{"vodEpId": gconv.Int64(epID)}, time.Minute)
		if e != nil {
			return e
		}
		var qualities []map[string]any
		if yqkDecode(raw, &qualities) != nil {
			return errors.New("小柒清晰度数据格式变化")
		}
		if len(qualities) > 8 {
			return errors.New("小柒清晰度数量异常")
		}
		sort.SliceStable(qualities, func(i, j int) bool {
			return gconv.Bool(qualities[i]["defaultSelect"]) && !gconv.Bool(qualities[j]["defaultSelect"])
		})
		blockedLegacy := false
		media.Variants = yqkResolveQualityURLs(ctx, qualities, 3*time.Second, func(ctx context.Context, q map[string]any) (xq.CMSMediaVariant, error) {
			var empty xq.CMSMediaVariant
			resolution := gconv.Int(q["vodResolution"])
			response, e := client.PlayURL(ctx, gconv.Int64(epID), resolution)
			if e != nil {
				return empty, e
			}
			var data map[string]any
			if response.DecodeData(&data) != nil {
				return empty, errors.New("小柒清晰度播放数据无效")
			}
			rawURL := strings.TrimSpace(gconv.String(data["playUrl"]))
			if safeCollectorURL(ctx, rawURL) != nil {
				return empty, errors.New("小柒播放地址无效或不可达")
			}
			if legacyDoubanMediaHost(playbackHost(rawURL)) {
				collectors, err := all(ctx, "SELECT name,api_url,status FROM sx_collect_api")
				if err != nil || !yqkMediaPermitted(rawURL, collectors) {
					blockedLegacy = true
					return empty, errors.New("该播放地址所属的豆瓣源已停用")
				}
			}
			label := strings.TrimSpace(gconv.String(q["showName"]))
			for _, key := range []string{"name", "resolutionName", "vodResolutionName", "label"} {
				if label == "" {
					label = strings.TrimSpace(gconv.String(q[key]))
				}
			}
			if label == "" {
				label = fmt.Sprintf("清晰度%d", resolution)
			}
			return xq.CMSMediaVariant{URL: rawURL, Label: label}, nil
		})
		if len(media.Variants) > 0 {
			media.URL = media.Variants[0].URL
		}
		if media.URL == "" {
			if blockedLegacy {
				return errors.New("该播放地址所属的豆瓣源已停用，请更换线路")
			}
			return errors.New("小柒该集暂无游客可播放的清晰度")
		}
		return nil
	})
	return
}
