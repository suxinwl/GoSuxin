package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const fourKVMBaseURL = "https://www.4kvm.net"

var (
	re4KVMYear     = regexp.MustCompile(`\b(?:19|20)\d{2}\b`)
	re4KVMUserLink = regexp.MustCompile(`(?i)\buserlink\s*:\s*['"]([^'"]+)`)
	re4KVMFallback = regexp.MustCompile(`(?is)(?:window\._pdf|_pdf)\s*=\s*['"]([^'"]+)`)
)

func (d *Downloader) fourKVMBaseURL() string {
	if d != nil && d.cfg.SourceURLs != nil {
		if value := strings.TrimSpace(d.cfg.SourceURLs[source4KVM]); value != "" {
			return strings.TrimRight(value, "/")
		}
	}
	return fourKVMBaseURL
}

func fourKVMURL(base, path string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}

func fourKVMCategory(classify int) string {
	switch classify {
	case 1:
		return "\u7535\u5f71"
	case 2:
		return "\u7535\u89c6\u5267"
	case 3:
		return "\u52a8\u6f2b"
	case 4:
		return "\u7efc\u827a"
	default:
		return ""
	}
}

func parse4KVMCards(body string, classify int) []Drama {
	return parse4KVMCardsDOM(body, classify)
}

func (d *Downloader) fetch4KVMDramas(ctx context.Context) ([]Drama, error) {
	maxPages := d.cfg.MaxPagesPerSort
	if maxPages <= 0 {
		maxPages = 5
	}
	if maxPages > 20 {
		maxPages = 20
	}
	base := d.fourKVMBaseURL()
	var all []Drama
	seen := map[string]bool{}
	for _, classify := range []int{1, 2, 3, 4} {
		for page := 1; page <= maxPages; page++ {
			if err := ctx.Err(); err != nil {
				return all, err
			}
			reqURL := fourKVMURL(base, "/filter") + "?classify=" + strconv.Itoa(classify) + "&sort_by=update_time&order=desc&page=" + strconv.Itoa(page)
			body, err := d.fetchProviderText(ctx, reqURL, base+"/")
			if err != nil {
				return all, err
			}
			pageItems := parse4KVMCards(body, classify)
			if len(pageItems) == 0 {
				break
			}
			for _, drama := range pageItems {
				if !seen[drama.ID] {
					seen[drama.ID] = true
					all = append(all, drama)
				}
			}
			reportLibraryProgress(ctx, source4KVM, pageItems, nil, false)
		}
	}
	return all, nil
}

type fourKVMChapterRef struct {
	Slug        string
	DataID      string
	Line        string
	Episode     string
	Title       string
	DefaultLine bool
}

func parse4KVMChapterRefs(body string) []fourKVMChapterRef {
	return parse4KVMChapterRefsDOM(fourKVMParseHTML(body))
}

func fourKVMDetailTitle(body string) string {
	return parse4KVMDetailDOM(fourKVMParseHTML(body), "").Title
}

func (d *Downloader) fetch4KVMDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	base := d.fourKVMBaseURL()
	pageURL := fourKVMURL(base, "/play/"+url.PathEscape(sourceID))
	body, err := d.fetchProviderText(ctx, pageURL, base+"/")
	if err != nil {
		return Drama{}, nil, err
	}
	doc := fourKVMParseHTML(body)
	drama := parse4KVMDetailDOM(doc, sourceID)
	refs := parse4KVMChapterRefsDOM(doc)
	if len(refs) == 0 {
		return drama, nil, errors.New("4KVM has no available episodes")
	}
	chapters := make([]Chapter, 0, len(refs))
	maxEpisode := 0
	for _, ref := range refs {
		// A /play/ slug identifies one episode page. Keep the catalog entry
		// bound to the film while storing each real chapter page separately.
		query := url.Values{"dataid": {ref.DataID}, "quality": {"1080"}, "chapter": {ref.Slug}}
		marker := "4kvm://" + url.QueryEscape(sourceID) + "?" + query.Encode()
		chapters = append(chapters, Chapter{
			ID: providerChapterID(source4KVM, sourceID, ref.Line+"-"+ref.Episode+"-"+ref.DataID), Source: source4KVM,
			Title: ref.Title, VideoURL: marker, CurrentEpisode: rawEpisodeFromString(ref.Episode), PageURL: fourKVMURL(base, "/play/"+url.PathEscape(ref.Slug)), Referer: base + "/",
			SourceLine: ref.Line, DefaultLine: ref.DefaultLine,
		})
		if episode, parseErr := strconv.Atoi(ref.Episode); parseErr == nil && episode > maxEpisode {
			maxEpisode = episode
		}
	}
	drama.TotalEpisode = maxEpisode
	return drama, chapters, nil
}

func (d *Downloader) fetch4KVMChapters(ctx context.Context, sourceID string) (string, []Chapter, error) {
	drama, chapters, err := d.fetch4KVMDetail(ctx, sourceID)
	return drama.Title, chapters, err
}

func rawEpisodeFromString(value string) json.RawMessage {
	if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && n > 0 {
		return rawEpisode(n)
	}
	return json.RawMessage(strconv.Quote(strings.TrimSpace(value)))
}

func (d *Downloader) search4KVMDramas(ctx context.Context, keyword string) ([]Drama, error) {
	base := d.fourKVMBaseURL()
	reqURL := fourKVMURL(base, "/search") + "?q=" + url.QueryEscape(keyword)
	body, err := d.fetchProviderText(ctx, reqURL, base+"/")
	if err != nil {
		return nil, err
	}
	return parse4KVMCards(body, 0), nil
}

type fourKVMQualityURL struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Bitrate     int    `json:"bitrate"`
	Locked      bool   `json:"locked"`
	IsVIP       bool   `json:"isvip"`
}

type fourKVMPlayResponse struct {
	Code int `json:"code"`
	Data struct {
		CurrentQuality int                 `json:"current_quality"`
		QualityURLs    []fourKVMQualityURL `json:"quality_urls"`
	} `json:"data"`
}

func parse4KVMPlayResponse(body string) (fourKVMPlayResponse, error) {
	var response fourKVMPlayResponse
	if err := json.Unmarshal([]byte(body), &response); err != nil {
		return response, err
	}
	if response.Code != 0 && response.Code != 200 {
		return response, fmt.Errorf("4KVM 播放接口返回错误码 %d", response.Code)
	}
	return response, nil
}

func fourKVMFallbackHosts(body string) []string {
	m := re4KVMFallback.FindStringSubmatch(body)
	if len(m) != 2 {
		return nil
	}
	decoded, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil {
		return nil
	}
	var hosts []string
	if json.Unmarshal(decoded, &hosts) != nil {
		return nil
	}
	return hosts
}

func fourKVMReplaceHost(raw, host string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || host == "" {
		return ""
	}
	u.Host = host
	return u.String()
}

func fourKVMPickMedia(response fourKVMPlayResponse, quality string) string {
	quality = strings.ToLower(strings.TrimSpace(quality))
	best := ""
	for _, item := range response.Data.QualityURLs {
		if item.Locked || item.IsVIP || !isProviderHTTPMediaURL(item.URL) {
			continue
		}
		if best == "" {
			best = item.URL
		}
		name := strings.ToLower(item.Title + " " + item.Description + " " + strconv.Itoa(item.Bitrate))
		if quality != "" && strings.Contains(name, quality) {
			return item.URL
		}
	}
	return best
}

func (d *Downloader) resolve4KVMChapter(ctx context.Context, task Task, chapter Chapter) (providerMedia, error) {
	ref, ok := parse4KVMPlaybackRef(chapter.VideoURL)
	if !ok {
		return providerMedia{}, errors.New("4KVM 分集播放参数无效")
	}
	slug, dataID, quality := ref.ChapterSlug, ref.DataID, ref.Quality
	if task.DownloadQuality > 0 {
		quality = strconv.Itoa(task.DownloadQuality)
	}
	base := d.fourKVMBaseURL()
	// Legacy markers use their own main slug as the chapter page. Never use
	// a stale parent PageURL for another episode's anonymous playback token.
	pageURL := fourKVMURL(base, "/play/"+url.PathEscape(slug))
	body, err := d.fetchProviderText(ctx, pageURL, base+"/")
	if err != nil {
		return providerMedia{}, fmt.Errorf("4KVM 读取播放令牌失败: %w", err)
	}
	userLinkMatch := re4KVMUserLink.FindStringSubmatch(body)
	if len(userLinkMatch) != 2 || strings.TrimSpace(userLinkMatch[1]) == "" {
		return providerMedia{}, errors.New("4KVM 页面未提供播放令牌")
	}
	playURL, err := build4KVMPlayURL(ctx, dataID, slug, quality, strings.TrimSpace(userLinkMatch[1]))
	if err != nil {
		return providerMedia{}, fmt.Errorf("4KVM 生成播放地址失败: %w", err)
	}
	apiURL := fourKVMURL(base, playURL)
	playBody, err := d.fetchProviderText(ctx, apiURL, pageURL)
	if err != nil {
		return providerMedia{}, fmt.Errorf("4KVM 请求播放地址失败: %w", err)
	}
	response, err := parse4KVMPlayResponse(playBody)
	if err != nil {
		return providerMedia{}, err
	}
	mediaURL := fourKVMPickMedia(response, quality)
	if mediaURL == "" {
		return providerMedia{}, errors.New("4KVM 没有可用的非会员播放地址")
	}
	selectedURL := mediaURL
	hosts := fourKVMFallbackHosts(body)
	selected, err := d.fourKVMSelectMedia(ctx, mediaURL, hosts, base+"/")
	if err != nil {
		return providerMedia{}, err
	}
	selectedParsed, _ := url.Parse(selected.URL)
	originalParsed, _ := url.Parse(selectedURL)
	seen := make(map[string]bool)
	for _, item := range response.Data.QualityURLs {
		if item.Locked || item.IsVIP || item.URL == "" || item.URL == "1" {
			continue
		}
		address := item.URL
		// Reuse the validated fallback only for variants on the same CDN.
		// Each distinct variant remains tied to its own real provider address.
		if parsed, parseErr := url.Parse(address); parseErr != nil || !isProviderHTTPMediaURL(address) {
			continue
		} else if selectedParsed != nil && originalParsed != nil && parsed.Host == originalParsed.Host && selectedParsed.Host != originalParsed.Host {
			address = fourKVMReplaceHost(address, selectedParsed.Host)
		}
		if seen[address] {
			continue
		}
		seen[address] = true
		label := strings.TrimSpace(item.Title)
		if label == "" {
			label = strings.TrimSpace(item.Description)
		}
		if len([]rune(label)) > 24 {
			label = string([]rune(label)[:24])
		}
		height := 0
		for _, candidate := range []int{2160, 1440, 1080, 720, 576, 540, 480, 360, 240} {
			if strings.Contains(item.Title+" "+item.Description, strconv.Itoa(candidate)) {
				height = candidate
				break
			}
		}
		selected.Variants = append(selected.Variants, providerMedia{URL: address, Referer: base + "/", Quality: height, Label: label})
		if item.URL == selectedURL {
			selected.Quality, selected.Label = height, label
		}
	}
	return selected, nil
}

type fourKVMPlaybackRef struct {
	FilmSlug    string
	ChapterSlug string
	DataID      string
	Quality     string
}

func parse4KVMPlaybackRef(raw string) (fourKVMPlaybackRef, bool) {
	invalid := fourKVMPlaybackRef{}
	if !strings.HasPrefix(raw, "4kvm://") {
		return invalid, false
	}
	value := strings.TrimPrefix(raw, "4kvm://")
	parts := strings.SplitN(value, "?", 2)
	var err error
	slug, err := url.QueryUnescape(parts[0])
	if err != nil || !fourKVMSlugPattern.MatchString(slug) || len(slug) > 64 || len(parts) != 2 {
		return invalid, false
	}
	query, err := url.ParseQuery(parts[1])
	if err != nil {
		return invalid, false
	}
	for key, values := range query {
		if (key != "dataid" && key != "quality" && key != "chapter") || len(values) != 1 {
			return invalid, false
		}
	}
	chapterSlug := slug
	if values, provided := query["chapter"]; provided {
		chapterSlug = values[0]
		if !fourKVMSlugPattern.MatchString(chapterSlug) || len(chapterSlug) > 64 {
			return invalid, false
		}
	}
	dataID := query.Get("dataid")
	quality := firstNonEmpty(query.Get("quality"), "1080")
	switch quality {
	case "2160", "1440", "1080", "720", "576", "540", "480", "360", "240":
	default:
		return invalid, false
	}
	return fourKVMPlaybackRef{FilmSlug: slug, ChapterSlug: chapterSlug, DataID: dataID, Quality: quality}, fourKVMPositiveID.MatchString(dataID)
}

func parse4KVMMarker(raw string) (slug, dataID, quality string, ok bool) {
	ref, ok := parse4KVMPlaybackRef(raw)
	return ref.FilmSlug, ref.DataID, ref.Quality, ok
}
