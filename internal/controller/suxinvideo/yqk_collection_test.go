package suxinvideo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	xq "github.com/suxinwl/GoSuxin/internal/xiaoqiapp"
)

func yqkFixtureSource(kind string) source {
	return source{Code: "yqk_" + kind, Episodes: []episode{
		{Name: "第01集", URL: "yqk://12/" + kind + "/1001"},
		{Name: "第160集", URL: "yqk://12/" + kind + "/1160"},
	}}
}

func TestYQKOwnershipAndStrictEpisodes(t *testing.T) {
	collectors := []row{{"id": 1, "api_url": "https://caiji.dbzy5.com/api.php/provide/vod", "name": "豆瓣", "status": 0}, {"id": 2, "api_url": yqkSourceURL, "status": 1}}
	old := source{Code: "dbm3u8", Episodes: []episode{{Name: "第1集", URL: "https://vodcnd09.ajupf.com/old.m3u8"}}}
	baidu, other := yqkFixtureSource("2"), yqkFixtureSource("8")
	sources := []source{old, baidu, other}
	got := availableSources(row{"api_id": 1}, sources, map[string]row{"yqk_8": {"status": 0}}, collectors)
	if len(got) != 1 || got[0].Code != "yqk_2" {
		t.Fatalf("aggregate Baidu must survive disabled standalone Douban and player: %#v", got)
	}
	collectors[1]["status"] = 0
	if got = availableSources(row{"api_id": 1}, sources, nil, collectors); len(got) != 0 {
		t.Fatal("disabled YQK lines survived through unrelated original collector")
	}
	spoof := yqkFixtureSource("2")
	spoof.Code = "yqk_8"
	if yqkAllowedSource(spoof) || yqkAllowedCode("yqk_999") || yqkAllowedCode("dbm3u8") {
		t.Fatal("unknown or mismatched source identity accepted")
	}
	if discoverySourceAllowed(baidu, row{"api_url": "https://foreign.example"}, nil) || !discoverySourceAllowed(baidu, row{"api_url": yqkSourceURL}, nil) {
		t.Fatal("aggregate namespace ownership was ignored")
	}
	from, raw := mergePlay("yqk_2", "第001集$yqk://12/2/11#第1期$yqk://12/2/12", "yqk_2", "EP01$yqk://12/2/13#第1话$yqk://12/2/14")
	merged := playlist(row{"play_from": from, "play_url": raw})
	if len(merged) != 1 || len(merged[0].Episodes) != 3 || merged[0].Episodes[0].URL != "yqk://12/2/13" {
		t.Fatalf("numeric identity renewal or qualified-label preservation failed: %#v", merged)
	}
}

func TestYQKDiscoveryKeepsAllVerifiedMarkers(t *testing.T) {
	var active, maximum, probes atomic.Int32
	deps := discoveryProviderDeps{
		fetch: func(_ context.Context, raw string, query url.Values) (macPayload, error) {
			if raw != yqkSourceURL {
				t.Error("aggregate dispatch lost the stable source URL")
			}
			item := discoveryFixtureItem()
			if query.Get("ac") == "detail" {
				var sources []source
				for _, code := range yqkPlaybackCodes() {
					sources = append(sources, yqkFixtureSource(strings.TrimPrefix(code, "yqk_")))
				}
				item["vod_play_from"], item["vod_play_url"] = serializeDiscoverySources(sources)
			}
			return macPayload{Code: 1, List: []map[string]any{item}}, nil
		},
		visible: func(_ context.Context, _ int64, sources []source) ([]source, error) { return sources, nil },
		probe: func(context.Context, string) error {
			t.Error("native marker reached a direct HTTP probe")
			return errors.New("unsupported")
		},
		nativeProbe: func(ctx context.Context, marker string) error {
			n := active.Add(1)
			defer active.Add(-1)
			for old := maximum.Load(); n > old && !maximum.CompareAndSwap(old, n); old = maximum.Load() {
			}
			probes.Add(1)
			select {
			case <-time.After(time.Millisecond):
			case <-ctx.Done():
				return ctx.Err()
			}
			_, _, _, err := yqkMarkerParts(marker)
			return err
		},
	}
	missingRegion := discoveryFixtureTarget()
	missingRegion.Area, missingRegion.SourceAPIID, missingRegion.SourceAPIVID = "", 2, "12"
	enriched := enrichDiscoveryTargetWith(context.Background(), missingRegion, row{"id": 2, "api_url": yqkSourceURL, "status": 1}, deps.fetch)
	if discoveryRegion(enriched.Area) != "cn" {
		t.Fatal("original YQK remote ID could not fill missing metadata")
	}
	result := discoverCollectorWith(context.Background(), discoveryFixtureTarget(), row{"id": 2, "api_url": yqkSourceURL, "status": 1}, deps)
	if result.Error != "" || len(result.Sources) != 18 || probes.Load() != 36 || maximum.Load() > 3 {
		t.Fatalf("aggregate was truncated, over-parallelized, or insufficiently verified: lines=%d probes=%d max=%d error=%s", len(result.Sources), probes.Load(), maximum.Load(), result.Error)
	}
	for _, src := range result.Sources {
		if !yqkAllowedSource(src) {
			t.Fatal("temporary media URL replaced stable marker")
		}
	}
	deps.nativeProbe = func(_ context.Context, marker string) error {
		if marker == "yqk://12/2/1160" {
			return errors.New("HTTP 403")
		}
		return nil
	}
	result = discoverCollectorWith(context.Background(), discoveryFixtureTarget(), row{"id": 2, "api_url": yqkSourceURL, "status": 1}, deps)
	if len(result.Sources) != 17 {
		t.Fatalf("one working first episode certified broken latest episode: %d", len(result.Sources))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := verifyYQKDiscoverySources(ctx, []source{yqkFixtureSource("2")}, "", deps.nativeProbe); len(got) != 0 {
		t.Fatal("cancelled probe certified an untested line")
	}
}

func TestYQKDiscoveryResolvesAndProbesMedia(t *testing.T) {
	var keys, segments atomic.Int32
	transport := make([]byte, 188*3)
	transport[0], transport[188], transport[376] = 0x47, 0x47, 0x47
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != "https://provider.example/" {
			t.Error("media probe omitted provider referer")
		}
		switch r.URL.Path {
		case "/video.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\n#EXTINF:6,\npart.ts\n#EXT-X-ENDLIST\n")
		case "/key.bin":
			keys.Add(1)
			_, _ = w.Write(make([]byte, 16))
		case "/part.ts":
			segments.Add(1)
			_, _ = w.Write(transport)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	resolve := func(_ context.Context, marker string) (xq.CMSMedia, error) {
		if marker != "yqk://12/2/1001" {
			t.Error("resolver did not receive the stored episode marker")
		}
		return xq.CMSMedia{URL: server.URL + "/video.m3u8", Referer: "https://provider.example/"}, nil
	}
	check := func(_ context.Context, raw string) error {
		if !strings.HasPrefix(raw, server.URL+"/") {
			return errors.New("outside fixture")
		}
		return nil
	}
	if err := probeYQKDiscoveryWith(context.Background(), "yqk://12/2/1001", resolve, server.Client(), check); err != nil || keys.Load() != 1 || segments.Load() != 1 {
		t.Fatalf("resolved HLS/key/segment were not validated: key=%d segment=%d error=%v", keys.Load(), segments.Load(), err)
	}
	if err := probeYQKDiscoveryWith(context.Background(), "https://arbitrary.example/video.m3u8", resolve, server.Client(), check); err == nil {
		t.Fatal("arbitrary URL accepted in native probe")
	}
}
