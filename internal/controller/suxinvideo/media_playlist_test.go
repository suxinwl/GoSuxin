package suxinvideo

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestMediaPlaylistDetection(t *testing.T) {
	for _, tt := range []struct {
		mime, url string
		want      bool
	}{
		{"application/vnd.apple.mpegURL", "https://cdn.example/token", true},
		{"application/octet-stream", "https://cdn.example/a.M3U8?token=x", true},
		{"video/mp2t", "https://cdn.example/segment.ts?playlist=a.m3u8", false},
		{"video/mp2t", "https://cdn.example/a.m3u8/segment.ts", false},
	} {
		if got := isMediaPlaylist(tt.mime, tt.url); got != tt.want {
			t.Errorf("isMediaPlaylist(%q, %q) = %v", tt.mime, tt.url, got)
		}
	}
}

func TestMediaPlaylistRedirectAndInvalidBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/entry.m3u8" {
			http.Redirect(w, r, "/final/playlist.m3u8", http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, "\ufeff#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"enc.key\"\n#EXTINF:6,\npart.ts\n")
	}))
	defer server.Close()
	res, err := server.Client().Get(server.URL + "/entry.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, base, err := readMediaPlaylist(res)
	if err != nil {
		t.Fatal(err)
	}
	rewritten := rewritePlaylist(body, base, func(s string) string { return s })
	if !strings.Contains(rewritten, server.URL+"/final/enc.key") || !strings.Contains(rewritten, server.URL+"/final/part.ts") {
		t.Fatalf("relative resource used pre-redirect base: %s", rewritten)
	}
	u, _ := url.Parse("https://cdn.example/test.m3u8")
	for _, invalid := range []string{"", "<html>Forbidden</html>", `{"error":"expired"}`, "#EXTM3U\n" + strings.Repeat("x", 4<<20)} {
		response := &http.Response{Body: io.NopCloser(strings.NewReader(invalid)), Request: &http.Request{URL: u}}
		if _, _, err := readMediaPlaylist(response); err == nil {
			t.Fatal("invalid or oversized upstream playlist was accepted")
		}
	}
}

func TestSharedPlaybackTransportPolicy(t *testing.T) {
	transport := playbackHTTPClient.Transport.(*http.Transport)
	if transport.TLSHandshakeTimeout <= 0 || transport.IdleConnTimeout <= 0 || transport.MaxIdleConnsPerHost < 2 {
		t.Fatal("playback must bound TLS setup and reuse segment connections")
	}
	if err := playbackHTTPClient.CheckRedirect(&http.Request{URL: &url.URL{Scheme: "http", Host: "127.0.0.1"}}, nil); err == nil {
		t.Fatal("shared transport allowed a redirect to a private host")
	}
	if err := safeMediaURL(context.Background(), "http://127.0.0.1/video.m3u8"); err == nil {
		t.Fatal("playback accepted private media URL")
	}
}
