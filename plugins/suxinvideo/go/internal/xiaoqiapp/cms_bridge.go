package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CMSBridge exposes only the Hongguo and 4KVM providers to SuxinVideo.
// Other providers bundled with the source library are never registered here.
type CMSBridge struct {
	downloader *Downloader
	mu         sync.Mutex
	cursors    map[int]map[int]cmsCursor
}

type cmsCursor struct {
	offset    int
	sessionID string
}

type CMSPage struct {
	Dramas  []Drama
	HasMore bool
}

type CMSMedia struct {
	URL      string
	Referer  string
	Key      []byte
	Quality  int
	Duration time.Duration
	Variants []CMSMediaVariant
}

type CMSMediaVariant struct {
	URL      string
	Referer  string
	Key      []byte
	Quality  int
	Label    string
	Duration time.Duration
}

func NewCMSBridge() *CMSBridge {
	cfg := defaultConfig()
	cfg.dataDir = "data/suxinvideo-provider"
	cfg.settingsLoaded = true
	cfg.OutputDir = "data/suxinvideo-provider/downloads"
	cfg.RequestConcurrency = 2
	cfg.RequestIntervalMS = 350
	cfg.Retries = 1
	return &CMSBridge{downloader: NewDownloader(cfg), cursors: make(map[int]map[int]cmsCursor)}
}

func CMSCategories(provider string) []string {
	switch provider {
	case sourceHongguo:
		return []string{"真人剧", "漫剧", "AI剧"}
	case source4KVM:
		return []string{"电影", "电视剧", "动漫", "综艺"}
	}
	return nil
}

func (bridge *CMSBridge) List(ctx context.Context, provider string, category, page int) (CMSPage, error) {
	if page < 1 || page > 20 {
		return CMSPage{}, errors.New("页码无效")
	}
	categories := CMSCategories(provider)
	if categories == nil || category < 0 || category > len(categories) {
		return CMSPage{}, errors.New("片源分类无效")
	}
	if category > 0 {
		return bridge.listCategory(ctx, provider, category, page)
	}
	result := CMSPage{}
	seen := make(map[string]bool)
	var failures []error
	for index := range categories {
		part, err := bridge.listCategory(ctx, provider, index+1, page)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", categories[index], err))
			continue
		}
		result.HasMore = result.HasMore || part.HasMore
		for _, drama := range part.Dramas {
			if !seen[drama.ID] {
				seen[drama.ID] = true
				result.Dramas = append(result.Dramas, drama)
			}
		}
	}
	if len(result.Dramas) == 0 && len(failures) > 0 {
		return CMSPage{}, errors.Join(failures...)
	}
	return result, nil
}

func (bridge *CMSBridge) listCategory(ctx context.Context, provider string, category, page int) (CMSPage, error) {
	if provider == source4KVM {
		base := bridge.downloader.fourKVMBaseURL()
		address := fourKVMURL(base, "/filter") + "?classify=" + strconv.Itoa(category) + "&sort_by=update_time&order=desc&page=" + strconv.Itoa(page)
		body, err := bridge.downloader.fetchProviderText(ctx, address, base+"/")
		if err != nil {
			return CMSPage{}, err
		}
		items := parse4KVMCards(body, category)
		return CMSPage{Dramas: items, HasMore: len(items) > 0 && page < 20}, nil
	}
	if provider != sourceHongguo {
		return CMSPage{}, errors.New("不支持的片源")
	}
	genre := hongguoAppGenres[category-1]
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	if bridge.cursors[category] == nil {
		bridge.cursors[category] = map[int]cmsCursor{1: {}}
	}
	_, cached := bridge.cursors[category][page]
	var appErr error
	if !cached {
		// A direct jump must reproduce the source's session cursor. Bound the
		// catch-up so a forged page number cannot create thousands of requests.
		if page > 20 {
			return CMSPage{}, errors.New("请从第一页连续翻页，最多查看前 20 页")
		}
		for previous := 1; previous < page; previous++ {
			if _, found := bridge.cursors[category][previous+1]; found {
				continue
			}
			if _, err := bridge.fetchHongguoPage(ctx, category, previous, genre.scene, genre.key, genre.name); err != nil {
				appErr = err
				break
			}
		}
	}
	var result CMSPage
	if appErr == nil {
		result, appErr = bridge.fetchHongguoPage(ctx, category, page, genre.scene, genre.key, genre.name)
	}
	if appErr == nil {
		return result, nil
	}
	webRoutes := []string{"real-drama", "comic-drama", "ai-drama"}
	items, totalPages, webErr := bridge.downloader.fetchHongguoCategoryPage(ctx, webRoutes[category-1]+"?page="+strconv.Itoa(page), genre.name)
	if webErr == nil {
		return CMSPage{Dramas: items, HasMore: len(items) > 0 && page < totalPages && page < 20}, nil
	}
	return CMSPage{}, fmt.Errorf("红果 App 分类失败: %v；网页备用入口失败: %w", appErr, webErr)
}

func (bridge *CMSBridge) fetchHongguoPage(ctx context.Context, category, page int, scene, key, name string) (CMSPage, error) {
	cursor := bridge.cursors[category][page]
	payload := map[string]any{
		"req_scene": scene, "offset": cursor.offset, "limit": 18,
		"req_type": "only_content", "need_selector_panel": false, "client_req_type": 3,
		"session_id": cursor.sessionID, "filter_ids": "",
		"select_items": map[string]any{
			"genre": []string{key}, "sort": []string{"online_time"}, "gender": []string{},
			"category_dim_theme": []string{}, "category_dim_role": []string{}, "category_dim_epoch": []string{},
			"online_time": []string{}, "creation_status": []string{},
		},
	}
	if page > 1 {
		payload["client_req_type"] = 2
	}
	response, err := bridge.downloader.hongguoAppRequest(ctx, http.MethodPost, "/reading/distribution/category/landpage/v/", nil, payload)
	if err != nil {
		return CMSPage{}, err
	}
	data := nestedMap(response, "data")
	rows, ok := data["video_data"].([]any)
	if !ok {
		return CMSPage{}, errors.New("红果分类响应格式异常")
	}
	result := CMSPage{HasMore: data["has_more"] == true}
	for _, raw := range rows {
		if drama := hongguoDramaFromAny(raw, name); drama.ID != "" {
			result.Dramas = append(result.Dramas, drama)
		}
	}
	if result.HasMore && page < 20 {
		next, err := strconv.Atoi(mapString(data, "next_offset"))
		if err != nil || next <= cursor.offset || next > 1_000_000 {
			return CMSPage{}, errors.New("红果分页未前进")
		}
		bridge.cursors[category][page+1] = cmsCursor{offset: next, sessionID: mapString(data, "session_id")}
	}
	if page >= 20 {
		result.HasMore = false
	}
	return result, nil
}

// Search uses the provider's real search endpoint. Providers without a
// supported keyword API must not silently return an unrelated catalog page.
func (bridge *CMSBridge) Search(ctx context.Context, provider, keyword string) ([]Drama, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" || len([]rune(keyword)) > 100 {
		return nil, errors.New("search keyword is invalid")
	}
	switch provider {
	case sourceHongguo:
		result, err := bridge.downloader.searchHongguoDramas(ctx, keyword)
		return result.Dramas, err
	case source4KVM:
		return bridge.downloader.search4KVMDramas(ctx, keyword)
	default:
		return nil, errors.New("provider does not support keyword search")
	}
}

func (bridge *CMSBridge) Detail(ctx context.Context, provider, sourceID string) (Drama, []Chapter, error) {
	if provider == sourceHongguo {
		if !hongguoNumericID.MatchString(sourceID) {
			return Drama{}, nil, errors.New("红果剧集 ID 无效")
		}
		entry, err := bridge.downloader.hongguoAppDetail(ctx, sourceID)
		if err == nil {
			return entry.Drama, entry.Chapters, nil
		}
		title, chapters, webErr := bridge.downloader.fetchHongguoWebChapters(ctx, sourceID)
		if webErr != nil {
			return Drama{}, nil, fmt.Errorf("红果详情失败：%v；网页备用入口：%w", err, webErr)
		}
		return Drama{ID: providerDramaID(sourceHongguo, sourceID), Source: sourceHongguo, SourceID: sourceID, Title: title, Name: title, Category: "短剧"}, chapters, nil
	}
	if provider == source4KVM {
		if !fourKVMSlugPattern.MatchString(sourceID) {
			return Drama{}, nil, errors.New("4KVM 影片 ID 无效")
		}
		return bridge.downloader.fetch4KVMDetail(ctx, sourceID)
	}
	return Drama{}, nil, errors.New("不支持的片源")
}

func (bridge *CMSBridge) Resolve(ctx context.Context, provider, sourceID, marker string) (CMSMedia, error) {
	var media providerMedia
	var err error
	switch provider {
	case sourceHongguo:
		if !hongguoNumericID.MatchString(sourceID) || !strings.HasPrefix(marker, "hongguo-cenc://") {
			return CMSMedia{}, errors.New("红果播放参数无效")
		}
		media, err = bridge.downloader.resolveHongguoMedia(ctx, Task{DramaID: providerDramaID(sourceHongguo, sourceID), Chapter: Chapter{Source: sourceHongguo, VideoURL: marker}})
	case source4KVM:
		ref, valid := parse4KVMPlaybackRef(marker)
		if !valid || sourceID != ref.FilmSlug {
			return CMSMedia{}, errors.New("4KVM 播放参数无效")
		}
		media, err = bridge.downloader.resolve4KVMChapter(ctx, Task{DramaID: providerDramaID(source4KVM, sourceID)}, Chapter{Source: source4KVM, VideoURL: marker})
	default:
		return CMSMedia{}, errors.New("不支持的片源")
	}
	if err != nil {
		return CMSMedia{}, err
	}
	u, parseErr := url.Parse(media.URL)
	if parseErr != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return CMSMedia{}, errors.New("片源未返回有效播放地址")
	}
	result := CMSMedia{URL: media.URL, Referer: media.Referer, Key: append([]byte(nil), media.CENCKey...), Quality: media.Quality, Duration: media.Duration}
	for _, variant := range media.Variants {
		if variant.URL == "" {
			continue
		}
		result.Variants = append(result.Variants, CMSMediaVariant{
			URL: variant.URL, Referer: variant.Referer, Key: append([]byte(nil), variant.CENCKey...),
			Quality: variant.Quality, Label: variant.Label, Duration: variant.Duration,
		})
	}
	return result, nil
}
