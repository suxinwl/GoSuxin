package suxinvideo

import (
	"context"
	"strings"
	"testing"
)

func TestLiveEngineMediaAddressBoundary(t *testing.T) {
	source := LiveStream{SourceKind: "provider", ProviderKey: "public"}
	for _, raw := range []string{"http://127.0.0.1:9180/internal/media/abc/index.m3u8", "http://127.0.0.1:9180/internal/media/opaque.ts"} {
		if !liveEngineMediaURL(source, raw) {
			t.Fatal("fixed opaque origin rejected")
		}
	}
	for _, raw := range []string{"http://127.0.0.1:9181/internal/media/a", "http://localhost:9180/internal/media/a", "http://127.0.0.1:9180/api/extractors", "http://127.0.0.1:9180/internal/media/../health", "http://127.0.0.1:9180/internal/media/%2e%2e/health", "http://127.0.0.1:9180/internal/media/a%2fb", "http://user:secret@127.0.0.1:9180/internal/media/a", "https://127.0.0.1:9180/internal/media/a", "http://192.168.10.10:9180/internal/media/a"} {
		if liveEngineMediaURL(source, raw) {
			t.Fatal("engine origin boundary escaped")
		}
	}
	if liveEngineMediaURL(LiveStream{SourceKind: "url", ProviderKey: "public"}, "http://127.0.0.1:9180/internal/media/a") {
		t.Fatal("imported stream acquired engine private exemption")
	}
	if safeCollectorURL(context.Background(), "http://127.0.0.1:9180/internal/media/a") == nil {
		t.Fatal("ordinary source URL policy weakened")
	}
}
func TestLiveBridgePlaylistReferencesStayOpaque(t *testing.T) {
	manifest := "#EXTM3U\n#EXT-X-MEDIA-SEQUENCE:35\n#EXT-X-TARGETDURATION:2\n#EXTINF:2,\npart000000035.ts\n"
	result := rewritePlaylist(manifest, "livebridge://opaque/index.m3u8", func(raw string) string {
		if raw != "livebridge://opaque/part000000035.ts" {
			t.Fatal("bridge path changed")
		}
		return "/suxinvideo/live/media?session=fixture&asset=opaque"
	})
	if strings.Contains(result, "livebridge://") || strings.Contains(result, "#EXT-X-ENDLIST") || !strings.Contains(result, "#EXT-X-MEDIA-SEQUENCE:35") {
		t.Fatal("dynamic bridge window changed")
	}
	for _, name := range []string{"../internal.secret", "part1.ts/../secret", "part1.ts?x=1", "index.m3u8.tmp"} {
		if liveBridgeName.MatchString(name) {
			t.Fatal("bridge filesystem path escaped")
		}
	}
}
