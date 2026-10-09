package suxinvideo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"github.com/suxinwl/GoSuxin/internal/erciyuan"
	xq "github.com/suxinwl/GoSuxin/internal/xiaoqiapp"
)

type erciyuanPlaybackFixture func(context.Context, string) (erciyuan.Media, error)

func (f erciyuanPlaybackFixture) Resolve(ctx context.Context, marker string) (erciyuan.Media, error) {
	return f(ctx, marker)
}

func erciyuanPlaybackSource(line string) source {
	return source{Code: "ecy_" + line, Episodes: []episode{
		{Name: "第160集", URL: "erciyuan://35604/" + line + "/0"},
		{Name: "第1集", URL: "erciyuan://35604/" + line + "/159"},
	}}
}

func TestErciyuanPlaybackMarkerAndDynamicResolution(t *testing.T) {
	marker := "erciyuan://35604/aa03/159"
	if id, err := nativeErciyuanSeries("ecy_aa03", marker); err != nil || id != "35604" {
		t.Fatalf("original episode marker failed: %q %v", id, err)
	}
	for _, tc := range []struct{ code, marker string }{
		{"ecy_aa02", marker}, {"aa03", marker}, {"ecy_unknown", marker},
		{"ecy_aa03", marker + "?url=http://private"}, {"ecy_aa03", "erciyuan://35604:80/aa03/159"},
		{"ecy_aa03", "erciyuan://user@35604/aa03/159"}, {"ecy_aa03", "erciyuan://35604/aa03/%31"},
		{"ecy_aa03", "erciyuan://0/aa03/0"}, {"ecy_aa03", "https://cdn.example/1.m3u8"},
	} {
		if _, err := nativeErciyuanSeries(tc.code, tc.marker); err == nil {
			t.Fatalf("foreign marker or line accepted: %+v", tc)
		}
	}
	calls := 0
	provider := erciyuanPlaybackFixture(func(_ context.Context, got string) (erciyuan.Media, error) {
		calls++
		if got != marker {
			t.Fatal("stable marker changed before resolving")
		}
		return erciyuan.Media{URL: fmt.Sprintf("https://cdn.example/episode.mp4?version=%d", calls), Headers: map[string]string{"Referer": "https://source.example/", "Cookie": "private"}}, nil
	})
	first, headers, err := resolveErciyuanPlaybackWith(context.Background(), marker, provider)
	second, _, nextErr := resolveErciyuanPlaybackWith(context.Background(), marker, provider)
	if err != nil || nextErr != nil || calls != 2 || first.URL == second.URL || headers["Cookie"] != "" || first.Referer != "https://source.example/" {
		t.Fatal("playback cached an ephemeral media address or leaked credentials")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := resolveErciyuanPlaybackWith(ctx, marker, provider); !errors.Is(err, context.Canceled) || calls != 2 {
		t.Fatal("canceled resolve continued to call provider")
	}
	_, _, err = resolveErciyuanPlaybackWith(context.Background(), marker, erciyuanPlaybackFixture(func(context.Context, string) (erciyuan.Media, error) {
		return erciyuan.Media{}, errors.New("https://secret.example/media?sign=private")
	}))
	if err == nil || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "secret") {
		t.Fatal("upstream signed URL escaped through public resolver error")
	}
}

func TestErciyuanPlaybackVisibilityAndProvenance(t *testing.T) {
	src := erciyuanPlaybackSource("aa03")
	collector := row{"id": 27, "api_url": erciyuanSourceURL, "status": 1}
	if codes := collectorPlaybackCodes(erciyuanSourceURL); len(codes) != 4 {
		t.Fatal("native provider did not claim its four supported lines")
	}
	spoof := "https://unrelated.example/api/from/ecy_aa03/"
	if len(collectorPlaybackCodes(spoof)) != 0 || discoverySourceAllowed(src, row{"id": 28, "api_url": spoof}, []row{collector}) {
		t.Fatal("ordinary MacCMS endpoint claimed reserved native namespace")
	}
	if !discoverySourceAllowed(src, collector, []row{collector}) {
		t.Fatal("authorized native collector rejected its stable markers")
	}
	got := availableSources(row{"api_id": 4}, []source{src}, nil, []row{collector})
	if len(got) != 1 || got[0].Name != "二次元·电信" {
		t.Fatalf("merged native line inherited unrelated primary owner or missing name: %+v", got)
	}
	if len(availableSources(row{"api_id": 4}, []source{src}, map[string]row{"ecy_aa03": {"status": 0}}, []row{collector})) != 0 {
		t.Fatal("disabled player remained visible")
	}
	collector["status"] = 0
	ordinary := source{Code: "hnm3u8", Episodes: []episode{{Name: "第1集", URL: "https://media.example/index.m3u8"}}}
	got = availableSources(row{"api_id": 27}, []source{src, ordinary}, nil, []row{collector, {"id": 8, "api_url": "https://hongniuzy2.com/api", "status": 1}})
	if len(got) != 1 || got[0].Code != ordinary.Code {
		t.Fatal("disabled native collector affected other merged line or remained visible")
	}
	collector["status"] = 1
	foreign := erciyuanPlaybackSource("aa03")
	foreign.Episodes[0].URL = "erciyuan://99/aa03/0"
	for _, invalid := range []source{foreign, {Code: "ecy_aa02", Episodes: src.Episodes}, {Code: "hnm3u8", Episodes: src.Episodes}} {
		if len(availableSources(row{"api_id": 27}, []source{invalid}, nil, []row{collector})) != 0 {
			t.Fatal("foreign film, line or native marker was presented as authorized source")
		}
	}
}

func TestErciyuanMediaHeaderPolicy(t *testing.T) {
	input := map[string]string{
		"user-agent": "media-client", "Accept": "*/*", "Accept-Language": "zh-CN",
		"Referer": "https://source.example/watch", "Origin": "https://source.example",
		"Authorization": "secret", "Cookie": "secret", "Range": "bytes=9-", "Host": "private", "Connection": "close",
		"X-Header": "unsupported", "Invalid": "bad\r\nHeader: bad",
	}
	want := map[string]string{"User-Agent": "media-client", "Accept": "*/*", "Accept-Language": "zh-CN", "Referer": "https://source.example/watch", "Origin": "https://source.example"}
	if got := nativeMediaHeaders(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("media header allowlist differs: %+v", got)
	}
	for _, invalid := range []map[string]string{
		{"User-Agent": "one\r\nHost: private"}, {"Referer": "https://user:secret@source.example/"},
		{"Origin": "https://source.example/watch"}, {"Origin": "https://source.example/?key=secret"},
	} {
		if len(nativeMediaHeaders(invalid)) != 0 {
			t.Fatal("invalid media header retained")
		}
	}
	request, _ := http.NewRequest(http.MethodGet, "https://cdn.xhscdn.com/media.mp4", nil)
	request.Header.Set("Range", "bytes=0-32767")
	applyNativeMediaHeaders(request, input)
	if request.Header.Get("Referer") != "" || request.Header.Get("Origin") != "" || request.Header.Get("Range") != "bytes=0-32767" || request.Header.Get("User-Agent") != "media-client" {
		t.Fatal("CDN origin policy or transport range was overwritten")
	}
}

func TestErciyuanDiscoveryBoundedFourLinesAndOriginalIndexes(t *testing.T) {
	sources := []source{erciyuanPlaybackSource("aa02"), erciyuanPlaybackSource("aa03"), erciyuanPlaybackSource("dd02"), erciyuanPlaybackSource("4k01")}
	var mu sync.Mutex
	calls := map[string]int{}
	var active, peak atomic.Int32
	probe := func(ctx context.Context, marker string) error {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		mu.Lock()
		calls[marker]++
		mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
		}
		if marker == "erciyuan://35604/dd02/0" {
			return errors.New("latest media unavailable")
		}
		return nil
	}
	got := verifyErciyuanDiscoverySources(context.Background(), sources, "35604", "episode:1", probe)
	if len(got) != 3 || peak.Load() > 2 || calls["erciyuan://35604/aa03/159"] != 1 || got[1].Episodes[0].URL != "erciyuan://35604/aa03/159" {
		t.Fatalf("line/sample verification lost index/order or ignored failure/concurrency: %d %d %+v", len(got), peak.Load(), calls)
	}
	if len(verifyErciyuanDiscoverySources(context.Background(), sources, "99", "", func(context.Context, string) error {
		t.Fatal("marker with foreign film ID reached media resolver")
		return nil
	})) != 0 {
		t.Fatal("foreign markers certified")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if len(verifyErciyuanDiscoverySources(ctx, sources, "35604", "", probe)) != 0 {
		t.Fatal("canceled job retained unverified media")
	}
}

func TestErciyuanDiscoveryIdentityAliasesAndDisabledCollector(t *testing.T) {
	for _, name := range []string{"国漫", "日漫", "经典番剧", "特摄", "动态漫画"} {
		if discoveryKind(name) != "anime" {
			t.Fatalf("real native category %s does not map to anime", name)
		}
	}
	if discoveryKind("剧场版") != "anime_movie" {
		t.Fatal("animated movie category conflated with animated series")
	}
	collector := row{"id": 27, "status": 0, "api_url": erciyuanSourceURL}
	deps := discoveryProviderDeps{fetch: func(context.Context, string, url.Values) (macPayload, error) {
		t.Fatal("disabled collector issued a search request")
		return macPayload{}, nil
	}}
	if result := discoverCollectorWith(context.Background(), discoveryFixtureTarget(), collector, deps); len(result.Sources) != 0 || result.Error == "" {
		t.Fatal("disabled source returned verified lines")
	}
	for _, tc := range []struct{ field, value string }{{"vod_year", ""}, {"vod_year", "2024"}, {"vod_area", ""}, {"type_name", "剧场版"}, {"vod_name", "仙逆第二季"}} {
		item := discoveryFixtureItem()
		item["type_name"] = "国漫"
		item[tc.field] = tc.value
		if discoveryMatches(discoveryFixtureTarget(), item, 27) {
			t.Fatalf("new source relaxed strict foreign film identity: %+v", tc)
		}
	}
}

func TestErciyuanDiscoveryAndHealthProbeActualEncryptedHLS(t *testing.T) {
	transport := make([]byte, 188*3)
	transport[0], transport[188], transport[376] = 0x47, 0x47, 0x47
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("User-Agent") != "fixture-player" || r.Header.Get("Referer") != "https://source.example/" {
			t.Error("provider media headers did not reach manifest/key/segment")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/working.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\n#EXTINF:6,\nvideo.ts\n#EXT-X-ENDLIST\n")
		case "/key.bin":
			_, _ = w.Write(make([]byte, 16))
		case "/video.ts":
			_, _ = w.Write(transport)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	checks := 0
	guard := func(context.Context, string) error { checks++; return nil }
	resolve := func(_ context.Context, marker string) (xq.CMSMedia, map[string]string, error) {
		path := "/working.m3u8"
		if strings.HasSuffix(marker, "/0") {
			path = "/missing.m3u8"
		}
		return xq.CMSMedia{URL: server.URL + path}, map[string]string{"User-Agent": "fixture-player", "Referer": "https://source.example/"}, nil
	}
	if err := probeErciyuanDiscoveryWith(context.Background(), "erciyuan://35604/aa03/159", resolve, server.Client(), guard); err != nil || checks != 3 || requests.Load() != 3 {
		t.Fatalf("media probe skipped HLS key/segment or SSRF guard: checks=%d requests=%d err=%v", checks, requests.Load(), err)
	}
	probe := sourceHealthProbe{client: server.Client(), checkURL: guard, resolveErciyuan: resolve}
	src := erciyuanPlaybackSource("aa03")
	if !eligibleSourceHealth(src) {
		t.Fatal("validated native line does not expose existing health mechanism")
	}
	if failed, reason := probe.line(context.Background(), src, 0); failed || reason != "" {
		t.Fatalf("one broken episode hid a line with a working episode: %s", reason)
	}
	probe.resolveErciyuan = func(context.Context, string) (xq.CMSMedia, map[string]string, error) {
		return xq.CMSMedia{}, nil, errors.New("parser rejected current ad URL")
	}
	if failed, _ := probe.line(context.Background(), src, 0); failed {
		t.Fatal("inconclusive API/parser failure banned whole provider line")
	}
	probe.resolveErciyuan = func(context.Context, string) (xq.CMSMedia, map[string]string, error) {
		return xq.CMSMedia{URL: server.URL + "/missing.m3u8"}, map[string]string{"User-Agent": "fixture-player", "Referer": "https://source.example/"}, nil
	}
	if failed, _ := probe.line(context.Background(), src, 0); !failed {
		t.Fatal("all verified 404 media samples did not count as failed")
	}
}

// Run real SQL over inline rows only. No business configuration or film is
// inserted, changed or removed, including the disabled-provider scenarios.
func TestErciyuanListingsReadOnlySQL(t *testing.T) {
	if os.Getenv("SUXIN_ERCY_LIST_CHECK") != "1" {
		t.Skip("set SUXIN_ERCY_LIST_CHECK=1 for read-only native listing SQL acceptance")
	}
	t.Setenv("SUXIN_INTEGRATION", "1")
	ctx, cancel := context.WithTimeout(yqkIntegrationContext(t), 15*time.Second)
	defer cancel()
	condition := strings.ReplaceAll(erciyuanListingCondition("v."), "sx_collect_api", "(SELECT ? api_url, ? status) fixture_collect_api")
	for _, tc := range []struct {
		name, from string
		status     int
		visible    bool
	}{
		{"disabled native four lines", "ecy_aa02$$$ecy_aa03$$$ecy_dd02$$$ecy_4k01", 0, false},
		{"enabled native four lines", "ecy_aa02$$$ecy_aa03$$$ecy_dd02$$$ecy_4k01", 1, true},
		{"disabled native single line", "ecy_aa03", 0, false},
		{"disabled native mixed with independent line", "ecy_aa03$$$hnm3u8", 0, true},
		{"independent source only", "hnm3u8", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := one(ctx, "SELECT "+condition+" visible FROM (SELECT ? play_from) v", erciyuanSourceURL, tc.status, tc.from)
			if err != nil || (gconv.Int(result["visible"]) == 1) != tc.visible {
				t.Fatalf("native aggregate listing behavior differs: visible=%v err=%v", result["visible"], err)
			}
		})
	}
}
