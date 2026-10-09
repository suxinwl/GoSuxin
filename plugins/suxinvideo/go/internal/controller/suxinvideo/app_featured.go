package suxinvideo

import (
	"context"
	"crypto/hmac"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

func appFeaturedResolveToken(ctx context.Context, id string) string {
	expires := time.Now().Add(2 * time.Hour).Unix()
	return strconv.FormatInt(expires, 10) + "." + appFeaturedSignature(ctx, id, expires)
}

func appFeaturedSignature(ctx context.Context, id string, expires int64) string {
	return signProxy(ctx, "app-featured:"+yqkSiteKey(ctx)+":"+id, expires)
}

func validateAppFeaturedToken(id, token string, now int64, signature func(string, int64) string) bool {
	if !yqkPositiveID(id) || len(token) > 100 {
		return false
	}
	expiry, supplied, ok := strings.Cut(token, ".")
	expires, err := strconv.ParseInt(expiry, 10, 64)
	if !ok || err != nil || len(supplied) != 64 || expires < now || expires > now+2*3600 {
		return false
	}
	return hmac.Equal([]byte(supplied), []byte(signature(id, expires)))
}

func appResolveFeatured(ctx context.Context, id, token string) (row, error) {
	if !validateAppFeaturedToken(id, token, time.Now().Unix(), func(id string, expires int64) string {
		return appFeaturedSignature(ctx, id, expires)
	}) {
		return nil, appError(http.StatusBadRequest, "影片链接已过期，请刷新精选列表")
	}
	film, err := yqkSiteImport(ctx, id)
	if err != nil {
		return nil, err
	}
	localID := gconv.Int64(film["id"])
	if localID <= 0 {
		return nil, appError(http.StatusServiceUnavailable, "影片暂时无法载入，请稍后重试")
	}
	return row{"vod_id": localID}, nil
}

func appFeatured(ctx context.Context, channel, topic, page, size int, order string) (row, error) {
	if (channel != 0 && !yqkSiteChannelIDs[channel]) || topic < 0 || topic > 1000000000 || (channel == 0 && topic != 0) || page < 1 || page > 10000 || size < 1 || size > 100 || (order != "" && order != "time" && order != "hits") {
		return nil, appError(http.StatusBadRequest, "精选频道或分页参数无效")
	}
	if order == "" || channel != 0 {
		order = "time"
	}
	apiID, err := yqkSiteSource(ctx)
	if err != nil {
		return nil, appError(http.StatusServiceUnavailable, "小柒精选暂时不可用")
	}
	revision, err := appCatalogRevision(ctx)
	if err != nil {
		return nil, err
	}
	// APP capacities have separate cursor chains from the website's 24-card pages.
	key := fmt.Sprintf("%s:app:%d", yqkSiteKey(ctx), size)
	fetch := func(ctx context.Context, channel, topic, page int, order, cursor string) (yqkSiteRemotePage, error) {
		return fetchYQKSiteChannelSized(ctx, channel, topic, page, size, order, cursor)
	}
	remote, err := yqkSitePageForKeyWithFetch(ctx, key, channel, topic, page, order, fetch)
	if err != nil {
		return nil, appError(http.StatusServiceUnavailable, "精选列表已更新或暂时不可用，请从第一页重试")
	}
	rows := yqkSiteRows(ctx, remote.Items)
	ids, args := make([]string, 0, len(rows)), []any{apiID}
	for _, item := range rows {
		link, _ := url.Parse(gconv.String(item["link"]))
		if link == nil {
			continue
		}
		id := link.Query().Get("id")
		item["__featured_remote_id"] = id
		ids = append(ids, id)
		args = append(args, id)
	}
	hidden := map[string]bool{}
	if len(ids) > 0 {
		known, err := appRows(ctx, "SELECT id,api_vid,status,name,class,type_id FROM sx_vod WHERE api_id=? AND api_vid IN ("+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+")", args...)
		if err != nil {
			return nil, err
		}
		for _, item := range known {
			if gconv.Int(item["status"]) != 1 || !contentVodAllowed(ctx, item) {
				hidden[gconv.String(item["api_vid"])] = true
			}
		}
	}
	matched := appLocalizeRecommendations(ctx, rows)
	appPrepareFilms(ctx, matched)
	items := appFeaturedCards(rows, matched, hidden, func(id string) string { return appFeaturedResolveToken(ctx, id) })
	// Remote cards never pass through local source-score/taxonomy hydration.
	for _, item := range items {
		if gconv.Int64(item["id"]) != 0 {
			continue
		}
		item["score"] = gconv.Float64(item["score"])
		item["is_short"] = channel == 50
		item["is_anime"] = channel == 8
		if pic := gconv.String(item["pic"]); safePlayerAddress(pic) {
			item["pic"] = imageLink(ctx, pic)
		}
	}
	topics := append([]yqkSiteTopic{}, remote.Topics...)
	notice := remote.Notice
	if channel == 0 {
		notice = "按实际下一页浏览，总页数由片源逐页提供"
	} else if topic != 0 {
		notice = "专题片单，片源未提供精确影片总数"
	}
	return row{"items": items, "page": page, "pages": max(1, remote.TotalPages), "size": size, "total": remote.Total, "exact_pages": channel != 0, "has_next": remote.HasNext, "topics": topics, "notice": notice, "catalog_revision": revision}, nil
}

// Order is the provider's genuine curated order. Uncollected cards carry a
// signed deferred resolver, never a remote number disguised as a local vod ID.
func appFeaturedCards(remote, matched []row, hidden map[string]bool, token func(string) string) []row {
	byRemote := make(map[string]row, len(matched))
	for _, item := range matched {
		byRemote[gconv.String(item["__featured_remote_id"])] = item
	}
	items := make([]row, 0, len(remote))
	for _, original := range remote {
		id := gconv.String(original["__featured_remote_id"])
		if !yqkPositiveID(id) || hidden[id] {
			continue
		}
		source := byRemote[id]
		if source == nil || gconv.Int64(source["id"]) <= 0 {
			source = original
		}
		item := row{}
		for key, value := range source {
			if key != "__featured_remote_id" && key != "link" {
				item[key] = value
			}
		}
		if gconv.Int64(item["id"]) <= 0 {
			item["id"], item["provider"], item["remote_id"], item["resolve_token"] = int64(0), "yqk", id, token(id)
		}
		items = append(items, item)
	}
	return items
}
