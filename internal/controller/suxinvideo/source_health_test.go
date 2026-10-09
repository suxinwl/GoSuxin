package suxinvideo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	xq "github.com/suxinwl/GoSuxin/internal/xiaoqiapp"
)

func healthTestSource(urls ...string) source {
	src := source{Code: "testm3u8", Name: "测试线路"}
	for i, raw := range urls {
		src.Episodes = append(src.Episodes, episode{Name: fmt.Sprintf("第%d集", i+1), URL: raw})
	}
	return src
}

func TestSourceHealthIdentityAndEligibility(t *testing.T) {
	src := healthTestSource("https://cdn.example/1.m3u8", "https://cdn.example/2.mp4")
	if !eligibleSourceHealth(src) {
		t.Fatal("direct media line should be eligible")
	}
	first := sourceHealthFingerprint(src)
	src.Name = "修正显示名"
	if sourceHealthFingerprint(src) != first {
		t.Fatal("display label must not reset health")
	}
	src.Episodes[0].URL = "https://cdn.example/new.m3u8"
	if sourceHealthFingerprint(src) == first {
		t.Fatal("updated playback URL must immediately escape an old quarantine")
	}
	for _, test := range []source{
		{Code: "native", Episodes: []episode{{URL: "hongguo://123/456"}}},
		{Code: "web", Episodes: []episode{{URL: "https://cdn.example/play/1"}}},
		{Code: "parsed", Parse: "https://parse.example/?url={url}", Episodes: []episode{{URL: "https://cdn.example/1.m3u8"}}},
		{Code: "mixed", Episodes: []episode{{URL: "https://cdn.example/1.m3u8"}, {URL: "https://cdn.example/play/2"}}},
		{},
	} {
		if eligibleSourceHealth(test) {
			t.Errorf("cannot safely auto-hide unsupported source: %v", test)
		}
	}
	if sourceHealthSignInput(1, first) == sourceHealthSignInput(2, first) {
		t.Fatal("report signatures must bind the film")
	}
}

func TestSourceHealthFailurePolicy(t *testing.T) {
	now := int64(1_000_000)
	if sourceHealthDue(now, now-59, 0, 0) || sourceHealthDue(now, 0, now+1, 0) || sourceHealthDue(now, 0, 0, now+1) {
		t.Fatal("repeated reports must respect interval, lease and quarantine")
	}
	if !sourceHealthDue(now, now-60, now, now) {
		t.Fatal("expired lease/quarantine must allow recovery")
	}
	rounds, hidden := sourceHealthOutcome(0, 0, now, true)
	if rounds != 1 || hidden != 0 {
		t.Fatal("one failed round must not hide a line")
	}
	rounds, hidden = sourceHealthOutcome(rounds, now, now+60, true)
	if rounds != 2 || hidden != now+60+1800 {
		t.Fatal("second separated failure must quarantine for thirty minutes")
	}
	if rounds, hidden = sourceHealthOutcome(1, now, now+60, false); rounds != 0 || hidden != 0 {
		t.Fatal("a successful/inconclusive check must reset failures")
	}
	if rounds, hidden = sourceHealthOutcome(1, now-901, now, true); rounds != 1 || hidden != 0 {
		t.Fatal("unrelated old failure must not count as a consecutive failure")
	}
}

func TestSourceHealthSampling(t *testing.T) {
	src := healthTestSource("one", "two", "three", "four", "five")
	got := sourceHealthSample(src, 2)
	if strings.Join(got, ",") != "three,one,five" {
		t.Fatalf("want reported and edge episodes, got %v", got)
	}
	got = sourceHealthSample(healthTestSource("one", "one", "two"), 0)
	if strings.Join(got, ",") != "one,two" {
		t.Fatalf("duplicate episode URLs must not count as independent samples: %v", got)
	}
}

func healthTestProbe(server *httptest.Server) sourceHealthProbe {
	return sourceHealthProbe{client: server.Client(), checkURL: func(context.Context, string) error { return nil }}
}

func TestSourceHealthHLSAndFirstSegment(t *testing.T) {
	transport := make([]byte, 188*3)
	transport[0], transport[188], transport[376] = 0x47, 0x47, 0x47
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/working.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXTINF:6,\nvideo.ts\n#EXT-X-ENDLIST\n")
		case "/segment-fail.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXTINF:6,\nmissing.ts\n#EXT-X-ENDLIST\n")
		case "/key-fail.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"missing.key\"\n#EXTINF:6,\nvideo.ts\n")
		case "/encrypted.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"good.key\"\n#EXTINF:6,\nvideo.ts\n")
		case "/unsupported.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"good.key\"\n#EXTINF:6,\nvideo.ts\n")
		case "/video.ts":
			_, _ = w.Write(transport)
		case "/good.key":
			_, _ = w.Write(make([]byte, 16))
		case "/empty.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-ENDLIST\n")
		case "/html.m3u8":
			fmt.Fprint(w, "<html>upstream failed</html>")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	probe := healthTestProbe(server)
	for _, path := range []string{"working.m3u8", "encrypted.m3u8"} {
		if failed, reason := probe.line(context.Background(), healthTestSource(server.URL+"/"+path), 0); failed || reason != "" {
			t.Errorf("%s should work: failed=%v reason=%s", path, failed, reason)
		}
	}
	for _, path := range []string{"segment-fail.m3u8", "key-fail.m3u8", "missing.m3u8", "empty.m3u8", "html.m3u8"} {
		if failed, reason := probe.line(context.Background(), healthTestSource(server.URL+"/"+path), 0); !failed || reason == "" {
			t.Errorf("%s should be confirmed failed: failed=%v reason=%s", path, failed, reason)
		}
	}
	if failed, _ := probe.line(context.Background(), healthTestSource(server.URL+"/segment-fail.m3u8", server.URL+"/working.m3u8"), 0); failed {
		t.Fatal("one broken episode must not hide a line with a working episode")
	}
	if failed, _ := probe.line(context.Background(), healthTestSource(server.URL+"/unsupported.m3u8"), 0); failed {
		t.Fatal("unsupported encryption is inconclusive, not proof of a dead line")
	}
}

func TestSourceHealthMasterPlaylistAlternatives(t *testing.T) {
	transport := make([]byte, 376)
	transport[0], transport[188] = 0x47, 0x47
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=200000\nbroken.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=100000\nworking.m3u8\n")
		case "/broken-master.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=200000\nbroken.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=100000\nmissing.m3u8\n")
		case "/working.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXTINF:6,\nvideo.ts\n")
		case "/cycle.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=200000\ncycle.m3u8\n")
		case "/video.ts":
			_, _ = w.Write(transport)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	probe := healthTestProbe(server)
	if failed, reason := probe.line(context.Background(), healthTestSource(server.URL+"/master.m3u8"), 0); failed || reason != "" {
		t.Fatalf("working alternative rendition must retain the source: %s", reason)
	}
	if failed, _ := probe.line(context.Background(), healthTestSource(server.URL+"/broken-master.m3u8"), 0); !failed {
		t.Fatal("all failed variants should count as a failed sample")
	}
	if failed, _ := probe.line(context.Background(), healthTestSource(server.URL+"/cycle.m3u8"), 0); failed {
		t.Fatal("uninspectable recursive master must not auto-hide")
	}
}

func TestSourceHealthProbeTimeoutAndAddressGuard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	probe := healthTestProbe(server)
	probe.client.Timeout = 15 * time.Millisecond
	if failed, _ := probe.line(context.Background(), healthTestSource(server.URL+"/timeout.m3u8"), 0); !failed {
		t.Fatal("independently bounded upstream timeout should fail its sample")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if failed, _ := probe.line(ctx, healthTestSource(server.URL+"/timeout.m3u8"), 0); failed {
		t.Fatal("incomplete overall check must not count as confirmed failure")
	}
	checks := 0
	probe.checkURL = func(context.Context, string) error {
		checks++
		return errors.New("private target")
	}
	if _, _, err := probe.read(context.Background(), "http://127.0.0.1/private", 16, true); err == nil || checks != 1 {
		t.Fatal("every requested URL must go through address validation")
	}
}

func TestSourceHealthYQKEligibility(t *testing.T) {
	src := source{Code: "yqk_5", Episodes: []episode{{Name: "第1集", URL: "yqk://781/5/1"}}}
	if !eligibleSourceHealth(src) {
		t.Fatal("validated YQK stable markers must expose the existing signed report mechanism")
	}
	before := sourceHealthFingerprint(src)
	src.Episodes[0].URL = "yqk://781/5/2"
	if sourceHealthFingerprint(src) == before {
		t.Fatal("a changed YQK playlist must immediately escape previous health state")
	}
	for _, invalid := range []source{
		{Code: "yqk_5", Parse: "https://parser.example/", Episodes: src.Episodes},
		{Code: "yqk_2", Episodes: src.Episodes},
		{Code: "yqk_999", Episodes: []episode{{URL: "yqk://781/999/1"}}},
		{Code: "yqk_5", Episodes: []episode{{URL: "https://cdn.example/1.m3u8"}}},
	} {
		if eligibleSourceHealth(invalid) {
			t.Fatal("unverified marker ownership or parser source accepted")
		}
	}
}

func TestSourceHealthYQKSamplesAndQualities(t *testing.T) {
	transport := make([]byte, 376)
	transport[0], transport[188] = 0x47, 0x47
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != "https://app.example/" {
			http.Error(w, "referer", http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/good.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXTINF:6,\nvideo.ts\n")
		case "/video.ts":
			_, _ = w.Write(transport)
		case "/unsupported.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"key\"\n#EXTINF:6,\nvideo.ts\n")
		case "/temporary.m3u8":
			http.Error(w, "temporary", http.StatusServiceUnavailable)
		default:
			http.Error(w, "forbidden", http.StatusForbidden)
		}
	}))
	defer server.Close()
	src := source{Code: "yqk_5"}
	for i := 1; i <= 5; i++ {
		src.Episodes = append(src.Episodes, episode{Name: fmt.Sprint(i), URL: fmt.Sprintf("yqk://781/5/%d", i)})
	}
	probe := healthTestProbe(server)
	var calls []string
	goodID, otherQuality, temporary, unsupported, unavailable := "", false, false, false, false
	probe.resolveYQK = func(ctx context.Context, marker string) (xq.CMSMedia, error) {
		_, _, id, err := yqkMarkerParts(marker)
		if err != nil {
			t.Fatal("resolver received arbitrary input")
		}
		calls = append(calls, id)
		if unavailable {
			return xq.CMSMedia{}, errors.New("private APP transport detail must not be persisted")
		}
		media := xq.CMSMedia{URL: server.URL + "/bad.m3u8", Referer: "https://app.example/"}
		if id == goodID {
			media.URL = server.URL + "/good.m3u8"
		}
		if otherQuality {
			media.Variants = []xq.CMSMediaVariant{{URL: media.URL}, {URL: server.URL + "/good.m3u8"}}
		}
		if temporary {
			media.URL = server.URL + "/temporary.m3u8"
		}
		if unsupported {
			media.URL = server.URL + "/unsupported.m3u8"
		}
		return media, nil
	}
	if failed, reason := probe.line(context.Background(), src, 2); !failed || reason == "" {
		t.Fatalf("all sampled media must independently fail: failed=%v reason=%s", failed, reason)
	}
	if strings.Join(calls, ",") != "3,1,5" {
		t.Fatalf("must check reported + first + last, at most three: %v", calls)
	}
	goodID = "5"
	if failed, _ := probe.line(context.Background(), src, 2); failed {
		t.Fatal("a single broken episode must never hide a line with another playable sample")
	}
	goodID, otherQuality = "", true
	if failed, _ := probe.line(context.Background(), src, 2); failed {
		t.Fatal("a working alternate quality must preserve the entire source")
	}
	otherQuality, temporary = false, true
	if failed, _ := probe.line(context.Background(), src, 2); failed {
		t.Fatal("temporary 503 cannot prove the film line is unavailable")
	}
	temporary, unsupported = false, true
	if failed, _ := probe.line(context.Background(), src, 2); failed {
		t.Fatal("unsupported native encryption must fail open")
	}
	unsupported, unavailable = false, true
	if failed, reason := probe.line(context.Background(), src, 2); failed || strings.Contains(reason, "private") {
		t.Fatal("shared APP resolution errors must not hide lines or expose transport details")
	}
	calls = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if failed, _ := probe.line(ctx, src, 2); failed || len(calls) != 0 {
		t.Fatal("canceled probes must not invoke resolution or count as a source failure")
	}
}

func TestSourceHealthYQKTimeoutAndShortCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	probe := healthTestProbe(server)
	probe.client.Timeout = 10 * time.Millisecond
	probe.resolveYQK = func(context.Context, string) (xq.CMSMedia, error) {
		return xq.CMSMedia{URL: server.URL + "/video.m3u8"}, nil
	}
	src := source{Code: "yqk_5", Episodes: []episode{{URL: "yqk://999/5/111"}}}
	if failed, _ := probe.line(context.Background(), src, 0); failed {
		t.Fatal("ordinary network timeout must not count against native source health")
	}
	marker := src.Episodes[0].URL
	want := xq.CMSMedia{URL: "https://cdn.example/cached.m3u8"}
	sourceHealthRememberYQK(marker, want)
	got, err := sourceHealthResolveYQK(context.Background(), marker)
	if err != nil || got.URL != want.URL {
		t.Fatal("health checks must reuse recently resolved foreground media")
	}
	sourceHealthYQKCache.Lock()
	entry := sourceHealthYQKCache.items[marker]
	if remaining := time.Until(entry.until); remaining <= 0 || remaining > 30*time.Second {
		t.Error("cache TTL must expire before the next sixty-second confirmation round")
	}
	entry.until = time.Now().Add(-time.Second)
	sourceHealthYQKCache.items[marker] = entry
	sourceHealthYQKCache.Unlock()
	for i := 1; i <= 140; i++ {
		sourceHealthRememberYQK(fmt.Sprintf("yqk://999/5/%d", 1000+i), want)
	}
	sourceHealthYQKCache.Lock()
	_, retained := sourceHealthYQKCache.items[marker]
	count := len(sourceHealthYQKCache.items)
	sourceHealthYQKCache.items = map[string]sourceHealthMediaEntry{}
	sourceHealthYQKCache.Unlock()
	if retained || count > 128 {
		t.Fatalf("expired cache entries or capacity not bounded: retained=%v count=%d", retained, count)
	}
}
