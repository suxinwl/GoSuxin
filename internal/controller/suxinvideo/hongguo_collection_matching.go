package suxinvideo

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

var errHongguoIdentityAmbiguous = errors.New("红果同名短剧存在相互冲突或未核实的影片身份，已保留各记录及原资源，等待核实")
var errHongguoIdentityIncomplete = errors.New("红果同名短剧标题过短且缺少独立年份或地区，暂不新增重复影片，等待核实")

func hongguoCollectionSourceEnabled(collector row) bool {
	return collector != nil && strings.TrimSpace(gconv.String(collector["api_url"])) == hongguoSourceURL && gconv.Int(collector["status"]) == 1
}

func validateHongguoCollectedPlaylist(collector row, remoteID, from, play string) (string, string, error) {
	if !hongguoCollectionSourceEnabled(collector) {
		return "", "", errors.New("红果采集源不存在或已停用")
	}
	if !onlyDigits(remoteID) || strings.Trim(remoteID, "0") == "" {
		return "", "", errors.New("红果影片编号无效")
	}
	sources := playlist(row{"play_from": from, "play_url": play})
	if strings.TrimSpace(from) != "hongguo" || len(sources) != 1 || sources[0].Code != "hongguo" || len(sources[0].Episodes) == 0 {
		return "", "", errors.New("红果影片详情未返回单一有效原生线路")
	}
	if len(strings.Split(play, "#")) != len(sources[0].Episodes) {
		return "", "", errors.New("红果影片包含无效分集")
	}
	for _, ep := range sources[0].Episodes {
		series, _, valid := hongguoDiscoveryMarker(ep.URL)
		if !valid || series != remoteID || strings.TrimSpace(ep.Name) == "" || strings.ContainsAny(ep.Name, "$\r\n") {
			return "", "", errors.New("红果分集与影片编号不符")
		}
	}
	sources[0] = discoveryNormalizeSource(sources[0])
	from, play = serializeDiscoverySources(sources)
	return from, play, nil
}

// A stored native line is identity evidence only when every episode belongs to
// the same remote drama. A different native drama must never replace that line.
func hongguoStoredSeries(vod row) (string, bool) {
	series := ""
	for _, src := range playlist(vod) {
		if src.Code != "hongguo" {
			continue
		}
		if len(src.Episodes) == 0 {
			return "", false
		}
		for _, ep := range src.Episodes {
			id, _, valid := hongguoDiscoveryMarker(ep.URL)
			if !valid || (series != "" && id != series) {
				return "", false
			}
			series = id
		}
	}
	return series, true
}

func hongguoIncomingSeries(from, play string) string {
	series, valid := hongguoStoredSeries(row{"play_from": from, "play_url": play})
	if !valid {
		return ""
	}
	return series
}

func hongguoFilmTarget(ctx context.Context, vod row, apiID int64, remoteID string) (discoveryTarget, error) {
	kind, err := discoveryCategoryKind(ctx, gconv.Int64(vod["type_id"]))
	return hongguoCollectionTargetWithKind(vod, kind, apiID, remoteID), err
}

func hongguoCollectionTargetWithKind(vod row, kind string, apiID int64, remoteID string) discoveryTarget {
	target := discoveryTarget{ID: gconv.Int64(vod["id"]), Name: gconv.String(vod["name"]), Year: gconv.String(vod["year"]), Area: gconv.String(vod["area"]), Kind: kind}
	if target.Kind == "" && discoveryKind(gconv.String(vod["class"])) == "short" {
		target.Kind = "short"
	}
	series, valid := hongguoStoredSeries(vod)
	bound := valid && series == remoteID
	owned := gconv.Int64(vod["api_id"]) == apiID && gconv.String(vod["api_vid"]) == remoteID
	if bound || owned {
		target.SourceAPIID, target.SourceAPIVID = apiID, remoteID
		if owned || bound && target.Kind == "" {
			// Older list imports used genre tags as their category. Its own native
			// ID proves it is a short drama; preserve the existing type_id here.
			target.Kind = "short"
		}
	}
	return target
}

func hongguoCollectionMatches(vod row, target discoveryTarget, apiID int64, item map[string]any) bool {
	remoteID := gconv.String(item["vod_id"])
	series, valid := hongguoStoredSeries(vod)
	owned := gconv.Int64(vod["api_id"]) == apiID && gconv.String(vod["api_vid"]) == remoteID
	if (!owned && (!valid || series != "" && series != remoteID)) ||
		(gconv.Int64(vod["api_id"]) == apiID && gconv.String(vod["api_vid"]) != remoteID) {
		return false
	}
	if discoveryKind(target.Kind) != "short" || discoveryKind(gconv.String(item["type_name"])) != "short" {
		return false
	}
	// Complete independently stored metadata or the exact native remote ID can
	// establish a short title. Missing metadata requires the long exact title
	// exception, which still rejects explicit remake/country contradictions.
	return discoveryMatches(target, item, apiID) || discoveryHongguoMatches(target, item)
}

// Symmetric collection: a complete native Hongguo line independently proves
// the existing short-drama catalog. A foreign short-drama import may add its
// own lines under the same distinctive exact title, without filling fabricated
// native metadata or treating unknown/movie/series categories as short dramas.
func hongguoForeignCollectionMatches(vod row, target discoveryTarget, item map[string]any) bool {
	series, valid := hongguoStoredSeries(vod)
	return valid && series != "" && discoveryKind(target.Kind) == "short" && discoveryHongguoMatches(target, item)
}

// Older ordinary-source imports could put multiple native dramas in one line.
// Repair only its original, independently pinned API film, after exact-title
// validation. Keep all foreign lines and their ordering; malformed native
// episode indices cannot safely be preserved and must come from fresh detail.
func replaceHongguoCollectedLine(vod row, incoming source) (string, string) {
	sources, replaced := playlist(vod), false
	result := make([]source, 0, len(sources)+1)
	for _, src := range sources {
		if src.Code == "hongguo" {
			if !replaced {
				result, replaced = append(result, incoming), true
			}
			continue
		}
		result = append(result, src)
	}
	if !replaced {
		result = append(result, incoming)
	}
	return serializeDiscoverySources(result)
}

// Known aliases of this exact native drama can have incomplete metadata. Two
// unbound same-title rows need complete matching metadata to be synced; otherwise
// they may be distinct remakes and the audit must leave them separate.
func hongguoCollectionAliasesCompatible(a, b discoveryTarget, apiID int64, remoteID string) bool {
	knownA, knownB := a.SourceAPIID == apiID && a.SourceAPIVID == remoteID, b.SourceAPIID == apiID && b.SourceAPIVID == remoteID
	year := func(raw string) string {
		raw = strings.TrimSpace(raw)
		if !discoveryYearPattern.MatchString(raw) {
			return ""
		}
		return raw
	}
	for _, pair := range [][2]string{{year(a.Year), year(b.Year)}, {discoveryRegion(a.Area), discoveryRegion(b.Area)}} {
		if pair[0] != "" && pair[1] != "" && pair[0] != pair[1] {
			return false
		}
	}
	if knownA && knownB {
		return true
	}
	return year(a.Year) != "" && year(a.Year) == year(b.Year) && discoveryRegion(a.Area) != "" && discoveryRegion(a.Area) == discoveryRegion(b.Area)
}

func chooseHongguoCollectionTarget(ctx context.Context, apiID int64, item map[string]any, candidates []row, deps collectionMatchDeps) (row, error) {
	ordered := append([]row(nil), candidates...)
	sort.SliceStable(ordered, func(i, j int) bool { return gconv.Int64(ordered[i]["id"]) < gconv.Int64(ordered[j]["id"]) })
	remoteID := gconv.String(item["vod_id"])
	var selected []row
	var targets []discoveryTarget
	incomplete := false
	for _, candidate := range ordered {
		target, err := deps.target(ctx, candidate, apiID, remoteID)
		if err != nil {
			return nil, err
		}
		if !hongguoCollectionMatches(candidate, target, apiID, item) {
			if discoveryKind(target.Kind) == "short" && discoveryCompatible(target, item, apiID, true) && len([]rune(discoveryNormalizeTitle(target.Name))) < 8 {
				incomplete = true
			}
			continue
		}
		for _, previous := range targets {
			if !hongguoCollectionAliasesCompatible(previous, target, apiID, remoteID) {
				return nil, errHongguoIdentityAmbiguous
			}
		}
		result := make(row, len(candidate)+3)
		for key, value := range candidate {
			result[key] = value
		}
		result["__yqk_identity"], result["__collection_source"] = discoveryFilmIdentity(candidate), "hongguo"
		selected, targets = append(selected, result), append(targets, target)
	}
	if len(selected) == 0 {
		if incomplete {
			return nil, errHongguoIdentityIncomplete
		}
		return nil, nil
	}
	if len(selected) > 1 {
		for _, alias := range selected[1:] {
			alias["__collection_preserve_metadata"] = true
		}
		selected[0]["__collection_aliases"] = selected[1:]
	}
	return selected[0], nil
}

func findHongguoCollectionTarget(ctx context.Context, apiID int64, item map[string]any) (row, error) {
	name := gconv.String(item["vod_name"])
	candidates, err := all(ctx, "SELECT id,api_id,api_vid,name,class,year,area,type_id,play_from,play_url,pic,remarks FROM sx_vod WHERE name=? OR name_norm=? OR name_norm=? ORDER BY id LIMIT 100",
		name, normalizeVodName(name), normalizeVodName(discoveryNormalizeTitle(name)))
	if err != nil {
		return nil, err
	}
	if len(candidates) >= 100 {
		return nil, errHongguoIdentityAmbiguous
	}
	return chooseHongguoCollectionTarget(ctx, apiID, item, candidates, collectionMatchDeps{target: hongguoFilmTarget})
}

func validateHongguoCollectionIdentity(ctx context.Context, existing row, apiID int64, item map[string]any) error {
	target, err := hongguoFilmTarget(ctx, existing, apiID, gconv.String(item["vod_id"]))
	if err != nil {
		return err
	}
	if !hongguoCollectionMatches(existing, target, apiID, item) {
		return errors.New("红果影片身份与已有记录冲突，已保留原影片资源")
	}
	return nil
}
