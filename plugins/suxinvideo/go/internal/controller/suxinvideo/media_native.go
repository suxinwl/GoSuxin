package suxinvideo

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	xq "github.com/suxinwl/GoSuxin/internal/xiaoqiapp"
	"github.com/suxinwl/GoSuxin/utility/gf"
)

type NativeResolveReq struct {
	g.Meta  `path:"/native/resolve" method:"get" noValApi:"1"`
	VodID   int64  `p:"vod_id"`
	Source  int    `p:"source"`
	Line    string `p:"line"`
	Episode int    `p:"episode"`
}
type NativeResolveRes struct{}

type NativeProxyReq struct {
	g.Meta `path:"/native/proxy" method:"get" noValApi:"1"`
	Token  string `p:"token"`
	URL    string `p:"url"`
	Sig    string `p:"sig"`
}
type NativeProxyRes struct{}

type NativeStreamReq struct {
	g.Meta `path:"/native/stream" method:"get" noValApi:"1"`
	Token  string  `p:"token"`
	Start  float64 `p:"start"`
}
type NativeStreamRes struct{}

type nativeSession struct {
	VodID   int64
	Source  int
	Line    string
	Episode int
	Media   xq.CMSMedia
	Headers map[string]string
	Expires time.Time
	Background bool
}

func native4KVMSlug(marker string) (string, bool) {
	if !strings.HasPrefix(marker, "4kvm://") {
		return "", false
	}
	slug, _, _ := strings.Cut(strings.TrimPrefix(marker, "4kvm://"), "?")
	if slug == "" || len(slug) > 64 {
		return "", false
	}
	for _, char := range slug {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_') {
			return "", false
		}
	}
	return slug, true
}

var nativePlayback = struct {
	sync.Mutex
	items map[string]nativeSession
}{items: map[string]nativeSession{}}

// Keep the actual player code in the session: playlist positions may change
// when another collector is disabled, and YQK has several independent players.
func nativeYQKSeries(code, marker string) (string, error) {
	vodID, kind, _, err := yqkMarkerParts(marker)
	if err != nil || code != yqkPlayerCode(kind) {
		return "", errors.New("小柒播放线路与分集不匹配")
	}
	return vodID, nil
}

func nativePlaybackItem(ctx context.Context, vodID int64, sourceIndex, episodeIndex int, sourceCode ...string) (string, string, string, error) {
	if err := AppAuthorizeGrantVod(ctx, vodID); err != nil {
		return "", "", "", err
	}
	if vodID < 1 || sourceIndex < 0 || episodeIndex < 0 {
		return "", "", "", errors.New("播放参数无效")
	}
	vod, err := one(ctx, "SELECT id,name,class,type_id,api_id,api_vid,play_from,play_url,vip,points,status FROM sx_vod WHERE id=?", vodID)
	if err != nil {
		return "", "", "", err
	}
	if vod == nil || gconv.Int(vod["status"]) != 1 || !contentVodAllowed(ctx, vod) {
		return "", "", "", errors.New("影片不存在或已下架")
	}
	user, err := currentUser(ctx)
	if err != nil {
		return "", "", "", err
	}
	if gconv.Int(vod["vip"]) == 1 && (user == nil || gconv.Int64(user["vip_expire"]) <= time.Now().Unix()) {
		return "", "", "", errors.New("需要有效会员资格")
	}
	if gconv.Int(vod["points"]) > 0 {
		if user == nil {
			return "", "", "", errors.New("请先登录并解锁影片")
		}
		owned, err := one(ctx, "SELECT id FROM sx_user_vod WHERE user_id=? AND vod_id=?", user["id"], vodID)
		if err != nil {
			return "", "", "", err
		}
		if owned == nil {
			return "", "", "", errors.New("请先解锁影片")
		}
	}
	sources, err := hydratePlayers(ctx, vod, playlist(vod))
	if err != nil {
		return "", "", "", err
	}
	if len(sourceCode) > 0 && sourceCode[0] != "" {
		sourceIndex = -1
		for i, src := range sources {
			if src.Code == sourceCode[0] {
				sourceIndex = i
				break
			}
		}
	}
	if sourceIndex < 0 || sourceIndex >= len(sources) || episodeIndex >= len(sources[sourceIndex].Episodes) {
		return "", "", "", errors.New("分集不存在")
	}
	marker := sources[sourceIndex].Episodes[episodeIndex].URL
	provider := sources[sourceIndex].Code
	if strings.HasPrefix(provider, "yqk_") {
		seriesID, parseErr := nativeYQKSeries(provider, marker)
		if parseErr != nil {
			return "", "", "", parseErr
		}
		return provider, seriesID, marker, nil
	}
	if strings.HasPrefix(provider, "ecy_") {
		seriesID, parseErr := nativeErciyuanSeries(provider, marker)
		if parseErr != nil {
			return "", "", "", parseErr
		}
		return provider, seriesID, marker, nil
	}
	if provider == "hongguo" {
		u, parseErr := url.Parse(marker)
		if parseErr != nil {
			return "", "", "", errors.New("红果播放地址无效")
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if u.Scheme != "hongguo" || len(parts) != 1 || !onlyDigits(u.Host) || !onlyDigits(parts[0]) {
			return "", "", "", errors.New("红果播放地址无效")
		}
		return provider, u.Host, "hongguo-cenc://" + parts[0], nil
	}
	if provider == "4kvm" && strings.HasPrefix(marker, "4kvm://") {
		if slug, ok := native4KVMSlug(marker); ok {
			return provider, slug, marker, nil
		}
	}
	return "", "", "", errors.New("分集不属于专用片源")
}

func newNativeSession(item nativeSession) (string, error) {
	var secret [24]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(secret[:])
	nativePlayback.Lock()
	defer nativePlayback.Unlock()
	for key, current := range nativePlayback.items {
		if time.Now().After(current.Expires) {
			delete(nativePlayback.items, key)
		}
	}
	if len(nativePlayback.items) >= 1000 {
		return "", errors.New("播放会话已满，请稍后重试")
	}
	nativePlayback.items[token] = item
	return token, nil
}

func getNativeSession(ctx context.Context, token string) (nativeSession, error) {
	if len(token) != 48 {
		return nativeSession{}, errors.New("播放凭据无效")
	}
	nativePlayback.Lock()
	item, found := nativePlayback.items[token]
	nativePlayback.Unlock()
	if !found || time.Now().After(item.Expires) {
		return nativeSession{}, errors.New("播放凭据已失效，请刷新播放页")
	}
	if _, _, _, err := nativePlaybackItem(ctx, item.VodID, item.Source, item.Episode, item.Line); err != nil {
		return nativeSession{}, err
	}
	return item, nil
}

func nativeProxyLink(ctx context.Context, token, address string, expires time.Time) string {
	sig := signProxy(ctx, token+"|"+address, expires.Unix())
	return appMediaGrantLink(ctx, "/suxinvideo/native/proxy?token="+url.QueryEscape(token)+"&url="+url.QueryEscape(address)+"&sig="+sig)
}

func nativePlaybackURL(ctx context.Context, token string, media xq.CMSMedia, expires time.Time) (string, string) {
	if len(media.Key) > 0 {
		return "/suxinvideo/native/stream?token=" + token, "mp4"
	}
	kind := "mp4"
	if strings.HasSuffix(strings.ToLower(strings.Split(media.URL, "?")[0]), ".m3u8") {
		kind = "m3u8"
	}
	return nativeProxyLink(ctx, token, media.URL, expires), kind
}

func (*Media) NativeResolve(ctx context.Context, req *NativeResolveReq) (*NativeResolveRes, error) {
	r := g.RequestFromCtx(ctx)
	r.Response.Header().Set("Cache-Control", "private, no-store")
	provider, seriesID, marker, err := nativePlaybackItem(ctx, req.VodID, req.Source, req.Episode, req.Line)
	if err != nil {
		badRequest(r, err.Error())
		return &NativeResolveRes{}, nil
	}
	var media xq.CMSMedia
	var mediaHeaders map[string]string
	if strings.HasPrefix(provider, "yqk_") {
		media, err = resolveYQK(ctx, marker)
	} else if strings.HasPrefix(provider, "ecy_") {
		media, mediaHeaders, err = resolveErciyuanPlayback(ctx, marker)
	} else {
		media, err = cmsProviders.Resolve(ctx, provider, seriesID, marker)
	}
	if err != nil {
		badRequest(r, err.Error())
		return &NativeResolveRes{}, nil
	}
	// Source selection cancels losing candidates. Their upstream responses must
	// not allocate two-hour sessions after the viewer has already switched away.
	if ctx.Err() != nil {
		return &NativeResolveRes{}, nil
	}
	if err := safeCollectorURL(ctx, media.URL); err != nil {
		badRequest(r, "片源返回了不安全的媒体地址")
		return &NativeResolveRes{}, nil
	}
	if strings.HasPrefix(provider, "yqk_") {
		sourceHealthRememberYQK(marker, media)
	}
	variants := media.Variants
	media.Variants = nil
	session := nativeSession{VodID: req.VodID, Source: req.Source, Line: provider, Episode: req.Episode, Media: media, Headers: mediaHeaders, Expires: time.Now().Add(2 * time.Hour)}
	token, err := newNativeSession(session)
	if err != nil {
		return nil, err
	}
	playURL, kind := nativePlaybackURL(ctx, token, media, session.Expires)
	qualities := make([]map[string]any, 0, len(variants))
	seen := map[string]bool{}
	for _, variant := range variants {
		if len(qualities) >= 8 || seen[variant.URL] || safeCollectorURL(ctx, variant.URL) != nil {
			continue
		}
		seen[variant.URL] = true
		choice := xq.CMSMedia{URL: variant.URL, Referer: variant.Referer, Key: variant.Key, Quality: variant.Quality, Duration: variant.Duration}
		choiceToken := token
		if choice.URL != media.URL || !bytes.Equal(choice.Key, media.Key) {
			choiceToken, err = newNativeSession(nativeSession{VodID: req.VodID, Source: req.Source, Line: provider, Episode: req.Episode, Media: choice, Expires: session.Expires})
			if err != nil {
				break
			}
		}
		choiceURL, choiceType := nativePlaybackURL(ctx, choiceToken, choice, session.Expires)
		label := strings.TrimSpace(variant.Label)
		if variant.Quality > 0 {
			label = strconv.Itoa(variant.Quality) + "P"
		}
		if label == "" {
			label = fmt.Sprintf("清晰度 %d", len(qualities)+1)
		}
		qualities = append(qualities, map[string]any{"label": label, "url": choiceURL, "type": choiceType, "duration": choice.Duration.Seconds(), "selected": choiceToken == token})
	}
	if len(qualities) > 0 && !seen[media.URL] {
		qualities = append([]map[string]any{{"label": "当前清晰度", "url": playURL, "type": kind, "duration": media.Duration.Seconds(), "selected": true}}, qualities...)
	}
	if len(qualities) < 2 {
		qualities = nil
	}
	r.Response.WriteJson(gf.Success().SetData(map[string]any{"url": playURL, "type": kind, "duration": media.Duration.Seconds(), "qualities": qualities}))
	return &NativeResolveRes{}, nil
}

func (*Media) NativeProxy(ctx context.Context, req *NativeProxyReq) (*NativeProxyRes, error) {
	r := g.RequestFromCtx(ctx)
	session, err := getNativeSession(ctx, req.Token)
	if err != nil || !hmac.Equal([]byte(req.Sig), []byte(signProxy(ctx, req.Token+"|"+req.URL, session.Expires.Unix()))) {
		r.Response.WriteStatus(http.StatusForbidden)
		return &NativeProxyRes{}, nil
	}
	if err := safeCollectorURL(ctx, req.URL); err != nil {
		badRequest(r, "媒体地址无效")
		return &NativeProxyRes{}, nil
	}
	upstream, err := http.NewRequestWithContext(ctx, http.MethodGet, req.URL, nil)
	if err != nil {
		return nil, err
	}
	upstream.Header.Set("User-Agent", mediaUserAgent)
	applyNativeMediaHeaders(upstream, session.Headers)
	applyMediaReferer(upstream, session.Media.Referer)
	if rangeValue := r.Header.Get("Range"); rangeValue != "" {
		upstream.Header.Set("Range", rangeValue)
	}
	response, err := playbackHTTPClient.Do(upstream)
	if err != nil {
		r.Response.WriteStatus(http.StatusBadGateway, "媒体请求失败")
		return &NativeProxyRes{}, nil
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		r.Response.WriteStatus(http.StatusBadGateway, "源站媒体不可用")
		return &NativeProxyRes{}, nil
	}
	contentType := response.Header.Get("Content-Type")
	if contentType != "" {
		r.Response.Header().Set("Content-Type", contentType)
	}
	r.Response.Header().Set("Cache-Control", "private, no-store")
	if isMediaPlaylist(contentType, response.Request.URL.String()) || isMediaPlaylist("", req.URL) {
		body, base, err := readMediaPlaylist(response)
		if err != nil {
			r.Response.WriteStatus(http.StatusBadGateway, err.Error())
			return &NativeProxyRes{}, nil
		}
		r.Response.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		filtered := filterMediaPlaylist(ctx, body, base)
		if strings.HasPrefix(session.Line, "ecy_") {
			filtered = filterErciyuanMediaPlaylist(ctx, body, base, session.Headers)
		}
		mediaAdFilterHeaders(r.Response.Header(), filtered)
		r.Response.Write(rewritePlaylist(filtered.Playlist, base, func(address string) string {
			return nativeProxyLink(ctx, req.Token, address, session.Expires)
		}))
		return &NativeProxyRes{}, nil
	}
	if value := response.Header.Get("Content-Range"); value != "" {
		r.Response.Header().Set("Content-Range", value)
	}
	_, copyErr := copyMediaBinary(r.Response, response.StatusCode, io.LimitReader(response.Body, 4<<30))
	if copyErr != nil {
		g.Log().Warning(ctx, "Native media proxy:", copyErr)
	}
	r.ExitAll()
	return &NativeProxyRes{}, nil
}

func ffmpegBinary() (string, error) {
	if value := os.Getenv("SUXIN_FFMPEG"); value != "" {
		return value, nil
	}
	if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
		local := filepath.Join("resource", "static", "suxinvideo", "bin", "windows-amd64", "ffmpeg.exe")
		if _, err := os.Stat(local); err == nil {
			return local, nil
		}
	}
	return exec.LookPath("ffmpeg")
}

func (*Media) NativeStream(ctx context.Context, req *NativeStreamReq) (*NativeStreamRes, error) {
	r := g.RequestFromCtx(ctx)
	session, err := getNativeSession(ctx, req.Token)
	if err != nil || len(session.Media.Key) != 16 {
		r.Response.WriteStatus(http.StatusForbidden)
		return &NativeStreamRes{}, nil
	}
	if err := safeCollectorURL(ctx, session.Media.URL); err != nil {
		badRequest(r, "媒体地址无效")
		return &NativeStreamRes{}, nil
	}
	if math.IsNaN(req.Start) || math.IsInf(req.Start, 0) || req.Start < 0 || req.Start > 6*3600 || session.Media.Duration > 0 && req.Start >= session.Media.Duration.Seconds() {
		badRequest(r, "播放位置无效")
		return &NativeStreamRes{}, nil
	}
	binary, err := ffmpegBinary()
	if err != nil {
		r.Response.WriteStatus(http.StatusServiceUnavailable, "未安装 FFmpeg")
		return &NativeStreamRes{}, nil
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-rw_timeout", "20000000", "-protocol_whitelist", "http,https,tcp,tls,crypto,httpproxy", "-decryption_key", hex.EncodeToString(session.Media.Key)}
	if session.Media.Referer != "" {
		args = append(args, "-headers", "Referer: "+session.Media.Referer+"\r\n")
	}
	if req.Start > 0 {
		args = append(args, "-ss", strconv.FormatFloat(req.Start, 'f', 3, 64))
	}
	args = append(args, "-i", session.Media.URL, "-map", "0:v:0", "-map", "0:a:0?", "-sn", "-dn",
		"-vf", "fps=30,scale=trunc(iw/2)*2:trunc(ih/2)*2,setsar=1",
		"-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency", "-profile:v", "baseline", "-pix_fmt", "yuv420p", "-crf", "23", "-threads", "2",
		"-c:a", "aac", "-b:a", "128k", "-ar", "48000", "-ac", "2",
		"-movflags", "+frag_keyframe+empty_moov+default_base_moof", "-frag_duration", "1000000", "-f", "mp4", "pipe:1")
	// The HTTP request context cancels FFmpeg when the browser changes episodes,
	// seeks to another range, or closes the playback page.
	command := exec.CommandContext(r.Context(), binary, args...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	r.Response.Header().Set("Content-Type", "video/mp4")
	r.Response.Header().Set("Cache-Control", "private, no-store")
	r.Response.Header().Set("X-Content-Type-Options", "nosniff")
	output := newMediaBinaryWriter(r.Response, http.StatusOK)
	command.Stdout = output
	if err := command.Run(); err != nil && r.Context().Err() == nil && !strings.Contains(stderr.String(), "Broken pipe") {
		// FFmpeg's argument list includes the media key. Never log it.
		g.Log().Warning(ctx, fmt.Sprintf("Native media remux failed: %T", err))
	}
	_ = output.Close()
	r.ExitAll()
	return &NativeStreamRes{}, nil
}
