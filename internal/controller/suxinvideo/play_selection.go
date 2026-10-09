package suxinvideo

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

// Only unqualified episode numbers are interchangeable. Season labels,
// previews, specials and 上/下 remain distinct even if they contain digits.
var canonicalEpisodeNumber = regexp.MustCompile(`(?i)^(?:第\s*([0-9]+)\s*集|(?:ep|e)\s*([0-9]+)|([0-9]+))$`)

func playbackEpisodeIdentity(name string) (string, int) {
	name = strings.TrimSpace(name)
	if match := canonicalEpisodeNumber.FindStringSubmatch(name); match != nil {
		for _, digits := range match[1:] {
			if digits == "" {
				continue
			}
			if number, err := strconv.Atoi(digits); err == nil && number >= 0 && number < 10_000_000 {
				return "episode:" + strconv.Itoa(number), number
			}
		}
	}
	return "label:" + name, -1
}

// Movie labels vary across suppliers: the title, HD国语 and 第01集 can
// describe the same feature. Require a known movie category and one item
// per participating line; series, trailers and split editions stay distinct.
var moviePlaybackCategories = map[string]bool{
	"电影": true, "电影片": true, "动画片": true, "动画电影": true, "动漫电影": true,
	"剧情片": true, "动作片": true, "喜剧片": true, "爱情片": true, "科幻片": true,
	"恐怖片": true, "惊悚片": true, "悬疑片": true, "犯罪片": true, "战争片": true,
	"奇幻片": true, "冒险片": true, "武侠片": true, "伦理片": true,
}

func moviePlaybackCategory(ctx context.Context, typeID int64) (bool, error) {
	seen := map[int64]bool{}
	for depth := 0; typeID > 0 && depth < 8 && !seen[typeID]; depth++ {
		seen[typeID] = true
		category, err := one(ctx, "SELECT name,pid FROM sx_type WHERE id=?", typeID)
		if err != nil {
			return false, err
		}
		if category == nil {
			return false, nil
		}
		if moviePlaybackCategories[strings.TrimSpace(gconv.String(category["name"]))] {
			return true, nil
		}
		typeID = gconv.Int64(category["pid"])
	}
	return false, nil
}

// Use the same taxonomy as the anime channel, including animated films and
// imported child categories. An erciyuan line alone is not genre evidence:
// that provider also supplies live-action films and television series.
func animePlaybackMetadata(vod row) bool {
	labels := strings.FieldsFunc(gconv.String(vod["class"]), func(r rune) bool {
		return strings.ContainsRune(",，、/|;； \t\r\n", r)
	})
	for _, label := range labels {
		if strings.EqualFold(label, "anime") || playbackCategoryHasChannel(label, 8) {
			return true
		}
	}
	return false
}

func playbackCategoryHasChannel(name string, channel int) bool {
	for _, current := range localCategoryChannels(name) {
		if current == channel {
			return true
		}
	}
	return false
}

func animePlaybackCategory(ctx context.Context, vod row) (bool, error) {
	if animePlaybackMetadata(vod) {
		return true, nil
	}
	seen := map[int64]bool{}
	for id, depth := gconv.Int64(vod["type_id"]), 0; id > 0 && depth < 8 && !seen[id]; depth++ {
		seen[id] = true
		category, err := one(ctx, "SELECT name,pid FROM sx_type WHERE id=?", id)
		if err != nil {
			return false, err
		}
		if category == nil {
			return false, nil
		}
		if playbackCategoryHasChannel(gconv.String(category["name"]), 8) {
			return true, nil
		}
		id = gconv.Int64(category["pid"])
	}
	return false, nil
}

func playbackPreferredProvider(shortDrama, anime bool) (code, name string) {
	if shortDrama {
		return "hongguo", "红果"
	}
	if anime {
		return "erciyuan", "二次元"
	}
	return "yqk", "小柒APP / 小柒"
}

type compilationPlayFallback struct {
	Code string `json:"code"`
	Key  string `json:"key"`
}

// A single full-series compilation is not episode one. Only a fresh entry
// into an explicitly labelled compilation may prefer Hongguo's first chapter;
// the identities remain distinct and an existing resume position never crosses.
func shortDramaCompilationStart(req *PlayReq, original []source, visible []playSource, selected int, requestedKey string, shortDrama bool, startPosition int64) (string, *compilationPlayFallback) {
	if !shortDrama || req == nil || req.Episode != 0 || startPosition != 0 {
		return requestedKey, nil
	}
	code := req.Line
	if code == "" && selected >= 0 && selected < len(visible) {
		code = visible[selected].Code
	}
	var compilation *source
	for i := range original {
		if original[i].Code == code {
			compilation = &original[i]
			break
		}
	}
	if compilation == nil || len(compilation.Episodes) != 1 {
		return requestedKey, nil
	}
	label := strings.Join(strings.Fields(compilation.Episodes[0].Name), "")
	if label != "全集" && label != "全集完结" {
		return requestedKey, nil
	}
	originKey, _ := playbackEpisodeIdentity(compilation.Episodes[0].Name)
	if originKey != requestedKey {
		return requestedKey, nil
	}
	for _, src := range visible {
		if src.Code != "hongguo" && src.BaseCode != "hongguo" {
			continue
		}
		for _, item := range src.Episodes {
			if item.Key == "episode:1" && item.Number == 1 && strings.HasPrefix(item.URL, "hongguo://") && strings.HasPrefix(item.PSrc, "/suxinvideo/native/resolve?") && item.Iframe == "" {
				return item.Key, &compilationPlayFallback{Code: code, Key: originKey}
			}
		}
	}
	return requestedKey, nil
}

var movieFeatureLabel = regexp.MustCompile(`(?i)^(?:正片|全片|完整版|高清|超清|蓝光|HD|BD|SD|TC|TS|FHD|UHD|4K|[1-9][0-9]{2,3}P|国语|粤语|英语|原声|中字|中英双字|中文字幕|双语)+$`)
var movieCompleteFirstEpisode = regexp.MustCompile(`^第0*1集完结$`)
var moviePartialLabel = regexp.MustCompile(`(?i)预告|花絮|解说|片段|试看|剪辑|删减|加长|抢先|上集|下集|上部|下部|上篇|下篇|上半|下半|合集|合辑|系列|trailer|teaser|preview|part|全集|第[0-9一二三四五六七八九十]+季|s[0-9]+e[0-9]+|[（(【\[](?:上|下|中)[）)】\]]`)
var movieSupplementLabel = regexp.MustCompile(`(?i)预告|花絮|解说|片段|试看|剪辑|特辑|幕后|trailer|teaser|preview`)
var movieSplitPlaybackLabel = regexp.MustCompile(`(?i)上集|下集|上部|下部|上篇|下篇|上半|下半|合集|合辑|系列|全集|part\s*[0-9一二三四五六七八九十]+|第[0-9一二三四五六七八九十]+(?:季|部|部分)|s[0-9]+e[0-9]+|[（(【\[](?:上|下|中)[）)】\]]|(?:第|ep?|)[0-9]+\s*[-~]\s*[0-9]+|(?:上|下|中)$`)

func movieFeatureIdentity(title, label string) bool {
	compact := func(s string) string { return strings.Join(strings.Fields(s), "") }
	label, title = compact(label), compact(title)
	if label == "" || moviePartialLabel.MatchString(label) || moviePartialLabel.MatchString(title) {
		return false
	}
	if label == title || movieFeatureLabel.MatchString(label) || movieCompleteFirstEpisode.MatchString(label) {
		return true
	}
	_, number := playbackEpisodeIdentity(label)
	return number == 1
}

// Several entries can be complete versions of one movie (TC, HD, 1080P),
// rather than separate chapters. Inspect the original, unfiltered playlists
// so a disabled/hidden split release still prevents unsafe cross-part matching.
func moviePlaybackAlignmentAllowed(title string, movie bool, original []source) bool {
	compactTitle := strings.Join(strings.Fields(title), "")
	if !movie || moviePartialLabel.MatchString(compactTitle) {
		return false
	}
	for _, src := range original {
		for _, ep := range src.Episodes {
			label := strings.Join(strings.Fields(ep.Name), "")
			if label != "" && label == compactTitle {
				// Numeric movie titles (e.g. 1917) are not episode numbers.
				continue
			}
			if movieSplitPlaybackLabel.MatchString(label) {
				return false
			}
			_, number := playbackEpisodeIdentity(ep.Name)
			if number >= 0 {
				// Even one numbered item inside a multi-item release is
				// ambiguous. Never turn episode 1/2 into movie variants.
				if number != 1 || len(src.Episodes) > 1 {
					return false
				}
				continue
			}
			if len(src.Episodes) > 1 && !movieFeatureIdentity(title, ep.Name) && !movieSupplementLabel.MatchString(label) {
				// An unknown multi-item label is not proof of a complete
				// movie. Preserve the original identities conservatively.
				return false
			}
		}
	}
	return true
}

func moviePlaybackEpisodeKey(title, label string, movie bool) (string, int) {
	if movie && movieFeatureIdentity(title, label) {
		return "movie:feature", -1
	}
	return playbackEpisodeIdentity(label)
}

func alignMoviePlaybackSources(title string, movie bool, sources []playSource) {
	if !movie {
		return
	}
	evidence := make([]source, 0, len(sources))
	for _, src := range sources {
		item := source{}
		for _, ep := range src.Episodes {
			item.Episodes = append(item.Episodes, episode{Name: ep.Name})
		}
		evidence = append(evidence, item)
	}
	if !moviePlaybackAlignmentAllowed(title, movie, evidence) {
		return
	}
	for i := range sources {
		for j := range sources[i].Episodes {
			ep := &sources[i].Episodes[j]
			ep.Key, ep.Number = moviePlaybackEpisodeKey(title, ep.Name, true)
		}
	}
}

func playSourceIndex(sources []source, line string, index int, explicitIndex bool) int {
	if line != "" {
		for i, src := range sources {
			if src.Code == line {
				return i
			}
		}
		// A bookmarked line may now be disabled. Its former numeric index can
		// refer to a different source, so select the normal default instead.
		return preferredSource(sources)
	}
	if explicitIndex && index >= 0 && index < len(sources) {
		return index
	}
	return preferredSource(sources)
}
