package app

import (
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/internal/mediaplaylist"
)

// cleanM3U8Playlist delegates to the same conservative filter used by the CMS.
// Legacy optional keywords are accepted only when they are valid ad hostnames;
// empty strings and loose segment signatures must never select every segment.
func cleanM3U8Playlist(raw string, extraKeywords ...string) string {
	var domains []string
	for _, keyword := range extraKeywords {
		normalized, err := mediaplaylist.NormalizeDomains(keyword)
		if err == nil && normalized != "" {
			domains = append(domains, strings.Fields(normalized)...)
		}
	}
	return mediaplaylist.Filter(raw, "", domains).Playlist
}

func m3u8Duration(raw string) time.Duration {
	var total time.Duration
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#EXTINF:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "#EXTINF:"))
		if comma := strings.IndexByte(value, ','); comma >= 0 {
			value = value[:comma]
		}
		seconds, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || seconds < 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
			continue
		}
		total += time.Duration(seconds * float64(time.Second))
	}
	return total
}

func rewriteM3U8(raw, playlistURL string) string {
	base, err := url.Parse(playlistURL)
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "URI=") && strings.Contains(trimmed, "/api/app/vid/sec") {
			lines[i] = regexp.MustCompile(`URI="[^"]+"`).ReplaceAllString(line, `URI="key.bin"`)
			continue
		}
		if err == nil && trimmed != "" && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
			ref, refErr := url.Parse(trimmed)
			if refErr == nil {
				lines[i] = base.ResolveReference(ref).String()
			}
		}
	}
	return strings.Join(lines, "\n")
}
