package suxinvideo

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	xq "github.com/suxinwl/GoSuxin/internal/xiaoqiapp"
	"github.com/suxinwl/GoSuxin/utility/gf"
)

// A failed browser request only requests a server check. Original playlists are
// retained: two independent failed checks quarantine this film's line for 30m.
const sourceHealthTable = `CREATE TABLE IF NOT EXISTS sx_vod_source_health (
 vod_id BIGINT NOT NULL, fingerprint CHAR(64) NOT NULL,
 source_code VARCHAR(200) NOT NULL DEFAULT '', fail_rounds INT NOT NULL DEFAULT 0,
 last_check BIGINT NOT NULL DEFAULT 0, checking_until BIGINT NOT NULL DEFAULT 0,
 hidden_until BIGINT NOT NULL DEFAULT 0, last_error VARCHAR(250) NOT NULL DEFAULT '',
 updated_at BIGINT NOT NULL DEFAULT 0,
 PRIMARY KEY (vod_id,fingerprint), KEY hidden_until (hidden_until)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`

func prepareSourceHealth(ctx context.Context) error { return execSQL(ctx, sourceHealthTable) }

func sourceHealthFingerprint(src source) string {
	// Display names may change without changing the actual resource.
	data, _ := json.Marshal(struct {
		Code, Parse string
		Episodes    []episode
	}{src.Code, src.Parse, src.Episodes})
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func eligibleSourceHealth(src source) bool {
	if src.Parse != "" || len(src.Episodes) == 0 {
		return false
	}
	if strings.HasPrefix(src.Code, "yqk_") {
		return yqkAllowedSource(src)
	}
	if strings.HasPrefix(src.Code, "ecy_") {
		return erciyuanAllowedSource(src)
	}
	for _, ep := range src.Episodes {
		u, err := url.Parse(ep.URL)
		if err != nil || !safePlayerAddress(ep.URL) || !(strings.HasSuffix(strings.ToLower(u.Path), ".m3u8") || strings.HasSuffix(strings.ToLower(u.Path), ".mp4")) {
			return false
		}
	}
	return true
}

func sourceHealthReportURL(ctx context.Context, vodID int64, src source) string {
	if vodID < 1 || !eligibleSourceHealth(src) || setting(ctx, "player_parse", "") != "" {
		return ""
	}
	exp := time.Now().Add(3 * time.Hour).Unix()
	fingerprint := sourceHealthFingerprint(src)
	values := url.Values{"vod_id": {strconv.FormatInt(vodID, 10)}, "source": {src.Code}, "fingerprint": {fingerprint}, "exp": {strconv.FormatInt(exp, 10)}}
	values.Set("sig", signProxy(ctx, sourceHealthSignInput(vodID, fingerprint), exp))
	return "/suxinvideo/source/report?" + values.Encode()
}

func sourceHealthSignInput(vodID int64, fingerprint string) string {
	return fmt.Sprintf("source-health:%d:%s", vodID, fingerprint)
}

func filterUnhealthySources(ctx context.Context, vodID int64, sources []source) []source {
	if vodID < 1 || len(sources) == 0 || setting(ctx, "player_parse", "") != "" {
		return sources
	}
	rows, err := all(ctx, "SELECT fingerprint FROM sx_vod_source_health WHERE vod_id=? AND hidden_until>?", vodID, time.Now().Unix())
	if err != nil {
		g.Log().Warning(ctx, "CMS source health filter:", err)
		return sources
	}
	hidden := make(map[string]bool, len(rows))
	for _, item := range rows {
		hidden[gconv.String(item["fingerprint"])] = true
	}
	result := make([]source, 0, len(sources))
	for _, src := range sources {
		if !eligibleSourceHealth(src) || !hidden[sourceHealthFingerprint(src)] {
			result = append(result, src)
		}
	}
	return result
}

type SourceReportReq struct {
	g.Meta      `path:"/source/report" method:"post" noValApi:"1"`
	VodID       int64  `p:"vod_id"`
	Source      string `p:"source"`
	Fingerprint string `p:"fingerprint"`
	Exp         int64  `p:"exp"`
	Sig         string `p:"sig"`
	Episode     int    `p:"episode"`
}
type SourceReportRes struct{}

func (*Media) SourceReport(ctx context.Context, req *SourceReportReq) (*SourceReportRes, error) {
	now := time.Now().Unix()
	if req.VodID < 1 || len(req.Source) > 200 || len(req.Fingerprint) != 64 || req.Exp < now || req.Exp > now+4*3600 || !hmac.Equal([]byte(req.Sig), []byte(signProxy(ctx, sourceHealthSignInput(req.VodID, req.Fingerprint), req.Exp))) {
		g.RequestFromCtx(ctx).Response.WriteStatus(http.StatusForbidden, "线路检测凭证无效，请刷新播放页")
		return &SourceReportRes{}, nil
	}
	job := sourceHealthJob{vodID: req.VodID, code: req.Source, fingerprint: req.Fingerprint, episode: req.Episode}
	// The worker re-reads the current database playlist before checking. No URL
	// supplied by the browser is accepted, and a report itself changes no data.
	accepted := enqueueSourceHealth(job)
	g.RequestFromCtx(ctx).Response.WriteJson(gf.Success().SetData(map[string]any{"accepted": accepted}))
	return &SourceReportRes{}, nil
}

type sourceHealthJob struct {
	vodID       int64
	code        string
	fingerprint string
	episode     int
}

var sourceHealthQueue = struct {
	sync.Mutex
	once    sync.Once
	jobs    chan sourceHealthJob
	pending map[string]bool
}{jobs: make(chan sourceHealthJob, 32), pending: make(map[string]bool)}

func enqueueSourceHealth(job sourceHealthJob) bool {
	sourceHealthQueue.once.Do(func() {
		for i := 0; i < 2; i++ {
			go func() {
				for item := range sourceHealthQueue.jobs {
					if runSourceHealthCheck(item) {
						// Keep the pending reservation while waiting so repeated
						// reports cannot bypass the minimum check interval.
						time.AfterFunc(time.Minute, func() {
							select {
							case sourceHealthQueue.jobs <- item:
							default:
								releaseSourceHealth(item)
							}
						})
					} else {
						releaseSourceHealth(item)
					}
				}
			}()
		}
	})
	key := sourceHealthJobKey(job)
	sourceHealthQueue.Lock()
	defer sourceHealthQueue.Unlock()
	if sourceHealthQueue.pending[key] || len(sourceHealthQueue.pending) >= 64 {
		return false
	}
	sourceHealthQueue.pending[key] = true
	select {
	case sourceHealthQueue.jobs <- job:
		return true
	default:
		delete(sourceHealthQueue.pending, key)
		return false
	}
}

func sourceHealthJobKey(job sourceHealthJob) string {
	return fmt.Sprintf("%d:%s", job.vodID, job.fingerprint)
}
func releaseSourceHealth(job sourceHealthJob) {
	sourceHealthQueue.Lock()
	delete(sourceHealthQueue.pending, sourceHealthJobKey(job))
	sourceHealthQueue.Unlock()
}

func currentSourceHealthCandidate(ctx context.Context, job sourceHealthJob) (source, bool, error) {
	if setting(ctx, "player_parse", "") != "" {
		return source{}, false, nil
	}
	vod, err := one(ctx, "SELECT id,api_id,play_from,play_url FROM sx_vod WHERE id=? AND status=1", job.vodID)
	if err != nil || vod == nil {
		return source{}, false, err
	}
	sources, err := hydratePlayers(ctx, vod, playlist(vod))
	if err != nil {
		return source{}, false, err
	}
	for _, src := range sources {
		if src.Code == job.code && sourceHealthFingerprint(src) == job.fingerprint && eligibleSourceHealth(src) {
			return src, true, nil
		}
	}
	return source{}, false, nil
}

func sourceHealthDue(now, lastCheck, checkingUntil, hiddenUntil int64) bool {
	return checkingUntil <= now && hiddenUntil <= now && lastCheck <= now-60
}

func sourceHealthOutcome(rounds int, lastCheck, now int64, failed bool) (int, int64) {
	if !failed {
		return 0, 0
	}
	if now-lastCheck > 15*60 {
		rounds = 0
	}
	rounds++
	if rounds >= 2 {
		return rounds, now + 30*60
	}
	return rounds, 0
}

func runSourceHealthCheck(job sourceHealthJob) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	src, found, err := currentSourceHealthCandidate(ctx, job)
	if err != nil {
		g.Log().Warning(ctx, "CMS source check load:", err)
		return false
	}
	if !found {
		return false
	}
	now := time.Now().Unix()
	if err = execSQL(ctx, "INSERT IGNORE INTO sx_vod_source_health(vod_id,fingerprint,source_code,updated_at) VALUES(?,?,?,?)", job.vodID, job.fingerprint, job.code, now); err != nil {
		g.Log().Warning(ctx, "CMS source check create:", err)
		return false
	}
	state, err := one(ctx, "SELECT fail_rounds,last_check,checking_until,hidden_until FROM sx_vod_source_health WHERE vod_id=? AND fingerprint=?", job.vodID, job.fingerprint)
	if err != nil || state == nil || !sourceHealthDue(now, gconv.Int64(state["last_check"]), gconv.Int64(state["checking_until"]), gconv.Int64(state["hidden_until"])) {
		return false
	}
	lease, err := g.DB().Exec(ctx, "UPDATE sx_vod_source_health SET checking_until=? WHERE vod_id=? AND fingerprint=? AND checking_until<=? AND hidden_until<=? AND last_check<=?", now+120, job.vodID, job.fingerprint, now, now, now-60)
	if err != nil {
		g.Log().Warning(ctx, "CMS source check lease:", err)
		return false
	}
	affected, err := lease.RowsAffected()
	if err != nil || affected != 1 {
		return false
	}
	// Match the playback proxy's response timeout so a merely slower source
	// does not fail a stricter health check than its real playback request.
	client := safeCollectorHTTPClient(12 * time.Second)
	defer client.CloseIdleConnections()
	probe := sourceHealthProbe{client: client, checkURL: safeMediaURL, resolveYQK: sourceHealthResolveYQK, resolveErciyuan: resolveErciyuanPlayback}
	failed, reason := probe.line(ctx, src, job.episode)
	// Use a fresh context for saving even when a bounded probe times out.
	saveCtx, saveCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer saveCancel()
	if _, current, checkErr := currentSourceHealthCandidate(saveCtx, job); checkErr != nil || !current {
		// Collection or an administrator may replace the URL during a slow
		// network check. Never quarantine a stale snapshot of that playlist.
		failed, reason = false, "播放配置已更新或无法核实，取消本次检测"
	}
	now = time.Now().Unix()
	rounds, hidden := sourceHealthOutcome(gconv.Int(state["fail_rounds"]), gconv.Int64(state["last_check"]), now, failed)
	if err = execSQL(saveCtx, "UPDATE sx_vod_source_health SET fail_rounds=?,last_check=?,checking_until=0,hidden_until=?,last_error=?,updated_at=? WHERE vod_id=? AND fingerprint=?", rounds, now, hidden, cutRunes(reason, 250), now, job.vodID, job.fingerprint); err != nil {
		g.Log().Warning(saveCtx, "CMS source check save:", err)
		return false
	}
	return failed && rounds == 1
}

var errSourceProbeUnsupported = errors.New("该播放格式无法可靠自动检测")
var errSourceProbeTransient = errors.New("播放源暂时连接失败，请稍后复查")

type sourceHealthProbe struct {
	client          *http.Client
	checkURL        func(context.Context, string) error
	resolveYQK      func(context.Context, string) (xq.CMSMedia, error)
	resolveErciyuan erciyuanMediaResolver
	startup         bool
}

type sourceHealthMediaEntry struct {
	media xq.CMSMedia
	until time.Time
}

var sourceHealthYQKCache = struct {
	sync.Mutex
	items map[string]sourceHealthMediaEntry
}{items: map[string]sourceHealthMediaEntry{}}

// A successful foreground resolve can be reused briefly by its failure check.
// Failed resolutions are never cached, and the second confirmation round must
// resolve afresh after the existing one-minute minimum interval.
func sourceHealthRememberYQK(marker string, media xq.CMSMedia) {
	if _, _, _, err := yqkMarkerParts(marker); err != nil || media.URL == "" {
		return
	}
	now := time.Now()
	sourceHealthYQKCache.Lock()
	defer sourceHealthYQKCache.Unlock()
	for key, entry := range sourceHealthYQKCache.items {
		if !now.Before(entry.until) {
			delete(sourceHealthYQKCache.items, key)
		}
	}
	if len(sourceHealthYQKCache.items) >= 128 {
		for key := range sourceHealthYQKCache.items {
			delete(sourceHealthYQKCache.items, key)
			break
		}
	}
	sourceHealthYQKCache.items[marker] = sourceHealthMediaEntry{media: media, until: now.Add(30 * time.Second)}
}

func sourceHealthResolveYQK(ctx context.Context, marker string) (xq.CMSMedia, error) {
	if err := ctx.Err(); err != nil {
		return xq.CMSMedia{}, err
	}
	sourceHealthYQKCache.Lock()
	entry, ok := sourceHealthYQKCache.items[marker]
	if ok && !time.Now().Before(entry.until) {
		delete(sourceHealthYQKCache.items, marker)
		ok = false
	}
	sourceHealthYQKCache.Unlock()
	if ok {
		return entry.media, nil
	}
	media, err := resolveYQK(ctx, marker)
	if err == nil && ctx.Err() == nil {
		sourceHealthRememberYQK(marker, media)
	}
	return media, err
}

func sourceHealthSample(src source, reported int) []string {
	indexes := []int{reported, 0, len(src.Episodes) - 1, len(src.Episodes) / 2}
	seen := map[string]bool{}
	result := make([]string, 0, 3)
	for _, index := range indexes {
		if index < 0 || index >= len(src.Episodes) {
			continue
		}
		raw := src.Episodes[index].URL
		if !seen[raw] {
			result = append(result, raw)
			seen[raw] = true
		}
		if len(result) == 3 {
			break
		}
	}
	return result
}

func (p sourceHealthProbe) line(ctx context.Context, src source, reported int) (bool, string) {
	if !eligibleSourceHealth(src) {
		return false, errSourceProbeUnsupported.Error()
	}
	var reasons []string
	for _, raw := range sourceHealthSample(src, reported) {
		if ctx.Err() != nil {
			return false, "本次线路检测未完成"
		}
		var err error
		if strings.HasPrefix(src.Code, "yqk_") {
			err = p.yqkMedia(ctx, raw)
		} else if strings.HasPrefix(src.Code, "ecy_") {
			err = p.erciyuanMedia(ctx, raw)
		} else {
			err = p.media(ctx, raw, 0, map[string]bool{})
		}
		if err == nil {
			// One working episode is enough to preserve the film's whole line.
			return false, ""
		}
		if errors.Is(err, errSourceProbeUnsupported) {
			return false, errSourceProbeUnsupported.Error()
		}
		reasons = append(reasons, err.Error())
	}
	if ctx.Err() != nil {
		return false, "本次线路检测未完成"
	}
	return len(reasons) > 0, strings.Join(reasons, "；")
}

func (p sourceHealthProbe) erciyuanMedia(ctx context.Context, marker string) error {
	if _, _, _, err := erciyuanCollectionMarker(marker); err != nil || p.resolveErciyuan == nil {
		return errSourceProbeUnsupported
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	media, headers, err := p.resolveErciyuan(ctx, marker)
	if err != nil || len(media.Key) > 0 || !safePlayerAddress(media.URL) {
		// API/parse failures (including rejection of an individual ad URL) do
		// not prove that every other episode of this film's line has failed.
		return errSourceProbeUnsupported
	}
	err = probeDiscoveryMediaHeaders(ctx, media, headers, p.client, p.checkURL)
	if ctx.Err() != nil || errors.Is(err, errSourceProbeUnsupported) || errors.Is(err, errSourceProbeTransient) {
		return errSourceProbeUnsupported
	}
	return err
}

func (p sourceHealthProbe) yqkMedia(ctx context.Context, marker string) error {
	if _, _, _, err := yqkMarkerParts(marker); err != nil || p.resolveYQK == nil {
		return errSourceProbeUnsupported
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	media, err := p.resolveYQK(ctx, marker)
	if err != nil {
		// The shared APP API being unavailable does not establish that this
		// film's media line has failed. Never persist upstream error details.
		return errSourceProbeUnsupported
	}
	if len(media.Variants) > 8 {
		return errSourceProbeUnsupported
	}
	variants := append([]xq.CMSMediaVariant{{URL: media.URL, Referer: media.Referer, Key: media.Key}}, media.Variants...)
	seen := map[string]bool{}
	inconclusive, tested := false, false
	for _, variant := range variants {
		if variant.URL == "" || seen[variant.URL] {
			continue
		}
		seen[variant.URL] = true
		if ctx.Err() != nil || len(variant.Key) > 0 || !safePlayerAddress(variant.URL) {
			inconclusive = true
			continue
		}
		client := *p.client
		transport := client.Transport
		if transport == nil {
			transport = http.DefaultTransport
		}
		referer := variant.Referer
		if referer == "" {
			referer = media.Referer
		}
		client.Transport = discoveryRefererTransport{base: transport, referer: referer}
		probe := sourceHealthProbe{client: &client, checkURL: p.checkURL}
		err := probe.media(ctx, variant.URL, 0, map[string]bool{})
		tested = true
		if err == nil {
			return nil
		}
		inconclusive = inconclusive || errors.Is(err, errSourceProbeUnsupported) || errors.Is(err, errSourceProbeTransient)
	}
	if ctx.Err() != nil || inconclusive || !tested {
		return errSourceProbeUnsupported
	}
	return errors.New("小柒该集所有可检测清晰度均不可用")
}

func (p sourceHealthProbe) read(ctx context.Context, raw string, limit int64, partial bool) ([]byte, *url.URL, error) {
	if err := p.checkURL(ctx, raw); err != nil {
		return nil, nil, errSourceProbeTransient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, nil, errors.New("播放地址无效")
	}
	if partial {
		req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", limit-1))
	}
	req.Header.Set("User-Agent", mediaUserAgent)
	response, err := p.client.Do(req)
	if err != nil {
		return nil, nil, errSourceProbeTransient
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
			return nil, nil, errSourceProbeTransient
		}
		return nil, nil, fmt.Errorf("播放源返回 HTTP %d", response.StatusCode)
	}
	readLimit := limit
	if !partial {
		readLimit++
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, readLimit))
	if err != nil {
		return nil, nil, errSourceProbeTransient
	}
	if len(body) == 0 {
		return nil, nil, errors.New("播放数据为空或读取失败")
	}
	if !partial && int64(len(body)) > limit {
		return nil, nil, errSourceProbeUnsupported
	}
	base := response.Request.URL
	return body, base, nil
}

func (p sourceHealthProbe) media(ctx context.Context, raw string, depth int, seen map[string]bool) error {
	if depth > 3 || seen[raw] {
		return errSourceProbeUnsupported
	}
	seen[raw] = true
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("播放地址无效")
	}
	if strings.HasSuffix(strings.ToLower(u.Path), ".mp4") {
		body, _, err := p.read(ctx, raw, 4096, true)
		if err != nil {
			return err
		}
		if len(body) >= 12 && string(body[4:8]) == "ftyp" {
			return nil
		}
		return errSourceProbeUnsupported
	}
	body, base, err := p.read(ctx, raw, 1<<20, false)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(strings.TrimPrefix(string(body), "\ufeff"))
	if !strings.HasPrefix(text, "#EXTM3U") {
		return errors.New("播放源未返回有效 HLS 清单")
	}
	resolve := func(value string) string {
		rel, err := url.Parse(value)
		if err != nil {
			return ""
		}
		return base.ResolveReference(rel).String()
	}
	var variants []string
	var segment, key, init string
	var variantNext, encrypted, unsupportedKey bool
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			variantNext = true
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-KEY:") && segment == "" {
			unsupportedKey = !strings.Contains(line, "METHOD=AES-128") && !strings.Contains(line, "METHOD=NONE")
			encrypted = strings.Contains(line, "METHOD=AES-128")
			key = ""
			if m := hlsURI.FindStringSubmatch(line); encrypted && len(m) == 2 {
				key = resolve(m[1])
			}
		}
		if strings.HasPrefix(line, "#EXT-X-MAP:") && segment == "" {
			if m := hlsURI.FindStringSubmatch(line); len(m) == 2 {
				init = resolve(m[1])
			}
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if variantNext {
			variants = append(variants, resolve(line))
			variantNext = false
		} else if segment == "" {
			segment = resolve(line)
		}
	}
	if len(variants) > 0 {
		if len(variants) > 16 {
			return errSourceProbeUnsupported
		}
		inconclusive := false
		for _, variant := range variants {
			// Each alternative gets its own path set (shared renditions are valid).
			path := make(map[string]bool, len(seen))
			for value, visited := range seen {
				path[value] = visited
			}
			err := p.media(ctx, variant, depth+1, path)
			if err == nil {
				return nil
			}
			inconclusive = inconclusive || errors.Is(err, errSourceProbeUnsupported) || errors.Is(err, errSourceProbeTransient)
			if ctx.Err() != nil {
				return errSourceProbeUnsupported
			}
		}
		if inconclusive {
			return errSourceProbeUnsupported
		}
		return errors.New("全部 HLS 清晰度均不可用")
	}
	if unsupportedKey || (encrypted && key == "") {
		return errSourceProbeUnsupported
	}
	if segment == "" {
		return errors.New("HLS 清单没有可播放分片")
	}
	if p.startup {
		// First playback validates the manifest and every referenced media
		// address, then lets the authorized player fetch key/init/segments.
		// Full candidate/failure probes continue to verify actual bytes below.
		for _, address := range []string{segment, key, init} {
			if address != "" {
				if err := p.checkURL(ctx, address); err != nil {
					return errors.New("HLS 清单引用了不可用的媒体地址")
				}
			}
		}
		return nil
	}
	if encrypted {
		keyData, _, err := p.read(ctx, key, 16, false)
		if err != nil {
			return err
		}
		if len(keyData) != 16 {
			return errors.New("HLS 密钥长度无效")
		}
	}
	if init != "" {
		if _, _, err = p.read(ctx, init, 4096, true); err != nil {
			return err
		}
	}
	data, _, err := p.read(ctx, segment, 4096, true)
	if err != nil {
		return err
	}
	trimmed := bytes.TrimSpace(data)
	if bytes.HasPrefix(trimmed, []byte("<")) || bytes.HasPrefix(trimmed, []byte("{")) {
		return errors.New("HLS 分片返回了错误页面")
	}
	if len(data) >= 188 && encrypted {
		return nil
	}
	// Match the proxy's conservative PNG/TS recognition. Reading a partial
	// representation for a probe does not change the offsets returned to clients.
	if payload, wrapped := unwrapPNGTransportStream(bytes.NewReader(data)); wrapped {
		data, _ = io.ReadAll(payload)
	}
	if len(data) > 188 && data[0] == 0x47 && data[188] == 0x47 {
		return nil
	}
	if len(data) >= 12 {
		switch string(data[4:8]) {
		case "ftyp", "styp", "moof", "sidx", "mdat":
			return nil
		}
	}
	return errSourceProbeUnsupported
}
