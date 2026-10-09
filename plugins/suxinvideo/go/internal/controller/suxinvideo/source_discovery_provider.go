package suxinvideo

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	xq "github.com/suxinwl/GoSuxin/internal/xiaoqiapp"
)

type discoveryTarget struct {
	ID                       int64
	Name, Year, Area, Kind   string
	SourceAPIID              int64
	SourceAPIVID, EpisodeKey string
}

type discoveryProviderResult struct {
	CollectorID   int64
	CollectorName string
	Sources       []source
	Error         string
	RatingItem    row
}

// Dependencies are scoped to an invocation so fixture tests never replace
// production network guards or shared globals.
type discoveryProviderDeps struct {
	fetch         func(context.Context, string, url.Values) (macPayload, error)
	visible       func(context.Context, int64, []source) ([]source, error)
	probe         func(context.Context, string) error
	nativeProbe   func(context.Context, string) error
	fourKVMProbe  func(context.Context, string) error
	erciyuanProbe func(context.Context, string) error
	hongguoProbe  func(context.Context, string) error
}

func discoveryCollectorSupported(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == fourKVMSourceURL || raw == hongguoSourceURL || isYQKSource(raw) || isErciyuanSource(raw) {
		return true
	}
	u, err := url.Parse(raw)
	return err == nil && u.Hostname() != "" && u.User == nil && (u.Scheme == "http" || u.Scheme == "https")
}

// Existing installations can have missing region/category metadata. Only an
// enabled original provider's exact stored remote ID may fill those gaps; a
// foreign search candidate cannot supply the evidence used to match itself.
func enrichDiscoveryTarget(ctx context.Context, target discoveryTarget, collector row) discoveryTarget {
	return enrichDiscoveryTargetWith(ctx, target, collector, fetchCollectSource)
}

func enrichDiscoveryTargetWith(parent context.Context, target discoveryTarget, collector row, fetch func(context.Context, string, url.Values) (macPayload, error)) discoveryTarget {
	if discoveryYearPattern.MatchString(strings.TrimSpace(target.Year)) && discoveryRegion(target.Area) != "" && discoveryKind(target.Kind) != "" {
		return target
	}
	if target.SourceAPIID < 1 || target.SourceAPIID != gconv.Int64(collector["id"]) || gconv.Int(collector["status"]) != 1 || target.SourceAPIVID == "" {
		return target
	}
	raw := strings.TrimSpace(gconv.String(collector["api_url"]))
	if !discoveryCollectorSupported(raw) {
		return target
	}
	ctx, cancel := context.WithTimeout(parent, 12*time.Second)
	defer cancel()
	detail, err := fetch(ctx, raw, url.Values{"ac": {"detail"}, "ids": {target.SourceAPIVID}})
	if err != nil || ctx.Err() != nil {
		return target
	}
	for i, item := range detail.List {
		if i >= 40 {
			break
		}
		if gconv.String(item["vod_id"]) != target.SourceAPIVID || !discoveryCompatible(target, item, target.SourceAPIID, true) {
			continue
		}
		if !discoveryYearPattern.MatchString(strings.TrimSpace(target.Year)) {
			if year := strings.TrimSpace(gconv.String(item["vod_year"])); discoveryYearPattern.MatchString(year) {
				target.Year = year
			}
		}
		if discoveryRegion(target.Area) == "" && discoveryRegion(gconv.String(item["vod_area"])) != "" {
			target.Area = strings.TrimSpace(gconv.String(item["vod_area"]))
		}
		if discoveryKind(target.Kind) == "" {
			target.Kind = discoveryKind(gconv.String(item["type_name"]))
		}
		return target
	}
	return target
}

func discoverCollector(ctx context.Context, target discoveryTarget, collector row) discoveryProviderResult {
	client := safeCollectorHTTPClient(12 * time.Second)
	defer client.CloseIdleConnections()
	probe := sourceHealthProbe{client: client, checkURL: safeMediaURL}
	return discoverCollectorWith(ctx, target, collector, discoveryProviderDeps{
		fetch: fetchCollectSource,
		visible: func(ctx context.Context, collectorID int64, sources []source) ([]source, error) {
			if setting(ctx, "player_parse", "") != "" {
				return nil, errors.New("已配置解析播放器，不能自动验证直连线路")
			}
			players, err := all(ctx, "SELECT code,name,`parse`,status FROM sx_player")
			if err != nil {
				return nil, err
			}
			collectors, err := all(ctx, "SELECT id,name,api_url,status FROM sx_collect_api")
			if err != nil {
				return nil, err
			}
			configs := make(map[string]row, len(players))
			for _, player := range players {
				configs[gconv.String(player["code"])] = player
			}
			return dedupePageSources(availableSources(row{"api_id": collectorID}, sources, configs, collectors)), nil
		},
		probe: func(ctx context.Context, raw string) error {
			return probe.media(ctx, raw, 0, map[string]bool{})
		},
		nativeProbe: func(ctx context.Context, marker string) error {
			return probeYQKDiscoveryWith(ctx, marker, resolveYQK, client, safeMediaURL)
		},
		fourKVMProbe: func(ctx context.Context, marker string) error {
			return probe4KVMDiscoveryWith(ctx, marker, func(ctx context.Context, slug, marker string) (xq.CMSMedia, error) {
				return cmsProviders.Resolve(ctx, "4kvm", slug, marker)
			}, client, safeMediaURL)
		},
		erciyuanProbe: func(ctx context.Context, marker string) error {
			return probeErciyuanDiscoveryWith(ctx, marker, resolveErciyuanPlayback, client, safeMediaURL)
		},
		hongguoProbe: func(ctx context.Context, marker string) error {
			return probeHongguoDiscoveryWith(ctx, marker, func(ctx context.Context, series, video string) (xq.CMSMedia, error) {
				return cmsProviders.Resolve(ctx, "hongguo", series, video)
			}, client, safeMediaURL, probeHongguoEncryptedMedia)
		},
	})
}

func discoverCollectorWith(parent context.Context, target discoveryTarget, collector row, deps discoveryProviderDeps) discoveryProviderResult {
	result := discoveryProviderResult{CollectorID: gconv.Int64(collector["id"]), CollectorName: gconv.String(collector["name"])}
	if result.CollectorID < 1 || gconv.Int(collector["status"]) != 1 {
		result.Error = "采集源已停用"
		return result
	}
	raw := strings.TrimSpace(gconv.String(collector["api_url"]))
	isYQK := isYQKSource(raw)
	is4KVM := raw == fourKVMSourceURL
	isErciyuan := isErciyuanSource(raw)
	isHongguo := raw == hongguoSourceURL
	if !discoveryCollectorSupported(raw) {
		result.Error = "该片源暂不支持按影片身份自动搜索"
		return result
	}
	if discoveryNormalizeTitle(target.Name) == "" {
		result.Error = "影片名称为空，无法匹配同片资源"
		return result
	}
	if isHongguo && discoveryKind(target.Kind) != "short" {
		result.Error = "红果仅补充同名短剧，当前影片不是短剧"
		return result
	}
	providerBudget := 20 * time.Second
	if isYQK || isErciyuan {
		// This aggregate contains up to 18 distinct lines. Its bounded work
		// budget must not be identical to a single-line MacCMS provider.
		providerBudget = 60 * time.Second
	}
	if isHongguo {
		providerBudget = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, providerBudget)
	defer cancel()
	fetch := func(query url.Values) (macPayload, error) {
		requestCtx, requestCancel := context.WithTimeout(ctx, 12*time.Second)
		defer requestCancel()
		return deps.fetch(requestCtx, raw, query)
	}
	search, err := fetch(url.Values{"wd": {strings.TrimSpace(target.Name)}, "ac": {"videolist"}, "pg": {"1"}})
	if err != nil {
		result.Error = "采集源搜索超时或失败"
		return result
	}
	if isErciyuan && discoveryKind(target.Kind) == "anime_movie" {
		matched := false
		for _, candidate := range search.List {
			matched = matched || discoveryCompatible(target, candidate, result.CollectorID, true)
		}
		// The native catalog's title search preserves a space after 剧场版.
		// Try that single spelling variant only; final identity checks still
		// require the same complete distinctive title and independent detail.
		alternate := erciyuanTheatricalSearchKeyword(target.Name)
		if !matched && alternate != "" && alternate != strings.TrimSpace(target.Name) {
			search, err = fetch(url.Values{"wd": {alternate}, "ac": {"videolist"}, "pg": {"1"}})
			if err != nil {
				result.Error = "二次元剧场版搜索超时或失败"
				return result
			}
		}
	}
	if len(search.List) > 40 {
		search.List = search.List[:40]
	}
	seenIDs, seenLines := map[string]bool{}, map[string]bool{}
	ratings := map[string]row{}
	ratingCandidates := map[string]bool{}
	for _, candidate := range search.List {
		if discoveryCompatible(target, candidate, result.CollectorID, true) || isHongguo && discoveryHongguoMatches(target, candidate) {
			if id := strings.TrimSpace(gconv.String(candidate["vod_id"])); id != "" {
				ratingCandidates[id] = true
			}
		}
	}
	if isErciyuan && discoveryKind(target.Kind) == "anime_movie" && len(ratingCandidates) > 1 {
		result.Error = "二次元同名剧场版存在多个上游编号，暂不合并未核实的影片"
		return result
	}
	candidates := 0
	lineLimit := 3
	if isYQK {
		lineLimit = len(yqkPlayers)
	} else if isErciyuan {
		lineLimit = len(erciyuanPlayers)
	}
	for _, candidate := range search.List {
		if ctx.Err() != nil || candidates >= 2 || len(result.Sources) >= lineLimit {
			break
		}
		if !contentMacAllowed(ctx, candidate) || !(discoveryCompatible(target, candidate, result.CollectorID, true) || isHongguo && discoveryHongguoMatches(target, candidate)) {
			continue
		}
		id := strings.TrimSpace(gconv.String(candidate["vod_id"]))
		if id == "" || len(id) > 64 || seenIDs[id] {
			continue
		}
		seenIDs[id] = true
		candidates++
		detail, detailErr := fetch(url.Values{"ac": {"detail"}, "ids": {id}})
		if detailErr != nil {
			result.Error = "影片详情请求超时或失败"
			continue
		}
		for i, item := range detail.List {
			if i >= 40 || ctx.Err() != nil || len(result.Sources) >= lineLimit {
				break
			}
			erciyuanTheatrical := isErciyuan && discoveryErciyuanTheatricalMatch(target, item)
			if !contentMacAllowed(ctx, item) || strings.TrimSpace(gconv.String(item["vod_id"])) != id || !(discoveryMatches(target, item, result.CollectorID) || isHongguo && discoveryHongguoMatches(target, item) || erciyuanTheatrical) {
				continue
			}
			// Film identity, rather than media availability, establishes rating
			// provenance. Retain only declared rating fields from this detail.
			rating := row{"vod_id": id, "vod_name": item["vod_name"]}
			for _, field := range []string{"vod_douban_score", "vod_score", "score"} {
				if value, exists := item[field]; exists {
					rating[field] = value
				}
			}
			ratings[id] = rating
			sources := playlist(row{"play_from": item["vod_play_from"], "play_url": item["vod_play_url"]})
			if erciyuanTheatrical {
				sources, _ = erciyuanTheatricalFeatureSources(target.Name, item)
			}
			sources, visibleErr := deps.visible(ctx, result.CollectorID, sources)
			if visibleErr != nil {
				result.Error = "无法核实播放器和采集源启用状态"
				continue
			}
			if isHongguo {
				for _, src := range sources {
					if src.Code != "hongguo" || seenLines[src.Code] || src.Parse != "" || deps.hongguoProbe == nil {
						continue
					}
					src = discoveryNormalizeSource(src)
					valid := len(src.Episodes) > 0
					for _, ep := range src.Episodes {
						if series, _, ok := hongguoDiscoveryMarker(ep.URL); !ok || series != id {
							valid = false
							break
						}
					}
					samples := discoverySampleURLs(src, target.EpisodeKey)
					valid = valid && len(samples) > 0
					if valid {
						for _, marker := range samples {
							probeCtx, probeCancel := context.WithTimeout(ctx, 20*time.Second)
							probeErr := deps.hongguoProbe(probeCtx, marker)
							probeCancel()
							if probeErr != nil || ctx.Err() != nil {
								valid = false
								break
							}
						}
					}
					if valid {
						seenLines[src.Code] = true
						result.Sources = append(result.Sources, src)
					} else {
						result.Error = "红果线路未通过首集及最新集媒体检测"
					}
				}
				continue
			}
			if isErciyuan {
				episodeKey := target.EpisodeKey
				if erciyuanTheatrical {
					// Every retained item has already been proven a whole-film
					// version. Its HD label is not a TV episode-number identity.
					episodeKey = ""
				}
				verified := verifyErciyuanDiscoverySources(ctx, sources, id, episodeKey, deps.erciyuanProbe)
				for _, src := range verified {
					if !seenLines[src.Code] && len(result.Sources) < lineLimit {
						seenLines[src.Code] = true
						result.Sources = append(result.Sources, src)
					}
				}
				if len(verified) == 0 {
					result.Error = "二次元线路未通过首集及最新集媒体检测"
				}
				continue
			}
			if isYQK {
				verified := verifyYQKDiscoverySources(ctx, sources, target.EpisodeKey, deps.nativeProbe)
				for _, src := range verified {
					if !seenLines[src.Code] && len(result.Sources) < lineLimit {
						seenLines[src.Code] = true
						result.Sources = append(result.Sources, src)
					}
				}
				if len(verified) == 0 {
					result.Error = "小柒线路未通过首集及最新集媒体检测"
				}
				continue
			}
			if is4KVM {
				for _, src := range sources {
					if src.Code != "4kvm" || seenLines[src.Code] || strings.TrimSpace(src.Parse) != "" || deps.fourKVMProbe == nil {
						continue
					}
					src = discoveryNormalizeSource(src)
					valid := len(src.Episodes) > 0
					for _, ep := range src.Episodes {
						if film, ok := fourKVMDiscoveryMarker(ep.URL); !ok || film != id {
							valid = false
							break
						}
					}
					samples := discoverySampleURLs(src, target.EpisodeKey)
					valid = valid && len(samples) > 0
					if valid {
						for _, marker := range samples {
							probeCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
							probeErr := deps.fourKVMProbe(probeCtx, marker)
							cancel()
							if probeErr != nil || ctx.Err() != nil {
								valid = false
								break
							}
						}
					}
					if valid {
						seenLines[src.Code] = true
						result.Sources = append(result.Sources, src)
					} else {
						result.Error = "4KVM 线路未通过首集及最新集媒体检测"
					}
				}
				continue
			}
			for _, src := range sources {
				if ctx.Err() != nil || len(result.Sources) >= 3 {
					break
				}
				src = discoveryNormalizeSource(src)
				if seenLines[src.Code] || !eligibleSourceHealth(src) {
					continue
				}
				samples := discoverySampleURLs(src, target.EpisodeKey)
				valid := len(samples) > 0
				for _, sample := range samples {
					probeCtx, probeCancel := context.WithTimeout(ctx, 12*time.Second)
					probeErr := deps.probe(probeCtx, sample)
					probeCancel()
					if probeErr != nil || ctx.Err() != nil {
						valid = false
						result.Error = "同片线路未通过首集及最新集媒体检测"
						break
					}
				}
				if valid {
					seenLines[src.Code] = true
					result.Sources = append(result.Sources, src)
				}
			}
		}
	}
	// A source with multiple matching remote editions has no unambiguous
	// single rating. Its playlists retain their existing independent rules.
	if len(ratings) == 1 && len(ratingCandidates) == 1 {
		for _, rating := range ratings {
			result.RatingItem = rating
		}
	}
	if len(result.Sources) > 0 {
		result.Error = ""
	} else if ctx.Err() != nil {
		result.Error = "本次片源搜索已超时，未添加未经验证的线路"
	} else if result.Error == "" {
		result.Error = "未找到身份匹配且可播放的直连线路"
	}
	return result
}

// Hongguo's real web/App metadata frequently omits country and release year.
// Keep this exception provider specific and restricted to proven short-drama
// targets with distinctive exact titles. Explicit contradictions still fail.
// A complete native detail and playable first/latest samples are required later.
func discoveryHongguoMatches(target discoveryTarget, item map[string]any) bool {
	title := discoveryNormalizeTitle(target.Name)
	if discoveryKind(target.Kind) != "short" || len([]rune(title)) < 8 || title != discoveryNormalizeTitle(gconv.String(item["vod_name"])) || discoveryKind(gconv.String(item["type_name"])) != "short" {
		return false
	}
	remoteYear := strings.TrimSpace(gconv.String(item["vod_year"]))
	if discoveryYearPattern.MatchString(target.Year) && discoveryYearPattern.MatchString(remoteYear) && target.Year != remoteYear {
		return false
	}
	region := discoveryRegion(gconv.String(item["vod_area"]))
	return region == "" || discoveryRegion(target.Area) == "" || region == discoveryRegion(target.Area)
}

var hongguoDiscoveryPattern = regexp.MustCompile(`^hongguo://([0-9]{1,32})/([0-9]{1,32})$`)

func hongguoDiscoveryMarker(marker string) (series, video string, valid bool) {
	parts := hongguoDiscoveryPattern.FindStringSubmatch(marker)
	if len(parts) != 3 || strings.Trim(parts[1], "0") == "" || strings.Trim(parts[2], "0") == "" {
		return "", "", false
	}
	return parts[1], "hongguo-cenc://" + parts[2], true
}

func probeHongguoDiscoveryWith(ctx context.Context, marker string, resolve func(context.Context, string, string) (xq.CMSMedia, error), client *http.Client, checkURL func(context.Context, string) error, decrypt func(context.Context, xq.CMSMedia) error) error {
	series, video, ok := hongguoDiscoveryMarker(marker)
	if !ok || resolve == nil {
		return errors.New("红果分集标识无效")
	}
	media, err := resolve(ctx, series, video)
	if err != nil || media.URL == "" || len(media.Key) != 0 && len(media.Key) != 16 {
		return errors.New("红果分集解析失败")
	}
	// Hongguo's MP4 URLs may be extensionless. Probe the actual container,
	// rather than applying the generic '.mp4' filename heuristic.
	probeClient := *client
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	probeClient.Transport = discoveryRefererTransport{base: base, referer: media.Referer}
	probe := sourceHealthProbe{client: &probeClient, checkURL: checkURL}
	body, _, probeErr := probe.read(ctx, media.URL, 4096, true)
	isMP4 := len(body) >= 12 && string(body[4:8]) == "ftyp"
	if probeErr != nil || !isMP4 && (len(media.Key) > 0 || probeDiscoveryMedia(ctx, media, client, checkURL) != nil) {
		return errors.New("红果媒体未就绪")
	}
	if len(media.Key) == 16 {
		if decrypt == nil || decrypt(ctx, media) != nil {
			return errors.New("红果媒体未通过现有解密路径检测")
		}
	}
	return nil
}

func probeHongguoEncryptedMedia(ctx context.Context, media xq.CMSMedia) error {
	binary, err := ffmpegBinary()
	if err != nil || len(media.Key) != 16 {
		return errors.New("红果媒体检测不可用")
	}
	// Use exactly the already supported native playback decryption path, but
	// decode only one frame. Keys, signed URLs and decoder output are never logged.
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-rw_timeout", "12000000", "-protocol_whitelist", "http,https,tcp,tls,crypto,httpproxy", "-decryption_key", hex.EncodeToString(media.Key)}
	if media.Referer != "" {
		args = append(args, "-headers", "Referer: "+media.Referer+"\r\n")
	}
	args = append(args, "-i", media.URL, "-map", "0:v:0", "-frames:v", "1", "-an", "-sn", "-dn", "-f", "null", "-")
	if exec.CommandContext(ctx, binary, args...).Run() != nil {
		return errors.New("红果媒体检测失败")
	}
	return nil
}

// Resolve only the stable DB marker, validate actual media with the same
// bounded HLS/key/segment probe, and retain markers in persisted playlists.
func probeYQKDiscoveryWith(ctx context.Context, marker string, resolve func(context.Context, string) (xq.CMSMedia, error), client *http.Client, checkURL func(context.Context, string) error) error {
	if _, _, _, err := yqkMarkerParts(marker); err != nil {
		return errors.New("小柒分集标识无效")
	}
	media, err := resolve(ctx, marker)
	if err != nil || len(media.Key) > 0 {
		return errors.New("小柒分集解析失败或格式暂不支持检测")
	}
	return probeDiscoveryMedia(ctx, media, client, checkURL)
}

func fourKVMDiscoveryMarker(marker string) (string, bool) {
	slug, ok := native4KVMSlug(marker)
	if !ok {
		return "", false
	}
	_, query, found := strings.Cut(marker, "?")
	values, err := url.ParseQuery(query)
	if !found || err != nil || len(values["dataid"]) != 1 || !onlyDigits(values.Get("dataid")) || strings.Trim(values.Get("dataid"), "0") == "" || len(values.Get("dataid")) > 32 {
		return "", false
	}
	for key, items := range values {
		if key != "dataid" && key != "quality" && key != "chapter" || len(items) != 1 {
			return "", false
		}
	}
	if chapter, present := values["chapter"]; present {
		if _, valid := native4KVMSlug("4kvm://" + chapter[0]); !valid || strings.Contains(chapter[0], "?") {
			return "", false
		}
	}
	if quality := values.Get("quality"); quality != "" {
		if !onlyDigits(quality) || len(quality) > 4 {
			return "", false
		}
	}
	return slug, true
}

func probe4KVMDiscoveryWith(ctx context.Context, marker string, resolve func(context.Context, string, string) (xq.CMSMedia, error), client *http.Client, checkURL func(context.Context, string) error) error {
	slug, ok := fourKVMDiscoveryMarker(marker)
	if !ok {
		return errors.New("4KVM 分集标识无效")
	}
	media, err := resolve(ctx, slug, marker)
	if err != nil || len(media.Key) > 0 {
		return errors.New("4KVM 分集解析失败或格式暂不支持检测")
	}
	return probeDiscoveryMedia(ctx, media, client, checkURL)
}

func probeDiscoveryMedia(ctx context.Context, media xq.CMSMedia, client *http.Client, checkURL func(context.Context, string) error) error {
	return probeDiscoveryMediaHeaders(ctx, media, nil, client, checkURL)
}

func probeDiscoveryMediaHeaders(ctx context.Context, media xq.CMSMedia, headers map[string]string, client *http.Client, checkURL func(context.Context, string) error) error {
	probeClient := *client
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	probeClient.Transport = discoveryRefererTransport{base: base, referer: media.Referer, headers: nativeMediaHeaders(headers)}
	probe := sourceHealthProbe{client: &probeClient, checkURL: checkURL}
	return probe.media(ctx, media.URL, 0, map[string]bool{})
}

type discoveryRefererTransport struct {
	base    http.RoundTripper
	referer string
	headers map[string]string
}

func (t discoveryRefererTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	applyNativeMediaHeaders(cloned, t.headers)
	applyMediaReferer(cloned, t.referer)
	return t.base.RoundTrip(cloned)
}

func verifyYQKDiscoverySources(ctx context.Context, input []source, episodeKey string, probe func(context.Context, string) error) []source {
	if probe == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	sources := make([]source, 0, len(yqkPlayers))
	seen := map[string]bool{}
	for _, src := range input {
		if len(sources) >= len(yqkPlayers) {
			break
		}
		if seen[src.Code] || strings.TrimSpace(src.Parse) != "" || !yqkAllowedSource(src) {
			continue
		}
		seen[src.Code] = true
		sources = append(sources, discoveryNormalizeSource(src))
	}
	passed := make([]bool, len(sources))
	work := make(chan int, len(sources))
	for index := range sources {
		work <- index
	}
	close(work)
	var workers sync.WaitGroup
	for i := 0; i < 3; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range work {
				if ctx.Err() != nil {
					return
				}
				samples := discoverySampleURLs(sources[index], episodeKey)
				valid := len(samples) > 0
				// Reserve time for the later batches too. Otherwise three slow
				// leading lines consume the whole aggregate budget before the
				// independently working lines at the end get any attempt.
				lineCtx, lineCancel := context.WithTimeout(ctx, yqkDiscoveryProbeBudget(ctx, len(sources)-index, 3, 24*time.Second))
				for sampleIndex, marker := range samples {
					probeCtx, cancel := context.WithTimeout(lineCtx, yqkDiscoveryProbeBudget(lineCtx, len(samples)-sampleIndex, 1, 12*time.Second))
					err := probe(probeCtx, marker)
					cancel()
					if err != nil || lineCtx.Err() != nil || ctx.Err() != nil {
						valid = false
						break
					}
				}
				lineCancel()
				passed[index] = valid
			}
		}()
	}
	workers.Wait()
	result := make([]source, 0, len(sources))
	for index, src := range sources {
		if passed[index] {
			result = append(result, src)
		}
	}
	return result
}

// Divide the remaining deadline between unstarted batches. The inner use with
// one worker also reserves a fair share for the final episode of a series.
func yqkDiscoveryProbeBudget(ctx context.Context, remaining, workers int, ceiling time.Duration) time.Duration {
	budget := ceiling
	if deadline, ok := ctx.Deadline(); ok {
		batches := (max(remaining, 1) + max(workers, 1) - 1) / max(workers, 1)
		budget = min(budget, time.Until(deadline)/time.Duration(batches))
	}
	return max(budget, time.Nanosecond)
}

func discoveryNormalizeTitle(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == '\u200b' || r == '\ufeff' {
			return -1
		}
		if r >= '\uff01' && r <= '\uff5e' {
			r -= 0xfee0
		}
		return unicode.ToLower(r)
	}, strings.TrimSpace(value))
}

func discoveryKind(value string) string {
	value = discoveryNormalizeTitle(value)
	switch value {
	case "movie", "series", "anime", "anime_movie", "variety", "documentary", "short":
		return value
	case "国漫", "日漫", "经典番剧", "特摄", "动态漫画":
		return "anime"
	case "剧场版":
		return "anime_movie"
	}
	for _, token := range []string{"短剧", "短片", "古装仙侠", "反转爽剧", "现代言情", "女频恋爱", "脑洞悬疑", "年代穿越", "逆袭", "shortdrama"} {
		if strings.Contains(value, token) {
			return "short"
		}
	}
	for _, token := range []string{"动画片", "动画电影", "动漫电影"} {
		if strings.Contains(value, token) {
			return "anime_movie"
		}
	}
	if strings.Contains(value, "动漫") || strings.Contains(value, "动画") {
		return "anime"
	}
	if strings.Contains(value, "纪录") || strings.Contains(value, "记录片") {
		return "documentary"
	}
	if strings.Contains(value, "综艺") {
		return "variety"
	}
	if strings.Contains(value, "电影") || moviePlaybackCategories[value] {
		return "movie"
	}
	if strings.Contains(value, "剧") {
		return "series"
	}
	return ""
}

var discoveryRegionSeparator = regexp.MustCompile(`[,，、/|;；]+`)
var discoveryYearPattern = regexp.MustCompile(`^[12][0-9]{3}$`)

func discoveryRegion(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	aliases := map[string]string{"中国": "cn", "china": "cn", "中国大陆": "cn", "大陆": "cn", "内地": "cn", "中国内地": "cn", "mainlandchina": "cn", "中国香港": "hk", "香港": "hk", "中国台湾": "tw", "台湾": "tw", "美国": "us", "日本": "jp", "韩国": "kr", "泰国": "th", "英国": "gb"}
	parts := discoveryRegionSeparator.Split(value, -1)
	normalized := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		part = discoveryNormalizeTitle(part)
		if part == "" || part == "未知" || part == "其他" || part == "其它" {
			return ""
		}
		if alias := aliases[part]; alias != "" {
			part = alias
		}
		if !seen[part] {
			normalized = append(normalized, part)
			seen[part] = true
		}
	}
	sort.Strings(normalized)
	return strings.Join(normalized, "/")
}

func discoveryMatches(target discoveryTarget, item map[string]any, collectorID int64) bool {
	return discoveryCompatible(target, item, collectorID, false)
}

func discoveryCompatible(target discoveryTarget, item map[string]any, collectorID int64, allowMissing bool) bool {
	if discoveryNormalizeTitle(target.Name) == "" || discoveryNormalizeTitle(target.Name) != discoveryNormalizeTitle(gconv.String(item["vod_name"])) {
		return false
	}
	known := target.SourceAPIID == collectorID && target.SourceAPIVID != "" && target.SourceAPIVID == gconv.String(item["vod_id"])
	year := func(raw string) string {
		raw = strings.TrimSpace(raw)
		if !discoveryYearPattern.MatchString(raw) {
			return ""
		}
		return raw
	}
	pairs := [][2]string{{year(target.Year), year(gconv.String(item["vod_year"]))}, {discoveryRegion(target.Area), discoveryRegion(gconv.String(item["vod_area"]))}, {discoveryKind(target.Kind), discoveryKind(gconv.String(item["type_name"]))}}
	for index, pair := range pairs {
		if pair[0] == "" || pair[1] == "" {
			if !allowMissing && !known {
				return false
			}
		} else if pair[0] != pair[1] {
			searchOnly := allowMissing && strings.TrimSpace(gconv.String(item["vod_play_from"])) == "" && strings.TrimSpace(gconv.String(item["vod_play_url"])) == ""
			if index == 2 && discoveryTheatricalAnimeMatch(target, item, searchOnly) {
				continue
			}
			return false
		}
	}
	return true
}

// Some providers classify a theatrical anime feature under their broad anime
// category. Accept that narrow taxonomic difference only with an explicit
// theatrical title and complete feature evidence. Missing/contradictory years
// or regions are still handled by discoveryCompatible, without relaxation.
func discoveryTheatricalAnimeMatch(target discoveryTarget, item map[string]any, searchOnly bool) bool {
	if discoveryKind(target.Kind) != "anime_movie" || discoveryKind(gconv.String(item["type_name"])) != "anime" {
		return false
	}
	title := discoveryNormalizeTitle(target.Name)
	if title == "" || title != discoveryNormalizeTitle(gconv.String(item["vod_name"])) ||
		(!strings.Contains(title, "剧场版") && !strings.Contains(title, "劇場版")) ||
		strings.Contains(title, "tv") || strings.Contains(title, "电视") || strings.Contains(title, "電視") ||
		moviePartialLabel.MatchString(title) {
		return false
	}
	if searchOnly {
		// Search entries omit playlists. This only permits fetching the detail;
		// the final match below is never established by a title alone.
		return true
	}
	sources := playlist(row{"play_from": item["vod_play_from"], "play_url": item["vod_play_url"]})
	if len(sources) == 0 || !moviePlaybackAlignmentAllowed(target.Name, true, sources) {
		return false
	}
	features := 0
	for _, src := range sources {
		for _, ep := range src.Episodes {
			if movieSupplementLabel.MatchString(ep.Name) {
				continue
			}
			if !movieFeatureIdentity(target.Name, ep.Name) || strings.TrimSpace(ep.URL) == "" {
				return false
			}
			features++
		}
	}
	return features > 0
}

func discoveryNormalizeSource(src source) source {
	src.Code, src.Name, src.Parse = strings.TrimSpace(src.Code), strings.TrimSpace(src.Name), strings.TrimSpace(src.Parse)
	episodes := make([]episode, 0, len(src.Episodes))
	seen := map[string]bool{}
	for _, ep := range src.Episodes {
		ep.Name, ep.URL = strings.TrimSpace(ep.Name), strings.TrimSpace(ep.URL)
		key, _ := playbackEpisodeIdentity(ep.Name)
		if ep.Name == "" || ep.URL == "" || seen[key] {
			continue
		}
		seen[key] = true
		episodes = append(episodes, ep)
	}
	sort.SliceStable(episodes, func(i, j int) bool {
		_, left := playbackEpisodeIdentity(episodes[i].Name)
		_, right := playbackEpisodeIdentity(episodes[j].Name)
		if left >= 0 && right >= 0 {
			return left < right
		}
		return left >= 0 && right < 0
	})
	src.Episodes = episodes
	return src
}

func discoverySampleURLs(src source, currentKey string) []string {
	regular := make([]episode, 0, len(src.Episodes))
	var current string
	for _, ep := range src.Episodes {
		key, number := playbackEpisodeIdentity(ep.Name)
		if number >= 0 {
			regular = append(regular, ep)
		}
		if currentKey != "" && key == currentKey {
			current = ep.URL
		}
	}
	if currentKey != "" && current == "" {
		return nil
	}
	if len(regular) == 0 {
		regular = src.Episodes
	}
	if len(regular) == 0 {
		return nil
	}
	samples := []string{regular[0].URL, regular[len(regular)-1].URL, current}
	seen := map[string]bool{}
	result := make([]string, 0, 3)
	for _, raw := range samples {
		if raw != "" && !seen[raw] {
			seen[raw] = true
			result = append(result, raw)
		}
	}
	return result
}
