package suxinvideo

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"github.com/suxinwl/GoSuxin/internal/erciyuan"
)

const erciyuanSourceURL = "erciyuan://app"

var erciyuanCollector = erciyuan.NewClient(safeCollectorHTTPClient(30 * time.Second))

var erciyuanPlayers = map[string]string{
	"ecy_aa02": "二次元·极速", "ecy_aa03": "二次元·电信",
	"ecy_dd02": "二次元·有广", "ecy_4k01": "二次元·4K",
}

func isErciyuanSource(raw string) bool { return strings.TrimSpace(raw) == erciyuanSourceURL }

func requiredCollectPlaylistError(raw string, item map[string]any) error {
	if strings.TrimSpace(gconv.String(item["vod_play_url"])) != "" {
		return nil
	}
	if isErciyuanSource(raw) {
		return errors.New("二次元影片详情未返回可用线路")
	}
	if isYQKSource(raw) {
		return errors.New("小柒影片详情未返回可用线路")
	}
	return nil
}

// A recent 二次元 feed has exactly one page per actual category, including
// empty filtered pages. Scheduled jobs visit those six pages rather than
// stopping after Japanese anime; manual "only this page" retains its meaning.
func erciyuanCollectJobScope(mode, raw string, hours int, onePage bool) (int, bool) {
	if mode != "manual" && isErciyuanSource(raw) {
		return max(1, hours), false
	}
	return hours, onePage
}

func erciyuanPlaybackCodes() []string {
	return []string{"ecy_aa02", "ecy_aa03", "ecy_dd02", "ecy_4k01"}
}

func erciyuanCollectionMarker(raw string) (string, string, int, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "erciyuan" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Port() != "" || !onlyDigits(u.Host) {
		return "", "", 0, errors.New("二次元分集标识无效")
	}
	id, err := strconv.ParseInt(u.Host, 10, 64)
	if err != nil || id < 1 || strconv.FormatInt(id, 10) != u.Host || u.RawPath != "" {
		return "", "", 0, errors.New("二次元影片编号无效")
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) != 2 {
		return "", "", 0, errors.New("二次元分集标识无效")
	}
	if _, ok := erciyuanPlayers["ecy_"+parts[0]]; !ok {
		return "", "", 0, errors.New("二次元线路无效")
	}
	episode, err := strconv.Atoi(parts[1])
	if err != nil || episode < 0 || episode >= 10000 || strconv.Itoa(episode) != parts[1] {
		return "", "", 0, errors.New("二次元分集编号无效")
	}
	return u.Host, parts[0], episode, nil
}

func erciyuanAllowedSource(src source) bool {
	if _, ok := erciyuanPlayers[src.Code]; !ok || len(src.Episodes) == 0 || len(src.Episodes) > 10000 {
		return false
	}
	film := ""
	for _, ep := range src.Episodes {
		id, line, _, err := erciyuanCollectionMarker(ep.URL)
		if err != nil || "ecy_"+line != src.Code || (film != "" && film != id) {
			return false
		}
		film = id
	}
	return true
}

type erciyuanCollectProvider interface {
	Categories(context.Context) ([]erciyuan.Category, error)
	List(context.Context, int, int, int) (erciyuan.Page, error)
	Search(context.Context, string) ([]map[string]any, error)
	Detail(context.Context, string) (erciyuan.Detail, error)
}

func fetchErciyuanCollectSource(ctx context.Context, params url.Values) (macPayload, error) {
	return fetchErciyuanCollectSourceWith(ctx, params, erciyuanCollector)
}

func erciyuanCollectionItem(item map[string]any) map[string]any {
	copy := make(map[string]any, len(item)+1)
	for key, value := range item {
		copy[key] = value
	}
	copy["__erciyuan"] = true
	return copy
}

func fetchErciyuanCollectSourceWith(ctx context.Context, params url.Values, provider erciyuanCollectProvider) (macPayload, error) {
	switch params.Get("ac") {
	case "list":
		categories, err := provider.Categories(ctx)
		if err != nil {
			return macPayload{}, err
		}
		classes := make([]map[string]any, 0, len(categories))
		for _, category := range categories {
			classes = append(classes, map[string]any{"type_id": category.ID, "type_name": category.Name})
		}
		return macPayload{Code: 1, Class: classes}, nil
	case "detail":
		id := strings.TrimSpace(params.Get("ids"))
		if !onlyDigits(id) || gconv.Int64(id) <= 0 || strconv.FormatInt(gconv.Int64(id), 10) != id {
			return macPayload{}, errors.New("二次元影片编号无效")
		}
		detail, err := provider.Detail(ctx, id)
		if err != nil {
			return macPayload{}, err
		}
		item, err := erciyuanDetailCollectionItem(id, detail)
		if err != nil {
			return macPayload{}, err
		}
		return macPayload{Code: 1, List: []map[string]any{item}, Total: 1, Page: 1, PageCount: 1}, nil
	case "videolist":
	default:
		return macPayload{}, errors.New("二次元采集操作不支持")
	}
	page, err := strconv.Atoi(params.Get("pg"))
	if err != nil || page < 1 || page > 10000 {
		return macPayload{}, errors.New("二次元采集页码无效")
	}
	if keyword := strings.TrimSpace(params.Get("wd")); keyword != "" {
		if page != 1 || len([]rune(keyword)) > 120 {
			return macPayload{}, errors.New("二次元搜索仅支持第一页及 120 字以内关键词")
		}
		items, err := provider.Search(ctx, keyword)
		if err != nil {
			return macPayload{}, err
		}
		list := make([]map[string]any, 0, min(40, len(items)))
		for _, item := range items {
			list = append(list, erciyuanCollectionItem(item))
			if len(list) == 40 {
				break
			}
		}
		return macPayload{Code: 1, List: list, Total: len(list), Page: 1, PageCount: 1}, nil
	}
	category, hours := 0, 0
	for _, param := range []struct {
		name string
		dest *int
	}{{"t", &category}, {"h", &hours}} {
		if raw := params.Get(param.name); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 0 {
				return macPayload{}, errors.New("二次元分类或采集时段无效")
			}
			*param.dest = value
		}
	}
	if hours > 720 {
		return macPayload{}, errors.New("二次元采集时段无效")
	}
	if category > 0 {
		if _, ok := erciyuanCategoryAliases[category]; !ok {
			return macPayload{}, errors.New("二次元分类无效")
		}
	}
	result, err := provider.List(ctx, category, page, hours)
	if err != nil {
		return macPayload{}, err
	}
	if len(result.Items) > 1000 || result.Page < 1 || result.PageCount < 1 || result.Total < 0 {
		return macPayload{}, errors.New("二次元影片列表格式无效")
	}
	list := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		list = append(list, erciyuanCollectionItem(item))
	}
	return macPayload{Code: 1, List: list, Total: result.Total, Page: result.Page, PageCount: result.PageCount}, nil
}

func erciyuanDetailCollectionItem(id string, detail erciyuan.Detail) (map[string]any, error) {
	item := erciyuanCollectionItem(detail.Item)
	if gconv.String(item["vod_id"]) != id || strings.TrimSpace(gconv.String(item["vod_name"])) == "" {
		return nil, errors.New("二次元影片详情身份无效")
	}
	var codes, groups []string
	seen := make(map[string]bool)
	for _, line := range detail.Lines {
		code := strings.TrimPrefix(line.Code, "ecy_")
		if _, ok := erciyuanPlayers["ecy_"+code]; !ok || seen[code] {
			return nil, errors.New("二次元返回了未知或重复线路")
		}
		seen[code] = true
		if len(line.Episodes) == 0 {
			continue
		}
		if len(line.Episodes) > 10000 {
			return nil, errors.New("二次元分集数量过多")
		}
		episodes := make([]string, 0, len(line.Episodes))
		for _, ep := range line.Episodes {
			name := strings.TrimSpace(strings.NewReplacer("$", "", "#", "").Replace(ep.Name))
			if name == "" {
				return nil, errors.New("二次元分集名称缺失")
			}
			filmID, markerLine, _, err := erciyuanCollectionMarker(ep.URL)
			if err != nil || filmID != id || markerLine != code {
				return nil, errors.New("二次元分集与影片或线路不符")
			}
			// Preserve the provider's original index. Invalid upstream episodes
			// may have been omitted, leaving intentional gaps in this sequence.
			episodes = append(episodes, name+"$"+ep.URL)
		}
		codes = append(codes, "ecy_"+code)
		groups = append(groups, strings.Join(episodes, "#"))
	}
	if len(codes) == 0 {
		return nil, errors.New("二次元影片详情未返回可用线路")
	}
	item["vod_play_from"], item["vod_play_url"] = strings.Join(codes, "$$$"), strings.Join(groups, "$$$")
	return item, nil
}

func erciyuanCollectionSourceEnabled(collector row) bool {
	return collector != nil && isErciyuanSource(gconv.String(collector["api_url"])) && gconv.Int(collector["status"]) == 1
}

func validateErciyuanCollectedPlaylist(collector row, remoteID, from, play string) (string, string, error) {
	if !erciyuanCollectionSourceEnabled(collector) {
		return "", "", errors.New("二次元采集源不存在或已停用")
	}
	if !onlyDigits(remoteID) || gconv.Int64(remoteID) <= 0 || strconv.FormatInt(gconv.Int64(remoteID), 10) != remoteID {
		return "", "", errors.New("二次元影片编号无效")
	}
	sources := playlist(row{"play_from": from, "play_url": play})
	if len(sources) == 0 || len(sources) != len(strings.Split(from, "$$$")) {
		return "", "", errors.New("二次元影片详情未返回可用线路")
	}
	seen := make(map[string]bool)
	for i, src := range sources {
		if !erciyuanAllowedSource(src) || seen[src.Code] {
			return "", "", errors.New("二次元返回了未知线路或无效分集")
		}
		seen[src.Code] = true
		for _, ep := range src.Episodes {
			id, _, _, _ := erciyuanCollectionMarker(ep.URL)
			if id != remoteID {
				return "", "", errors.New("二次元分集与影片编号不符")
			}
		}
		sources[i] = discoveryNormalizeSource(src)
	}
	from, play = serializeDiscoverySources(sources)
	return from, play, nil
}

func erciyuanFilmTarget(ctx context.Context, vod row, apiID int64, remoteID string) (discoveryTarget, error) {
	kind, err := discoveryCategoryKind(ctx, gconv.Int64(vod["type_id"]))
	target := discoveryTarget{ID: gconv.Int64(vod["id"]), Name: gconv.String(vod["name"]), Year: gconv.String(vod["year"]), Area: gconv.String(vod["area"]), Kind: kind}
	for _, src := range playlist(vod) {
		if !erciyuanAllowedSource(src) {
			continue
		}
		for _, ep := range src.Episodes {
			id, _, _, markerErr := erciyuanCollectionMarker(ep.URL)
			if markerErr == nil && id == remoteID {
				target.SourceAPIID, target.SourceAPIVID = apiID, remoteID
				return target, err
			}
		}
	}
	return target, err
}

func validateErciyuanCollectionIdentity(ctx context.Context, existing row, apiID int64, item map[string]any) error {
	target, err := erciyuanFilmTarget(ctx, existing, apiID, gconv.String(item["vod_id"]))
	if err != nil {
		return err
	}
	if gconv.Int64(existing["api_id"]) == apiID && gconv.String(existing["api_vid"]) == gconv.String(item["vod_id"]) {
		target.SourceAPIID, target.SourceAPIVID = apiID, gconv.String(item["vod_id"])
	}
	if area := gconv.String(existing["__collection_area"]); discoveryRegion(target.Area) == "" && discoveryRegion(area) != "" {
		target.Area = area
	}
	if !discoveryMatches(target, item, apiID) && !discoveryErciyuanTheatricalMatch(target, item) {
		return errors.New("二次元影片身份与已有记录冲突，已保留原影片资源")
	}
	return nil
}
