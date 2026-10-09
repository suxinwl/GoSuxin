package suxinvideo

import (
	"context"
	"net/url"
	"strings"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	xq "github.com/suxinwl/GoSuxin/internal/xiaoqiapp"
)

func appChannels() []row {
	items := make([]row, 0, len(yqkDefaultSiteChannels))
	for _, channel := range yqkDefaultSiteChannels {
		items = append(items, row{"id": channel.ID, "name": channel.Name})
	}
	return items
}

func appLimitRows(items []row, limit int) []row {
	if len(items) > limit {
		return items[:limit]
	}
	return items
}

func appHomeListRows(ctx context.Context, where, order string, args []any, limit int) ([]row, error) {
	args = append(append([]any{}, args...), limit)
	return appRows(ctx, "SELECT v.id,v.type_id,v.name,v.pic,v.remarks,v.year,v.score,v.area,v.vip,v.points,v.updatetime,v.class FROM sx_vod v WHERE "+where+" ORDER BY "+order+" LIMIT ?", args...)
}

func appListChannelFilms(ctx context.Context, channelID int, word string, typeID int64, page, size int, order, year, area string) (row, error) {
	page = clampPage(page)
	if size < 1 || size > 100 {
		size = 24
	}
	if order == "" {
		order = "time"
	}
	if order == "hot" {
		order = "hits"
	}
	if len([]rune(word)) > 100 {
		return nil, appError(400, "搜索词过长")
	}
	tax, err := loadLocalChannelTaxonomy(ctx)
	if err != nil {
		return nil, err
	}
	query, err := buildLocalChannelQuery(&YQKChannelReq{ID: channelID, Type: typeID, Page: page, Order: order, Year: year, Area: area}, tax, publicVodListingCondition(ctx, "v"))
	if err != nil {
		return nil, appError(400, err.Error())
	}
	if word != "" {
		query.Where += " AND (v.name LIKE ? OR v.sub LIKE ?)"
		query.Args = append(query.Args, "%"+word+"%", "%"+word+"%")
	}
	count, err := one(ctx, "SELECT COUNT(*) total FROM sx_vod v WHERE "+query.Where, query.Args...)
	if err != nil {
		return nil, err
	}
	args := append(append([]any{}, query.Args...), size, (page-1)*size)
	items, err := appRows(ctx, "SELECT v.id,v.type_id,v.name,v.pic,v.remarks,v.year,v.score,v.area,v.vip,v.points,v.updatetime FROM sx_vod v WHERE "+query.Where+" ORDER BY "+query.Order+" LIMIT ? OFFSET ?", args...)
	if err != nil {
		return nil, err
	}
	appPrepareFilms(ctx, items)
	return row{"items": items, "page": page, "size": size, "total": gconv.Int64(count["total"]), "pages": max(1, (gconv.Int64(count["total"])+int64(size)-1)/int64(size))}, nil
}

// Native recommendations must link to stable local films, never the HTML-only
// remote redirect links. Ambiguous same-title versions are omitted instead of
// silently opening a different production.
func appLocalizeRecommendations(ctx context.Context, items []row) []row {
	items = appLimitRows(items, 64)
	result := make([]row, 0, len(items))
	names, ids, args := []string{}, []int64{}, []any{}
	seenNames, seenIDs := map[string]bool{}, map[int64]bool{}
	for _, item := range items {
		if name := strings.TrimSpace(gconv.String(item["name"])); name != "" && !seenNames[name] {
			seenNames[name] = true
			names = append(names, name)
		}
		if id := gconv.Int64(item["id"]); id > 0 && !seenIDs[id] {
			seenIDs[id] = true
			ids = append(ids, id)
		}
	}
	conditions := []string{}
	if len(names) > 0 {
		conditions = append(conditions, "name IN ("+strings.TrimSuffix(strings.Repeat("?,", len(names)), ",")+")")
		for _, name := range names {
			args = append(args, name)
		}
	}
	if len(ids) > 0 {
		conditions = append(conditions, "id IN ("+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+")")
		for _, id := range ids {
			args = append(args, id)
		}
	}
	if len(conditions) == 0 {
		return result
	}
	// Lookup indexed titles in one bounded query. Searching every remote ID
	// with LIKE over the large play_url column caused minute-long app starts.
	matched, err := appRows(ctx, "SELECT id,type_id,name,pic,remarks,year,score,area,vip,content,class,play_from,play_url,api_vid FROM sx_vod WHERE ("+strings.Join(conditions, " OR ")+") AND "+publicVodListingCondition(ctx, "")+" ORDER BY id LIMIT 256", args...)
	if err != nil {
		return result
	}
	byName, byID := map[string][]row{}, map[int64]row{}
	for _, film := range matched {
		name := gconv.String(film["name"])
		byName[name] = append(byName[name], film)
		byID[gconv.Int64(film["id"])] = film
	}
	seen := map[int64]bool{}
	for _, item := range items {
		id := gconv.Int64(item["id"])
		film := byID[id]
		if film == nil {
			link, _ := url.Parse(gconv.String(item["link"]))
			candidates := byName[strings.TrimSpace(gconv.String(item["name"]))]
			if len(candidates) > 1 && link != nil {
				exact := []row{}
				remoteID := link.Query().Get("id")
				for _, candidate := range candidates {
					play := gconv.String(candidate["play_url"])
					if strings.Contains(link.Path, "/yqk/vod") && yqkPositiveID(remoteID) && strings.Contains(play, "yqk://"+remoteID+"/") || strings.Contains(link.Path, "/4kvm/vod") && fourKVMSiteSlugPattern.MatchString(remoteID) && (strings.Contains(play, "4kvm://"+remoteID) || gconv.String(candidate["api_vid"]) == remoteID) {
						exact = append(exact, candidate)
					}
				}
				candidates = exact
			}
			if len(candidates) != 1 {
				continue
			}
			film = candidates[0]
			id = gconv.Int64(film["id"])
		}
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		copy := row{}
		for k, v := range film {
			if k != "play_url" && k != "play_from" && k != "api_vid" {
				copy[k] = v
			}
		}
		if pic := gconv.String(item["pic"]); pic != "" {
			copy["pic"] = pic
		}
		copy["id"] = id
		if remoteID := gconv.String(item["__featured_remote_id"]); remoteID != "" {
			copy["__featured_remote_id"] = remoteID
		}
		result = append(result, copy)
	}
	return result
}

func appLocalizeHeroes(ctx context.Context, input []homeHero) []homeHero {
	result := make([]homeHero, 0, len(input))
	for _, hero := range input {
		if !hero.Film {
			result = append(result, hero)
			continue
		}
		u, _ := url.Parse(hero.Link)
		id := int64(0)
		if u != nil && (u.Path == "/suxinvideo/detail" || u.Path == "/suxinvideo/play") {
			id = gconv.Int64(u.Query().Get("id"))
		}
		if id < 1 {
			matched := appLocalizeRecommendations(ctx, []row{{"id": 0, "name": hero.Name, "pic": hero.Pic, "link": hero.Link}})
			if len(matched) != 1 {
				continue
			}
			id = gconv.Int64(matched[0]["id"])
		}
		hero.Link = "/suxinvideo/detail?id=" + gconv.String(id)
		result = append(result, hero)
	}
	return result
}

func appFourKVMRecommendations(ctx context.Context) ([]homeHero, []row, []row) {
	if _, err := fourKVMSiteSource(ctx); err != nil {
		return nil, nil, nil
	}
	home := fourKVMSiteSnapshot(ctx).Home
	heroes := []homeHero{}
	recent, hot := []row{}, []row{}
	for _, banner := range home.Banners {
		items := fourKVMSiteRows(ctx, []xq.Drama{banner.Drama})
		if len(items) == 0 {
			continue
		}
		hero := filmHero(items[0])
		hero.Link = gconv.String(items[0]["link"])
		hero.Pic = banner.ImageURL
		heroes = append(heroes, hero)
	}
	for _, section := range home.Sections {
		if section.Key == "recent" {
			recent = appLocalizeRecommendations(ctx, fourKVMSiteRows(ctx, section.Dramas))
		}
		if section.Key == "hot" {
			hot = appLocalizeRecommendations(ctx, fourKVMSiteRows(ctx, section.Dramas))
		}
	}
	if len(recent) == 0 {
		recent = fourKVMSiteLocalRows(ctx, "recent", 24)
	}
	if len(hot) == 0 {
		hot = fourKVMSiteLocalRows(ctx, "hot", 12)
	}
	return appLocalizeHeroes(ctx, heroes), recent, hot
}
