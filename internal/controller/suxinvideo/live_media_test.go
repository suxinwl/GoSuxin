package suxinvideo

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type liveFixtureTransport func(*http.Request) (*http.Response, error)

func (f liveFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func liveFixtureResponse(r *http.Request, body, kind string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{kind}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

func TestLiveMediaContentTypeRecognizesDisguisedTransportStream(t *testing.T) {
	packets := make([]byte, 188*6)
	for n := 0; n < 6; n++ {
		packets[n*188] = 0x47
	}
	for _, body := range [][]byte{packets, packets[37:]} {
		before := string(body)
		if got := liveMediaContentType("image/jpeg", body); got != "video/mp2t" {
			t.Fatalf("disguised TS was described as %q", got)
		}
		if string(body) != before {
			t.Fatal("content type normalization modified the media payload")
		}
	}
	if got := liveMediaContentType("image/jpeg", []byte("0123456789abcdef")); got != "application/octet-stream" {
		t.Fatalf("opaque AES key was described as %q", got)
	}
	if got := liveMediaContentType("image/jpeg", []byte("\x00\x00\x00\x18ftypisom\x00\x00\x00\x00")); got != "video/mp4" {
		t.Fatalf("fragmented MP4 was described as %q", got)
	}
	if got := liveMediaContentType("audio/aac", []byte{0xff, 0xf1, 0x00}); got != "audio/aac" {
		t.Fatalf("valid audio type changed: %q", got)
	}
	if got := liveMediaContentType("image/jpeg", append([]byte{0x47}, make([]byte, 1200)...)); got == "video/mp2t" {
		t.Fatal("a single matching byte is insufficient to recognize TS")
	}
}

func TestLiveProbeChecksVariantKeyInitAndFragment(t *testing.T) {
	old := liveHTTPClient
	t.Cleanup(func() { liveHTTPClient = old })
	seen := []string{}
	liveHTTPClient = &http.Client{Transport: liveFixtureTransport(func(r *http.Request) (*http.Response, error) {
		seen = append(seen, r.URL.Path)
		body, kind := "", "application/vnd.apple.mpegurl"
		switch r.URL.Path {
		case "/tv/root.m3u8":
			body = "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=5000000,RESOLUTION=1920x1080\nvariant/index.m3u8\n"
		case "/tv/variant/index.m3u8":
			body = "#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXT-X-MEDIA-SEQUENCE:502\n#EXT-X-KEY:METHOD=AES-128,URI=\"../key\"\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:6,\nsegment.ts\n"
		case "/tv/key":
			body = strings.Repeat("k", 16)
			kind = "application/octet-stream"
		case "/tv/variant/init.mp4":
			body = "fixture-init"
			kind = "video/mp4"
		case "/tv/variant/segment.ts":
			body = strings.Repeat("media", 1000)
			kind = "video/mp2t"
		default:
			t.Fatalf("unexpected asset %s", r.URL.Path)
		}
		if r.Header.Get("User-Agent") != "fixture-client" {
			t.Fatal("source request headers were not applied")
		}
		return liveFixtureResponse(r, body, kind), nil
	})}
	got, err := liveProbeStream(context.Background(), LiveStream{ID: 987654, URL: "https://8.8.8.8/tv/root.m3u8", Headers: map[string]string{"User-Agent": "fixture-client"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Quality != "1080p" || len(got.Qualities) != 1 || got.Qualities[0].Width != 1920 || got.Qualities[0].URL != "https://8.8.8.8/tv/variant/index.m3u8" {
		t.Fatalf("bad quality metadata: %#v", got)
	}
	if len(seen) != 5 {
		t.Fatalf("key/init/fragment not all checked: %v", seen)
	}
}
func TestLiveProbeRejectsErrorMediaAndPrivateReferences(t *testing.T) {
	old := liveHTTPClient
	t.Cleanup(func() { liveHTTPClient = old })
	for _, fixture := range []struct{ name, playlist, fragment string }{
		{"html fragment", "#EXTM3U\n#EXTINF:6,\npart.ts\n", "<html>denied</html>"},
		{"private reference", "#EXTM3U\n#EXTINF:6,\nhttp://127.0.0.1/private.ts\n", "media"},
		{"invalid key", "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key\"\n#EXTINF:6,\npart.ts\n", "not-a-key"},
		{"DRM", "#EXTM3U\n#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"key\"\n#EXTINF:6,\npart.ts\n", "media"},
		{"no segments", "#EXTM3U\n#EXT-X-TARGETDURATION:6\n", "media"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			liveHTTPClient = &http.Client{Transport: liveFixtureTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/live.m3u8" {
					return liveFixtureResponse(r, fixture.playlist, "application/vnd.apple.mpegurl"), nil
				}
				return liveFixtureResponse(r, fixture.fragment, "application/octet-stream"), nil
			})}
			if _, err := liveProbeStream(context.Background(), LiveStream{URL: "https://8.8.8.8/live.m3u8"}); err == nil {
				t.Fatal("invalid stream accepted")
			}
		})
	}
}
func TestLiveAssetLinksHideUpstreamAndPreserveMovingWindow(t *testing.T) {
	token, item, err := newLiveSession(123, 456)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { liveSessions.Lock(); delete(liveSessions.Items, token); liveSessions.Unlock() })
	raw := "#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXT-X-MEDIA-SEQUENCE:1001\n#EXT-X-PROGRAM-DATE-TIME:2026-10-03T12:00:00Z\n#EXT-X-KEY:METHOD=AES-128,URI=\"key?secret=private\"\n#EXTINF:6,\nfirst.ts\n#EXT-X-DISCONTINUITY\n#EXTINF:6,\nsecond.ts\n"
	rewritten := rewritePlaylist(raw, "https://8.8.8.8/live/index.m3u8", func(raw string) string { return item.link(token, raw, false) })
	for _, preserved := range []string{"#EXT-X-MEDIA-SEQUENCE:1001", "#EXT-X-PROGRAM-DATE-TIME:", "#EXT-X-DISCONTINUITY", "#EXTINF:6,"} {
		if !strings.Contains(rewritten, preserved) {
			t.Fatalf("lost live metadata: %s", preserved)
		}
	}
	if strings.Contains(rewritten, "secret") || strings.Contains(rewritten, "8.8.8.8") || strings.Contains(rewritten, "#EXT-X-ENDLIST") {
		t.Fatal("live proxy leaked upstream or converted to VOD")
	}
	if len(item.Assets) != 3 {
		t.Fatalf("wrong authorized assets: %d", len(item.Assets))
	}
	unknown := item.Assets["arbitrary"]
	if unknown.URL != "" {
		t.Fatal("unregistered asset was authorized")
	}
	original := item.link(token, "https://8.8.8.8/live/master.m3u8", true)
	item.mu.Lock()
	item.Expires = time.Now().Add(4 * time.Hour)
	item.mu.Unlock()
	if original != item.link(token, "https://8.8.8.8/live/master.m3u8", true) {
		t.Fatal("renewal changed asset identity")
	}
	item.mu.Lock()
	item.Expires = time.Now().Add(-time.Second)
	item.mu.Unlock()
	if _, err := getLiveSession(token); err == nil {
		t.Fatal("expired live session accepted")
	}
}
func TestLiveLineSelectionKeepsFailedFallbackAndExclusions(t *testing.T) {
	rows := []LiveStream{{ID: 1, Enabled: true, Health: "unhealthy", Priority: 100}, {ID: 2, Enabled: true, Health: "healthy", Priority: 10}, {ID: 3, Enabled: true, Health: "healthy", Priority: 20}, {ID: 4, Enabled: false, Health: "healthy", Priority: 200}}
	got := liveOrderedStreams(rows, 0, nil)
	if len(got) != 3 || got[0].ID != 3 || got[1].ID != 2 || got[2].ID != 1 {
		t.Fatalf("bad fallback order: %#v", got)
	}
	got = liveOrderedStreams(rows, 0, []int64{3, 2})
	if len(got) != 1 || got[0].ID != 1 {
		t.Fatal("failed but enabled fallback disappeared")
	}
	got = liveOrderedStreams(rows, 1, nil)
	if len(got) != 1 || got[0].ID != 1 {
		t.Fatal("manual selection changed line")
	}
}
func TestLiveCredentialHeadersStayAtSourceOrigin(t *testing.T) {
	old := liveHTTPClient
	t.Cleanup(func() { liveHTTPClient = old })
	liveHTTPClient = &http.Client{Transport: liveFixtureTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "8.8.4.4" && (r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "") {
			t.Fatal("origin credentials leaked to playlist CDN")
		}
		return liveFixtureResponse(r, "payload", "application/octet-stream"), nil
	})}
	stream := LiveStream{URL: "https://8.8.8.8/live.m3u8", Headers: map[string]string{"Authorization": "secret", "Cookie": "private=1", "User-Agent": "fixture"}}
	response, err := liveRequest(context.Background(), stream, "https://8.8.4.4/segment.ts", "")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
}

type liveSlowFixtureBody struct {
	ctx    context.Context
	reader io.Reader
	waited bool
}

func (b *liveSlowFixtureBody) Read(p []byte) (int, error) {
	if !b.waited {
		b.waited = true
		select {
		case <-time.After(30 * time.Millisecond):
		case <-b.ctx.Done():
			return 0, b.ctx.Err()
		}
	}
	return b.reader.Read(p)
}
func (b *liveSlowFixtureBody) Close() error { return nil }

func TestLiveMediaBudgetAllowsCompleteFragmentWithoutExtendingProbe(t *testing.T) {
	old := liveHTTPClient
	t.Cleanup(func() { liveHTTPClient = old })
	liveHTTPClient = &http.Client{Timeout: 10 * time.Millisecond, Transport: liveFixtureTransport(func(r *http.Request) (*http.Response, error) {
		response := liveFixtureResponse(r, "complete media fragment", "video/MP2T")
		response.Body = &liveSlowFixtureBody{ctx: r.Context(), reader: strings.NewReader("complete media fragment")}
		return response, nil
	})}
	stream := LiveStream{URL: "https://8.8.8.8/live.m3u8"}
	response, err := liveRequest(context.Background(), stream, "https://8.8.8.8/fragment.ts", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(response.Body)
	response.Body.Close()
	if err == nil {
		t.Fatal("detection read exceeded its budget without failing")
	}
	response, err = liveRequestWithBudget(context.Background(), stream, "https://8.8.8.8/fragment.ts", "", 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || string(body) != "complete media fragment" {
		t.Fatal("full media body was cut off", err)
	}
	if liveHTTPClient.Timeout != 10*time.Millisecond {
		t.Fatal("media request changed the shared detection timeout")
	}
}
