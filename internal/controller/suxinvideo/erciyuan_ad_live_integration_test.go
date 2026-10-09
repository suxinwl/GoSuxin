package suxinvideo

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/suxinwl/GoSuxin/internal/erciyuan"
	"github.com/suxinwl/GoSuxin/internal/mediaplaylist"
)

// Opt-in read-only playback verification. Original playlists remain private;
// the audit contains only counts, durations, cache timings and provider IDs.
func TestErciyuanAdLiveReadOnly(t *testing.T) {
	if os.Getenv("SUXIN_ERCIYUAN_AD_INSPECT") != "1" {
		t.Skip("set SUXIN_ERCIYUAN_AD_INSPECT=1 for read-only upstream ad verification")
	}
	t.Setenv("SUXIN_INTEGRATION", "1")
	ctx, cancel := context.WithTimeout(yqkIntegrationContext(t), 2*time.Minute)
	defer cancel()
	client := erciyuan.NewClient(safeCollectorHTTPClient(15 * time.Second))
	dir := filepath.Join("data", "tmp", "ecy-ad-removal")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	var report []map[string]any
	for _, marker := range []string{"erciyuan://64933/dd02/0", "erciyuan://35604/dd02/159", "erciyuan://35604/dd02/158"} {
		id, line, index, _ := erciyuan.ParseMarker(marker)
		media, err := client.Resolve(ctx, marker)
		if err != nil {
			t.Fatal("reviewed native sample failed to resolve")
		}
		address := media.URL
		var raw, base string
		for depth := 0; depth < 3; depth++ {
			if safeMediaURL(ctx, address) != nil {
				t.Fatal("unsafe upstream manifest")
			}
			request, _ := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
			request.Header.Set("User-Agent", mediaUserAgent)
			applyNativeMediaHeaders(request, media.Headers)
			response, e := playbackHTTPClient.Do(request)
			if e != nil {
				t.Fatal("upstream manifest unavailable")
			}
			raw, base, err = readMediaPlaylist(response)
			response.Body.Close()
			if err != nil || response.StatusCode != http.StatusOK {
				t.Fatal("upstream returned no complete manifest")
			}
			if !strings.Contains(raw, "#EXT-X-STREAM-INF:") {
				break
			}
			baseURL, _ := url.Parse(base)
			for _, uri := range strings.Split(raw, "\n") {
				if uri != "" && !strings.HasPrefix(uri, "#") {
					child, e := url.Parse(uri)
					if e != nil {
						t.Fatal("malformed child manifest")
					}
					address = baseURL.ResolveReference(child).String()
					break
				}
			}
		}
		candidates := mediaplaylist.ErciyuanAdCandidates(raw, base)
		started := time.Now()
		filtered := filterErciyuanMediaPlaylist(ctx, raw, base, media.Headers)
		elapsed := time.Since(started)
		started = time.Now()
		cached := filterErciyuanMediaPlaylist(ctx, raw, base, media.Headers)
		cachedElapsed := time.Since(started)
		if filtered.Removed != 3 || cached.Playlist != filtered.Playlist || filtered.RemovedSeconds < 10.3659 || filtered.RemovedSeconds > 10.3661 {
			t.Fatalf("reviewed native%s episode%d failed safe commercial filtering: removed%d seconds%.3f", id, index, filtered.Removed, filtered.RemovedSeconds)
		}
		if err := os.WriteFile(filepath.Join(dir, "live-"+id+"-"+strconv.Itoa(index)+"-original.private.m3u8"), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "live-"+id+"-"+strconv.Itoa(index)+"-filtered.private.m3u8"), []byte(filtered.Playlist), 0600); err != nil {
			t.Fatal(err)
		}
		report = append(report, map[string]any{"provider": "erciyuan", "film": id, "line": line, "episode_index": index,
			"candidate_blocks": len(candidates), "removed_segments": filtered.Removed, "removed_seconds": filtered.RemovedSeconds,
			"initial_filter_ms": elapsed.Milliseconds(), "cached_filter_ms": cachedElapsed.Milliseconds(), "cache_identical": cached.Playlist == filtered.Playlist})
		t.Logf("native%s episode%d removed3 %.3fseconds candidates%d initial%dms cached%dms", id, index, filtered.RemovedSeconds, len(candidates), elapsed.Milliseconds(), cachedElapsed.Milliseconds())
	}
	if err := hongguoAuditWriteJSON(filepath.Join(dir, "live-filter.safe.json"), report, false); err != nil {
		t.Fatal(err)
	}
}
