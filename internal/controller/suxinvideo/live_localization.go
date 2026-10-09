package suxinvideo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

var liveGroupAliases = map[string]string{
	"entertainment": "娱乐", "shop": "购物", "movies": "电影", "undefined": "未分类",
	"business": "财经", "general": "综合", "news": "新闻", "sports": "体育",
	"movies;series": "电影／剧集", "religious": "宗教", "culture": "文化", "kids": "少儿",
	"education": "教育", "documentary": "纪录", "music": "音乐", "lifestyle": "生活",
	"education;outdoor": "教育／户外", "classic": "经典", "weather": "气象",
	"education;science": "教育／科普", "animation;kids": "动画／少儿", "general;travel": "综合／旅游",
	"legislative": "政务", "education;lifestyle;science": "教育／生活／科普", "kids;lifestyle": "少儿／生活",
	"science": "科普", "family": "家庭", "series": "剧集", "animation": "动画", "travel": "旅游",
}

func liveGroupDisplayName(name string) string {
	name = strings.TrimSpace(name)
	if alias := liveGroupAliases[strings.ToLower(name)]; alias != "" {
		return alias
	}
	return name
}

// A translated label must retain the original category identity. The reverse
// mapping also lets a local Seed reuse a group after an earlier name upgrade.
func liveGroupIdentity(name string) string {
	if name == "港澳台" {
		return liveIdentity(liveRegionalGroupKey)
	}
	if name == "CCTV" {
		return liveIdentity(liveCCTVGroupKey)
	}
	key := strings.ToLower(strings.TrimSpace(name))
	for raw, alias := range liveGroupAliases {
		if name == alias {
			key = raw
			break
		}
	}
	return liveIdentity(key)
}

func liveFindImportGroup(ctx context.Context, name string) (row, error) {
	group, err := one(ctx, "SELECT id FROM sx_live_group WHERE identity_key=?", liveGroupIdentity(name))
	if err != nil || group != nil {
		return group, err
	}
	// Manually named groups may have unrelated identity keys. A unique exact
	// name can be reused, but an ambiguous name never selects an arbitrary ID.
	groups, err := all(ctx, "SELECT id FROM sx_live_group WHERE name=? ORDER BY id LIMIT 2", name)
	if err != nil {
		return nil, err
	}
	if len(groups) == 1 {
		return groups[0], nil
	}
	if len(groups) > 1 {
		return nil, errors.New("直播分组名称不唯一；请在后台确认分组")
	}
	return nil, nil
}

// LocalizeLiveCatalog updates display labels only. Administrative edits, EPG
// identifiers, identities, aliases, ordering, enabled state and membership are
// untouched. It is safe to repeat on an existing installation.
func LocalizeLiveCatalog(ctx context.Context) error {
	groups, err := all(ctx, "SELECT id,name FROM sx_live_group WHERE manual_edited=0")
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	for _, group := range groups {
		old := gconv.String(group["name"])
		if name := liveGroupDisplayName(old); name != old {
			if err = execSQL(ctx, "UPDATE sx_live_group SET name=?,updated=? WHERE id=? AND manual_edited=0 AND name=?", name, now, group["id"], old); err != nil {
				return err
			}
		}
	}
	channels, err := all(ctx, "SELECT id,tvg_id,name FROM sx_live_channel WHERE manual_edited=0")
	if err != nil {
		return err
	}
	for _, channel := range channels {
		old := gconv.String(channel["name"])
		name := liveDisplayName(gconv.String(channel["tvg_id"]), old)
		if name != old {
			if err = execSQL(ctx, "UPDATE sx_live_channel SET name=?,updated=? WHERE id=? AND manual_edited=0 AND name=?", name, now, channel["id"], old); err != nil {
				return err
			}
		}
	}
	return nil
}

// Source numbering uses ID order, including disabled sources, so a temporary
// failure, manual priority change or health sort cannot renumber the backups.
func liveStreamDisplayName(r row) string {
	if gconv.Int(r["manual_edited"]) != 0 || gconv.Int(r["line_no"]) < 1 {
		return gconv.String(r["name"])
	}
	n := gconv.Int(r["line_no"])
	name := "线路1"
	if n > 1 {
		name = fmt.Sprintf("备用%d", n)
	}
	quality := strings.TrimSpace(gconv.String(r["quality"]))
	if quality != "" {
		name += " · " + quality
	}
	return name
}
