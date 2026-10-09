package app

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

// Work is the user-facing representation of a title. A work keeps the
// provider records intact while presenting one card to the UI.
type Work struct {
	ID                   string       `json:"id"`
	Title                string       `json:"title"`
	Name                 string       `json:"name"`
	Source               string       `json:"source,omitempty"`
	SourceID             string       `json:"sourceId,omitempty"`
	NormalizedTitle      string       `json:"normalizedTitle"`
	MediaType            string       `json:"mediaType"`
	CategoryPath         []string     `json:"categoryPath,omitempty"`
	RawCategory          string       `json:"rawCategory,omitempty"`
	RawCategories        []string     `json:"rawCategories,omitempty"`
	Desc                 string       `json:"desc,omitempty"`
	Intro                string       `json:"intro,omitempty"`
	Cover                any          `json:"cover,omitempty"`
	CoverURL             any          `json:"coverUrl,omitempty"`
	TotalEpisode         any          `json:"totalEpisode,omitempty"`
	Duration             any          `json:"duration,omitempty"`
	EpisodeDuration      any          `json:"episodeDuration,omitempty"`
	ReleaseStatus        string       `json:"releaseStatus,omitempty"`
	OnlineDate           string       `json:"onlineDate,omitempty"`
	Score                string       `json:"score,omitempty"`
	Views                string       `json:"views,omitempty"`
	Heat                 string       `json:"heat,omitempty"`
	Tags                 []string     `json:"tags,omitempty"`
	ProviderCategory     string       `json:"providerCategory,omitempty"`
	ProviderCategoryType string       `json:"providerCategoryType,omitempty"`
	Aliases              []string     `json:"aliases,omitempty"`
	PrimarySource        string       `json:"primarySource"`
	PrimaryDramaID       string       `json:"primaryDramaId"`
	Sources              []WorkSource `json:"sources"`
	SourceCount          int          `json:"sourceCount"`
	RawIDs               []string     `json:"rawIds,omitempty"`
}

type WorkSource struct {
	Source       string `json:"source"`
	SourceID     string `json:"sourceId"`
	DramaID      string `json:"dramaId"`
	Title        string `json:"title"`
	EpisodeCount any    `json:"episodeCount,omitempty"`
	Status       string `json:"status,omitempty"`
	Priority     int    `json:"priority"`
	Available    bool   `json:"available"`
	RawCategory  string `json:"rawCategory,omitempty"`
}

var workSeasonPattern = regexp.MustCompile(`(?i)(?:第\s*[0-9一二三四五六七八九十百]+\s*季|\bS\s*[0-9]+\b|\s+季)$`)
var workYearPattern = regexp.MustCompile(`(?:19|20)\d{2}`)
var workNumberPattern = regexp.MustCompile(`[-+]?\d+(?:\.\d+)?`)

var workSourcePriority = []string{
	sourceHongguo, sourceLZ, sourceFF, sourceWJ, sourceBF, sourceHN,
	sourceSD, sourceBD, sourceXL, sourceYQK, sourceHuangdou, sourceHuangguoAI, sourceHuangguoVideo, source4KVM,
}

// workCatalogVersion is persisted alongside the raw library cache. Bumping it
// allows a future normalizer/category change to rebuild the derived index while
// keeping every original Drama record intact.
const workCatalogVersion = 3

var workSourcePriorityMu sync.RWMutex
var configuredWorkSourcePriority = append([]string(nil), workSourcePriority...)

func normalizeSourcePriority(values []string) []string {
	result := make([]string, 0, len(workSourcePriority))
	seen := map[string]bool{}
	appendSource := func(value string) {
		if mapped, ok := map[string]string{"红果": sourceHongguo, "量子": sourceLZ, "非凡": sourceFF, "无极": sourceWJ, "暴风": sourceBF, "红牛": sourceHN, "闪电": sourceSD, "百度": sourceBD, "迅雷": sourceXL, "一起看": sourceYQK, "黄豆": sourceHuangdou, "黄果": sourceHuangguoAI}[strings.TrimSpace(value)]; ok {
			value = mapped
		}
		value = canonicalProviderSource(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		result = append(result, value)
	}
	appendSource(sourceHongguo)
	for _, value := range values {
		appendSource(value)
	}
	for _, value := range workSourcePriority {
		appendSource(value)
	}
	return result
}

func setWorkSourcePriority(values []string) []string {
	result := normalizeSourcePriority(values)
	workSourcePriorityMu.Lock()
	configuredWorkSourcePriority = append([]string(nil), result...)
	workSourcePriorityMu.Unlock()
	return result
}

func getWorkSourcePriority() []string {
	workSourcePriorityMu.RLock()
	defer workSourcePriorityMu.RUnlock()
	return append([]string(nil), configuredWorkSourcePriority...)
}

func sourcePriorityIndex(source string) int {
	source = canonicalProviderSource(source)
	for index, candidate := range getWorkSourcePriority() {
		if candidate == source {
			return index
		}
	}
	return len(workSourcePriority) + 1
}

func normalizeWorkTitle(title string) string {
	// Keep the normalizer dependency-free. Full-width ASCII characters are
	// folded explicitly, while the remaining Unicode text is preserved.
	title = strings.Map(func(r rune) rune {
		if r >= 0xFF01 && r <= 0xFF5E {
			return r - 0xFEE0
		}
		if r == 0x3000 {
			return ' '
		}
		return r
	}, strings.TrimSpace(title))
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func workSeason(title string) string {
	match := workSeasonPattern.FindString(strings.TrimSpace(title))
	if match == "" {
		return ""
	}
	return normalizeWorkTitle(match)
}

func workYear(drama Drama) string {
	for _, value := range []string{drama.OnlineDate, drama.SortName, drama.Remark} {
		if match := workYearPattern.FindString(value); match != "" {
			return match
		}
	}
	return ""
}

func workEpisodeCount(drama Drama) int {
	for _, value := range []any{drama.TotalEpisode, drama.TotalEpisodeSnake, drama.EpisodeCount, drama.EpisodeCountSnake, drama.ChapterCount, drama.ChapterCountSnake, drama.Total} {
		text := strings.TrimSpace(workValueText(value))
		if text == "" {
			continue
		}
		if count, err := strconv.Atoi(text); err == nil && count > 0 {
			return count
		}
		if match := workNumberPattern.FindString(text); match != "" {
			if count, err := strconv.Atoi(match); err == nil && count > 0 {
				return count
			}
		}
	}
	return 0
}

// workDurationSeconds accepts the numeric seconds used by Red Fruit as well
// as the common text forms returned by other providers. Red Fruit's series
// duration is the total duration of all episodes; callers divide it by the
// episode count when deciding whether the episodes are short.
func workDurationSeconds(value any) float64 {
	text := strings.TrimSpace(strings.ToLower(workValueText(value)))
	if text == "" {
		return 0
	}
	if strings.HasPrefix(text, "pt") {
		var total float64
		for _, unit := range []struct {
			suffix string
			factor float64
		}{
			{"h", 3600}, {"m", 60}, {"s", 1},
		} {
			if index := strings.Index(text, unit.suffix); index >= 0 {
				prefix := text[:index]
				matches := workNumberPattern.FindAllString(prefix, -1)
				match := ""
				if len(matches) > 0 {
					match = matches[len(matches)-1]
				}
				if value, err := strconv.ParseFloat(match, 64); err == nil {
					total += value * unit.factor
				}
			}
		}
		if total > 0 {
			return total
		}
	}
	if strings.Contains(text, ":") {
		parts := strings.Split(text, ":")
		if len(parts) >= 2 && len(parts) <= 3 {
			seconds, err := strconv.ParseFloat(strings.TrimSpace(parts[len(parts)-1]), 64)
			if err == nil {
				minutes, _ := strconv.ParseFloat(strings.TrimSpace(parts[len(parts)-2]), 64)
				hours := float64(0)
				if len(parts) == 3 {
					hours, _ = strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
				}
				return hours*3600 + minutes*60 + seconds
			}
		}
	}
	match := workNumberPattern.FindString(text)
	if match == "" {
		return 0
	}
	number, err := strconv.ParseFloat(match, 64)
	if err != nil || number <= 0 {
		return 0
	}
	switch {
	case strings.Contains(text, "毫秒"), strings.HasSuffix(text, "ms"):
		number /= 1000
	case strings.Contains(text, "小时"), strings.HasSuffix(text, "h"):
		number *= 3600
	case strings.Contains(text, "分钟"), strings.Contains(text, "分"), strings.HasSuffix(text, "min"):
		number *= 60
	case number > 100000:
		// A few providers expose milliseconds without a unit.
		number /= 1000
	}
	return number
}

func hongguoAnimeMarker(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if strings.Contains(value, "漫剧") || strings.Contains(value, "动漫") || strings.Contains(value, "动画") || strings.Contains(value, "漫画") || strings.Contains(value, "番剧") || strings.Contains(value, "二次元") || strings.Contains(value, "少儿") || strings.Contains(value, "儿童") || strings.Contains(value, "亲子") || strings.Contains(value, "comic") || strings.Contains(value, "animation") {
		return true
	}
	// Red Fruit's comic feed occasionally uses compact labels such as
	// “钓鱼漫” or “动态漫”. Restrict the suffix check to short category/tag
	// values so ordinary titles such as “浪漫” are not treated as animation.
	return strings.HasSuffix(value, "漫") && len([]rune(value)) <= 8 && !strings.Contains(value, "浪漫")
}

func hongguoShortMarker(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(value, "真人剧") || strings.Contains(value, "真人视频") || strings.Contains(value, "短剧") || strings.Contains(value, "short_play") || strings.Contains(value, "short-play") || strings.Contains(value, "short drama") || strings.Contains(value, "short-drama") || strings.Contains(value, "real-drama") || strings.Contains(value, "real_drama") || strings.Contains(value, "real-video") || strings.Contains(value, "real_video") || strings.Contains(value, "ai剧") || strings.Contains(value, "ai短剧") || strings.Contains(value, "ai_series") || strings.Contains(value, "ai-series") || strings.Contains(value, "ai_video") || strings.Contains(value, "ai-video")
}

func hongguoMediaType(drama Drama) string {
	if dramaProvider(drama) != sourceHongguo {
		return ""
	}
	hasShortMarker := false
	for _, value := range []string{drama.ProviderCategory, drama.ProviderCategoryType, drama.ProviderContentType} {
		if hongguoAnimeMarker(value) {
			return "anime"
		}
		if hongguoShortMarker(value) {
			hasShortMarker = true
		}
	}
	// Some older rows have no dedicated provider marker, but their original
	// genre/tags still identify a Red Fruit comic feed.
	for _, value := range append([]string{drama.CategoryName, drama.CategoryNameSnake, drama.TypeName, drama.TypeNameSnake, drama.Category}, drama.Tags...) {
		if hongguoAnimeMarker(value) {
			return "anime"
		}
		if hongguoShortMarker(value) {
			hasShortMarker = true
		}
	}
	if hasShortMarker {
		return "short-drama"
	}
	for _, value := range []string{drama.CategoryName, drama.CategoryNameSnake, drama.TypeName, drama.TypeNameSnake, drama.Category} {
		category := strings.ToLower(strings.TrimSpace(value))
		if strings.Contains(category, "电影") || strings.Contains(category, "纪录") || strings.Contains(category, "综艺") || strings.Contains(category, "体育") {
			// Preserve an explicit non-drama top-level category even when a
			// provider also reports a large episode count.
			return ""
		}
	}

	episodes := workEpisodeCount(drama)
	episodeDuration := workDurationSeconds(drama.EpisodeDuration)
	if episodeDuration > 0 {
		if episodeDuration <= 15*60 {
			return "short-drama"
		}
		return ""
	}
	totalDuration := workDurationSeconds(drama.Duration)
	if totalDuration > 0 {
		if episodes > 1 && totalDuration/float64(episodes) < 5*60 {
			return "short-drama"
		}
		if episodes <= 1 && totalDuration <= 15*60 {
			return "short-drama"
		}
		// A known long duration is useful negative evidence; do not fall back
		// to the episode-count heuristic in that case.
		return ""
	}
	// Historical Red Fruit catalog rows did not persist their feed or
	// duration. The catalog is overwhelmingly short-form and its long
	// episode counts provide a bounded, deterministic backfill for those rows.
	// Red Fruit's catalog is made up of mini-episodes; five or more episodes
	// is enough to distinguish it from one-off provider records while still
	// leaving the tiny ambiguous tail available for duration-based updates.
	if episodes >= 5 {
		return "short-drama"
	}
	return ""
}

func workMediaType(drama Drama) string {
	if media := hongguoMediaType(drama); media != "" {
		return media
	}
	rawCategory := firstNonEmpty(drama.CategoryName, drama.CategoryNameSnake, drama.TypeName, drama.TypeNameSnake, drama.Category, drama.ChannelName)
	category := strings.ToLower(rawCategory)
	if mapped := categoryMapping(dramaProvider(drama), rawCategory); mapped != "" {
		category = strings.ToLower(mapped)
	}
	switch {
	case strings.Contains(category, "短剧"), strings.Contains(category, "爽文"), strings.Contains(category, "脑洞"), strings.Contains(category, "女频"), strings.Contains(category, "男频"):
		return "short-drama"
	case strings.Contains(category, "动漫"), strings.Contains(category, "动画"):
		return "anime"
	case strings.Contains(category, "综艺"):
		return "variety"
	case strings.Contains(category, "纪录"):
		return "documentary"
	case strings.Contains(category, "体育"), strings.Contains(category, "足球"), strings.Contains(category, "篮球"):
		return "sports"
	case strings.Contains(category, "电影"), strings.Contains(category, "伦理片"), strings.Contains(category, "剧情片"), strings.Contains(category, "动作片"), strings.Contains(category, "喜剧片"), strings.Contains(category, "爱情片"), strings.Contains(category, "恐怖片"), strings.Contains(category, "科幻片"), strings.Contains(category, "战争片"):
		return "movie"
	case strings.Contains(category, "剧"), strings.Contains(category, "泰剧"), strings.Contains(category, "韩剧"):
		return "tv"
	default:
		return "other"
	}
}

func workTopCategory(mediaType string) string {
	switch mediaType {
	case "movie":
		return "电影"
	case "tv":
		return "电视剧"
	case "anime":
		return "动漫"
	case "variety":
		return "综艺"
	case "documentary":
		return "纪录片"
	case "sports":
		return "体育"
	case "short-drama":
		return "短剧"
	default:
		return "其他"
	}
}

func workCategoryPath(drama Drama) []string {
	media := workMediaType(drama)
	path := []string{workTopCategory(media)}
	raw := strings.TrimSpace(firstNonEmpty(drama.CategoryName, drama.CategoryNameSnake, drama.TypeName, drama.TypeNameSnake, drama.Category, drama.ChannelName))
	if mapped := categoryMapping(dramaProvider(drama), raw); mapped != "" {
		parts := strings.FieldsFunc(mapped, func(r rune) bool { return r == '/' || r == '>' || r == '|' })
		if len(parts) > 0 {
			path = append([]string{strings.TrimSpace(parts[0])}, path[1:]...)
			for _, part := range parts[1:] {
				if strings.TrimSpace(part) != "" {
					path = append(path, strings.TrimSpace(part))
				}
			}
			return path
		}
	}
	if raw != "" && raw != path[0] {
		path = append(path, raw)
	}
	return path
}

func workID(normalizedTitle, mediaType, season, year string, ordinal int) string {
	key := strings.Join([]string{normalizedTitle, mediaType, season, year, strconv.Itoa(ordinal)}, "\x1f")
	sum := sha256.Sum256([]byte(key))
	return "work:" + hex.EncodeToString(sum[:12])
}

func workGroupID(normalizedTitle, mediaType, season, year string, group []Drama) string {
	// The conservative grouping key is stable when a new source binding is
	// added. Raw source IDs are kept in Work.Sources and are intentionally not
	// part of the public Work identity.
	return workID(normalizedTitle, mediaType, season, year, 0)
}

func workValueText(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func workSourceHasEpisode(source WorkSource, episode int) bool {
	if episode < 1 || source.EpisodeCount == nil {
		return true
	}
	count, err := strconv.Atoi(strings.TrimSpace(workValueText(source.EpisodeCount)))
	if err != nil || count <= 0 {
		return true
	}
	return episode <= count
}

func firstWorkValue(values ...any) any {
	for _, value := range values {
		if workValueText(value) != "" {
			return value
		}
	}
	return nil
}

func workCompatible(existing Drama, candidate Drama) bool {
	if workMediaType(existing) != workMediaType(candidate) {
		return false
	}
	if left, right := workYear(existing), workYear(candidate); left != "" && right != "" && left != right {
		return false
	}
	if left, right := workSeason(existing.Title), workSeason(candidate.Title); left != "" && right != "" && left != right {
		return false
	}
	return true
}

func workMetadataFromDrama(work *Work, drama Drama) {
	if work.Title == "" {
		work.Title = drama.DisplayTitle()
		work.Name = work.Title
	}
	if work.Desc == "" {
		work.Desc = firstNonEmpty(drama.Desc, drama.Intro)
	}
	if work.Intro == "" {
		work.Intro = firstNonEmpty(drama.Intro, drama.Desc)
	}
	if work.Cover == nil || workValueText(work.Cover) == "" {
		work.Cover, work.CoverURL = drama.Cover, drama.CoverURL
	}
	if workValueText(work.TotalEpisode) == "" {
		work.TotalEpisode = firstWorkValue(drama.TotalEpisode, drama.TotalEpisodeSnake, drama.EpisodeCount, drama.EpisodeCountSnake, drama.ChapterCount, drama.ChapterCountSnake, drama.Total)
	}
	if workValueText(work.Duration) == "" {
		work.Duration = drama.Duration
	}
	if workValueText(work.EpisodeDuration) == "" {
		work.EpisodeDuration = drama.EpisodeDuration
	}
	if work.ProviderCategory == "" {
		work.ProviderCategory = drama.ProviderCategory
	}
	if work.ProviderCategoryType == "" {
		work.ProviderCategoryType = drama.ProviderCategoryType
	}
	if work.ReleaseStatus == "" {
		work.ReleaseStatus = drama.ReleaseStatus
	}
	if work.OnlineDate == "" {
		work.OnlineDate = drama.OnlineDate
	}
	if work.Score == "" {
		work.Score = drama.Score
	}
	if work.Views == "" {
		work.Views = drama.Views
	}
	if work.Heat == "" {
		work.Heat = drama.Heat
	}
	seen := make(map[string]bool, len(work.Tags))
	for _, tag := range work.Tags {
		seen[tag] = true
	}
	for _, tag := range drama.Tags {
		if tag != "" && !seen[tag] {
			work.Tags = append(work.Tags, tag)
			seen[tag] = true
		}
	}
	for _, category := range workCategoryPath(drama) {
		found := false
		for _, value := range work.CategoryPath {
			if value == category {
				found = true
				break
			}
		}
		if !found {
			work.CategoryPath = append(work.CategoryPath, category)
		}
	}
	raw := firstNonEmpty(drama.CategoryName, drama.CategoryNameSnake, drama.TypeName, drama.TypeNameSnake, drama.Category, drama.ChannelName)
	if raw != "" {
		if work.RawCategory == "" {
			work.RawCategory = raw
		}
		for _, value := range work.RawCategories {
			if value == raw {
				return
			}
		}
		work.RawCategories = append(work.RawCategories, raw)
	}
}

func buildWorkCatalog(dramas []Drama) []Work {
	groups := make([][]Drama, 0)
	byBase := make(map[string][]int)
	for _, drama := range onlySupportedDramas(dramas) {
		title := normalizeWorkTitle(drama.DisplayTitle())
		if title == "" {
			continue
		}
		base := title + "\x1f" + workMediaType(drama)
		matched := -1
		for _, index := range byBase[base] {
			compatible := true
			for _, existing := range groups[index] {
				if !workCompatible(existing, drama) {
					compatible = false
					break
				}
			}
			if compatible {
				matched = index
				break
			}
		}
		if matched < 0 {
			matched = len(groups)
			groups = append(groups, nil)
			byBase[base] = append(byBase[base], matched)
		}
		groups[matched] = append(groups[matched], drama)
	}

	works := make([]Work, 0, len(groups))
	for _, group := range groups {
		if len(group) == 0 {
			continue
		}
		primary := group[0]
		sort.SliceStable(group, func(left, right int) bool {
			return sourcePriorityIndex(dramaProvider(group[left])) < sourcePriorityIndex(dramaProvider(group[right]))
		})
		primary = group[0]
		media := workMediaType(primary)
		season := workSeason(primary.Title)
		year := workYear(primary)
		work := Work{ID: workGroupID(normalizeWorkTitle(primary.DisplayTitle()), media, season, year, group), Title: primary.DisplayTitle(), Name: primary.DisplayTitle(), Source: dramaProvider(primary), SourceID: primary.SourceID, PrimaryDramaID: primary.ID, NormalizedTitle: normalizeWorkTitle(primary.DisplayTitle()), MediaType: media, PrimarySource: dramaProvider(primary), SourceCount: len(group)}
		for _, drama := range group {
			workMetadataFromDrama(&work, drama)
			source, sourceID, _ := splitProviderDramaID(drama.ID)
			rawCategory := firstNonEmpty(drama.CategoryName, drama.CategoryNameSnake, drama.TypeName, drama.TypeNameSnake, drama.Category, drama.ChannelName)
			work.Sources = append(work.Sources, WorkSource{Source: source, SourceID: sourceID, DramaID: drama.ID, Title: drama.DisplayTitle(), EpisodeCount: firstWorkValue(drama.TotalEpisode, drama.TotalEpisodeSnake, drama.EpisodeCount, drama.EpisodeCountSnake, drama.ChapterCount, drama.ChapterCountSnake, drama.Total), Priority: sourcePriorityIndex(source), Status: "ready", Available: true, RawCategory: rawCategory})
			work.RawIDs = append(work.RawIDs, drama.ID)
		}
		works = append(works, work)
	}
	sort.SliceStable(works, func(left, right int) bool {
		leftTitle, rightTitle := strings.ToLower(works[left].Title), strings.ToLower(works[right].Title)
		if leftTitle != rightTitle {
			return leftTitle < rightTitle
		}
		// A deterministic tie-breaker keeps equal-title groups fixed even when
		// provider records finish a background refresh in a different order.
		return works[left].ID < works[right].ID
	})
	return applyWorkOverrides(works)
}

// workCatalogLocked returns a snapshot of the derived index. Callers must hold
// app.mu. A copy is returned so request filters cannot mutate the cached slice.
func (app *UIApp) workCatalogLocked() []Work {
	if app.workCatalog != nil && app.workCatalogRevision == app.libraryRevision {
		return append([]Work(nil), app.workCatalog...)
	}
	app.workCatalog = buildWorkCatalog(app.dramas)
	app.workCatalogRevision = app.libraryRevision
	return append([]Work(nil), app.workCatalog...)
}

func (app *UIApp) workCatalogSnapshot() []Work {
	app.mu.Lock()
	defer app.mu.Unlock()
	return app.workCatalogLocked()
}

func workByID(works []Work, id string) (Work, bool) {
	for _, work := range works {
		if work.ID == id {
			return work, true
		}
	}
	return Work{}, false
}

func workPrimaryDrama(work Work, dramas []Drama) (Drama, bool) {
	for _, source := range work.Sources {
		if source.DramaID != "" {
			for _, drama := range dramas {
				if drama.ID == source.DramaID {
					return drama, true
				}
			}
		}
	}
	return Drama{}, false
}

func workForDramaID(works []Work, dramaID string) (Work, bool) {
	for _, work := range works {
		for _, source := range work.Sources {
			if source.DramaID == dramaID {
				return work, true
			}
		}
	}
	return Work{}, false
}

// workForLegacyID keeps bookmarks made before a media-type correction usable.
// Work IDs include the media type, so a reclassified Red Fruit title would
// otherwise make an old /play/work:... link look missing after the cache is
// rebuilt.
func workForLegacyID(works []Work, id string) (Work, bool) {
	for _, work := range works {
		season := workSeason(work.Title)
		year := ""
		if match := workYearPattern.FindString(work.OnlineDate); match != "" {
			year = match
		}
		for _, media := range []string{"tv", "other", "anime", "movie", "variety", "documentary", "sports", "short-drama"} {
			if workID(work.NormalizedTitle, media, season, year, 0) == id {
				return work, true
			}
		}
	}
	return Work{}, false
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func (app *UIApp) workIDForDramaID(dramaID string) string {
	if strings.TrimSpace(dramaID) == "" {
		return ""
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if work, ok := workForDramaID(app.workCatalogLocked(), dramaID); ok {
		return work.ID
	}
	return ""
}
