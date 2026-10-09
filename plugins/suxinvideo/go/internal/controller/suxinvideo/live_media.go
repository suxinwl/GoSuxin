package suxinvideo

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
)

type LiveQuality struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}
type LiveProbe struct {
	URL       string
	Qualities []LiveQuality
	LatencyMS int64
	Quality   string
}
type liveProbeMemo struct {
	Probe LiveProbe
	At    time.Time
}

var liveProbeMemos = struct {
	sync.Mutex
	Items map[string]liveProbeMemo
}{Items: map[string]liveProbeMemo{}}

var liveHTTPClient = safeCollectorHTTPClient(12 * time.Second)
var liveAttribute = regexp.MustCompile(`([A-Z0-9-]+)=("[^"]*"|[^,]*)`)

func liveAttributes(line string) map[string]string {
	out := map[string]string{}
	for _, part := range liveAttribute.FindAllStringSubmatch(line, -1) {
		out[part[1]] = strings.Trim(part[2], `"`)
	}
	return out
}

// Credentials belong to the source's origin. Never forward them to an unrelated
// host named by an imported playlist; ordinary UA/Referer headers may follow CDNs.
func liveRequest(ctx context.Context, stream LiveStream, raw string, byteRange string) (*http.Response, error) {
	return liveRequestWithBudget(ctx, stream, raw, byteRange, 0)
}

// A full broadcast fragment may take longer than the small detection sample.
// Reuse the transport and address policy while bounding the entire media read.
func liveRequestWithBudget(ctx context.Context, stream LiveStream, raw string, byteRange string, budget time.Duration) (*http.Response, error) {
	if liveEngineMediaURL(stream, raw) {
		return liveEngineRequest(ctx, stream, raw, byteRange, budget)
	}
	if err := safeCollectorURL(ctx, raw); err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", mediaUserAgent)
	origin, _ := url.Parse(stream.URL)
	for key, value := range stream.Headers {
		key = http.CanonicalHeaderKey(key)
		if strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("直播请求头无效")
		}
		switch key {
		case "User-Agent", "Referer", "Origin", "Accept", "Accept-Language":
			request.Header.Set(key, value)
		case "Authorization", "Cookie", "X-Token", "X-Api-Key":
			if origin != nil && strings.EqualFold(origin.Host, request.URL.Host) {
				request.Header.Set(key, value)
			}
		}
	}
	if byteRange != "" {
		request.Header.Set("Range", byteRange)
	}
	client := *liveHTTPClient
	if budget > 0 {
		client.Timeout = budget
	}
	redirect := client.CheckRedirect
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) > 0 && !strings.EqualFold(via[0].URL.Host, next.URL.Host) {
			next.Header.Del("Authorization")
			next.Header.Del("Cookie")
			next.Header.Del("X-Token")
			next.Header.Del("X-Api-Key")
		}
		if redirect != nil {
			return redirect(next, via)
		}
		return safeCollectorURL(next.Context(), next.URL.String())
	}
	return client.Do(request)
}

func liveManifest(ctx context.Context, stream LiveStream, raw string) (string, string, error) {
	response, err := liveRequest(ctx, stream, raw, "")
	if err != nil {
		return "", "", fmt.Errorf("直播清单请求超时或失败")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("直播清单返回 HTTP %d", response.StatusCode)
	}
	return readMediaPlaylist(response)
}

func liveVariants(body, base string) []LiveQuality {
	uri, _ := url.Parse(base)
	result := []LiveQuality{}
	var attrs map[string]string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			attrs = liveAttributes(line)
			continue
		}
		if attrs == nil || line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		relative, err := url.Parse(line)
		if err != nil {
			attrs = nil
			continue
		}
		w, h := 0, 0
		parts := strings.Split(attrs["RESOLUTION"], "x")
		if len(parts) == 2 {
			w, _ = strconv.Atoi(parts[0])
			h, _ = strconv.Atoi(parts[1])
		}
		label := attrs["NAME"]
		if h > 0 {
			label = strconv.Itoa(h) + "p"
		}
		if label == "" {
			label = "原始清晰度"
		}
		result = append(result, LiveQuality{ID: strconv.Itoa(len(result) + 1), Label: label, URL: uri.ResolveReference(relative).String(), Width: w, Height: h})
		attrs = nil
		if len(result) >= 20 {
			break
		}
	}
	return result
}

// Probe one playable variant, its key/init (when present) and one media fragment.
// Metadata alone must not promote an HTML error page to a working TV channel.
func liveProbeStream(ctx context.Context, stream LiveStream) (LiveProbe, error) {
	if stream.SourceKind == "provider" && stream.MediaType == "flv" {
		raw, err := liveBridgeAcquire(ctx, stream, liveViewerFromContext(ctx))
		if err != nil {
			return LiveProbe{}, err
		}
		return LiveProbe{URL: raw, Quality: stream.Quality}, nil
	}
	if stream.MediaType == "mp4" {
		response, err := liveRequest(ctx, stream, stream.URL, "bytes=0-8191")
		if err != nil {
			return LiveProbe{}, err
		}
		defer response.Body.Close()
		sample, err := io.ReadAll(io.LimitReader(response.Body, 8192))
		if err != nil || (response.StatusCode != 200 && response.StatusCode != 206) || liveMediaContentType("", sample) != "video/mp4" {
			return LiveProbe{}, fmt.Errorf("回放视频无法读取")
		}
		return LiveProbe{URL: stream.URL, Quality: stream.Quality}, nil
	}
	began := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()
	root, base, err := liveManifest(ctx, stream, stream.URL)
	if err != nil {
		return LiveProbe{}, err
	}
	result := LiveProbe{URL: base, Qualities: liveVariants(root, base), Quality: "原始清晰度"}
	body := root
	for depth := 0; strings.Contains(body, "#EXT-X-STREAM-INF:"); depth++ {
		if depth >= 3 {
			return LiveProbe{}, fmt.Errorf("直播清单嵌套过深")
		}
		variants := liveVariants(body, base)
		if len(variants) == 0 {
			return LiveProbe{}, fmt.Errorf("直播没有可用清晰度")
		}
		result.Quality = variants[0].Label
		body, base, err = liveManifest(ctx, stream, variants[0].URL)
		if err != nil {
			return LiveProbe{}, err
		}
	}
	if !strings.Contains(body, "#EXTINF:") {
		return LiveProbe{}, fmt.Errorf("直播清单没有媒体分片")
	}
	if strings.Contains(body, "#EXT-X-PART:") {
		return LiveProbe{}, fmt.Errorf("首版暂不支持低延迟分片直播")
	}
	parsed, _ := url.Parse(base)
	fragment := ""
	checks := []string{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXT-X-KEY:") {
			attrs := liveAttributes(line)
			if attrs["METHOD"] != "NONE" && (attrs["METHOD"] != "AES-128" || (attrs["KEYFORMAT"] != "" && attrs["KEYFORMAT"] != "identity")) {
				return LiveProbe{}, fmt.Errorf("不支持该直播加密格式")
			}
			if attrs["METHOD"] == "AES-128" {
				if attrs["URI"] == "" {
					return LiveProbe{}, fmt.Errorf("直播密钥地址缺失")
				}
				checks = append(checks, "key:"+attrs["URI"])
			}
		}
		if strings.HasPrefix(line, "#EXT-X-MAP:") {
			a := liveAttributes(line)
			if a["URI"] != "" {
				checks = append(checks, "init:"+a["URI"])
			}
		}
		if line != "" && !strings.HasPrefix(line, "#") && fragment == "" {
			fragment = line
		}
	}
	if fragment == "" {
		return LiveProbe{}, fmt.Errorf("直播分片地址缺失")
	}
	checks = append(checks, "media:"+fragment)
	if len(checks) > 32 {
		return LiveProbe{}, fmt.Errorf("直播清单引用过多")
	}
	for _, part := range checks {
		kind, raw, _ := strings.Cut(part, ":")
		relative, err := url.Parse(raw)
		if err != nil {
			return LiveProbe{}, fmt.Errorf("直播媒体地址无效")
		}
		response, err := liveRequest(ctx, stream, parsed.ResolveReference(relative).String(), "")
		if err != nil {
			return LiveProbe{}, fmt.Errorf("直播媒体分片或密钥请求失败")
		}
		sample, readErr := io.ReadAll(io.LimitReader(response.Body, 8193))
		response.Body.Close()
		if readErr != nil || response.StatusCode < 200 || response.StatusCode >= 300 || len(sample) == 0 {
			return LiveProbe{}, fmt.Errorf("直播媒体分片或密钥不可用")
		}
		if kind == "key" && len(sample) != 16 {
			return LiveProbe{}, fmt.Errorf("直播 AES 密钥长度无效")
		}
		lower := strings.ToLower(strings.TrimSpace(string(sample[:min(len(sample), 200)])))
		if strings.HasPrefix(lower, "<html") || strings.HasPrefix(lower, "<!doctype") || strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/html") {
			return LiveProbe{}, fmt.Errorf("直播源返回错误页面")
		}
	}
	if len(result.Qualities) == 0 {
		result.Qualities = []LiveQuality{{ID: "auto", Label: "原始清晰度", URL: result.URL}}
	}
	result.LatencyMS = time.Since(began).Milliseconds()
	liveProbeMemos.Lock()
	if len(liveProbeMemos.Items) >= 1024 {
		for key, memo := range liveProbeMemos.Items {
			if time.Since(memo.At) > 2*time.Minute {
				delete(liveProbeMemos.Items, key)
			}
		}
	}
	if len(liveProbeMemos.Items) < 1024 {
		liveProbeMemos.Items[liveProbeMemoKey(stream)] = liveProbeMemo{Probe: result, At: time.Now()}
	}
	liveProbeMemos.Unlock()
	return result, nil
}

func liveProbeMemoKey(stream LiveStream) string {
	keys := make([]string, 0, len(stream.Headers))
	for key := range stream.Headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	raw := stream.URL
	for _, key := range keys {
		raw += "\n" + key + ":" + stream.Headers[key]
	}
	hash := sha256.Sum256([]byte(raw))
	return strconv.FormatInt(stream.ID, 10) + ":" + hex.EncodeToString(hash[:])
}
func livePlaybackProbe(ctx context.Context, stream LiveStream) (LiveProbe, error) {
	liveProbeMemos.Lock()
	memo, ok := liveProbeMemos.Items[liveProbeMemoKey(stream)]
	liveProbeMemos.Unlock()
	if ok && stream.Health == "healthy" && time.Since(memo.At) < 2*time.Minute {
		return memo.Probe, nil
	}
	return liveProbeStream(ctx, stream)
}

type liveAsset struct {
	URL       string
	Seen      time.Time
	Permanent bool
}
type liveMediaSession struct {
	mu                  sync.Mutex
	ChannelID, StreamID int64
	SourceURL           string
	Expires             time.Time
	Assets              map[string]liveAsset
	Viewer              LiveViewer
	RuntimeStream       LiveStream
	SourceRevision      int64
	Mode                string
	ProgrammeID         int64
	SourceExpire        int64
}

var liveSessions = struct {
	sync.Mutex
	Items map[string]*liveMediaSession
}{Items: map[string]*liveMediaSession{}}

const liveSessionLifetime = 2 * time.Hour

func (s *liveMediaSession) link(token, raw string, permanent bool) string {
	sum := sha256.Sum256([]byte(raw))
	id := hex.EncodeToString(sum[:12])
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	finite := s.Mode == "catchup" || s.Mode == "event_replay"
	if !finite && len(s.Assets) > 128 {
		for key, item := range s.Assets {
			if !item.Permanent && now.Sub(item.Seen) > 10*time.Minute {
				delete(s.Assets, key)
			}
		}
	}
	if existing, ok := s.Assets[id]; ok && existing.Permanent {
		permanent = true
	}
	// A pathological playlist cannot consume unlimited memory in a long session.
	limit := 2048
	if finite {
		// Finite multi-hour matches can contain thousands of fragments. Keep the
		// complete supported manifest seekable for the entire session lifetime.
		limit = 50000
	}
	if len(s.Assets) >= limit {
		oldest := ""
		at := now
		for key, item := range s.Assets {
			if !item.Permanent && item.Seen.Before(at) {
				oldest, at = key, item.Seen
			}
		}
		if oldest != "" {
			delete(s.Assets, oldest)
		}
	}
	s.Assets[id] = liveAsset{URL: raw, Seen: now, Permanent: permanent}
	return "/suxinvideo/live/media?session=" + url.QueryEscape(token) + "&asset=" + id
}
func newLiveSession(channelID, streamID int64) (string, *liveMediaSession, error) {
	token, err := appToken()
	if err != nil {
		return "", nil, err
	}
	now := time.Now()
	liveSessions.Lock()
	defer liveSessions.Unlock()
	for key, item := range liveSessions.Items {
		item.mu.Lock()
		expired := now.After(item.Expires)
		item.mu.Unlock()
		if expired {
			delete(liveSessions.Items, key)
		}
	}
	if len(liveSessions.Items) >= 2048 {
		return "", nil, appError(503, "直播会话已满，请稍后重试")
	}
	item := &liveMediaSession{ChannelID: channelID, StreamID: streamID, Expires: now.Add(liveSessionLifetime), Assets: map[string]liveAsset{}}
	liveSessions.Items[token] = item
	return token, item, nil
}
func getLiveSession(token string) (*liveMediaSession, error) {
	liveSessions.Lock()
	item := liveSessions.Items[token]
	liveSessions.Unlock()
	if item == nil {
		return nil, appError(403, "直播会话已失效，请重新连接")
	}
	item.mu.Lock()
	expired := time.Now().After(item.Expires)
	item.mu.Unlock()
	if expired {
		return nil, appError(403, "直播会话已过期，请重新连接")
	}
	return item, nil
}
func checkLiveSession(ctx context.Context, item *liveMediaSession) (LiveStream, error) {
	channel, err := liveChannel(ctx, item.ChannelID)
	if err != nil || !channel.Enabled {
		return LiveStream{}, appError(403, "直播频道已停用")
	}
	stream, err := liveStream(ctx, item.StreamID)
	if err != nil || !stream.Enabled || stream.ChannelID != item.ChannelID {
		return LiveStream{}, appError(403, "直播线路已停用")
	}
	if err := liveStreamAccess(ctx, stream, item.Viewer); err != nil {
		return LiveStream{}, err
	}
	if stream.SourceKind == "provider" {
		if stream.SourceRevision != item.SourceRevision {
			return LiveStream{}, appError(403, "直播账号或来源配置已更新，请重新连接")
		}
		item.mu.Lock()
		runtime := item.RuntimeStream
		item.mu.Unlock()
		return runtime, nil
	}
	if item.SourceURL != "" && stream.URL != item.SourceURL {
		return LiveStream{}, appError(403, "直播线路已更新，请重新连接")
	}
	return stream, nil
}

func liveMediaHandler(r *ghttp.Request) {
	r.Response.Header().Set("Cache-Control", "private, no-store")
	r.Response.Header().Set("X-Content-Type-Options", "nosniff")
	item, err := getLiveSession(r.Get("session").String())
	if err != nil {
		appWrite(r, nil, err)
		return
	}
	stream, err := checkLiveSession(r.Context(), item)
	if err != nil {
		appWrite(r, nil, err)
		return
	}
	id := r.Get("asset").String()
	item.mu.Lock()
	asset, ok := item.Assets[id]
	if ok {
		asset.Seen = time.Now()
		item.Assets[id] = asset
	}
	item.mu.Unlock()
	if !ok {
		appWrite(r, nil, appError(403, "直播媒体授权无效"))
		return
	}
	if strings.HasPrefix(asset.URL, "livebridge://") {
		body, name, err := liveBridgeRead(asset.URL)
		if err != nil {
			appWrite(r, nil, err)
			return
		}
		if strings.HasSuffix(name, ".m3u8") {
			r.Response.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			r.Response.Write(rewritePlaylist(string(body), asset.URL, func(raw string) string { return item.link(r.Get("session").String(), raw, false) }))
		} else {
			r.Response.Header().Set("Content-Type", "video/mp2t")
			r.Response.Write(body)
		}
		return
	}
	if item.Mode != "" && item.Mode != "live" && stream.MediaType == "mp4" {
		liveFiniteMedia(r, item, stream, asset.URL)
		return
	}
	response, err := liveRequestWithBudget(r.Context(), stream, asset.URL, r.Header.Get("Range"), 30*time.Second)
	if err != nil {
		appWrite(r, nil, appError(502, "直播源连接失败，请重试或换线"))
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		appWrite(r, nil, appError(502, "直播源暂时不可用，请重试或换线"))
		return
	}
	ct := response.Header.Get("Content-Type")
	if ct != "" {
		r.Response.Header().Set("Content-Type", ct)
	}
	reader := bufio.NewReader(response.Body)
	header, _ := reader.Peek(16)
	manifestPrefix := strings.TrimSpace(strings.TrimPrefix(string(header), "\ufeff"))
	if isMediaPlaylist(ct, response.Request.URL.String()) || isMediaPlaylist("", asset.URL) || strings.HasPrefix(manifestPrefix, "#EXTM3U") {
		response.Body = io.NopCloser(reader)
		body, base, err := readMediaPlaylist(response)
		if err != nil {
			appWrite(r, nil, appError(502, "直播清单无效"))
			return
		}
		// Preserve the moving live window, sequence numbers and discontinuities.
		r.Response.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		token := r.Get("session").String()
		r.Response.Write(rewritePlaylist(body, base, func(raw string) string { return item.link(token, raw, false) }))
		return
	}
	if v := response.Header.Get("Content-Range"); v != "" {
		r.Response.Header().Set("Content-Range", v)
	}
	if v := response.Header.Get("Accept-Ranges"); v != "" {
		r.Response.Header().Set("Accept-Ranges", v)
	}
	body, err := io.ReadAll(io.LimitReader(reader, (64<<20)+1))
	if err != nil || len(body) == 0 || len(body) > 64<<20 {
		appWrite(r, nil, appError(502, "直播分片读取失败"))
		return
	}
	// Some public HLS origins label transport-stream fragments as JPEG files.
	// Preserve the exact bytes and Range response, but describe actual media so
	// native extractors do not infer an image format from the origin header.
	r.Response.Header().Set("Content-Type", liveMediaContentType(ct, body))
	if response.StatusCode == http.StatusPartialContent {
		r.Response.WriteHeader(http.StatusPartialContent)
	}
	r.Response.Write(body)
}

func liveMediaContentType(upstream string, body []byte) string {
	// Require five MPEG-TS sync bytes at 188-byte intervals. Range requests may
	// start inside a packet, so inspect at most one packet of possible offsets.
	for offset := 0; offset < 188 && offset+4*188 < len(body); offset++ {
		ts := true
		for packet := 0; packet < 5; packet++ {
			if body[offset+packet*188] != 0x47 {
				ts = false
				break
			}
		}
		if ts {
			return "video/mp2t"
		}
	}
	if len(body) >= 16 {
		switch string(body[4:8]) {
		case "ftyp", "styp", "moof", "sidx", "moov", "mdat":
			return "video/mp4"
		}
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(upstream, ";")[0]))
	if mediaType == "" || strings.HasPrefix(mediaType, "image/") || strings.HasPrefix(mediaType, "text/") {
		// This also leaves standard AES-128 keys and encrypted fragments opaque;
		// the client decrypts them using the unchanged playlist and payload.
		return "application/octet-stream"
	}
	return upstream
}
