package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

type macCMSSourceDef struct {
	ID      string
	Name    string
	BaseURL string
}

var defaultMacCMSSources = map[string]macCMSSourceDef{
	sourceLZ:  {ID: sourceLZ, Name: "量子", BaseURL: "http://cj.lziapi.com/api.php/provide/vod/"},
	sourceFF:  {ID: sourceFF, Name: "非凡", BaseURL: "http://cj.ffzyapi.com/api.php/provide/vod/"},
	sourceWJ:  {ID: sourceWJ, Name: "无极/无尽", BaseURL: "https://api.wujinapi.me/api.php/provide/vod/"},
	sourceBF:  {ID: sourceBF, Name: "暴风", BaseURL: "https://bfzyapi.com/api.php/provide/vod/"},
	sourceHN:  {ID: sourceHN, Name: "红牛", BaseURL: "https://www.hongniuzy2.com/api.php/provide/vod/"},
	sourceSD:  {ID: sourceSD, Name: "闪电", BaseURL: "https://sdzyapi.com/api.php/provide/vod/"},
	sourceBD:  {ID: sourceBD, Name: "百度", BaseURL: "https://api.apibdzy.com/api.php/provide/vod/"},
	sourceXL:  {ID: sourceXL, Name: "迅雷/新浪", BaseURL: "https://api.xinlangapi.com/xinlangapi.php/provide//vod/"},
	sourceYQK: {ID: sourceYQK, Name: "一起看", BaseURL: "https://api.yqk88.com/api.php/provide/vod/"},
}

func isMacCMSSource(source string) bool {
	canon := canonicalProviderSource(source)
	_, ok := defaultMacCMSSources[canon]
	return ok
}

func (d *Downloader) macCMSBaseURL(source string) string {
	canon := canonicalProviderSource(source)
	if d.cfg.SourceURLs != nil {
		if custom, ok := d.cfg.SourceURLs[canon]; ok && strings.TrimSpace(custom) != "" {
			return strings.TrimRight(strings.TrimSpace(custom), "/") + "/"
		}
	}
	if def, ok := defaultMacCMSSources[canon]; ok {
		return def.BaseURL
	}
	return ""
}

type macCMSResponse struct {
	Code      int           `json:"code"`
	Msg       string        `json:"msg"`
	Page      any           `json:"page"`
	PageCount any           `json:"pagecount"`
	Limit     any           `json:"limit"`
	Total     any           `json:"total"`
	Class     []macCMSClass `json:"class"`
	List      []macCMSItem  `json:"list"`
}

type macCMSClass struct {
	TypeID   any    `json:"type_id"`
	TypeName string `json:"type_name"`
}

type macCMSItem struct {
	VodID       any    `json:"vod_id"`
	VodName     string `json:"vod_name"`
	TypeName    string `json:"type_name"`
	VodPic      string `json:"vod_pic"`
	VodRemarks  string `json:"vod_remarks"`
	VodContent  string `json:"vod_content"`
	VodYear     string `json:"vod_year"`
	VodTime     string `json:"vod_time"`
	VodPlayFrom string `json:"vod_play_from"`
	VodPlayURL  string `json:"vod_play_url"`
}

func parseVodID(val any) string {
	switch v := val.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case int:
		return strconv.Itoa(v)
	case json.Number:
		return v.String()
	default:
		return fmt.Sprintf("%v", val)
	}
}

func (item macCMSItem) toDrama(source string) Drama {
	vodID := parseVodID(item.VodID)
	fullID := providerDramaID(source, vodID)
	releaseStatus := "ongoing"
	if strings.Contains(item.VodRemarks, "完") {
		releaseStatus = "finished"
	}
	date := strings.TrimSpace(item.VodYear)
	if date == "" && len(item.VodTime) >= 10 {
		date = item.VodTime[:10]
	}
	category := strings.TrimSpace(item.TypeName)
	if category == "" {
		category = "短剧"
	}
	title := strings.TrimSpace(item.VodName)
	return Drama{
		ID:            fullID,
		Source:        source,
		SourceID:      vodID,
		Title:         title,
		Name:          title,
		Desc:          strings.TrimSpace(item.VodContent),
		Intro:         strings.TrimSpace(item.VodContent),
		Cover:         strings.TrimSpace(item.VodPic),
		CoverURL:      strings.TrimSpace(item.VodPic),
		Category:      category,
		CategoryName:  category,
		ChannelName:   category,
		Remark:        strings.TrimSpace(item.VodRemarks),
		OnlineDate:    date,
		ReleaseStatus: releaseStatus,
	}
}

func (d *Downloader) fetchMacCMSCategories(ctx context.Context, source string) ([]string, error) {
	baseURL := d.macCMSBaseURL(source)
	if baseURL == "" {
		return nil, fmt.Errorf("未配置站源 %s 的地址", source)
	}
	reqURL := baseURL + "?ac=list"
	body, err := d.fetchProviderText(ctx, reqURL, baseURL)
	if err != nil {
		return nil, err
	}
	var resp macCMSResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("解析 %s 分类失败: %w", source, err)
	}
	var typeIDs []string
	for _, cls := range resp.Class {
		name := strings.TrimSpace(cls.TypeName)
		typeID := parseVodID(cls.TypeID)
		if typeID == "" || typeID == "0" {
			continue
		}
		if d.cfg.FullCategories {
			typeIDs = append(typeIDs, typeID)
			continue
		}
		if strings.Contains(name, "短剧") || strings.Contains(name, "微短剧") || strings.Contains(name, "爽文") || strings.Contains(name, "微剧") {
			typeIDs = append(typeIDs, typeID)
		}
	}
	return typeIDs, nil
}

func (d *Downloader) fetchMacCMSDramas(ctx context.Context, source string) ([]Drama, error) {
	canon := canonicalProviderSource(source)
	baseURL := d.macCMSBaseURL(canon)
	if baseURL == "" {
		return nil, fmt.Errorf("未配置站源 %s 的地址", source)
	}

	typeIDs, err := d.fetchMacCMSCategories(ctx, canon)
	if err != nil {
		typeIDs = nil
	}

	maxPages := d.cfg.MaxPagesPerSort
	if maxPages <= 0 {
		maxPages = 5
	}
	if maxPages > 20 {
		maxPages = 20
	}

	seen := make(map[string]bool)
	var dramas []Drama

	fetchPage := func(page int, typeID string) ([]Drama, int, error) {
		reqURL := fmt.Sprintf("%s?ac=detail&pg=%d", baseURL, page)
		if typeID != "" {
			reqURL += "&t=" + typeID
		}
		body, err := d.fetchProviderText(ctx, reqURL, baseURL)
		if err != nil {
			return nil, 0, err
		}
		var resp macCMSResponse
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			return nil, 0, err
		}
		pageCount := 1
		switch pc := resp.PageCount.(type) {
		case float64:
			pageCount = int(pc)
		case int:
			pageCount = pc
		case string:
			if n, err := strconv.Atoi(pc); err == nil && n > 0 {
				pageCount = n
			}
		}
		var pageDramas []Drama
		for _, item := range resp.List {
			vodID := parseVodID(item.VodID)
			if vodID == "" || vodID == "0" {
				continue
			}
			if typeID == "" && !d.cfg.FullCategories {
				name := strings.TrimSpace(item.TypeName)
				if !strings.Contains(name, "短剧") && !strings.Contains(name, "微短剧") && !strings.Contains(name, "爽文") && !strings.Contains(name, "微剧") {
					continue
				}
			}
			dr := item.toDrama(canon)
			pageDramas = append(pageDramas, dr)
		}
		return pageDramas, pageCount, nil
	}

	if len(typeIDs) > 0 {
		for _, typeID := range typeIDs {
			if ctx.Err() != nil {
				return dramas, ctx.Err()
			}
			for page := 1; page <= maxPages; page++ {
				pageDramas, totalPages, err := fetchPage(page, typeID)
				if err != nil || len(pageDramas) == 0 {
					break
				}
				for _, dr := range pageDramas {
					if !seen[dr.ID] {
						seen[dr.ID] = true
						dramas = append(dramas, dr)
					}
				}
				reportLibraryProgress(ctx, canon, pageDramas, nil, false)
				if page >= totalPages {
					break
				}
			}
		}
	} else {
		for page := 1; page <= maxPages; page++ {
			if ctx.Err() != nil {
				return dramas, ctx.Err()
			}
			pageDramas, totalPages, err := fetchPage(page, "")
			if err != nil || len(pageDramas) == 0 {
				break
			}
			for _, dr := range pageDramas {
				if !seen[dr.ID] {
					seen[dr.ID] = true
					dramas = append(dramas, dr)
				}
			}
			reportLibraryProgress(ctx, canon, pageDramas, nil, false)
			if page >= totalPages {
				break
			}
		}
	}

	return dramas, nil
}

func (d *Downloader) fetchMacCMSChapters(ctx context.Context, source, sourceID string) (string, []Chapter, error) {
	canon := canonicalProviderSource(source)
	baseURL := d.macCMSBaseURL(canon)
	if baseURL == "" {
		return "", nil, fmt.Errorf("未配置站源 %s 的地址", source)
	}
	reqURL := fmt.Sprintf("%s?ac=detail&ids=%s", baseURL, url.QueryEscape(sourceID))
	body, err := d.fetchProviderText(ctx, reqURL, baseURL)
	if err != nil {
		return "", nil, err
	}
	var resp macCMSResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return "", nil, fmt.Errorf("解析剧集详情失败: %w", err)
	}
	if len(resp.List) == 0 {
		return "", nil, errors.New("源站未找到该剧集信息")
	}
	item := resp.List[0]
	fullDramaID := providerDramaID(canon, sourceID)
	chapters := parseMacCMSChapters(fullDramaID, canon, item.VodPlayFrom, item.VodPlayURL)
	return strings.TrimSpace(item.VodName), chapters, nil
}

func (d *Downloader) searchMacCMSDramas(ctx context.Context, source, keyword string) ([]Drama, error) {
	canon := canonicalProviderSource(source)
	baseURL := d.macCMSBaseURL(canon)
	if baseURL == "" {
		return nil, fmt.Errorf("未配置站源 %s 的地址", source)
	}
	reqURL := fmt.Sprintf("%s?ac=detail&wd=%s", baseURL, url.QueryEscape(keyword))
	body, err := d.fetchProviderText(ctx, reqURL, baseURL)
	if err != nil {
		return nil, err
	}
	var resp macCMSResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("解析搜索结果失败: %w", err)
	}
	var dramas []Drama
	seen := make(map[string]bool)
	for _, item := range resp.List {
		vodID := parseVodID(item.VodID)
		if vodID == "" || vodID == "0" {
			continue
		}
		dr := item.toDrama(canon)
		if !seen[dr.ID] {
			seen[dr.ID] = true
			dramas = append(dramas, dr)
		}
	}
	return dramas, nil
}

func macCMSSourceMatchScore(source, fromName string) int {
	source = canonicalProviderSource(source)
	fromName = strings.ToLower(fromName)
	switch source {
	case sourceXL:
		if strings.Contains(fromName, "xl") || strings.Contains(fromName, "xunlei") || strings.Contains(fromName, "xinlang") {
			return 50
		}
	case sourceBF:
		if strings.Contains(fromName, "bf") || strings.Contains(fromName, "baofeng") {
			return 50
		}
	case sourceFF:
		if strings.Contains(fromName, "ff") || strings.Contains(fromName, "feifan") {
			return 50
		}
	case sourceWJ:
		if strings.Contains(fromName, "wj") || strings.Contains(fromName, "wujin") {
			return 50
		}
	case sourceLZ:
		if strings.Contains(fromName, "lz") || strings.Contains(fromName, "liangzi") {
			return 50
		}
	case sourceHN:
		if strings.Contains(fromName, "hn") || strings.Contains(fromName, "hongniu") {
			return 50
		}
	case sourceSD:
		if strings.Contains(fromName, "sd") || strings.Contains(fromName, "shandian") {
			return 50
		}
	case sourceBD:
		if strings.Contains(fromName, "bd") || strings.Contains(fromName, "baidu") || strings.Contains(fromName, "db") {
			return 50
		}
	}
	return 0
}

type macCMSParsedEpisode struct {
	title    string
	mediaURL string
}

type macCMSPlayGroup struct {
	index    int
	score    int
	episodes []macCMSParsedEpisode
}

func parseMacCMSChapters(dramaID, source, playFrom, playURL string) []Chapter {
	if strings.TrimSpace(playURL) == "" {
		return nil
	}
	fromGroups := strings.Split(playFrom, "$$$")
	urlGroups := strings.Split(playURL, "$$$")

	var groups []macCMSPlayGroup
	for idx, group := range urlGroups {
		score := 0
		if idx < len(fromGroups) {
			fromName := strings.ToLower(fromGroups[idx])
			if strings.Contains(fromName, "m3u8") {
				score += 10
			}
			score += macCMSSourceMatchScore(source, fromName)
		}
		if strings.Contains(strings.ToLower(group), ".m3u8") {
			score += 5
		}
		episodeItems := strings.Split(group, "#")
		var episodes []macCMSParsedEpisode
		for epIdx, epStr := range episodeItems {
			epStr = strings.TrimSpace(epStr)
			if epStr == "" {
				continue
			}
			title := fmt.Sprintf("第 %d 集", epIdx+1)
			mediaURL := epStr
			if parts := strings.SplitN(epStr, "$", 2); len(parts) == 2 {
				title = strings.TrimSpace(parts[0])
				mediaURL = strings.TrimSpace(parts[1])
			}
			if mediaURL == "" || !isProviderHTTPMediaURL(mediaURL) {
				continue
			}
			episodes = append(episodes, macCMSParsedEpisode{
				title:    title,
				mediaURL: mediaURL,
			})
		}
		if len(episodes) > 0 {
			groups = append(groups, macCMSPlayGroup{
				index:    idx,
				score:    score,
				episodes: episodes,
			})
		}
	}

	if len(groups) == 0 {
		return nil
	}

	sort.SliceStable(groups, func(i, j int) bool {
		return groups[i].score > groups[j].score
	})

	primaryGroup := groups[0]
	backupGroups := groups[1:]

	var chapters []Chapter
	for idx, ep := range primaryGroup.episodes {
		var backupURLs []string
		seenURLs := map[string]bool{ep.mediaURL: true}
		for _, bg := range backupGroups {
			if idx < len(bg.episodes) {
				bURL := bg.episodes[idx].mediaURL
				if bURL != "" && !seenURLs[bURL] {
					seenURLs[bURL] = true
					backupURLs = append(backupURLs, bURL)
				}
			}
		}
		epNum := idx + 1
		rawEp, _ := json.Marshal(epNum)
		chapters = append(chapters, Chapter{
			ID:             fmt.Sprintf("%s:%d", dramaID, epNum),
			Source:         source,
			Title:          ep.title,
			VideoURL:       ep.mediaURL,
			BackupURLs:     backupURLs,
			CurrentEpisode: rawEp,
		})
	}
	return chapters
}
