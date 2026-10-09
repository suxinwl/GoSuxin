package suxinvideo

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/suxinwl/GoSuxin/internal/mediaplaylist"
)

func normalizeMediaAdDomains(raw string) (string, error) {
	return mediaplaylist.NormalizeDomains(raw)
}

// Ordinary sources and signed APP sources use one policy before URL signing.
// Existing installations use the enabled default until an administrator saves
// an explicit preference. Reading the stored settings lets AdminSaveConfig
// take effect on the next playlist request without restarting the service.
func filterMediaPlaylist(ctx context.Context, raw, baseURL string) mediaplaylist.Result {
	if setting(ctx, "player_ad_filter", "1") != "1" {
		return mediaplaylist.Result{Playlist: raw}
	}
	normalized, err := normalizeMediaAdDomains(setting(ctx, "player_ad_domains", ""))
	var domains []string
	if err == nil && normalized != "" {
		domains = strings.Split(normalized, "\n")
	}
	return mediaplaylist.Filter(raw, baseURL, domains)
}

func mediaAdFilterHeaders(header http.Header, result mediaplaylist.Result) {
	header.Set("Cache-Control", "private, no-store")
	header.Set("X-Suxin-Ad-Segments", strconv.Itoa(result.Removed))
	header.Set("X-Suxin-Ad-Duration", fmt.Sprintf("%.3f", result.RemovedSeconds))
}
