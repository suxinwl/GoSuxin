package suxinvideo

import (
	"context"
	"strings"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

func shortDramaMetadata(vod row, sources []source) bool {
	class := strings.TrimSpace(gconv.String(vod["class"]))
	if strings.EqualFold(class, "short") || strings.Contains(class, "短剧") || strings.Contains(class, "短劇") {
		return true
	}
	for _, src := range sources {
		if src.Code == "hongguo" {
			return true
		}
	}
	return false
}

// Follow the same category aliases and ancestry as the public short-drama
// channel; imported databases are free to use different category IDs.
func shortDramaPlaybackCategory(ctx context.Context, vod row, sources []source) (bool, error) {
	if shortDramaMetadata(vod, sources) {
		return true, nil
	}
	seen := map[int64]bool{}
	for id, depth := gconv.Int64(vod["type_id"]), 0; id > 0 && depth < 8 && !seen[id]; depth++ {
		seen[id] = true
		category, err := one(ctx, "SELECT name,pid FROM sx_type WHERE id=?", id)
		if err != nil {
			return false, err
		}
		if category == nil {
			return false, nil
		}
		for _, channel := range localCategoryChannels(gconv.String(category["name"])) {
			if channel == 50 {
				return true, nil
			}
		}
		id = gconv.Int64(category["pid"])
	}
	return false, nil
}
