package suxinvideo

import (
	"context"
	"crypto/hmac"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	xq "github.com/suxinwl/GoSuxin/internal/xiaoqiapp"
)

type AppPlaybackRequest struct {
	VodID      int64  `json:"vod_id" p:"vod_id"`
	Line       string `json:"line" p:"line"`
	EpisodeKey string `json:"episode_key" p:"episode_key"`
	VersionKey string `json:"version_key" p:"version_key"`
	Episode    int    `json:"episode" p:"episode"`
	Quality    string `json:"quality" p:"quality"`
	PositionMS int64  `json:"position_ms" p:"position_ms"`
	Manual     bool   `json:"manual" p:"manual"`
}
type appDownloadContextKey struct{}

func AppIsDownloadContext(ctx context.Context) bool {
	value, _ := ctx.Value(appDownloadContextKey{}).(bool)
	return value
}

type AppQuality struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Type    string `json:"type"`
	Width   int    `json:"width,omitempty"`
	Height  int    `json:"height,omitempty"`
	Bitrate int64  `json:"bitrate,omitempty"`
}
type AppPlaybackDescriptor struct {
	VodID          int64        `json:"vod_id"`
	Line           string       `json:"line"`
	BaseCode       string       `json:"base_code"`
	VersionKey     string       `json:"version_key"`
	EpisodeKey     string       `json:"episode_key"`
	Episode        int          `json:"episode"`
	Name           string       `json:"name"`
	URL            string       `json:"url"`
	Type           string       `json:"type"`
	DurationMS     int64        `json:"duration_ms"`
	SeekMode       string       `json:"seek_mode"`
	ResumeOffsetMS int64        `json:"resume_offset_ms"`
	ExpiresAt      int64        `json:"expires_at"`
	Qualities      []AppQuality `json:"qualities"`
	Authorization  string       `json:"authorization,omitempty"`
	Revision       string       `json:"revision"`
	DownloadKey    string       `json:"download_key"`
	Quality        string       `json:"quality"`
}

func appProviderRank(code string, short, anime bool) int {
	if short {
		if code == "hongguo" {
			return 0
		}
		if code == "yqk_1" {
			return 10
		}
		if strings.HasPrefix(code, "yqk_") {
			return 20
		}
		return 30
	}
	if anime && strings.HasPrefix(code, "ecy_") {
		return 0
	}
	if code == "yqk_1" {
		return 10
	}
	if strings.HasPrefix(code, "yqk_") {
		return 20
	}
	return 30
}
func appPreferredLine(ctx context.Context, film row, sources []source) string {
	short, _ := shortDramaPlaybackCategory(ctx, film, sources)
	anime, _ := animePlaybackCategory(ctx, film)
	best, rank := "", 100
	for _, src := range sources {
		if len(src.Episodes) == 0 {
			continue
		}
		current := appProviderRank(vodAliasOwnerSource(src).Code, short, anime)
		if current < rank {
			best, rank = src.Code, current
		}
	}
	return best
}
func appSourceViews(ctx context.Context, film row, sources []source) []row {
	views := playerViewSources(ctx, gconv.Int64(film["id"]), sources)
	movie, _ := moviePlaybackCategory(ctx, gconv.Int64(film["type_id"]))
	movie = moviePlaybackAlignmentAllowed(gconv.String(film["name"]), movie, playlist(film))
	alignMoviePlaybackSources(gconv.String(film["name"]), movie, views)
	result := make([]row, 0, len(views))
	for _, src := range views {
		episodes := make([]row, 0, len(src.Episodes))
		for _, ep := range src.Episodes {
			episodes = append(episodes, row{"key": ep.Key, "number": ep.Number, "name": ep.Name})
		}
		result = append(result, row{"code": src.Code, "base_code": src.BaseCode, "version_key": src.VersionKey, "name": src.Name, "episodes": episodes})
	}
	return result
}
func appChooseEpisode(ctx context.Context, film row, sources []source, input *AppPlaybackRequest) (int, int, error) {
	if input == nil || input.Episode < 0 || input.PositionMS < 0 || input.PositionMS > 24*3600*1000 {
		return 0, 0, appError(400, "播放参数无效")
	}
	if len(sources) == 0 {
		return 0, 0, appError(404, "影片暂无可用线路")
	}
	line := input.Line
	if !input.Manual || line == "" {
		preferred := appPreferredLine(ctx, film, sources)
		if preferred != "" {
			line = preferred
		}
	}
	views := appSourceViews(ctx, film, sources)
	return appFindEpisode(views, line, input)
}

func appFindEpisode(views []row, preferred string, input *AppPlaybackRequest) (int, int, error) {
	indexes := []int{}
	for i, src := range views {
		if gconv.String(src["code"]) == preferred {
			indexes = append(indexes, i)
		}
	}
	if !input.Manual {
		for i, src := range views {
			if gconv.String(src["code"]) != preferred {
				indexes = append(indexes, i)
			}
		}
	}
	for _, si := range indexes {
		src := views[si]
		if input.VersionKey != "" && input.VersionKey != gconv.String(src["version_key"]) {
			continue
		}
		episodes := src["episodes"].([]row)
		ei := input.Episode
		if input.EpisodeKey != "" {
			ei = -1
			for i, ep := range episodes {
				if gconv.String(ep["key"]) == input.EpisodeKey {
					ei = i
					break
				}
			}
		}
		if ei >= 0 && ei < len(episodes) {
			return si, ei, nil
		}
	}
	return 0, 0, appError(404, "当前版本和分集暂无可用线路")
}

func appEpisodeIdentity(ctx context.Context, film row, name string) string {
	movie, _ := moviePlaybackCategory(ctx, gconv.Int64(film["type_id"]))
	movie = moviePlaybackAlignmentAllowed(gconv.String(film["name"]), movie, playlist(film))
	key, _ := moviePlaybackEpisodeKey(gconv.String(film["name"]), name, movie)
	return key
}

func AppResolvePlayback(ctx context.Context, input *AppPlaybackRequest) (*AppPlaybackDescriptor, error) {
	ctx, release := context.WithTimeout(ctx, 35*time.Second)
	defer release()
	if input == nil {
		return nil, appError(400, "播放参数无效")
	}
	if len(input.Line) > 100 || len(input.VersionKey) > 100 || len(input.EpisodeKey) > 200 || len(input.Quality) > 120 {
		return nil, appError(400, "播放身份参数过长")
	}
	film, err := appVisibleFilm(ctx, input.VodID)
	if err != nil {
		return nil, err
	}
	if _, err = AppAuthorizeFilm(ctx, film); err != nil {
		return nil, err
	}
	sources, err := hydratePlayers(ctx, film, playlist(film))
	if err != nil {
		return nil, err
	}
	si, ei, err := appChooseEpisode(ctx, film, sources, input)
	if err != nil {
		return nil, err
	}
	firstCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	descriptor, firstErr := appResolveSource(firstCtx, film, sources[si], si, ei, input, false)
	cancel()
	if firstErr == nil {
		return descriptor, nil
	}
	if input.Manual {
		return nil, firstErr
	}
	// Only after the preferred line fails do bounded parallel requests race.
	// Explicit version/episode identities prevent crossing seasons or cuts.
	key := appEpisodeIdentity(ctx, film, sources[si].Episodes[ei].Name)
	type candidate struct{ si, ei int }
	candidates := make([]candidate, 0)
	for i, src := range sources {
		if i == si || src.VersionKey != sources[si].VersionKey {
			continue
		}
		for j, ep := range src.Episodes {
			current := appEpisodeIdentity(ctx, film, ep.Name)
			if current == key {
				candidates = append(candidates, candidate{i, j})
				break
			}
		}
	}
	for start := 0; start < len(candidates); start += 3 {
		end := min(start+3, len(candidates))
		batchCtx, stop := context.WithTimeout(ctx, 10*time.Second)
		type result struct {
			descriptor *AppPlaybackDescriptor
			err        error
		}
		responses := make(chan result, end-start)
		for _, item := range candidates[start:end] {
			go func(item candidate) {
				d, e := appResolveSource(batchCtx, film, sources[item.si], item.si, item.ei, input, true)
				responses <- result{d, e}
			}(item)
		}
		for i := start; i < end; i++ {
			select {
			case result := <-responses:
				if result.err == nil {
					stop()
					return result.descriptor, nil
				}
			case <-batchCtx.Done():
				i = end
			}
		}
		stop()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, appError(502, "当前分集没有可播放线路，请稍后重试")
}

func appResolveSource(ctx context.Context, film row, src source, sourceIndex, episodeIndex int, input *AppPlaybackRequest, deepProbe bool) (*AppPlaybackDescriptor, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	ep := src.Episodes[episodeIndex]
	owner := vodAliasOwnerSource(src)
	ownerID := gconv.Int64(film["id"])
	if src.OwnerVodID > 0 {
		ownerID = src.OwnerVodID
	}
	if ownerID != gconv.Int64(film["id"]) {
		ownerFilm, e := appVisibleFilm(ctx, ownerID)
		if e != nil {
			return nil, e
		}
		if _, e = AppAuthorizeFilm(ctx, ownerFilm); e != nil {
			return nil, e
		}
	}
	key := appEpisodeIdentity(ctx, film, ep.Name)
	expires := time.Now().Add(2 * time.Hour)
	grant := ""
	if _, ok := AppPrincipalFromContext(ctx); ok {
		var err error
		grant, err = AppCreateMediaGrant(ctx, ownerID, expires)
		if err != nil {
			return nil, err
		}
	}
	identity := fmt.Sprintf("%d|%s|%s|%s", gconv.Int64(film["id"]), src.Code, src.VersionKey, key)
	// URL changes from signed providers do not alter the content revision. A
	// playlist update/version change does, invalidating mixed old/new downloads.
	revision := appHash(identity + "|" + ep.URL + "|" + input.Quality)
	d := &AppPlaybackDescriptor{VodID: gconv.Int64(film["id"]), Line: src.Code, BaseCode: owner.Code, VersionKey: src.VersionKey, EpisodeKey: key, Episode: episodeIndex, Name: ep.Name, SeekMode: "hls", ResumeOffsetMS: input.PositionMS, ExpiresAt: expires.Unix(), Qualities: []AppQuality{}, Authorization: grant, Revision: revision, DownloadKey: appHash(identity + "|" + revision), Quality: input.Quality}
	var media xq.CMSMedia
	var headers map[string]string
	var err error
	native := owner.Code == "hongguo" || owner.Code == "4kvm" || strings.HasPrefix(owner.Code, "yqk_") || strings.HasPrefix(owner.Code, "ecy_")
	if native {
		marker := ep.URL
		if strings.HasPrefix(owner.Code, "yqk_") {
			media, err = sourceHealthResolveYQK(ctx, marker)
		} else if strings.HasPrefix(owner.Code, "ecy_") {
			media, headers, err = resolveErciyuanPlayback(ctx, marker)
		} else if owner.Code == "hongguo" {
			u, parseErr := url.Parse(marker)
			if parseErr != nil || u.Scheme != "hongguo" || !onlyDigits(u.Host) || !onlyDigits(strings.Trim(u.Path, "/")) {
				return nil, appError(502, "红果分集地址无效")
			}
			media, err = cmsProviders.Resolve(ctx, "hongguo", u.Host, "hongguo-cenc://"+strings.Trim(u.Path, "/"))
		} else {
			slug, ok := native4KVMSlug(marker)
			if !ok {
				return nil, appError(502, "4KVM分集地址无效")
			}
			media, err = cmsProviders.Resolve(ctx, "4kvm", slug, marker)
		}
		if err != nil {
			return nil, appError(502, "片源解析失败，请尝试其他线路")
		}
		if err = safeCollectorURL(ctx, media.URL); err != nil {
			return nil, appError(502, "片源媒体地址无效")
		}
		variants := media.Variants
		media.Variants = nil
		for _, variant := range variants {
			if input.Quality != "" && (input.Quality == strconv.Itoa(variant.Quality) || input.Quality == variant.Label) {
				media = xq.CMSMedia{URL: variant.URL, Referer: variant.Referer, Key: variant.Key, Quality: variant.Quality, Duration: variant.Duration}
				break
			}
		}
		if media.Quality > 0 {
			d.Quality = strconv.Itoa(media.Quality)
			d.Revision = appHash(identity + "|" + ep.URL + "|" + d.Quality)
			d.DownloadKey = appHash(identity + "|" + d.Revision)
		}
		if len(media.Key) == 0 {
			budget := appStartupManifestBudget
			if deepProbe {
				budget = 6 * time.Second
			}
			probeCtx, stop := context.WithTimeout(ctx, budget)
			var probeErr error
			if deepProbe {
				probeErr = probeDiscoveryMediaHeaders(probeCtx, media, headers, playbackHTTPClient, safeCollectorURL)
			} else {
				probeErr = probeAppStartupMediaHeaders(probeCtx, media, headers, playbackHTTPClient, safeCollectorURL)
			}
			stop()
			if probeErr != nil {
				return nil, appError(502, "当前片源无法读取媒体分片，请尝试其他线路")
			}
		}
		makeURL := func(value xq.CMSMedia) (string, string, error) {
			if ctx.Err() != nil {
				return "", "", ctx.Err()
			}
			if err := safeCollectorURL(ctx, value.URL); err != nil {
				return "", "", err
			}
			session := nativeSession{VodID: ownerID, Source: sourceIndex, Line: owner.Code, Episode: episodeIndex, Media: value, Headers: headers, Expires: expires, Background: AppIsDownloadContext(ctx)}
			token, e := newNativeSession(session)
			if e != nil {
				return "", "", e
			}
			var address, kind string
			if len(value.Key) > 0 {
				address, e = appNativeHLSURL(ctx, token, value, expires)
				kind = "m3u8"
			} else {
				address, kind = nativePlaybackURL(ctx, token, value, expires)
			}
			address = AppAppendMediaAuthorization(address, grant, d.DownloadKey)
			return address, kind, e
		}
		d.URL, d.Type, err = makeURL(media)
		if err != nil {
			return nil, err
		}
		d.DurationMS = media.Duration.Milliseconds()
		if d.Type == "mp4" {
			d.SeekMode = "byte-range"
		}
		seen := map[string]bool{}
		for _, variant := range variants {
			if len(d.Qualities) >= 8 || seen[variant.URL] {
				continue
			}
			seen[variant.URL] = true
			address, kind, e := makeURL(xq.CMSMedia{URL: variant.URL, Referer: variant.Referer, Key: variant.Key, Quality: variant.Quality, Duration: variant.Duration})
			if e != nil {
				continue
			}
			label := variant.Label
			if variant.Quality > 0 {
				label = strconv.Itoa(variant.Quality) + "P"
			}
			d.Qualities = append(d.Qualities, AppQuality{ID: strconv.Itoa(variant.Quality), Name: label, URL: address, Type: kind, Height: variant.Quality})
		}
	} else {
		direct := ep.URL
		rule := src.Parse
		if rule == "" {
			rule = setting(ctx, "player_parse", "")
		}
		if strings.HasPrefix(strings.ToLower(rule), "m3u8:") {
			direct = strings.ReplaceAll(rule[6:], "{url}", direct)
		} else if rule != "" {
			return nil, appError(422, "当前解析线路不提供原生媒体，请选择其他线路")
		}
		if !safePlayerAddress(direct) || safeCollectorURL(ctx, direct) != nil {
			return nil, appError(502, "媒体地址无效")
		}
		parsed, _ := url.Parse(direct)
		path := strings.ToLower(parsed.Path)
		if !strings.HasSuffix(path, ".m3u8") && !strings.HasSuffix(path, ".mp4") && !strings.HasSuffix(path, ".mkv") && !strings.HasSuffix(path, ".ts") {
			return nil, appError(422, "当前线路仅提供网页播放器")
		}
		// Confirm the first useful media response; accepting HTTP 200 HTML was
		// the cause of apparently selected lines that never start in a player.
		probeCtx, stop := context.WithTimeout(ctx, 6*time.Second)
		request, e := http.NewRequestWithContext(probeCtx, http.MethodGet, direct, nil)
		if e != nil {
			stop()
			return nil, e
		}
		request.Header.Set("User-Agent", mediaUserAgent)
		request.Header.Set("Range", "bytes=0-4095")
		response, e := playbackHTTPClient.Do(request)
		if e != nil {
			stop()
			return nil, appError(502, "片源连接超时")
		}
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		response.Body.Close()
		stop()
		if response.StatusCode != 200 && response.StatusCode != 206 {
			return nil, appError(502, "片源媒体不可用")
		}
		if strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/html") {
			return nil, appError(502, "片源返回了网页内容")
		}
		d.Type = "mp4"
		d.SeekMode = "byte-range"
		if strings.HasSuffix(path, ".m3u8") {
			if !strings.Contains(string(body), "#EXTM3U") {
				return nil, appError(502, "播放清单无效")
			}
			d.Type = "m3u8"
			d.SeekMode = "hls"
		}
		d.URL = appMediaLink(ctx, ownerID, direct, expires.Unix(), grant, d.DownloadKey)
	}
	if len(d.Qualities) == 0 {
		d.Qualities = append(d.Qualities, AppQuality{ID: "source", Name: "源清晰度", URL: d.URL, Type: d.Type})
	}
	return d, nil
}

func AppAppendMediaAuthorization(address, grant, downloadKey string) string {
	u, err := url.Parse(address)
	if err != nil {
		return address
	}
	q := u.Query()
	if grant != "" {
		q.Set("app_grant", grant)
	}
	if downloadKey != "" {
		q.Set("download_key", downloadKey)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
func appMediaLink(ctx context.Context, vodID int64, address string, expires int64, grant, downloadKey string) string {
	q := url.Values{"vod_id": {strconv.FormatInt(vodID, 10)}, "url": {address}, "exp": {strconv.FormatInt(expires, 10)}, "sig": {signProxy(ctx, fmt.Sprintf("app-proxy:%d:%s", vodID, address), expires)}}
	if grant != "" {
		q.Set("app_grant", grant)
	}
	if downloadKey != "" {
		q.Set("download_key", downloadKey)
	}
	return "/suxinvideo/app/v1/media?" + q.Encode()
}
func appMediaProxy(r *ghttp.Request) {
	ctx := r.Context()
	id, address, exp, sig := r.Get("vod_id").Int64(), r.Get("url").String(), r.Get("exp").Int64(), r.Get("sig").String()
	if exp < time.Now().Unix() || exp > time.Now().Add(25*time.Hour).Unix() || !hmac.Equal([]byte(sig), []byte(signProxy(ctx, fmt.Sprintf("app-proxy:%d:%s", id, address), exp))) {
		appWrite(r, nil, appError(403, "媒体授权无效或已过期"))
		return
	}
	film, err := appVisibleFilm(ctx, id)
	if err == nil {
		_, err = AppAuthorizeFilm(ctx, film)
	}
	if err != nil {
		appWrite(r, nil, err)
		return
	}
	if safeCollectorURL(ctx, address) != nil {
		appWrite(r, nil, appError(400, "媒体地址无效"))
		return
	}
	upstream, err := http.NewRequestWithContext(ctx, r.Method, address, nil)
	if err != nil {
		appWrite(r, nil, err)
		return
	}
	upstream.Header.Set("User-Agent", mediaUserAgent)
	if value := r.Header.Get("Range"); value != "" {
		upstream.Header.Set("Range", value)
	}
	response, err := playbackHTTPClient.Do(upstream)
	if err != nil {
		appWrite(r, nil, appError(502, "媒体请求失败"))
		return
	}
	defer response.Body.Close()
	if response.StatusCode != 200 && response.StatusCode != 206 {
		appWrite(r, nil, appError(502, "源站媒体不可用"))
		return
	}
	r.Response.Header().Set("Cache-Control", "private, no-store")
	r.Response.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodGet && isMediaPlaylist(response.Header.Get("Content-Type"), response.Request.URL.String()) {
		body, base, e := readMediaPlaylist(response)
		if e != nil {
			appWrite(r, nil, appError(502, "媒体清单无效"))
			return
		}
		filtered := filterMediaPlaylist(ctx, body, base)
		mediaAdFilterHeaders(r.Response.Header(), filtered)
		r.Response.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		r.Response.Write(rewritePlaylist(filtered.Playlist, base, func(child string) string {
			return appMediaLink(ctx, id, child, exp, r.Get("app_grant").String(), r.Get("download_key").String())
		}))
		return
	}
	for _, name := range []string{"Content-Type", "Content-Range", "Content-Length", "Accept-Ranges"} {
		if value := response.Header.Get(name); value != "" {
			r.Response.Header().Set(name, value)
		}
	}
	if r.Method == http.MethodHead {
		r.Response.WriteHeader(response.StatusCode)
		return
	}
	_, _ = copyMediaBinary(r.Response, response.StatusCode, io.LimitReader(response.Body, 4<<30))
	r.ExitAll()
}

func appOfflineExpiry(now time.Time, film, user row) int64 {
	expires := now.Add(7 * 24 * time.Hour).Unix()
	if gconv.Int(film["vip"]) == 1 {
		expires = min(expires, gconv.Int64(user["vip_expire"]))
	}
	return expires
}
func appAuthorizeDownload(ctx context.Context, input *AppPlaybackRequest) (row, error) {
	ctx = context.WithValue(ctx, appDownloadContextKey{}, true)
	p, err := appRequirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	film, err := appVisibleFilm(ctx, input.VodID)
	if err != nil {
		return nil, err
	}
	if _, err = AppAuthorizeFilm(ctx, film); err != nil {
		return nil, err
	}
	input.Manual = input.Line != ""
	descriptor, err := AppResolvePlayback(ctx, input)
	if err != nil {
		return nil, err
	}
	id, err := appToken()
	if err != nil {
		return nil, err
	}
	expires := appOfflineExpiry(time.Now(), film, p.User)
	if err = execSQL(ctx, "INSERT INTO sx_app_license(id,user_id,session_id,device_id,vod_id,line,episode_key,version_key,quality,revision,expire,created) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", id, p.User["id"], p.SessionID, p.DeviceID, input.VodID, descriptor.Line, descriptor.EpisodeKey, descriptor.VersionKey, descriptor.Quality, descriptor.Revision, expires, time.Now().Unix()); err != nil {
		return nil, err
	}
	return row{"id": id, "device_id": p.DeviceID, "expires_at": expires, "revision": descriptor.Revision, "descriptor": descriptor, "issued_at": time.Now().Unix()}, nil
}
func appLicenses(ctx context.Context) ([]row, error) {
	p, err := appRequirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	items, err := appRows(ctx, "SELECT id,vod_id,line,episode_key,version_key,revision,expire expires_at,revoked FROM sx_app_license WHERE user_id=? AND device_id=? ORDER BY created DESC LIMIT 1000", p.User["id"], p.DeviceID)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		film, e := appVisibleFilm(ctx, gconv.Int64(item["vod_id"]))
		if e == nil {
			_, e = AppAuthorizeFilm(ctx, film)
		}
		if e != nil {
			item["revoked"] = true
			_ = execSQL(ctx, "UPDATE sx_app_license SET revoked=1 WHERE id=?", item["id"])
		} else {
			item["revoked"] = gconv.Bool(item["revoked"])
		}
	}
	return items, nil
}
func appRenewLicense(ctx context.Context, id string) (row, error) {
	ctx = context.WithValue(ctx, appDownloadContextKey{}, true)
	p, err := appRequirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	license, err := one(ctx, "SELECT * FROM sx_app_license WHERE id=? AND user_id=? AND device_id=? AND revoked=0", id, p.User["id"], p.DeviceID)
	if err != nil {
		return nil, err
	}
	if license == nil {
		return nil, appError(404, "下载授权不存在或已撤销")
	}
	film, err := appVisibleFilm(ctx, gconv.Int64(license["vod_id"]))
	if err != nil {
		return nil, err
	}
	if _, err = AppAuthorizeFilm(ctx, film); err != nil {
		return nil, err
	}
	descriptor, err := AppResolvePlayback(ctx, &AppPlaybackRequest{VodID: gconv.Int64(license["vod_id"]), Line: gconv.String(license["line"]), EpisodeKey: gconv.String(license["episode_key"]), VersionKey: gconv.String(license["version_key"]), Quality: gconv.String(license["quality"]), Manual: true})
	if err != nil {
		return nil, err
	}
	if descriptor.Revision != gconv.String(license["revision"]) {
		return nil, appError(409, "资源版本已变化，请重新下载")
	}
	expires := appOfflineExpiry(time.Now(), film, p.User)
	if err = execSQL(ctx, "UPDATE sx_app_license SET session_id=?,expire=? WHERE id=?", p.SessionID, expires, id); err != nil {
		return nil, err
	}
	return row{"id": id, "device_id": p.DeviceID, "expires_at": expires, "revision": descriptor.Revision, "descriptor": descriptor, "issued_at": time.Now().Unix()}, nil
}
