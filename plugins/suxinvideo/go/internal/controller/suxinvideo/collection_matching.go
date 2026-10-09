package suxinvideo

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

var errCollectionIdentityIncomplete = errors.New("已存在同名同年影片，但地区信息尚未独立核实；本条暂不新增，避免重复影片")

type collectionMatchDeps struct {
	target func(context.Context, row, int64, string) (discoveryTarget, error)
	enrich func(context.Context, discoveryTarget, row) (discoveryTarget, error)
}

// chooseCollectionTarget selects an existing identity without modifying rows.
// A coarse name_norm is only a candidate index; the final full Unicode-aware
// title, year, region and category checks are always mandatory.
func chooseCollectionTarget(ctx context.Context, apiID int64, item map[string]any, candidates []row, deps collectionMatchDeps) (row, error) {
	ordered := append([]row(nil), candidates...)
	sort.SliceStable(ordered, func(i, j int) bool { return gconv.Int64(ordered[i]["id"]) < gconv.Int64(ordered[j]["id"]) })
	remoteID := gconv.String(item["vod_id"])
	var theatricalTargets []discoveryTarget
	if gconv.Bool(item["__erciyuan"]) && discoveryKind(gconv.String(item["type_name"])) == "anime_movie" {
		for _, candidate := range ordered {
			target, err := deps.target(ctx, candidate, apiID, remoteID)
			if err != nil {
				return nil, err
			}
			if !discoveryErciyuanTheatricalMatch(target, item) {
				continue
			}
			if !erciyuanTheatricalStoredIDCompatible(candidate, item) {
				return nil, errErciyuanTheatricalAmbiguous
			}
			// The same stored native remote ID independently identifies an old
			// duplicate even if historical default metadata is wrong. Preserve
			// that row as an alias; its metadata cannot prove or disprove a
			// foreign candidate, and never supplies missing fields to one.
			if target.SourceAPIID == apiID && target.SourceAPIVID == remoteID || gconv.Int64(candidate["api_id"]) == apiID && gconv.String(candidate["api_vid"]) == remoteID {
				continue
			}
			for _, previous := range theatricalTargets {
				if !erciyuanTheatricalTargetsCompatible(previous, target) {
					return nil, errErciyuanTheatricalAmbiguous
				}
			}
			theatricalTargets = append(theatricalTargets, target)
		}
	}
	// A title shared by distinct native Hongguo dramas does not prove which
	// remake a foreign provider means. Defer it instead of selecting the oldest.
	seriesSeen := ""
	for _, candidate := range ordered {
		series, valid := hongguoStoredSeries(candidate)
		if !valid || series == "" {
			continue
		}
		target, err := deps.target(ctx, candidate, apiID, remoteID)
		if err != nil {
			return nil, err
		}
		if !hongguoForeignCollectionMatches(candidate, target, item) {
			continue
		}
		if seriesSeen != "" && seriesSeen != series {
			return nil, errHongguoIdentityAmbiguous
		}
		seriesSeen = series
	}
	enrichCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	unresolved, enrichments := false, 0
	for index, candidate := range ordered {
		if index >= 100 {
			break
		}
		target, err := deps.target(ctx, candidate, apiID, remoteID)
		if err != nil {
			return nil, err
		}
		if gconv.Int64(candidate["api_id"]) == apiID && gconv.String(candidate["api_vid"]) == remoteID {
			target.SourceAPIID, target.SourceAPIVID = apiID, remoteID
		}
		matchedHongguo := hongguoForeignCollectionMatches(candidate, target, item)
		matchedTheatrical := gconv.Bool(item["__erciyuan"]) && discoveryErciyuanTheatricalMatch(target, item)
		if !discoveryCompatible(target, item, apiID, true) && !matchedHongguo {
			continue
		}
		proven := target
		if !matchedHongguo && !matchedTheatrical && !discoveryMatches(proven, item, apiID) && discoveryRegion(proven.Area) == "" &&
			discoveryYearPattern.MatchString(strings.TrimSpace(proven.Year)) && discoveryKind(proven.Kind) != "" &&
			discoveryRegion(gconv.String(item["vod_area"])) != "" {
			if deps.enrich != nil && enrichments < 2 && enrichCtx.Err() == nil {
				enrichments++
				enriched, enrichErr := deps.enrich(enrichCtx, target, candidate)
				if enrichErr != nil {
					return nil, enrichErr
				}
				// Enrichment is allowed to supply only independently anchored area.
				proven.Area = enriched.Area
			}
			if discoveryRegion(proven.Area) == "" {
				unresolved = true
			}
		}
		if !discoveryMatches(proven, item, apiID) && !matchedHongguo && !matchedTheatrical {
			continue
		}
		result := make(row, len(candidate)+3)
		for key, value := range candidate {
			result[key] = value
		}
		result["__yqk_identity"] = discoveryFilmIdentity(candidate)
		result["__yqk_movie"] = target.Kind == "movie" || target.Kind == "anime_movie"
		if matchedHongguo || matchedTheatrical {
			result["__collection_preserve_metadata"] = true
		}
		if matchedTheatrical {
			result["__erciyuan_theatrical"] = true
			result["__erciyuan_theatrical_id"] = remoteID
		}
		if !matchedHongguo && !matchedTheatrical && discoveryRegion(gconv.String(candidate["area"])) == "" {
			area := strings.TrimSpace(proven.Area)
			// A pinned remote ID or stored YQK marker is also independent identity
			// evidence for its own metadata. It cannot establish another film.
			if discoveryRegion(area) == "" && proven.SourceAPIID == apiID && proven.SourceAPIVID == remoteID {
				area = strings.TrimSpace(gconv.String(item["vod_area"]))
			}
			if discoveryRegion(area) != "" {
				result["__collection_area"] = cutRunes(area, 40)
			}
		}
		return result, nil
	}
	if unresolved {
		return nil, errCollectionIdentityIncomplete
	}
	return nil, nil
}

func findNativeCollectionTarget(ctx context.Context, apiID int64, item map[string]any) (row, error) {
	name := gconv.String(item["vod_name"])
	// Include the width-normalized key too; whitespace and punctuation in this
	// lookup never relax the strict full-title check in chooseCollectionTarget.
	candidates, err := all(ctx, "SELECT id,api_id,api_vid,name,class,year,area,type_id,play_from,play_url,pic,remarks FROM sx_vod WHERE name=? OR name_norm=? OR name_norm=? ORDER BY id LIMIT 100",
		name, normalizeVodName(name), normalizeVodName(discoveryNormalizeTitle(name)))
	if err != nil {
		return nil, err
	}
	var collectors []row
	loaded := false
	target := yqkFilmTarget
	if gconv.Bool(item["__erciyuan"]) || strings.Contains(gconv.String(item["vod_play_from"]), "ecy_") {
		target = erciyuanFilmTarget
	}
	deps := collectionMatchDeps{
		target: target,
		enrich: func(ctx context.Context, target discoveryTarget, vod row) (discoveryTarget, error) {
			if !loaded {
				var err error
				collectors, err = all(ctx, "SELECT id,name,api_url,status FROM sx_collect_api ORDER BY id")
				if err != nil {
					return target, err
				}
				loaded = true
			}
			// Do not let the incoming provider supply missing metadata for a
			// foreign candidate. Use its original remote ID, then an existing
			// independent HTTP playback URL as in background source discovery.
			original := target
			original.SourceAPIID, original.SourceAPIVID = gconv.Int64(vod["api_id"]), gconv.String(vod["api_vid"])
			for _, collector := range collectors {
				if gconv.Int64(collector["id"]) == original.SourceAPIID {
					original = enrichDiscoveryTarget(ctx, original, collector)
					break
				}
			}
			if discoveryRegion(original.Area) == "" {
				independent := make([]row, 0, len(collectors))
				for _, collector := range collectors {
					if gconv.Int64(collector["id"]) != apiID {
						independent = append(independent, collector)
					}
				}
				original = enrichDiscoveryFromStoredLines(ctx, original, playlist(vod), independent, fetchCollectSource)
			}
			target.Area = original.Area
			return target, nil
		},
	}
	primary, err := chooseCollectionTarget(ctx, apiID, item, candidates, deps)
	if err != nil || primary == nil {
		return primary, err
	}
	aliases, err := collectionBoundAliases(ctx, primary, apiID, item, candidates, deps)
	if err != nil {
		return nil, err
	}
	if len(aliases) > 0 {
		primary["__collection_aliases"] = aliases
	}
	return primary, nil
}

// Old duplicate URLs can remain bookmarked. Refresh only duplicates already
// pinned to this exact YQK remote film, keeping both local records and IDs.
func collectionBoundAliases(ctx context.Context, primary row, apiID int64, item map[string]any, candidates []row, deps collectionMatchDeps) ([]row, error) {
	remoteID := gconv.String(item["vod_id"])
	var aliases []row
	for _, candidate := range candidates {
		if gconv.Int64(candidate["id"]) == gconv.Int64(primary["id"]) {
			continue
		}
		target, err := deps.target(ctx, candidate, apiID, remoteID)
		if err != nil {
			return nil, err
		}
		bound := target.SourceAPIID == apiID && target.SourceAPIVID == remoteID
		bound = bound || (gconv.Int64(candidate["api_id"]) == apiID && gconv.String(candidate["api_vid"]) == remoteID)
		if !bound {
			continue
		}
		alias, err := chooseCollectionTarget(ctx, apiID, item, []row{candidate}, collectionMatchDeps{target: deps.target})
		if err != nil {
			return nil, err
		}
		if alias != nil {
			alias["__collection_preserve_metadata"] = true
			aliases = append(aliases, alias)
		}
	}
	return aliases, nil
}

func validateYQKCollectionIdentity(ctx context.Context, existing row, apiID int64, item map[string]any) error {
	target, err := yqkFilmTarget(ctx, existing, apiID, gconv.String(item["vod_id"]))
	if err != nil {
		return err
	}
	if gconv.Int64(existing["api_id"]) == apiID && gconv.String(existing["api_vid"]) == gconv.String(item["vod_id"]) {
		target.SourceAPIID, target.SourceAPIVID = apiID, gconv.String(item["vod_id"])
	}
	if area := gconv.String(existing["__collection_area"]); discoveryRegion(target.Area) == "" && discoveryRegion(area) != "" {
		target.Area = area
	}
	if !discoveryMatches(target, item, apiID) && !hongguoForeignCollectionMatches(existing, target, item) {
		return errors.New("小柒影片身份与已有记录冲突，已保留原影片资源")
	}
	return nil
}
