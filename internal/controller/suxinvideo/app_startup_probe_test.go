package suxinvideo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	xq "github.com/suxinwl/GoSuxin/internal/xiaoqiapp"
)

func TestAppStartupProbeMasterValidatesAddressesWithoutFetchingMedia(t *testing.T) {
	var mu sync.Mutex
	requests := map[string]int{}
	checked := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests[r.URL.Path]++
		mu.Unlock()
		if r.Header.Get("Referer") != "https://upstream.example/" || r.Header.Get("Origin") != "https://upstream.example" {
			t.Error("Native provider headers were lost")
		}
		switch r.URL.Path {
		case "/master.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=800000\nchild.m3u8\n")
		case "/child.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:4,\nfirst.ts\n#EXT-X-ENDLIST\n")
		default:
			t.Error("Startup waited for key/init/segment bytes")
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer server.Close()
	check := func(_ context.Context, raw string) error {
		mu.Lock()
		checked[raw] = true
		mu.Unlock()
		return nil
	}
	ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	if err := probeAppStartupMediaHeaders(ctx, xq.CMSMedia{URL: server.URL + "/master.m3u8", Referer: "https://upstream.example/"}, map[string]string{"Origin": "https://upstream.example"}, server.Client(), check); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/key.bin", "/init.mp4", "/first.ts"} {
		if !checked[server.URL+path] || requests[path] != 0 {
			t.Fatalf("%s must be URL-validated without downloading its bytes", path)
		}
	}
	if requests["/master.m3u8"] != 1 || requests["/child.m3u8"] != 1 {
		t.Fatal("Master/child playlist must both be validated")
	}
}

func TestAppStartupProbeRejectsHTMLMissingSegmentsAndUnsupportedKeys(t *testing.T) {
	for _, body := range []string{"<html>denied</html>", "#EXTM3U\n#EXT-X-ENDLIST", "#EXTM3U\n#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"key\"\n#EXTINF:4,\nfirst.ts\n"} {
		t.Run(fmt.Sprintf("format%d", len(body)), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
			defer server.Close()
			if err := probeAppStartupMediaHeaders(context.Background(), xq.CMSMedia{URL: server.URL + "/playlist.m3u8"}, nil, server.Client(), func(context.Context, string) error { return nil }); err == nil {
				t.Fatal("HTTP 200 alone must not pass startup validation")
			}
		})
	}
}

func TestAppStartupProbeRetainsReferenceURLValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"http://blocked.example/key\"\n#EXTINF:4,\nfirst.ts\n")
	}))
	defer server.Close()
	err := probeAppStartupMediaHeaders(context.Background(), xq.CMSMedia{URL: server.URL + "/playlist.m3u8"}, nil, server.Client(), func(_ context.Context, raw string) error {
		if strings.Contains(raw, "blocked.example") {
			return errors.New("private media address")
		}
		return nil
	})
	if err == nil {
		t.Fatal("Referenced key addresses must retain SSRF validation")
	}
}

func TestAppStartupProbeHonorsDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	ctx, stop := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer stop()
	start := time.Now()
	if err := probeAppStartupMediaHeaders(ctx, xq.CMSMedia{URL: server.URL + "/slow.m3u8"}, nil, server.Client(), func(context.Context, string) error { return nil }); err == nil {
		t.Fatal("Timed out playlist must not be treated as healthy")
	}
	if time.Since(start) > time.Second {
		t.Fatal("Cancelled startup request did not terminate promptly")
	}
}

func TestAppStartupProbeDoesNotWeakenFallbackMediaVerification(t *testing.T) {
	var segmentReads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/first.ts" {
			segmentReads.Add(1)
			w.WriteHeader(http.StatusForbidden)
			return
		}
		fmt.Fprint(w, "#EXTM3U\n#EXTINF:4,\nfirst.ts\n#EXT-X-ENDLIST\n")
	}))
	defer server.Close()
	media := xq.CMSMedia{URL: server.URL + "/playlist.m3u8"}
	check := func(context.Context, string) error { return nil }
	if err := probeAppStartupMediaHeaders(context.Background(), media, nil, server.Client(), check); err != nil {
		t.Fatal(err)
	}
	if err := probeDiscoveryMediaHeaders(context.Background(), media, nil, server.Client(), check); err == nil || segmentReads.Load() != 1 {
		t.Fatal("Fallback must still reject an unreadable first media segment")
	}
}

func TestAppStartupProbeDoesNotWaitForSlowFirstSegment(t *testing.T) {
	var segmentReads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/first.ts" {
			segmentReads.Add(1)
			<-r.Context().Done()
			return
		}
		fmt.Fprint(w, "#EXTM3U\n#EXTINF:4,\nfirst.ts\n#EXT-X-ENDLIST\n")
	}))
	defer server.Close()
	media := xq.CMSMedia{URL: server.URL + "/playlist.m3u8"}
	check := func(context.Context, string) error { return nil }
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	start := time.Now()
	err := probeAppStartupMediaHeaders(ctx, media, nil, server.Client(), check)
	startupElapsed := time.Since(start)
	cancel()
	if err != nil || segmentReads.Load() != 0 {
		t.Fatalf("Preferred startup fetched first-segment bytes: %v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start = time.Now()
	err = probeDiscoveryMediaHeaders(ctx, media, nil, server.Client(), check)
	fallbackElapsed := time.Since(start)
	if err == nil || segmentReads.Load() != 1 {
		t.Fatal("Fallback must wait for media validation and return a timeout")
	}
	if startupElapsed >= fallbackElapsed || fallbackElapsed < 150*time.Millisecond {
		t.Fatalf("Playlist-only startup was not faster: startup=%s fallback=%s", startupElapsed, fallbackElapsed)
	}
	t.Logf("Controlled slow-segment source: preferred=%s fallback=%s", startupElapsed, fallbackElapsed)
}

func TestAppStartupProbeAcceptsValidColdPlaylistAfterThreeSeconds(t *testing.T) {
	var segmentReads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/playlist.m3u8" {
			segmentReads.Add(1)
			w.WriteHeader(http.StatusForbidden)
			return
		}
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
			fmt.Fprint(w, "#EXTM3U\n#EXTINF:4,\nfirst.ts\n#EXT-X-ENDLIST\n")
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), appStartupManifestBudget)
	defer cancel()
	if err := probeAppStartupMediaHeaders(ctx, xq.CMSMedia{URL: server.URL + "/playlist.m3u8"}, nil, server.Client(), func(context.Context, string) error { return nil }); err != nil {
		t.Fatalf("A valid cold manifest must not trigger unavailable-source fallback: %v", err)
	}
	if segmentReads.Load() != 0 {
		t.Fatal("Extending the manifest budget must not enable first-segment downloading")
	}
}
