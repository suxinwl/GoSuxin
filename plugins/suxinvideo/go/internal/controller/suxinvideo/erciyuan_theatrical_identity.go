package suxinvideo

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

var errErciyuanTheatricalAmbiguous = errors.New("二次元同名剧场版存在未核实或冲突的影片身份，已保留原影片资源")

func erciyuanTheatricalSearchKeyword(name string) string {
	name = strings.TrimSpace(name)
	marker := "剧场版"
	index := strings.Index(name, marker)
	if index < 0 {
		marker, index = "劇場版", strings.Index(name, "劇場版")
	}
	if index < 0 || len([]rune(strings.TrimSpace(name[:index]))) < 2 || len([]rune(strings.Trim(name[index+len(marker):], " \t:：-—·"))) < 4 || moviePartialLabel.MatchString(name) || strings.Contains(strings.ToLower(name), "tv") {
		return ""
	}
	return strings.TrimSpace(name[:index]) + marker + " " + strings.TrimSpace(name[index+len(marker):])
}

// The native theatrical catalog occasionally omits year and region. Its exact
// full title must include a distinctive subtitle, independently declare the
// theatrical category and supply a synopsis and complete feature playlist.
// This exception belongs to this provider, never to ordinary name-only matches.
func discoveryErciyuanTheatricalMatch(target discoveryTarget, item map[string]any) bool {
	if !gconv.Bool(item["__erciyuan"]) || discoveryKind(target.Kind) != "anime_movie" || discoveryKind(gconv.String(item["type_name"])) != "anime_movie" {
		return false
	}
	title := discoveryNormalizeTitle(target.Name)
	if title == "" || title != discoveryNormalizeTitle(gconv.String(item["vod_name"])) || moviePartialLabel.MatchString(title) || strings.Contains(title, "tv") {
		return false
	}
	marker := "剧场版"
	index := strings.Index(title, marker)
	if index < 0 {
		marker, index = "劇場版", strings.Index(title, "劇場版")
	}
	if index < 0 || len([]rune(title[:index])) < 2 || len([]rune(strings.Trim(title[index+len(marker):], ":：-—·"))) < 4 {
		return false
	}
	for _, pair := range [][2]string{{strings.TrimSpace(target.Year), strings.TrimSpace(gconv.String(item["vod_year"]))}} {
		if discoveryYearPattern.MatchString(pair[0]) && discoveryYearPattern.MatchString(pair[1]) && pair[0] != pair[1] {
			return false
		}
	}
	localArea, remoteArea := discoveryRegion(target.Area), discoveryRegion(gconv.String(item["vod_area"]))
	if localArea != "" && remoteArea != "" && localArea != remoteArea {
		return false
	}
	if discoveryYearPattern.MatchString(strings.TrimSpace(gconv.String(item["vod_year"]))) && remoteArea != "" {
		return false // Complete metadata belongs to the ordinary strict matcher.
	}
	content := filmDescription(gconv.String(item["vod_content"]))
	if len([]rune(content)) < 24 || discoveryNormalizeTitle(content) == title {
		return false
	}
	features, valid := erciyuanTheatricalFeatureSources(target.Name, item)
	return valid && len(features) > 0
}

// A native feature can have separately labelled quality versions or recordings.
// Never map genuine numbered chapters onto a whole movie. Recording releases
// do not establish a full feature and remain excluded from this narrow merge.
func erciyuanTheatricalFeatureSources(title string, item map[string]any) ([]source, bool) {
	remoteID := strings.TrimSpace(gconv.String(item["vod_id"]))
	if !onlyDigits(remoteID) || gconv.Int64(remoteID) < 1 || gconv.String(gconv.Int64(remoteID)) != remoteID {
		return nil, false
	}
	input := playlist(row{"play_from": item["vod_play_from"], "play_url": item["vod_play_url"]})
	if len(input) == 0 {
		return nil, false
	}
	features := make([]source, 0, len(input))
	for _, src := range input {
		if !erciyuanAllowedSource(src) || src.Parse != "" {
			return nil, false
		}
		recording := false
		for _, ep := range src.Episodes {
			id, _, _, err := erciyuanCollectionMarker(ep.URL)
			if err != nil || id != remoteID {
				return nil, false
			}
			label := discoveryNormalizeTitle(ep.Name)
			_, number := playbackEpisodeIdentity(ep.Name)
			if number > 1 || movieSplitPlaybackLabel.MatchString(label) {
				return nil, false
			}
			recording = recording || strings.Contains(label, "录屏") || strings.Contains(label, "錄屏")
		}
		if recording {
			continue
		}
		if !moviePlaybackAlignmentAllowed(title, true, []source{src}) {
			return nil, false
		}
		feature := src
		feature.Episodes = nil
		for _, ep := range src.Episodes {
			if movieSupplementLabel.MatchString(ep.Name) {
				continue
			}
			if !movieFeatureIdentity(title, ep.Name) {
				return nil, false
			}
			feature.Episodes = append(feature.Episodes, ep)
		}
		if len(feature.Episodes) > 0 {
			features = append(features, feature)
		}
	}
	return features, len(features) > 0
}

func erciyuanTheatricalTargetsCompatible(a, b discoveryTarget) bool {
	yearA, yearB := strings.TrimSpace(a.Year), strings.TrimSpace(b.Year)
	areaA, areaB := discoveryRegion(a.Area), discoveryRegion(b.Area)
	return discoveryYearPattern.MatchString(yearA) && yearA == yearB && areaA != "" && areaA == areaB && discoveryKind(a.Kind) == "anime_movie" && discoveryKind(b.Kind) == "anime_movie"
}

func erciyuanTheatricalStoredIDCompatible(vod row, item map[string]any) bool {
	wanted := strings.TrimSpace(gconv.String(item["vod_id"]))
	for _, src := range playlist(vod) {
		if _, native := erciyuanPlayers[src.Code]; !native {
			continue
		}
		if !erciyuanAllowedSource(src) {
			return false
		}
		for _, ep := range src.Episodes {
			id, _, _, err := erciyuanCollectionMarker(ep.URL)
			if err != nil || id != wanted {
				return false
			}
		}
	}
	return true
}

// The regular collector also validates media before using this metadata gap
// exception. Its normal collection path and independently identified matches
// keep their existing behavior.
func verifyErciyuanTheatricalCollection(ctx context.Context, target discoveryTarget, item map[string]any) (string, string, error) {
	if !discoveryErciyuanTheatricalMatch(target, item) {
		return "", "", errors.New("二次元剧场版尚未取得完整影片身份依据")
	}
	features, _ := erciyuanTheatricalFeatureSources(target.Name, item)
	client := safeCollectorHTTPClient(12 * time.Second)
	defer client.CloseIdleConnections()
	verified := verifyErciyuanDiscoverySources(ctx, features, gconv.String(item["vod_id"]), "", func(ctx context.Context, marker string) error {
		return probeErciyuanDiscoveryWith(ctx, marker, resolveErciyuanPlayback, client, safeMediaURL)
	})
	if len(verified) == 0 {
		return "", "", errors.New("二次元剧场版完整正片线路未通过媒体检测，已保留原影片资源")
	}
	from, play := serializeDiscoverySources(verified)
	return from, play, nil
}
