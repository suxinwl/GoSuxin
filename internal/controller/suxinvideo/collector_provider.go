package suxinvideo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	xq "github.com/suxinwl/GoSuxin/internal/xiaoqiapp"
)

const hongguoSourceURL = "hongguo://app"
const fourKVMSourceURL = "4kvm://site"

var cmsProviders = xq.NewCMSBridge()

type cmsCollectProvider interface {
	List(context.Context, string, int, int) (xq.CMSPage, error)
	Detail(context.Context, string, string) (xq.Drama, []xq.Chapter, error)
	Search(context.Context, string, string) ([]xq.Drama, error)
}

func nativeProvider(raw string) string {
	switch strings.TrimSpace(raw) {
	case erciyuanSourceURL:
		return "erciyuan"
	case hongguoSourceURL:
		return "hongguo"
	case fourKVMSourceURL:
		return "4kvm"
	default:
		return ""
	}
}

func fetchCollectSource(ctx context.Context, raw string, params url.Values) (macPayload, error) {
	if isErciyuanSource(raw) {
		return fetchErciyuanCollectSource(ctx, params)
	}
	if isYQKSource(raw) {
		return fetchYQK(ctx, params)
	}
	provider := nativeProvider(raw)
	if provider == "" {
		return fetchMac(ctx, raw, params)
	}
	return fetchNativeCollectSourceWith(ctx, provider, params, cmsProviders)
}

func fetchNativeCollectSourceWith(ctx context.Context, provider string, params url.Values, bridge cmsCollectProvider) (macPayload, error) {
	if provider != "4kvm" && provider != "hongguo" {
		return macPayload{}, errors.New("片源操作不支持")
	}
	action := params.Get("ac")
	switch action {
	case "list":
		categories := xq.CMSCategories(provider)
		classes := make([]map[string]any, 0, len(categories))
		for index, name := range categories {
			classes = append(classes, map[string]any{"type_id": index + 1, "type_name": name})
		}
		return macPayload{Code: 1, Class: classes}, nil
	case "videolist", "detail":
	default:
		return macPayload{}, errors.New("片源操作不支持")
	}
	if action == "detail" {
		id := strings.TrimSpace(params.Get("ids"))
		if id == "" || len(id) > 128 {
			return macPayload{}, errors.New("影片编号无效")
		}
		drama, chapters, err := bridge.Detail(ctx, provider, id)
		if err != nil {
			return macPayload{}, err
		}
		item := cmsProviderVodItem(provider, drama)
		item["vod_play_from"] = provider
		var episodes []string
		if provider == "4kvm" {
			for _, ep := range fourKVMCollectEpisodes(chapters) {
				episodes = append(episodes, ep.Name+"$"+ep.URL)
			}
		} else {
			for index, chapter := range chapters {
				videoID := strings.TrimPrefix(chapter.VideoURL, "hongguo-cenc://")
				if !onlyDigits(videoID) {
					continue
				}
				marker := "hongguo://" + id + "/" + videoID
				name := strings.ReplaceAll(strings.ReplaceAll(chapter.Title, "$", ""), "#", "")
				if name == "" {
					name = fmt.Sprintf("第%d集", index+1)
				}
				episodes = append(episodes, name+"$"+marker)
			}
		}
		if len(episodes) == 0 {
			return macPayload{}, errors.New("片源未返回可播放分集")
		}
		item["vod_play_url"] = strings.Join(episodes, "#")
		return macPayload{Code: 1, List: []map[string]any{item}, Total: 1, Page: 1, PageCount: 1}, nil
	}
	page, err := strconv.Atoi(params.Get("pg"))
	if err != nil || page < 1 || page > 10000 {
		return macPayload{}, errors.New("采集页码无效")
	}
	if keyword := strings.TrimSpace(params.Get("wd")); keyword != "" {
		limit := 100
		if provider == "hongguo" {
			limit = 80
		}
		if page != 1 || len([]rune(keyword)) > limit {
			return macPayload{}, errors.New("片源搜索参数无效，仅支持第一页及有限长度关键词")
		}
		dramas, err := bridge.Search(ctx, provider, keyword)
		if err != nil {
			return macPayload{}, err
		}
		items := make([]map[string]any, 0, min(40, len(dramas)))
		for _, drama := range dramas {
			item := cmsProviderVodItem(provider, drama)
			items = append(items, item)
			if len(items) == 40 {
				break
			}
		}
		return macPayload{Code: 1, List: items, Total: len(items), Page: 1, PageCount: 1}, nil
	}
	category := 0
	if params.Get("t") != "" {
		category, err = strconv.Atoi(params.Get("t"))
		if err != nil {
			return macPayload{}, errors.New("片源分类无效")
		}
	}
	result, err := bridge.List(ctx, provider, category, page)
	if err != nil {
		return macPayload{}, err
	}
	items := make([]map[string]any, 0, len(result.Dramas))
	for _, drama := range result.Dramas {
		items = append(items, cmsProviderVodItem(provider, drama))
	}
	pageCount := page
	if result.HasMore {
		pageCount++
	}
	return macPayload{Code: 1, List: items, Total: (page-1)*18 + len(items), Page: page, PageCount: pageCount}, nil
}

func cmsProviderVodItem(provider string, drama xq.Drama) map[string]any {
	item := cmsVodItem(drama)
	if provider == "hongguo" {
		// Every catalog entry is a short drama. Theme tags remain in vod_class;
		// they must not create top-level movie/series categories on list import.
		item["__hongguo"], item["type_name"] = true, "短剧"
	}
	return item
}

func cmsVodItem(drama xq.Drama) map[string]any {
	return map[string]any{
		"vod_id": drama.SourceID, "vod_name": drama.DisplayTitle(),
		"type_name": drama.Category, "vod_class": drama.Category,
		"vod_pic": drama.Cover, "vod_remarks": drama.Remark,
		"vod_content": drama.Desc, "vod_year": drama.OnlineDate, "vod_area": drama.Area,
		"vod_lang": drama.Language, "vod_director": drama.Director, "vod_actor": drama.Actor,
		"vod_douban_score": drama.Score, "vod_hits": drama.Views,
	}
}

// A CMS film has one 4KVM line. Prefer the line explicitly marked as the
// source default, then fill missing episode labels from the remaining lines.
// Numeric labels are equivalent; previews, features and specials stay distinct.
func fourKVMCollectEpisodes(chapters []xq.Chapter) []episode {
	defaultLine := ""
	for _, chapter := range chapters {
		if chapter.DefaultLine && chapter.SourceLine != "" {
			defaultLine = chapter.SourceLine
			break
		}
	}
	ordered := make([]xq.Chapter, 0, len(chapters))
	if defaultLine != "" {
		for _, chapter := range chapters {
			if chapter.SourceLine == defaultLine {
				ordered = append(ordered, chapter)
			}
		}
	}
	for _, chapter := range chapters {
		if defaultLine == "" || chapter.SourceLine != defaultLine {
			ordered = append(ordered, chapter)
		}
	}
	selected := make([]episode, 0, len(ordered))
	seen := map[string]bool{}
	for _, chapter := range ordered {
		if _, ok := fourKVMDiscoveryMarker(chapter.VideoURL); !ok {
			continue
		}
		name := strings.TrimSpace(strings.NewReplacer("$", "", "#", "").Replace(chapter.Title))
		if name == "" {
			var number int
			if err := json.Unmarshal(chapter.CurrentEpisode, &number); err != nil {
				var actual string
				if json.Unmarshal(chapter.CurrentEpisode, &actual) == nil {
					number, _ = strconv.Atoi(strings.TrimSpace(actual))
				}
			}
			if number > 0 && number < 10_000_000 {
				name = fmt.Sprintf("第%d集", number)
			} else {
				continue
			}
		}
		key, _ := playbackEpisodeIdentity(name)
		if !seen[key] {
			seen[key] = true
			selected = append(selected, episode{Name: name, URL: chapter.VideoURL})
		}
	}
	return discoveryNormalizeSource(source{Code: "4kvm", Episodes: selected}).Episodes
}

func onlyDigits(value string) bool {
	if value == "" || len(value) > 32 {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}
