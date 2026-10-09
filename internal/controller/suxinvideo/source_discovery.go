package suxinvideo

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"github.com/suxinwl/GoSuxin/utility/gf"
)

const sourceDiscoveryTable = `CREATE TABLE IF NOT EXISTS sx_source_discovery (
 vod_id BIGINT NOT NULL PRIMARY KEY, status VARCHAR(16) NOT NULL DEFAULT 'done',
 run_token CHAR(32) NOT NULL DEFAULT '', collector_fingerprint CHAR(64) NOT NULL DEFAULT '',
 lease_until BIGINT NOT NULL DEFAULT 0, next_check BIGINT NOT NULL DEFAULT 0,
 checked INT NOT NULL DEFAULT 0, total INT NOT NULL DEFAULT 0,
 added INT NOT NULL DEFAULT 0, updated INT NOT NULL DEFAULT 0, failed INT NOT NULL DEFAULT 0,
 message VARCHAR(250) NOT NULL DEFAULT '', started_at BIGINT NOT NULL DEFAULT 0,
 updated_at BIGINT NOT NULL DEFAULT 0, KEY discovery_lease (lease_until)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`

const sourceDiscoveryMatcherRevision = "2.3.35"
const sourceDiscoveryRunBudget = 180 * time.Second

type discoveryURLPair struct {
	Start  string `json:"start"`
	Status string `json:"status"`
}

type sourceDiscoveryState struct {
	Status         string        `json:"status"`
	Checked        int           `json:"checked"`
	Total          int           `json:"total"`
	Added          int           `json:"added"`
	Updated        int           `json:"updated"`
	Failed         int           `json:"failed"`
	Message        string        `json:"message"`
	NextCheck      int64         `json:"next_check"`
	Sources        *[]playSource `json:"sources,omitempty"`
	SourceRevision string        `json:"source_revision"`
}

type SourceDiscoverReq struct {
	g.Meta `path:"/source/discover" method:"post" noValApi:"1"`
	VodID  int64  `p:"vod_id"`
	Exp    int64  `p:"exp"`
	Sig    string `p:"sig"`
}
type SourceDiscoverRes struct{}
type SourceDiscoveryReq struct {
	g.Meta   `path:"/source/discovery" method:"get" noValApi:"1"`
	VodID    int64  `p:"vod_id"`
	Exp      int64  `p:"exp"`
	Sig      string `p:"sig"`
	Revision string `p:"revision"`
}
type SourceDiscoveryRes struct{}

func discoveryURLs(ctx context.Context, vodID int64) discoveryURLPair {
	exp := time.Now().Add(3 * time.Hour).Unix()
	query := url.Values{"vod_id": {strconv.FormatInt(vodID, 10)}, "exp": {strconv.FormatInt(exp, 10)}, "sig": {signProxy(ctx, fmt.Sprintf("source-discovery:%d", vodID), exp)}}.Encode()
	return discoveryURLPair{Start: "/suxinvideo/source/discover?" + query, Status: "/suxinvideo/source/discovery?" + query}
}

func validDiscoverySignature(ctx context.Context, id, exp int64, sig string) bool {
	now := time.Now().Unix()
	return id > 0 && exp >= now && exp <= now+4*3600 && len(sig) == 64 && hmac.Equal([]byte(sig), []byte(signProxy(ctx, fmt.Sprintf("source-discovery:%d", id), exp)))
}

func (*Media) SourceDiscover(ctx context.Context, req *SourceDiscoverReq) (*SourceDiscoverRes, error) {
	if !validDiscoverySignature(ctx, req.VodID, req.Exp, req.Sig) {
		g.RequestFromCtx(ctx).Response.WriteStatus(http.StatusForbidden, "补充片源凭证无效，请刷新页面")
		return &SourceDiscoverRes{}, nil
	}
	state, err := startSourceDiscovery(ctx, req.VodID)
	if err != nil {
		g.Log().Warning(ctx, "CMS source discovery start:", err)
		state = sourceDiscoveryState{Status: "error", Message: "暂时无法启动片源搜索，请稍后重试", NextCheck: time.Now().Add(time.Minute).Unix()}
	}
	g.RequestFromCtx(ctx).Response.WriteJson(gf.Success().SetData(state))
	return &SourceDiscoverRes{}, nil
}

func (*Media) SourceDiscovery(ctx context.Context, req *SourceDiscoveryReq) (*SourceDiscoveryRes, error) {
	if !validDiscoverySignature(ctx, req.VodID, req.Exp, req.Sig) {
		g.RequestFromCtx(ctx).Response.WriteStatus(http.StatusForbidden, "补充片源凭证无效，请刷新页面")
		return &SourceDiscoveryRes{}, nil
	}
	if req.Revision != "" {
		if decoded, err := hex.DecodeString(req.Revision); err != nil || len(decoded) != 32 {
			badRequest(g.RequestFromCtx(ctx), "片源版本无效")
			return &SourceDiscoveryRes{}, nil
		}
	}
	state, err := sourceDiscoverySnapshot(ctx, req.VodID, req.Revision)
	if err != nil {
		g.Log().Warning(ctx, "CMS source discovery status:", err)
		state = sourceDiscoveryState{Status: "error", Message: "暂时无法读取片源搜索进度，请稍后重试"}
	}
	g.RequestFromCtx(ctx).Response.WriteJson(gf.Success().SetData(state))
	return &SourceDiscoveryRes{}, nil
}

func prepareSourceDiscovery(ctx context.Context) error {
	if err := execSQL(ctx, sourceDiscoveryTable); err != nil {
		return err
	}
	if err := execSQL(ctx, "INSERT IGNORE INTO sx_config(`key`,value) VALUES('source_discovery_enable','1'),('source_discovery_interval','360')"); err != nil {
		return err
	}
	// Only called during process startup: in-memory queued work cannot survive it.
	return execSQL(ctx, "UPDATE sx_source_discovery SET status='error',run_token='',lease_until=0,next_check=0,message='服务重启，等待重新搜索',updated_at=? WHERE status IN ('queued','running')", time.Now().Unix())
}

type sourceDiscoveryJob struct {
	id          int64
	token       string
	identity    string
	target      discoveryTarget
	collectors  []row
	existing    []source
	fingerprint string
}

var discoveryProvider = discoverCollector
var sourceDiscoveryList = discoveryCollectors
var sourceDiscoveryEnrich = enrichDiscoveryTarget
var sourceDiscoveryUser = currentUser
var sourceDiscoveryWorkers = struct {
	once  sync.Once
	jobs  chan sourceDiscoveryJob
	slots chan struct{}
}{jobs: make(chan sourceDiscoveryJob, 8), slots: make(chan struct{}, 10)}

func initSourceDiscoveryWorkers() {
	sourceDiscoveryWorkers.once.Do(func() {
		for i := 0; i < 2; i++ {
			go func() {
				for job := range sourceDiscoveryWorkers.jobs {
					runSourceDiscovery(job)
					<-sourceDiscoveryWorkers.slots
				}
			}()
		}
	})
}

func discoveryHash(value any) string {
	data, _ := json.Marshal(value)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func discoveryFilmIdentity(vod row) string {
	return discoveryHash([]any{gconv.Int64(vod["id"]), gconv.String(vod["name"]), gconv.String(vod["year"]), gconv.String(vod["area"]), gconv.Int64(vod["type_id"]), gconv.Int64(vod["api_id"]), gconv.String(vod["api_vid"])})
}

func discoveryCategoryKind(ctx context.Context, typeID int64) (string, error) {
	seen := map[int64]bool{}
	for depth := 0; typeID > 0 && depth < 8 && !seen[typeID]; depth++ {
		seen[typeID] = true
		category, err := one(ctx, "SELECT name,pid FROM sx_type WHERE id=?", typeID)
		if err != nil || category == nil {
			return "", err
		}
		if kind := discoveryKind(gconv.String(category["name"])); kind != "" {
			return kind, nil
		}
		typeID = gconv.Int64(category["pid"])
	}
	return "", nil
}

func discoveryCollectors(ctx context.Context) ([]row, string, error) {
	rows, err := all(ctx, "SELECT id,name,api_url,status FROM sx_collect_api ORDER BY id")
	if err != nil {
		return nil, "", err
	}
	players, err := all(ctx, "SELECT code,status,`parse` FROM sx_player ORDER BY code")
	if err != nil {
		return nil, "", err
	}
	result := make([]row, 0, len(rows))
	for _, item := range rows {
		if gconv.Int(item["status"]) == 1 && discoveryCollectorSupported(gconv.String(item["api_url"])) {
			result = append(result, item)
		}
	}
	return result, discoveryHash([]any{rows, players}), nil
}

func discoveryState(record row) sourceDiscoveryState {
	if record == nil {
		return sourceDiscoveryState{Status: "done", Message: "可搜索同片的其它资源"}
	}
	return sourceDiscoveryState{Status: gconv.String(record["status"]), Checked: gconv.Int(record["checked"]), Total: gconv.Int(record["total"]), Added: gconv.Int(record["added"]), Updated: gconv.Int(record["updated"]), Failed: gconv.Int(record["failed"]), Message: gconv.String(record["message"]), NextCheck: gconv.Int64(record["next_check"])}
}

func discoveryDue(record row, fingerprint string, now int64) bool {
	return record == nil || (gconv.Int64(record["lease_until"]) <= now && (gconv.String(record["status"]) == "disabled" || gconv.Int64(record["next_check"]) <= now || gconv.String(record["collector_fingerprint"]) != fingerprint))
}

func discoveryFingerprint(ctx context.Context, vod row, collectorHash string) string {
	return discoveryHash([]string{sourceDiscoveryMatcherRevision, collectorHash, discoveryFilmIdentity(vod), setting(ctx, "source_discovery_enable", "1"), setting(ctx, "source_discovery_interval", "360")})
}

func startSourceDiscovery(ctx context.Context, id int64) (sourceDiscoveryState, error) {
	if setting(ctx, "source_discovery_enable", "1") != "1" {
		return sourceDiscoveryState{Status: "disabled", Message: "站点已关闭自动补充片源"}, nil
	}
	vod, err := one(ctx, "SELECT id,name,class,year,area,type_id,api_id,api_vid,status,play_from,play_url FROM sx_vod WHERE id=? AND status=1", id)
	if err != nil || vod == nil || !contentVodAllowed(ctx, vod) {
		return sourceDiscoveryState{Status: "error", Message: "影片不存在或已下架"}, err
	}
	collectors, fingerprint, err := sourceDiscoveryList(ctx)
	if err != nil {
		return sourceDiscoveryState{}, err
	}
	fingerprint = discoveryFingerprint(ctx, vod, fingerprint)
	record, err := one(ctx, "SELECT * FROM sx_source_discovery WHERE vod_id=?", id)
	if err != nil {
		return sourceDiscoveryState{}, err
	}
	now := time.Now().Unix()
	if !discoveryDue(record, fingerprint, now) {
		return discoveryState(record), nil
	}
	initSourceDiscoveryWorkers()
	select {
	case sourceDiscoveryWorkers.slots <- struct{}{}:
	default:
		return sourceDiscoveryState{Status: "busy", Message: "片源搜索繁忙，稍后会继续尝试", NextCheck: now + 5}, nil
	}
	reserved := true
	defer func() {
		if reserved {
			<-sourceDiscoveryWorkers.slots
		}
	}()
	kind, err := discoveryCategoryKind(ctx, gconv.Int64(vod["type_id"]))
	if err != nil {
		return sourceDiscoveryState{}, err
	}
	var secret [16]byte
	if _, err = rand.Read(secret[:]); err != nil {
		return sourceDiscoveryState{}, err
	}
	token := hex.EncodeToString(secret[:])
	if err = execSQL(ctx, "INSERT IGNORE INTO sx_source_discovery(vod_id) VALUES(?)", id); err != nil {
		return sourceDiscoveryState{}, err
	}
	claim, err := g.DB().Exec(ctx, `UPDATE sx_source_discovery SET status='queued',run_token=?,collector_fingerprint=?,lease_until=?,next_check=0,checked=0,total=?,added=0,updated=0,failed=0,message='等待搜索同片资源',started_at=?,updated_at=? WHERE vod_id=? AND lease_until<=? AND (next_check<=? OR collector_fingerprint<>? OR status='disabled')`, token, fingerprint, now+900, len(collectors), now, now, id, now, now, fingerprint)
	if err != nil {
		return sourceDiscoveryState{}, err
	}
	count, err := claim.RowsAffected()
	if err != nil {
		return sourceDiscoveryState{}, err
	}
	if count != 1 {
		latest, e := one(ctx, "SELECT * FROM sx_source_discovery WHERE vod_id=?", id)
		return discoveryState(latest), e
	}
	job := sourceDiscoveryJob{id: id, token: token, identity: discoveryFilmIdentity(vod), fingerprint: fingerprint, collectors: collectors, existing: playlist(vod), target: discoveryTarget{ID: id, Name: gconv.String(vod["name"]), Year: gconv.String(vod["year"]), Area: gconv.String(vod["area"]), Kind: kind, SourceAPIID: gconv.Int64(vod["api_id"]), SourceAPIVID: gconv.String(vod["api_vid"])}}
	select {
	case sourceDiscoveryWorkers.jobs <- job:
		reserved = false
		return sourceDiscoveryState{Status: "queued", Total: len(collectors), Message: "等待搜索同片资源"}, nil
	default:
		_ = execSQL(ctx, "UPDATE sx_source_discovery SET status='busy',lease_until=0,next_check=?,run_token='',message='片源搜索繁忙，请稍后重试' WHERE vod_id=? AND run_token=?", now+5, id, token)
		return sourceDiscoveryState{Status: "busy", Message: "片源搜索繁忙，稍后会继续尝试", NextCheck: now + 5}, nil
	}
}

func discoveryCooldownMinutes(raw string) int64 {
	minutes := gconv.Int64(raw)
	if minutes < 15 {
		return 15
	}
	if minutes > 1440 {
		return 1440
	}
	return minutes
}

func discoveryTerminalMessage(target discoveryTarget, status, message string) string {
	if status != "done" && status != "error" {
		return message
	}
	var missing []string
	if !discoveryYearPattern.MatchString(strings.TrimSpace(target.Year)) {
		missing = append(missing, "年份")
	}
	if discoveryRegion(target.Area) == "" {
		missing = append(missing, "地区")
	}
	if discoveryKind(target.Kind) == "" {
		missing = append(missing, "类型")
	}
	if len(missing) > 0 {
		message += "；影片" + strings.Join(missing, "、") + "信息不完整，暂无法匹配其它资源站"
	}
	return message
}

func runSourceDiscovery(job sourceDiscoveryJob) {
	ctx, cancel := context.WithTimeout(context.Background(), sourceDiscoveryRunBudget)
	defer cancel()
	now := time.Now().Unix()
	claim, err := g.DB().Exec(ctx, "UPDATE sx_source_discovery SET status='running',lease_until=?,message='正在搜索同片资源',updated_at=? WHERE vod_id=? AND run_token=? AND status='queued'", now+int64((sourceDiscoveryRunBudget+20*time.Second)/time.Second), now, job.id, job.token)
	if err != nil {
		g.Log().Warning(ctx, "CMS discovery worker start:", err)
		return
	}
	if count, _ := claim.RowsAffected(); count != 1 {
		return
	}
	for _, collector := range job.collectors {
		if gconv.Int64(collector["id"]) == job.target.SourceAPIID {
			job.target = sourceDiscoveryEnrich(ctx, job.target, collector)
			break
		}
	}
	job.target = enrichDiscoveryFromStoredLines(ctx, job.target, job.existing, job.collectors, fetchCollectSource)
	type completed struct {
		collector row
		result    discoveryProviderResult
	}
	work := make(chan row, len(job.collectors))
	results := make(chan completed, len(job.collectors))
	for _, collector := range discoveryPrioritizeCollectors(job.collectors, job.target.SourceAPIID, job.target.Kind) {
		work <- collector
	}
	close(work)
	var group sync.WaitGroup
	for i := 0; i < 3; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for collector := range work {
				if ctx.Err() != nil {
					return
				}
				result := discoveryProvider(ctx, job.target, collector)
				select {
				case results <- completed{collector, result}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() { group.Wait(); close(results) }()
	checked, added, updated, failed, found := 0, 0, 0, 0, 0
	status, message := "done", "已检查启用的资源站，暂无新的匹配线路"
	loop := true
	for loop {
		select {
		case <-ctx.Done():
			status, message, loop = "error", "本轮搜索已结束，部分资源站连接超时", false
		case item, ok := <-results:
			if !ok {
				loop = false
				continue
			}
			checked++
			if item.result.RatingItem != nil {
				if scoreErr := saveDiscoveredVodScore(ctx, job, item.collector, item.result); scoreErr != nil && !errors.Is(scoreErr, errDiscoveryChanged) {
					g.Log().Warning(ctx, "CMS discovered source score unavailable")
				}
			}
			if item.result.Error != "" {
				failed++
			} else if len(item.result.Sources) > 0 {
				a, u, retained, mergeErr := mergeDiscoveredSources(ctx, job, item.collector, item.result)
				if mergeErr != nil {
					failed++
					if errors.Is(mergeErr, errDiscoveryChanged) {
						status, message, loop = "error", "影片或资源站设置已更新，本轮搜索停止", false
						cancel()
					}
				} else {
					added, updated, found = added+a, updated+u, found+retained
				}
			}
			if loop {
				_ = execSQL(ctx, "UPDATE sx_source_discovery SET checked=?,added=?,updated=?,failed=?,message='正在检查同片播放线路',updated_at=? WHERE vod_id=? AND run_token=?", checked, added, updated, failed, time.Now().Unix(), job.id, job.token)
			}
		}
	}
	saveCtx, saveCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer saveCancel()
	cooldown := int64(15 * 60)
	if found > 0 {
		cooldown = discoveryCooldownMinutes(setting(saveCtx, "source_discovery_interval", "360")) * 60
		if status == "done" {
			message = "已完成片源搜索，可用线路已同步"
		}
	} else if failed > 0 && checked == failed {
		status, message = "error", "暂未获得可用片源，稍后会重新检查"
	}
	if setting(saveCtx, "source_discovery_enable", "1") != "1" {
		status, message = "disabled", "站点已关闭自动补充片源"
	}
	message = discoveryTerminalMessage(job.target, status, message)
	if err = execSQL(saveCtx, "UPDATE sx_source_discovery SET status=?,checked=?,added=?,updated=?,failed=?,message=?,next_check=?,lease_until=0,run_token='',updated_at=? WHERE vod_id=? AND run_token=?", status, checked, added, updated, failed, message, time.Now().Unix()+cooldown, time.Now().Unix(), job.id, job.token); err != nil {
		g.Log().Warning(saveCtx, "CMS discovery worker save:", err)
	}
}

// Start the aggregate provider early so it can check its independent lines
// within its own bounded budget, alongside the original provider and others.
func discoveryPrioritizeCollectors(collectors []row, originalID int64, kinds ...string) []row {
	ordered := append([]row(nil), collectors...)
	kind := ""
	if len(kinds) > 0 {
		kind = discoveryKind(kinds[0])
	}
	priority := func(item row) int {
		raw := gconv.String(item["api_url"])
		if kind == "short" && raw == hongguoSourceURL || (kind == "anime" || kind == "anime_movie") && isErciyuanSource(raw) {
			return -1
		}
		if isYQKSource(gconv.String(item["api_url"])) {
			return 0
		}
		if gconv.Int64(item["id"]) == originalID {
			return 1
		}
		return 2
	}
	sort.SliceStable(ordered, func(i, j int) bool { return priority(ordered[i]) < priority(ordered[j]) })
	return ordered
}

var errDiscoveryChanged = errors.New("discovery target or settings changed")

func discoverySourceAllowed(src source, collector row, collectors []row) bool {
	if !identifier.MatchString(src.Code) || len(src.Code) > 80 || len(src.Episodes) == 0 {
		return false
	}
	if isYQKSource(gconv.String(collector["api_url"])) {
		return yqkAllowedSource(src)
	}
	if isErciyuanSource(gconv.String(collector["api_url"])) {
		return erciyuanAllowedSource(src)
	}
	if gconv.String(collector["api_url"]) == hongguoSourceURL {
		if src.Code != "hongguo" || src.Parse != "" {
			return false
		}
		seriesID := ""
		for _, ep := range src.Episodes {
			series, _, valid := hongguoDiscoveryMarker(ep.URL)
			if !valid || seriesID != "" && series != seriesID {
				return false
			}
			seriesID = series
		}
		return true
	}
	if src.Code == "hongguo" {
		return false
	}
	if strings.HasPrefix(src.Code, "yqk_") || strings.HasPrefix(src.Code, "ecy_") {
		return false
	}
	for _, owner := range collectors {
		for _, code := range collectorPlaybackCodes(gconv.String(owner["api_url"])) {
			if code == src.Code && gconv.Int64(owner["id"]) != gconv.Int64(collector["id"]) {
				return false
			}
		}
	}
	if codes := collectorPlaybackCodes(gconv.String(collector["api_url"])); len(codes) > 0 {
		for _, code := range codes {
			if code == src.Code {
				return true
			}
		}
		return false
	}
	if src.Code == "dbm3u8" {
		// The shared code cannot overwrite a disabled supplier with an unknown
		// origin. Recognized Baidu URLs are the only active shared-code import.
		host := playbackHost(gconv.String(collector["api_url"]))
		if !strings.Contains(host, "bdzy") {
			return false
		}
		for _, ep := range src.Episodes {
			if !strings.Contains(playbackHost(ep.URL), "bdzy") {
				return false
			}
		}
		return true
	}
	return src.Code != "m3u8" && src.Code != "mp4" && src.Code != "no"
}

func mergeDiscoveredSources(ctx context.Context, job sourceDiscoveryJob, originalCollector row, result discoveryProviderResult) (added, updated, retained int, err error) {
	err = g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		state, e := tx.GetOne("SELECT run_token FROM sx_source_discovery WHERE vod_id=? FOR UPDATE", job.id)
		if e != nil {
			return e
		}
		if state == nil || state["run_token"].String() != job.token {
			return errDiscoveryChanged
		}
		config, e := tx.GetOne("SELECT value FROM sx_config WHERE `key`='source_discovery_enable'")
		if e != nil {
			return e
		}
		if config != nil && config["value"].String() != "1" {
			return errDiscoveryChanged
		}
		parser, e := tx.GetOne("SELECT value FROM sx_config WHERE `key`='player_parse'")
		if e != nil {
			return e
		}
		if parser != nil && strings.TrimSpace(parser["value"].String()) != "" {
			return errDiscoveryChanged
		}
		record, e := tx.GetOne("SELECT id,name,class,year,area,type_id,api_id,api_vid,status,play_from,play_url FROM sx_vod WHERE id=? FOR UPDATE", job.id)
		if e != nil {
			return e
		}
		vod := gconv.Map(record)
		if record == nil || gconv.Int(vod["status"]) != 1 || !contentVodAllowed(ctx, vod) || discoveryFilmIdentity(vod) != job.identity {
			return errDiscoveryChanged
		}
		collectorRows, e := tx.GetAll("SELECT id,name,api_url,status FROM sx_collect_api")
		if e != nil {
			return e
		}
		collectors := gconv.Maps(collectorRows)
		var collector row
		for _, item := range collectors {
			if gconv.Int64(item["id"]) == gconv.Int64(originalCollector["id"]) {
				collector = item
			}
		}
		if collector == nil || gconv.Int(collector["status"]) != 1 || gconv.String(collector["api_url"]) != gconv.String(originalCollector["api_url"]) || result.CollectorID != gconv.Int64(collector["id"]) {
			return errDiscoveryChanged
		}
		playerRows, e := tx.GetAll("SELECT code,name,`parse`,status FROM sx_player")
		if e != nil {
			return e
		}
		players := map[string]row{}
		for _, item := range gconv.Maps(playerRows) {
			players[gconv.String(item["code"])] = item
		}
		incoming := make([]source, 0, len(result.Sources))
		existingCodes := map[string]bool{}
		for _, src := range playlist(vod) {
			existingCodes[src.Code] = true
		}
		for _, src := range result.Sources {
			if existingCodes[src.Code] && src.Code != "dbm3u8" && len(collectorPlaybackCodes(gconv.String(collector["api_url"]))) == 0 && gconv.Int64(vod["api_id"]) != gconv.Int64(collector["id"]) {
				// An unrecognized collector/code pair has no provenance proving
				// that it owns an existing merged line. It may add a distinct
				// code, but cannot overwrite somebody else's unknown code.
				continue
			}
			if discoverySourceAllowed(src, collector, collectors) {
				incoming = append(incoming, src)
			}
		}
		incoming = availableSources(vod, incoming, players, collectors)
		retained = len(incoming)
		if retained == 0 {
			return nil
		}
		movie := job.target.Kind == "movie" || job.target.Kind == "anime_movie"
		merged, a, u := mergeDiscoveryFilmPlaylists(playlist(vod), incoming, job.target.Name, movie)
		from, urls := serializeDiscoverySources(merged)
		if len(from) > maxPlaybackSourceNames || len(urls) > 1<<20 {
			return errors.New("discovered playlist exceeds storage limit")
		}
		if a+u > 0 {
			if _, e = tx.Exec("UPDATE sx_vod SET play_from=?,play_url=? WHERE id=?", from, urls, job.id); e != nil {
				return e
			}
		}
		added, updated = a, u
		return nil
	})
	return
}

func saveDiscoveredVodScore(ctx context.Context, job sourceDiscoveryJob, collector row, result discoveryProviderResult) error {
	if result.RatingItem == nil || result.CollectorID != gconv.Int64(collector["id"]) {
		return nil
	}
	state, err := one(ctx, "SELECT run_token FROM sx_source_discovery WHERE vod_id=?", job.id)
	if err != nil {
		return err
	}
	if state == nil || gconv.String(state["run_token"]) != job.token {
		return errDiscoveryChanged
	}
	return saveVodSourceScoreWithIdentity(ctx, job.id, result.CollectorID, result.RatingItem, job.identity)
}

func discoveryEpisodeIdentity(ep episode) string {
	key, _ := playbackEpisodeIdentity(ep.Name)
	return key
}

func mergeDiscoveryPlaylists(existing, incoming []source) ([]source, int, int) {
	return mergeDiscoveryFilmPlaylists(existing, incoming, "", false)
}

func mergeDiscoveryFilmPlaylists(existing, incoming []source, title string, movie bool) ([]source, int, int) {
	movie = moviePlaybackAlignmentAllowed(title, movie, existing) && moviePlaybackAlignmentAllowed(title, movie, incoming)
	multiVersion := make(map[string]bool)
	for _, sources := range [][]source{existing, incoming} {
		for _, src := range sources {
			if len(src.Episodes) > 1 {
				multiVersion[src.Code] = true
			}
		}
	}
	result := append([]source(nil), existing...)
	indexes := map[string]int{}
	for i, src := range result {
		result[i].Episodes = append([]episode(nil), src.Episodes...)
		indexes[src.Code] = i
	}
	added, updated := 0, 0
	for _, src := range incoming {
		keyFor := func(ep episode) string {
			// Full-movie versions share a playback target, but remain
			// separate stored choices. Never overwrite TC with an HD URL
			// just because another version belongs to the same movie.
			key, _ := moviePlaybackEpisodeKey(title, ep.Name, movie && !multiVersion[src.Code])
			return key
		}
		index, exists := indexes[src.Code]
		if !exists {
			index = len(result)
			indexes[src.Code] = index
			result = append(result, source{Code: src.Code, Name: src.Name, Parse: src.Parse})
		}
		positions := map[string]int{}
		for i, ep := range result[index].Episodes {
			if _, known := positions[keyFor(ep)]; !known {
				positions[keyFor(ep)] = i
			}
		}
		// Normalize each incoming line before comparing with storage so a
		// repeated feed containing duplicate labels remains a true no-op.
		latest := make([]episode, 0, len(src.Episodes))
		latestIndex := map[string]int{}
		for _, ep := range src.Episodes {
			key := keyFor(ep)
			if i, ok := latestIndex[key]; ok {
				latest[i].URL = ep.URL
			} else {
				latestIndex[key] = len(latest)
				latest = append(latest, ep)
			}
		}
		changed := false
		for _, ep := range latest {
			if strings.TrimSpace(ep.URL) == "" || strings.ContainsAny(ep.Name+ep.URL, "#\r\n") || strings.Contains(ep.Name, "$") {
				continue
			}
			key := keyFor(ep)
			if i, ok := positions[key]; ok {
				if result[index].Episodes[i].URL != ep.URL {
					// Retain labels and indices from the existing playlist.
					result[index].Episodes[i].URL = ep.URL
					changed = true
				}
			} else {
				positions[key] = len(result[index].Episodes)
				result[index].Episodes = append(result[index].Episodes, ep)
				changed = true
			}
		}
		if !exists && len(result[index].Episodes) > 0 {
			sort.SliceStable(result[index].Episodes, func(a, b int) bool {
				_, an := playbackEpisodeIdentity(result[index].Episodes[a].Name)
				_, bn := playbackEpisodeIdentity(result[index].Episodes[b].Name)
				if an >= 0 && bn >= 0 {
					return an < bn
				}
				return an >= 0 && bn < 0
			})
			added++
		} else if exists && changed {
			updated++
		}
	}
	return result, added, updated
}

func serializeDiscoverySources(sources []source) (string, string) {
	var codes, groups []string
	for _, src := range sources {
		if len(src.Episodes) == 0 {
			continue
		}
		parts := make([]string, 0, len(src.Episodes))
		for _, ep := range src.Episodes {
			parts = append(parts, ep.Name+"$"+ep.URL)
		}
		codes, groups = append(codes, src.Code), append(groups, strings.Join(parts, "#"))
	}
	return strings.Join(codes, "$$$"), strings.Join(groups, "$$$")
}

func discoveryCanView(ctx context.Context, vod row) (bool, error) {
	if gconv.Int(vod["vip"]) != 1 && gconv.Int(vod["points"]) <= 0 {
		return true, nil
	}
	user, err := sourceDiscoveryUser(ctx)
	if err != nil || user == nil {
		return false, err
	}
	if gconv.Int(vod["vip"]) == 1 {
		return gconv.Int64(user["vip_expire"]) > time.Now().Unix(), nil
	}
	owned, err := one(ctx, "SELECT id FROM sx_user_vod WHERE user_id=? AND vod_id=?", user["id"], vod["id"])
	return owned != nil, err
}

func sourceDiscoverySnapshot(ctx context.Context, id int64, revision string) (sourceDiscoveryState, error) {
	vod, err := one(ctx, "SELECT * FROM sx_vod WHERE id=? AND status=1", id)
	if err != nil || vod == nil || !contentVodAllowed(ctx, vod) {
		return sourceDiscoveryState{Status: "error", Message: "影片不存在或已下架"}, err
	}
	record, err := one(ctx, "SELECT * FROM sx_source_discovery WHERE vod_id=?", id)
	if err != nil {
		return sourceDiscoveryState{}, err
	}
	state := discoveryState(record)
	if (state.Status == "running" || state.Status == "queued") && gconv.Int64(record["lease_until"]) <= time.Now().Unix() {
		state.Status, state.Message, state.NextCheck = "error", "上次搜索已中断，可重新尝试", 0
	}
	if setting(ctx, "source_discovery_enable", "1") != "1" {
		state.Status, state.Message = "disabled", "站点已关闭自动补充片源"
	} else if state.Status == "disabled" {
		state.Status, state.Message, state.NextCheck = "done", "自动补充片源已开启，可重新搜索", 0
	}
	if state.Status != "running" && state.Status != "queued" && state.Status != "disabled" && record != nil {
		_, fingerprint, e := sourceDiscoveryList(ctx)
		if e != nil {
			return state, e
		}
		if discoveryFingerprint(ctx, vod, fingerprint) != gconv.String(record["collector_fingerprint"]) {
			state.NextCheck, state.Message = 0, "影片或资源站设置已更新，可重新搜索"
		}
	}
	allowed, err := discoveryCanView(ctx, vod)
	if err != nil || !allowed {
		return state, err
	}
	visible, err := hydratePlayers(ctx, vod, playlist(vod))
	if err != nil {
		return state, err
	}
	state.SourceRevision = discoveryHash([]any{visible, setting(ctx, "player_parse", ""), gconv.String(vod["name"]), gconv.Int64(vod["type_id"])})
	if state.SourceRevision != revision {
		views := playerViewSources(ctx, id, visible)
		movie, e := moviePlaybackCategory(ctx, gconv.Int64(vod["type_id"]))
		if e != nil {
			return state, e
		}
		movie = moviePlaybackAlignmentAllowed(gconv.String(vod["name"]), movie, playlist(vod))
		alignMoviePlaybackSources(gconv.String(vod["name"]), movie, views)
		if views == nil {
			views = []playSource{}
		}
		state.Sources = &views
	}
	return state, nil
}
