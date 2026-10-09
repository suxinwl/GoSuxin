package suxinvideo

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"github.com/suxinwl/GoSuxin/utility/gf"
)

type CollectClassesReq struct {
	g.Meta `path:"/collect/classes" method:"get"`
	APIID  int64 `p:"api_id"`
}
type CollectClassesRes struct{}
type CollectPreviewReq struct {
	g.Meta `path:"/collect/preview" method:"get"`
	APIID  int64 `p:"api_id"`
	Page   int   `p:"page"`
	TypeID int   `p:"type_id"`
}
type CollectPreviewRes struct{}
type CollectDuplicatesReq struct {
	g.Meta `path:"/collect/duplicates" method:"get"`
}
type CollectDuplicatesRes struct{}
type CollectMergeReq struct {
	g.Meta `path:"/collect/merge" method:"post"`
	IDs    []int64 `p:"ids"`
}
type CollectMergeRes struct{}

func collectSourceURL(ctx context.Context, id int64) (string, error) {
	source, err := one(ctx, "SELECT api_url FROM sx_collect_api WHERE id=? AND status=1", id)
	if err != nil {
		return "", err
	}
	if source == nil {
		return "", fmt.Errorf("采集源不存在或已停用")
	}
	return gconv.String(source["api_url"]), nil
}

func (*Admin) CollectClasses(ctx context.Context, req *CollectClassesReq) (*CollectClassesRes, error) {
	raw, err := collectSourceURL(ctx, req.APIID)
	if err != nil {
		return nil, err
	}
	data, err := fetchCollectSource(ctx, raw, url.Values{"ac": {"list"}})
	if err != nil {
		return nil, err
	}
	g.RequestFromCtx(ctx).Response.WriteJson(gf.Success().SetData(map[string]any{"list": data.Class, "total": data.Total}))
	return &CollectClassesRes{}, nil
}
func (*Admin) CollectPreview(ctx context.Context, req *CollectPreviewReq) (*CollectPreviewRes, error) {
	r := g.RequestFromCtx(ctx)
	if req.Page < 1 || req.Page > 10000 || req.TypeID < 0 {
		badRequest(r, "预览参数无效")
		return &CollectPreviewRes{}, nil
	}
	raw, err := collectSourceURL(ctx, req.APIID)
	if err != nil {
		return nil, err
	}
	query := url.Values{"ac": {"videolist"}, "pg": {fmt.Sprint(req.Page)}}
	if req.TypeID > 0 {
		query.Set("t", fmt.Sprint(req.TypeID))
	}
	data, err := fetchCollectSource(ctx, raw, query)
	if err != nil {
		return nil, err
	}
	r.Response.WriteJson(gf.Success().SetData(map[string]any{"list": data.List, "total": data.Total, "page": data.Page, "pagecount": data.PageCount}))
	return &CollectPreviewRes{}, nil
}

func (*Admin) CollectDuplicates(ctx context.Context, _ *CollectDuplicatesReq) (*CollectDuplicatesRes, error) {
	vods, err := all(ctx, "SELECT id,name,year,play_from FROM sx_vod ORDER BY id LIMIT 10000")
	if err != nil {
		return nil, err
	}
	groups := map[string][]row{}
	keys := []string{}
	for _, vod := range vods {
		key := normalizeVodName(gconv.String(vod["name"]))
		if key == "" {
			continue
		}
		year := strings.TrimSpace(gconv.String(vod["year"]))
		key += "|" + year
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], vod)
	}
	sort.Strings(keys)
	result := make([]map[string]any, 0)
	for _, key := range keys {
		items := groups[key]
		if len(items) < 2 {
			continue
		}
		ids := make([]int64, 0, len(items))
		for _, item := range items {
			ids = append(ids, gconv.Int64(item["id"]))
		}
		result = append(result, map[string]any{"key": key, "name": items[0]["name"], "ids": ids})
		if len(result) >= 100 {
			break
		}
	}
	g.RequestFromCtx(ctx).Response.WriteJson(gf.Success().SetData(map[string]any{"list": result, "scanned": len(vods)}))
	return &CollectDuplicatesRes{}, nil
}

func (*Admin) CollectMerge(ctx context.Context, req *CollectMergeReq) (*CollectMergeRes, error) {
	r := g.RequestFromCtx(ctx)
	if len(req.IDs) < 2 || len(req.IDs) > 20 {
		badRequest(r, "合并参数无效")
		return &CollectMergeRes{}, nil
	}
	seen := map[int64]bool{}
	for _, id := range req.IDs {
		if id < 1 || seen[id] {
			badRequest(r, "影片 ID 无效")
			return &CollectMergeRes{}, nil
		}
		seen[id] = true
	}
	keep := req.IDs[0]
	err := g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		base, e := tx.GetOne("SELECT id,play_from,play_url FROM sx_vod WHERE id=? FOR UPDATE", keep)
		if e != nil {
			return e
		}
		if base == nil {
			return fmt.Errorf("主影片不存在")
		}
		from, play := base["play_from"].String(), base["play_url"].String()
		for _, id := range req.IDs[1:] {
			item, e := tx.GetOne("SELECT play_from,play_url FROM sx_vod WHERE id=? FOR UPDATE", id)
			if e != nil {
				return e
			}
			if item == nil {
				continue
			}
			from, play = mergePlay(from, play, item["play_from"].String(), item["play_url"].String())
			for _, table := range []string{"sx_comment", "sx_fav", "sx_play_record", "sx_user_vod"} {
				if _, e = tx.Exec("UPDATE IGNORE "+table+" SET vod_id=? WHERE vod_id=?", keep, id); e != nil {
					return e
				}
			}
			if _, e = tx.Exec("DELETE FROM sx_vod WHERE id=?", id); e != nil {
				return e
			}
		}
		_, e = tx.Exec("UPDATE sx_vod SET play_from=?,play_url=?,updatetime=? WHERE id=?", from, play, time.Now().Unix(), keep)
		return e
	})
	if err != nil {
		return nil, err
	}
	r.Response.WriteJson(gf.Success().SetData(true))
	return &CollectMergeRes{}, nil
}
