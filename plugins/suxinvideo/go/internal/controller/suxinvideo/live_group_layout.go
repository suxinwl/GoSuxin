package suxinvideo

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

const (
	liveRegionalGroupKey = "system:regional-hk-mo-tw"
	liveCCTVGroupKey     = "system:cctv"
)

var liveCCTVNumber = regexp.MustCompile(`^cctv([0-9]+)(plus|asia|europe|america)?\.cn$`)

type liveChannelLayout struct {
	Key, Name string
	GroupSort int
	Sort      int
}

func liveSystemChannelLayout(tvgID string) (liveChannelLayout, bool) {
	id := strings.ToLower(liveNormalizeTVGID(tvgID))
	region := liveStationRegions[id]
	// The public EPG catalogue still uses J2.hk for Hong Kong channel 82.
	// Accept TVB's current name too, without adding an unverified stream.
	if id == "tvbplus.hk" {
		region = "HK"
	}
	if region == "HK" || region == "MO" || region == "TW" {
		order := 0
		switch id {
		case "jade.hk":
			order = -10000
		case "phoenixchinesechannel.hk":
			order = -9999
		case "phoenixinfonewschannel.hk":
			order = -9998
		case "phoenixhongkongchannel.hk":
			order = -9997
		}
		return liveChannelLayout{liveRegionalGroupKey, "港澳台", -10000, order}, true
	}
	if liveCCTVChannels[id] {
		order := 1000
		if parts := liveCCTVNumber.FindStringSubmatch(id); parts != nil {
			n, _ := strconv.Atoi(parts[1])
			order = n * 10
			switch parts[2] {
			case "plus":
				order++
			case "asia":
				order++
			case "europe":
				order += 2
			case "america":
				order += 3
			}
		} else if id == "cctv4k.cn" {
			order = 800
		} else if id == "cctv8k.cn" {
			order = 810
		}
		return liveChannelLayout{liveCCTVGroupKey, "CCTV", -9999, order}, true
	}
	return liveChannelLayout{}, false
}

func liveEnsureSystemGroup(ctx context.Context, layout liveChannelLayout) (row, error) {
	key := liveIdentity(layout.Key)
	group, err := one(ctx, "SELECT id FROM sx_live_group WHERE identity_key=?", key)
	if err != nil || group != nil {
		return group, err
	}
	// Reuse a unique existing administrator-created group of the requested name;
	// never reset its manual display name, enabled state or ordering.
	groups, err := all(ctx, "SELECT id,identity_key FROM sx_live_group WHERE name=? ORDER BY id LIMIT 2", layout.Name)
	if err != nil {
		return nil, err
	}
	if len(groups) == 1 {
		// Bind the requested system role to this ID once, before returning it.
		// A later administrator rename/disable then remains discoverable by the
		// stable key; it cannot cause another group to be created on startup.
		if err := execSQL(ctx, "UPDATE sx_live_group SET identity_key=? WHERE id=? AND identity_key=?", key, groups[0]["id"], groups[0]["identity_key"]); err != nil {
			return nil, err
		}
		return one(ctx, "SELECT id FROM sx_live_group WHERE identity_key=?", key)
	}
	now := time.Now().Unix()
	if err := execSQL(ctx, `INSERT INTO sx_live_group(name,identity_key,sort,created,updated) VALUES(?,?,?,?,?)
 ON DUPLICATE KEY UPDATE identity_key=VALUES(identity_key)`, layout.Name, key, layout.GroupSort, now, now); err != nil {
		return nil, err
	}
	return one(ctx, "SELECT id FROM sx_live_group WHERE identity_key=?", key)
}

// UpgradeLiveChannelGroups assigns the requested default presentation once per
// non-manual channel. Repetition preserves IDs and every manually edited row;
// ordinary subscriptions also use the same rule to keep new channels in place.
func UpgradeLiveChannelGroups(ctx context.Context) error {
	groups := map[string]row{}
	for _, layout := range []liveChannelLayout{{Key: liveRegionalGroupKey, Name: "港澳台", GroupSort: -10000}, {Key: liveCCTVGroupKey, Name: "CCTV", GroupSort: -9999}} {
		group, err := liveEnsureSystemGroup(ctx, layout)
		if err != nil {
			return err
		}
		groups[layout.Key] = group
	}
	channels, err := all(ctx, "SELECT id,tvg_id,group_id,sort FROM sx_live_channel WHERE manual_edited=0")
	if err != nil {
		return err
	}
	for _, channel := range channels {
		layout, ok := liveSystemChannelLayout(gconv.String(channel["tvg_id"]))
		if !ok {
			continue
		}
		groupID := gconv.Int64(groups[layout.Key]["id"])
		if groupID == gconv.Int64(channel["group_id"]) && layout.Sort == gconv.Int(channel["sort"]) {
			continue
		}
		if err := execSQL(ctx, "UPDATE sx_live_channel SET group_id=?,sort=?,updated=? WHERE id=? AND manual_edited=0", groupID, layout.Sort, time.Now().Unix(), channel["id"]); err != nil {
			return err
		}
	}
	return nil
}
