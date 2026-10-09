package suxinvideo

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

// enrichDiscoveryFromStoredLines fills a missing region using an independent
// provider already tied to the stored film by an EXACT existing playback URL.
// A foreign title search, or a second copy of a YQK film, cannot prove itself.
func enrichDiscoveryFromStoredLines(parent context.Context, target discoveryTarget, existing []source, collectors []row, fetch func(context.Context, string, url.Values) (macPayload, error)) discoveryTarget {
	if fetch == nil || discoveryRegion(target.Area) != "" ||
		!discoveryYearPattern.MatchString(strings.TrimSpace(target.Year)) ||
		discoveryKind(target.Kind) == "" || discoveryNormalizeTitle(target.Name) == "" {
		return target
	}
	stored := discoveryStoredHTTPAnchors(existing)
	if len(stored) == 0 {
		return target
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	seenCollectors := map[int64]bool{}
	for _, collector := range collectors {
		if ctx.Err() != nil {
			break
		}
		id := gconv.Int64(collector["id"])
		raw := strings.TrimSpace(gconv.String(collector["api_url"]))
		if id <= 0 || id == target.SourceAPIID || seenCollectors[id] || gconv.Int(collector["status"]) != 1 {
			continue
		}
		seenCollectors[id] = true
		u, err := url.Parse(raw)
		if err != nil || u.User != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
			continue
		}
		owned := discoveryOwnedAnchors(stored, collector, collectors)
		if len(owned) == 0 {
			continue
		}
		lookupCtx, lookupCancel := context.WithTimeout(ctx, 8*time.Second)
		area := discoveryLookupAnchoredArea(lookupCtx, target, collector, collectors, owned, fetch)
		lookupCancel()
		if area != "" && ctx.Err() == nil {
			target.Area = area
			return target
		}
	}
	return target
}

func discoveryStoredHTTPAnchors(existing []source) []source {
	var sources []source
	for _, src := range existing {
		if strings.TrimSpace(src.Parse) != "" || !identifier.MatchString(src.Code) || strings.HasPrefix(src.Code, "yqk_") {
			continue
		}
		copy := source{Code: src.Code}
		for _, ep := range src.Episodes {
			// Do not strip or normalize query strings: URL equality is the
			// independent identity evidence, not just a host/path resemblance.
			if ep.URL != strings.TrimSpace(ep.URL) {
				continue
			}
			u, err := url.Parse(ep.URL)
			if err == nil && u.User == nil && u.Fragment == "" && (u.Scheme == "http" || u.Scheme == "https") &&
				safePlayerAddress(ep.URL) && (strings.HasSuffix(strings.ToLower(u.Path), ".m3u8") || strings.HasSuffix(strings.ToLower(u.Path), ".mp4")) {
				copy.Episodes = append(copy.Episodes, ep)
			}
		}
		if len(copy.Episodes) > 0 {
			sources = append(sources, copy)
		}
	}
	return sources
}

func discoveryOwnedAnchors(stored []source, collector row, collectors []row) map[string]map[string]bool {
	result := map[string]map[string]bool{}
	codes := collectorPlaybackCodes(gconv.String(collector["api_url"]))
	for _, src := range stored {
		known := src.Code == "dbm3u8" // Shared code uses URL ownership below.
		for _, code := range codes {
			known = known || src.Code == code
		}
		if !known || !discoverySourceAllowed(src, collector, collectors) {
			continue
		}
		if result[src.Code] == nil {
			result[src.Code] = map[string]bool{}
		}
		for _, ep := range src.Episodes {
			result[src.Code][ep.URL] = true
		}
	}
	return result
}

func discoveryLookupAnchoredArea(ctx context.Context, target discoveryTarget, collector row, collectors []row, owned map[string]map[string]bool, fetch func(context.Context, string, url.Values) (macPayload, error)) string {
	raw, collectorID := gconv.String(collector["api_url"]), gconv.Int64(collector["id"])
	seenIDs := map[string]bool{}
	details := 0
	for _, keyword := range discoveryAnchorKeywords(target.Name) {
		if ctx.Err() != nil || details >= 2 {
			break
		}
		search, err := fetch(ctx, raw, url.Values{"ac": {"videolist"}, "wd": {keyword}, "pg": {"1"}})
		if err != nil || ctx.Err() != nil {
			continue
		}
		for i, candidate := range search.List {
			if i >= 40 || details >= 2 || ctx.Err() != nil {
				break
			}
			if !discoveryCompatible(target, candidate, collectorID, true) {
				continue
			}
			id := strings.TrimSpace(gconv.String(candidate["vod_id"]))
			if id == "" || len(id) > 64 || seenIDs[id] {
				continue
			}
			seenIDs[id] = true
			details++
			response, err := fetch(ctx, raw, url.Values{"ac": {"detail"}, "ids": {id}})
			if err != nil || ctx.Err() != nil {
				continue
			}
			for index, item := range response.List {
				if index >= 40 {
					break
				}
				area := strings.TrimSpace(gconv.String(item["vod_area"]))
				if gconv.String(item["vod_id"]) != id || discoveryRegion(area) == "" ||
					!discoveryYearPattern.MatchString(strings.TrimSpace(gconv.String(item["vod_year"]))) ||
					discoveryKind(gconv.String(item["type_name"])) == "" ||
					!discoveryCompatible(target, item, collectorID, true) {
					continue
				}
				sources := playlist(row{"play_from": item["vod_play_from"], "play_url": item["vod_play_url"]})
				kind := discoveryKind(target.Kind)
				if (kind == "movie" || kind == "anime_movie") && !moviePlaybackAlignmentAllowed(target.Name, true, sources) {
					continue
				}
				for _, src := range sources {
					anchors := owned[src.Code]
					if len(anchors) == 0 || !discoverySourceAllowed(src, collector, collectors) {
						continue
					}
					for _, ep := range src.Episodes {
						if anchors[ep.URL] {
							return area
						}
					}
				}
			}
		}
	}
	return ""
}

// These are search spelling variants only. All returned identities must still
// match the FULL normalized title; no shortened title is accepted as evidence.
func discoveryAnchorKeywords(title string) []string {
	title = strings.TrimSpace(title)
	result := []string{title}
	seen := map[string]bool{title: true}
	for _, token := range []string{"剧场版", "劇場版"} {
		at := strings.Index(title, token)
		if at < 0 {
			continue
		}
		before, after := strings.TrimSpace(title[:at]), strings.TrimSpace(title[at+len(token):])
		for _, candidate := range []string{before + token + " " + after, before + " " + token + after, before + " " + token + " " + after} {
			candidate = strings.TrimSpace(candidate)
			if candidate != "" && !seen[candidate] {
				result = append(result, candidate)
				seen[candidate] = true
			}
		}
		break
	}
	return result
}
