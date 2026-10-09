package suxinvideo

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

const maxPlaybackSourceNames = 2048

// Names are isolated from standalone MacCMS players. In particular BD is
// Baidu, not the disabled legacy Douban provider sharing the dbm3u8 code.
var yqkPlayers = map[string]string{
	"1": "小柒APP", "8": "小柒·WJ", "27": "小柒·BF", "25": "小柒·SN",
	"3": "小柒·FF", "5": "小柒·SD", "12": "小柒·LZ", "24": "小柒·YZ",
	"38": "小柒·HH", "37": "小柒·JS", "36": "小柒·UK", "35": "小柒·MT",
	"34": "小柒·JY", "33": "小柒·KC", "31": "小柒·NN", "2": "小柒·百度",
	"30": "小柒·迅雷", "19": "小柒·红牛",
}

func yqkPlaybackCodes() []string {
	kinds := make([]string, 0, len(yqkPlayers))
	for kind := range yqkPlayers {
		kinds = append(kinds, kind)
	}
	sort.Slice(kinds, func(i, j int) bool { return gconv.Int(kinds[i]) < gconv.Int(kinds[j]) })
	for i := range kinds {
		kinds[i] = "yqk_" + kinds[i]
	}
	return kinds
}

func yqkAllowedCode(code string) bool {
	if !strings.HasPrefix(code, "yqk_") {
		return false
	}
	_, ok := yqkPlayers[strings.TrimPrefix(code, "yqk_")]
	return ok
}

func yqkAllowedSource(src source) bool {
	if !yqkAllowedCode(src.Code) || len(src.Episodes) == 0 {
		return false
	}
	for _, ep := range src.Episodes {
		_, kind, _, err := yqkMarkerParts(ep.URL)
		if err != nil || "yqk_"+kind != src.Code {
			return false
		}
	}
	return true
}

// Only extend storage, insert missing defaults and rename known old defaults.
// Status, custom names, schedules and all film/user/order data stay intact.
func prepareYQKSource(ctx context.Context) error {
	if err := prepareContentPolicy(ctx); err != nil {
		return err
	}
	column, err := one(ctx, "SELECT CHARACTER_MAXIMUM_LENGTH AS capacity FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='sx_vod' AND COLUMN_NAME='play_from'")
	if err != nil {
		return err
	}
	if column == nil {
		return fmt.Errorf("CMS video table is not installed")
	}
	if gconv.Int64(column["capacity"]) < maxPlaybackSourceNames {
		if err = execSQL(ctx, "ALTER TABLE sx_vod MODIFY COLUMN play_from varchar(2048) DEFAULT ''"); err != nil {
			return err
		}
	}
	if err = execSQL(ctx, "INSERT IGNORE INTO sx_config (`key`,`value`) VALUES('yqk_bootstrap_url',?),('yqk_navigation_enable','0'),('home_recommend_source','local'),('home_hero_source','managed')", yqkDefaultBootstrapURL); err != nil {
		return err
	}
	for _, code := range yqkPlaybackCodes() {
		if err = execSQL(ctx, "INSERT IGNORE INTO sx_player(code,name,`parse`,status) VALUES(?,?, '',1)", code, yqkPlayers[strings.TrimPrefix(code, "yqk_")]); err != nil {
			return err
		}
	}
	if err = execSQL(ctx, "INSERT INTO sx_collect_api(name,api_url,remark,status,collect_auto,collect_hours,addtime) SELECT ?,?,?,1,0,12,? WHERE NOT EXISTS (SELECT 1 FROM sx_collect_api WHERE api_url=?)", "小柒APP聚合资源", yqkSourceURL, "动态接口；各线路独立保存，播放时解析清晰度", time.Now().Unix(), yqkSourceURL); err != nil {
		return err
	}
	return renameYQKDefaultNames(ctx)
}

// Exact names are deliberate: administrators may use their own labels, even
// labels starting with the former brand. Do not alter those or other sources.
func renameYQKDefaultNames(ctx context.Context) error {
	for _, code := range yqkPlaybackCodes() {
		name := yqkPlayers[strings.TrimPrefix(code, "yqk_")]
		legacy := strings.Replace(name, "小柒", "一起看", 1)
		if err := execSQL(ctx, "UPDATE sx_player SET name=? WHERE code=? AND BINARY name=?", name, code, legacy); err != nil {
			return err
		}
	}
	if err := execSQL(ctx, "UPDATE sx_player SET name=? WHERE code=? AND BINARY name=?", "小柒APP", "yqk_1", "一起看 APP"); err != nil {
		return err
	}
	return execSQL(ctx, "UPDATE sx_collect_api SET name=? WHERE api_url=? AND BINARY name IN (?,?,?)", "小柒APP聚合资源", yqkSourceURL, "一起看APP聚合资源", "一起看 APP", "一起看APP")
}

func collectedEpisodeKey(code, raw string) string {
	if strings.HasPrefix(code, "yqk_") {
		parts := strings.SplitN(raw, "$", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) != "" {
			key, _ := playbackEpisodeIdentity(parts[0])
			return key
		}
		return "url:" + raw
	}
	return playEpisodeKey(raw)
}

func yqkFilmTarget(ctx context.Context, vod row, apiID int64, remoteID string) (discoveryTarget, error) {
	kind, err := discoveryCategoryKind(ctx, gconv.Int64(vod["type_id"]))
	target := discoveryTarget{ID: gconv.Int64(vod["id"]), Name: gconv.String(vod["name"]), Year: gconv.String(vod["year"]), Area: gconv.String(vod["area"]), Kind: kind}
	for _, src := range playlist(vod) {
		if !yqkAllowedSource(src) {
			continue
		}
		for _, ep := range src.Episodes {
			id, _, _, e := yqkMarkerParts(ep.URL)
			if e == nil && id == remoteID {
				target.SourceAPIID, target.SourceAPIVID = apiID, remoteID
				return target, err
			}
		}
	}
	return target, err
}

func findYQKCollectionTarget(ctx context.Context, apiID int64, item map[string]any) (row, error) {
	if gconv.Bool(item["__yqk"]) || strings.HasPrefix(gconv.String(item["vod_play_from"]), "yqk_") {
		return findNativeCollectionTarget(ctx, apiID, item)
	}
	// Preserve the existing 4KVM collection policy; YQK enrichment does not
	// implicitly widen another provider's title matching.
	name, remoteID := gconv.String(item["vod_name"]), gconv.String(item["vod_id"])
	candidates, err := all(ctx, "SELECT id,api_id,api_vid,name,class,year,area,type_id,play_from,play_url,pic,remarks FROM sx_vod WHERE name=? OR name_norm=? ORDER BY id LIMIT 100", name, normalizeVodName(name))
	if err != nil {
		return nil, err
	}
	for _, candidate := range candidates {
		target, e := yqkFilmTarget(ctx, candidate, apiID, remoteID)
		if e != nil {
			return nil, e
		}
		if discoveryMatches(target, item, apiID) {
			candidate["__yqk_identity"] = discoveryFilmIdentity(candidate)
			candidate["__yqk_movie"] = target.Kind == "movie" || target.Kind == "anime_movie"
			return candidate, nil
		}
	}
	return nil, nil
}

func validYQKRemoteID(value string) bool {
	n, err := strconv.ParseInt(value, 10, 64)
	return err == nil && n > 0
}
