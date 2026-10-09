package suxinvideo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"github.com/suxinwl/GoSuxin/internal/yqksign"
)

// These IDs are the eight channels published by the APP home/header API. They
// belong to that provider, and must never be treated as local sx_type IDs.
var yqkSiteChannelIDs = map[int]bool{50: true, 65: true, 2: true, 3: true, 8: true, 10: true, 56: true, 5: true}

type yqkSiteChannel struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type yqkSiteTopic struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type yqkSiteRemotePage struct {
	Items      []map[string]any
	NextVal    string
	HasNext    bool
	Topics     []yqkSiteTopic
	TopicID    int
	Page       int
	TotalPages int
	Total      int
	Notice     string
}

type yqkSiteRemoteHome struct {
	Channels                 []yqkSiteChannel
	Recent, Hot, Recommended []map[string]any
	PartialErrors            []string
}

func yqkSiteItems(address string, input []map[string]any) []map[string]any {
	seen := map[string]bool{}
	items := make([]map[string]any, 0, len(input))
	for _, item := range input {
		id, name := gconv.String(item["vodId"]), strings.TrimSpace(gconv.String(item["vodName"]))
		if !yqkPositiveID(id) || name == "" || len(name) > 512 || seen[id] {
			continue
		}
		seen[id] = true
		clean := map[string]any{"vodId": id, "vodName": name}
		// Copy only film metadata. APP advertising, jump URLs and account fields
		// do not enter the CMS's navigation, cards or hero banners.
		for _, key := range []string{"coverImg", "score", "remark", "intro", "flags", "watchingCountDesc"} {
			if value, ok := item[key]; ok {
				text := gconv.String(value)
				if len(text) <= 16384 {
					clean[key] = text
				}
			}
		}
		yqkRememberList(address, clean)
		items = append(items, clean)
		if len(items) >= 1000 {
			break
		}
	}
	return items
}

func yqkSiteCall(ctx context.Context, client *yqksign.Client, address, suffix, path string, params map[string]any, ttl time.Duration, output any) error {
	raw, err := yqkCachedCall(ctx, client, address+":site:"+suffix, path, params, ttl)
	if err != nil {
		return err
	}
	if yqkDecode(raw, output) != nil {
		return errors.New("小柒页面数据格式变化")
	}
	return nil
}

func fetchYQKSiteHome(ctx context.Context) (home yqkSiteRemoteHome, err error) {
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	err = withYQK(ctx, false, func(ctx context.Context, client *yqksign.Client, address string) error {
		home, err = yqkSiteHomeFromClient(ctx, client, address)
		return err
	})
	return
}

// Independent home sections survive an outage of any other section. Callers
// retain previous nonempty sections when PartialErrors is set, then retry soon.
func yqkSiteHomeFromClient(ctx context.Context, client *yqksign.Client, address string) (home yqkSiteRemoteHome, err error) {
	var header struct {
		Channels []map[string]any `json:"channeList"`
	}
	if e := yqkSiteCall(ctx, client, address, "home-header", "/v2/api/home/header", nil, 15*time.Minute, &header); e != nil {
		home.PartialErrors = append(home.PartialErrors, "导航分类获取失败")
	} else {
		seen := map[int]bool{}
		for _, channel := range header.Channels {
			id, name := gconv.Int(channel["channelId"]), strings.TrimSpace(gconv.String(channel["channelName"]))
			if yqkSiteChannelIDs[id] && name != "" && len(name) <= 128 && !seen[id] {
				home.Channels = append(home.Channels, yqkSiteChannel{ID: id, Name: name})
				seen[id] = true
			}
		}
		if len(home.Channels) == 0 {
			home.PartialErrors = append(home.PartialErrors, "导航分类为空")
		}
	}
	var recent struct {
		Items []map[string]any `json:"items"`
	}
	if e := yqkSiteCall(ctx, client, address, "home-recent", "/v1/api/search/queryNow", map[string]any{"nextCount": 24, "nextVal": "", "queryValueJson": "[]", "sortType": 1}, time.Minute, &recent); e != nil {
		home.PartialErrors = append(home.PartialErrors, "最新影视获取失败")
	} else {
		home.Recent = yqkSiteItems(address, recent.Items)
	}
	var hot []map[string]any
	if e := yqkSiteCall(ctx, client, address, "home-hot", "/v1/api/vodRank/getRankList", map[string]any{"channelId": 0, "rankType": 1}, time.Minute, &hot); e != nil {
		home.PartialErrors = append(home.PartialErrors, "热播排行获取失败")
	} else {
		home.Hot = yqkSiteItems(address, hot)
	}
	var screen struct {
		Hot []map[string]any `json:"hotVodList"`
	}
	if e := yqkSiteCall(ctx, client, address, "home-screen", "/v2/api/home/firstScreen", nil, 5*time.Minute, &screen); e != nil {
		home.PartialErrors = append(home.PartialErrors, "轮播推荐获取失败")
	} else {
		home.Recommended = yqkSiteItems(address, screen.Hot)
	}
	if len(home.Channels)+len(home.Recent)+len(home.Hot)+len(home.Recommended) == 0 {
		return home, errors.New("小柒未返回可用首页内容")
	}
	return home, nil
}

func fetchYQKSiteChannel(ctx context.Context, channelID, topicID, page int, order, cursor string) (result yqkSiteRemotePage, err error) {
	return fetchYQKSiteChannelSized(ctx, channelID, topicID, page, 24, order, cursor)
}

func fetchYQKSiteChannelSized(ctx context.Context, channelID, topicID, page, size int, order, cursor string) (result yqkSiteRemotePage, err error) {
	if page < 1 || page > 10000 || topicID < 0 || (channelID != 0 && !yqkSiteChannelIDs[channelID]) {
		return result, errors.New("小柒频道或页码无效")
	}
	if size < 1 || size > 100 {
		return result, errors.New("小柒每页数量无效")
	}
	if len(cursor) > 2048 || strings.ContainsAny(cursor, "\r\n") {
		return result, errors.New("小柒分页游标无效")
	}
	if order == "" {
		order = "time"
	}
	if order != "time" && order != "hits" {
		return result, errors.New("小柒排序无效")
	}
	if channelID == 0 && topicID != 0 {
		return result, errors.New("小柒专题不属于此频道")
	}
	if channelID == 0 && page > 1 && cursor == "" {
		return result, errors.New("小柒下一页缺少分页游标")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err = withYQK(ctx, false, func(ctx context.Context, client *yqksign.Client, address string) error {
		result, err = yqkSiteChannelFromClient(ctx, client, address, channelID, topicID, page, size, order, cursor)
		return err
	})
	return
}

func yqkSiteChannelFromClient(ctx context.Context, client *yqksign.Client, address string, channelID, topicID, page, size int, order, cursor string) (result yqkSiteRemotePage, err error) {
	err = func() error {
		result.Page, result.TopicID = page, topicID
		if channelID == 0 {
			sortType := 1
			if order == "hits" {
				sortType = 2
			}
			var data struct {
				Items   []map[string]any `json:"items"`
				NextVal string           `json:"nextVal"`
				HasNext bool             `json:"hasNext"`
			}
			params := map[string]any{"nextCount": size, "nextVal": cursor, "queryValueJson": "[]", "sortType": sortType}
			keyData, _ := json.Marshal(params)
			if e := yqkSiteCall(ctx, client, address, "all:"+string(keyData), "/v1/api/search/queryNow", params, time.Minute, &data); e != nil {
				return e
			}
			if data.HasNext && (data.NextVal == "" || data.NextVal == cursor || len(data.NextVal) > 2048) {
				return errors.New("小柒未返回有效的下一页游标")
			}
			result.Items = yqkSiteItems(address, data.Items)
			result.HasNext, result.NextVal = data.HasNext, data.NextVal
			result.TotalPages = page
			// Cursor lists have no exact total; -1 explicitly preserves that fact.
			result.Total = -1
			if result.HasNext {
				result.TotalPages++
			}
			return nil
		}
		var channel struct {
			Topics []map[string]any `json:"topicList"`
		}
		if e := yqkSiteCall(ctx, client, address, "channel:"+strconv.Itoa(channelID), "/v2/api/channel/topicListView", map[string]any{"channelId": channelID}, 10*time.Minute, &channel); e != nil {
			return e
		}
		if len(channel.Topics) > 100 {
			return errors.New("小柒频道专题超出限制")
		}
		previews := []map[string]any{}
		belongs := false
		seenTopics := map[int]bool{}
		for _, topic := range channel.Topics {
			id, name := gconv.Int(topic["vodTopicId"]), strings.TrimSpace(gconv.String(topic["topicName"]))
			if id < 1 || name == "" || len(name) > 256 || seenTopics[id] {
				continue
			}
			seenTopics[id] = true
			result.Topics = append(result.Topics, yqkSiteTopic{ID: id, Name: name})
			belongs = belongs || id == topicID
			previews = append(previews, gconv.Maps(topic["vodList"])...)
		}
		// Channel previews keep the provider's curated ordering. It is not a
		// chronological catalogue; only the all-videos endpoint offers that sort.
		result.Notice = "频道精选"
		if topicID == 0 {
			items := yqkSiteItems(address, previews)
			result.Total = len(items)
			result.TotalPages = max(1, (len(items)+size-1)/size)
			start := (page - 1) * size
			if start >= len(items) {
				result.Items = []map[string]any{}
				return nil
			}
			result.Items = items[start:min(start+size, len(items))]
			result.HasNext = page < result.TotalPages
			return nil
		}
		if !belongs {
			return errors.New("小柒专题不属于此频道")
		}
		var data struct {
			Items      []map[string]any `json:"items"`
			Page       int              `json:"pageIndex"`
			TotalPages int              `json:"totalPages"`
		}
		params := map[string]any{"vodTopicId": topicID, "pageIndex": page, "pageSize": size}
		if e := yqkSiteCall(ctx, client, address, fmt.Sprintf("topic:%d:%d:%d", topicID, page, size), "/v1/api/vodTopic/getVodList", params, 2*time.Minute, &data); e != nil {
			return e
		}
		if data.Page != page || data.TotalPages < 0 || data.TotalPages > 100000 {
			return errors.New("小柒专题分页数据无效")
		}
		result.Items = yqkSiteItems(address, data.Items)
		result.TotalPages = max(1, data.TotalPages)
		result.Total = -1
		result.HasNext = page < result.TotalPages
		result.Notice = "专题片单"
		return nil
	}()
	return
}
