package suxinvideo

import (
	"context"
	"net/url"
	"strings"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

// publicVodCondition omits titles whose only known collection source is disabled.
// A merged title stays visible because it can still have an enabled play line.
func publicVodCondition(ctx context.Context, alias string) string {
	contentCondition := loadContentPolicy(ctx).sqlCondition(alias)
	if alias != "" {
		alias += "."
	}
	return alias + "status=1 AND (" + alias + "api_id NOT IN (SELECT id FROM sx_collect_api WHERE status=0) OR " +
		alias + "play_from LIKE '%$$$%' OR " + alias + "play_url LIKE '%bdzy%') AND " + erciyuanListingCondition(alias) + contentCondition
}

// A native aggregate can have four lines owned by the same collector. The
// legacy $$$ heuristic alone would mistake those for independent providers
// and keep an unplayable card after the aggregate has been disabled. Mixed
// records remain eligible so their independent lines are still available.
func erciyuanListingCondition(alias string) string {
	const nativeOnly = "^ecy_(aa02|aa03|dd02|4k01)([$][$][$]ecy_(aa02|aa03|dd02|4k01))*$"
	return "(" + alias + "play_from NOT REGEXP '" + nativeOnly + "' OR EXISTS (SELECT 1 FROM sx_collect_api WHERE TRIM(api_url)='erciyuan://app' AND status=1))"
}

func playbackHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func hostIs(host, domain string) bool {
	return host == domain || strings.HasSuffix(host, "."+domain)
}

func legacyDoubanMediaHost(host string) bool {
	// These exact legacy CDN hosts are present in unmixed Douban records
	// (e.g. IDs 100/118 and 144/1040) as well as merged films. Do not infer
	// ownership of arbitrary IPs or unrelated hosts from the dbm3u8 code.
	return hostIs(host, "ajupf.com") || host == "185.92.188.176" || host == "vodcnd17.uvjtih.cn"
}

// Collection ownership must follow the playback line after films are merged.
// The film's api_id only identifies its original collector. Generic/shared
// codes are deliberately excluded because they do not identify one provider.
func collectorPlaybackCodes(raw string) []string {
	raw = strings.TrimSpace(raw)
	if isYQKSource(raw) {
		return yqkPlaybackCodes()
	}
	if isErciyuanSource(raw) {
		return erciyuanPlaybackCodes()
	}
	// 4kvm is an internal provider marker, not an RFC URL scheme (it starts
	// with a digit), so recognize native markers before URL parsing.
	for _, code := range []string{"hongguo", "4kvm"} {
		if strings.HasPrefix(raw, code+"://") {
			return []string{code}
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	host := strings.ToLower(u.Hostname())
	known := []struct {
		domain string
		codes  []string
	}{
		{"jszyapi.com", []string{"jsyun", "jsm3u8"}},
		{"maoyanapi.top", []string{"mym3u8"}},
		{"ffzyapi.com", []string{"ffm3u8"}},
		{"lziapi.com", []string{"liangzi", "lzm3u8"}},
		{"wujinapi.me", []string{"wjm3u8"}},
		{"bfzyapi.com", []string{"bfzym3u8"}},
		{"hongniuzy2.com", []string{"hnyun", "hnm3u8"}},
		{"sdzyapi.com", []string{"sdm3u8"}},
		{"xinlangapi.com", []string{"xlyun", "xlm3u8"}},
	}
	for _, entry := range known {
		if hostIs(host, entry.domain) {
			return entry.codes
		}
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "from" {
			code := strings.ToLower(parts[i+1])
			if code != "" && code != "dbm3u8" && code != "m3u8" && code != "mp4" && code != "no" && !strings.HasPrefix(code, "ecy_") {
				return []string{code}
			}
		}
	}
	return nil
}

func availableSources(vod row, sources []source, players map[string]row, collectors []row) []source {
	active := map[int64]bool{}
	codeOwners := map[string][]int64{}
	var doubanID, baiduID int64
	for _, collector := range collectors {
		id := gconv.Int64(collector["id"])
		active[id] = gconv.Int(collector["status"]) == 1
		for _, code := range collectorPlaybackCodes(gconv.String(collector["api_url"])) {
			codeOwners[code] = append(codeOwners[code], id)
		}
		name := gconv.String(collector["name"])
		host := playbackHost(gconv.String(collector["api_url"]))
		if strings.Contains(name, "豆瓣") || hostIs(host, "dbzy5.com") {
			doubanID = id
		} else if strings.Contains(name, "百度") || strings.Contains(host, "bdzy") {
			baiduID = id
		}
	}
	primaryID := gconv.Int64(vod["api_id"])
	result := make([]source, 0, len(sources))
	for _, src := range sources {
		if strings.HasPrefix(src.Code, "yqk_") && (!yqkAllowedSource(src) || len(codeOwners[src.Code]) == 0) {
			continue
		}
		if strings.HasPrefix(src.Code, "ecy_") {
			if !erciyuanAllowedSource(src) || len(codeOwners[src.Code]) == 0 {
				continue
			}
			if src.Name == "" || src.Name == src.Code {
				src.Name = erciyuanPlayers[src.Code]
			}
		}
		if player, exists := players[src.Code]; exists {
			if gconv.Int(player["status"]) != 1 {
				continue
			}
			if name := gconv.String(player["name"]); name != "" {
				src.Name = name
			}
			src.Parse = gconv.String(player["parse"])
		}
		kept := make([]episode, 0, len(src.Episodes))
		// dbm3u8 is published by both Baidu and Douban. The player table's
		// display name cannot identify the provider of a collected episode.
		providers := map[int64]bool{}
		for _, ep := range src.Episodes {
			if strings.HasPrefix(ep.URL, "erciyuan://") && !strings.HasPrefix(src.Code, "ecy_") {
				continue
			}
			ownerID := int64(0)
			if src.Code == "dbm3u8" {
				host := playbackHost(ep.URL)
				switch {
				case legacyDoubanMediaHost(host):
					ownerID = doubanID
				case strings.Contains(host, "bdzy"):
					ownerID = baiduID
				default:
					if primaryID == doubanID || primaryID == baiduID {
						ownerID = primaryID
					}
				}
			} else if owners := codeOwners[src.Code]; len(owners) == 1 {
				ownerID = owners[0]
			} else if len(owners) > 1 {
				// Duplicate configured endpoints do not establish ownership of
				// merged lines. Use the original collector only when it matches.
				for _, id := range owners {
					if id == primaryID {
						ownerID = id
						break
					}
				}
				if (strings.HasPrefix(src.Code, "yqk_") || strings.HasPrefix(src.Code, "ecy_")) && ownerID == 0 {
					// Duplicate aggregate settings must never make disabled YQK
					// lines appear enabled through an unrelated original API ID.
					ownerID = owners[0]
					for _, id := range owners {
						if active[id] {
							ownerID = id
							break
						}
					}
				}
			} else if len(sources) == 1 {
				ownerID = primaryID
			}
			if enabled, known := active[ownerID]; known && !enabled {
				continue
			}
			kept = append(kept, ep)
			providers[ownerID] = true
		}
		if len(kept) > 0 {
			if src.Code == "dbm3u8" {
				names := make([]string, 0, 3)
				if doubanID != 0 && providers[doubanID] {
					names = append(names, "豆瓣资源")
				}
				if baiduID != 0 && providers[baiduID] {
					names = append(names, "百度资源")
				}
				if len(names) > 0 {
					for ownerID := range providers {
						if ownerID == 0 || (ownerID != doubanID && ownerID != baiduID) {
							names = append(names, "其它资源")
							break
						}
					}
					src.Name = strings.Join(names, " / ")
				} else {
					// A shared player label cannot identify an unknown CDN.
					src.Name = "其它资源"
				}
			}
			src.Episodes = kept
			result = append(result, src)
		}
	}
	return result
}

// Many Apple CMS sources publish both a /play/<id> HTML page and the exact
// /play/<id>/index.m3u8 stream. Showing both creates a duplicate, and treating
// the HTML page as MP4 makes the player wait until its startup timeout.
func dedupePageSources(sources []source) []source {
	byCode := make(map[string]source, len(sources))
	for _, src := range sources {
		byCode[src.Code] = src
	}
	result := make([]source, 0, len(sources))
	for _, src := range sources {
		// Some built-in players explicitly append /index.m3u8 to their page
		// URL. That rule produces the same stream as the paired m3u8 source.
		parseRule := strings.TrimSpace(src.Parse)
		if strings.HasSuffix(src.Code, "yun") && (parseRule == "" || parseRule == "m3u8:{url}/index.m3u8") {
			stream, exists := byCode[strings.TrimSuffix(src.Code, "yun")+"m3u8"]
			if exists && sameSourceMedia(src, stream) {
				continue
			}
		}
		result = append(result, src)
	}
	return result
}

func sameSourceMedia(page, stream source) bool {
	if len(page.Episodes) == 0 || len(page.Episodes) != len(stream.Episodes) {
		return false
	}
	for i, ep := range page.Episodes {
		if ep.Name != stream.Episodes[i].Name {
			return false
		}
		pageURL, pageErr := url.Parse(ep.URL)
		streamURL, streamErr := url.Parse(stream.Episodes[i].URL)
		if pageErr != nil || streamErr != nil || pageURL.Scheme != streamURL.Scheme || pageURL.Host != streamURL.Host ||
			strings.TrimRight(pageURL.Path, "/")+"/index.m3u8" != streamURL.Path || pageURL.RawQuery != streamURL.RawQuery {
			return false
		}
	}
	return true
}
