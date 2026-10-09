package suxinvideo

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

// The remote IDs are scoped to this source. They must never be treated as
// local category IDs (remote 1 is Japanese anime, while local 1 is movies).
var erciyuanCategoryAliases = map[int][]string{
	1:  {"日本动漫", "日韩动漫", "日漫"},
	2:  {"国产动漫", "中国动漫", "国漫"},
	3:  {"动漫电影", "动画电影", "剧场版"},
	4:  {"经典番剧"},
	21: {"特摄"},
	27: {"动态漫画"},
}

func chooseErciyuanType(types []row, remoteID int) int64 {
	// Duplicate labels can exist below another root. Category names alone
	// cannot move imported anime into that unrelated navigation branch.
	allowed := make(map[int64]bool)
	for _, category := range types {
		if gconv.Int64(category["pid"]) == 0 && (gconv.String(category["name"]) == "动漫" || gconv.String(category["name"]) == "动画") {
			allowed[gconv.Int64(category["id"])] = true
		}
	}
	for pass := 0; pass < len(types); pass++ {
		changed := false
		for _, category := range types {
			id, pid := gconv.Int64(category["id"]), gconv.Int64(category["pid"])
			if id > 0 && pid > 0 && allowed[pid] && !allowed[id] {
				allowed[id], changed = true, true
			}
		}
		if !changed {
			break
		}
	}
	for _, name := range erciyuanCategoryAliases[remoteID] {
		for _, category := range types {
			if allowed[gconv.Int64(category["id"])] && strings.TrimSpace(gconv.String(category["name"])) == name {
				return gconv.Int64(category["id"])
			}
		}
	}
	return 0
}

// Reuse existing anime children without changing their visibility, order or
// parent. Missing categories are added beneath the existing anime root.
func ensureErciyuanType(ctx context.Context, item map[string]any) (int64, error) {
	remoteID := gconv.Int(item["type_id"])
	aliases, ok := erciyuanCategoryAliases[remoteID]
	if !ok {
		// Some search/play responses have only the original category name.
		for id, names := range map[int][]string{1: {"日漫"}, 2: {"国漫"}, 3: {"剧场版"}, 4: {"经典番剧"}, 21: {"特摄"}, 27: {"动态漫画"}} {
			if gconv.String(item["type_name"]) == names[0] {
				remoteID, aliases, ok = id, erciyuanCategoryAliases[id], true
				break
			}
		}
	}
	if !ok {
		return 0, errors.New("二次元影片分类无效，未写入站点导航")
	}
	types, err := all(ctx, "SELECT id,pid,name FROM sx_type ORDER BY id")
	if err != nil {
		return 0, err
	}
	if id := chooseErciyuanType(types, remoteID); id > 0 {
		return id, nil
	}
	parentID := int64(0)
	for _, category := range types {
		if gconv.Int64(category["pid"]) == 0 && (gconv.String(category["name"]) == "动漫" || gconv.String(category["name"]) == "动画") {
			parentID = gconv.Int64(category["id"])
			break
		}
	}
	if parentID == 0 {
		return 0, errors.New("站点未设置动漫主分类，请先在分类管理中创建")
	}
	result, err := g.DB().Exec(ctx, "INSERT INTO sx_type(pid,name,sort,status,show_home) VALUES(?,?,50,1,0)", parentID, aliases[0])
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// Existing source status, schedule and custom player labels survive upgrades.
func prepareErciyuanSource(ctx context.Context) error {
	for _, code := range erciyuanPlaybackCodes() {
		if err := execSQL(ctx, "INSERT IGNORE INTO sx_player(code,name,`parse`,status) VALUES(?,?, '',1)", code, erciyuanPlayers[code]); err != nil {
			return err
		}
	}
	return execSQL(ctx, "INSERT INTO sx_collect_api(name,api_url,remark,status,collect_auto,collect_hours,addtime) SELECT ?,?,?,1,1,12,? WHERE NOT EXISTS (SELECT 1 FROM sx_collect_api WHERE api_url=?)", "二次元", erciyuanSourceURL, "星次元动漫片源；分类及影片动态读取，分集播放时解析；自动采集各分类近期一页", time.Now().Unix(), erciyuanSourceURL)
}
