package suxinvideo

import (
	"archive/zip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestJellyfinTokenProtocolsAndLANBoundary(t *testing.T) {
	for _, test := range []struct{ header, value, query, want string }{{"X-Emby-Token", "first", "api_key=other", "first"}, {"Authorization", `MediaBrowser Client="TV", DeviceId="abc", Token="secret"`, "", "secret"}, {"Authorization", "Bearer token", "", "token"}, {"X-Emby-Authorization", `MediaBrowser DeviceId="t1"`, "api_key=query", "query"}} {
		request := httptest.NewRequest("GET", "http://192.168.10.10:8601/Items?"+test.query, nil)
		request.Header.Set(test.header, test.value)
		if got := jellyfinToken(request); got != test.want {
			t.Fatalf("auth protocol %s: %q", test.header, got)
		}
	}
	for address, want := range map[string]bool{"192.168.10.25:1234": true, "127.0.0.1:8000": true, "[::1]:5500": true, "[::ffff:192.168.10.25]:3000": true, "61.144.183.217:1234": false, "8.8.8.8:5555": false, "invalid": false} {
		request := httptest.NewRequest("GET", "http://local/", nil)
		request.RemoteAddr = address
		if jellyfinLANRequest(request) != want {
			t.Errorf("LAN boundary %s", address)
		}
	}
}
func TestJellyfinAnonymousMediaAndPublicNetworkRejected(t *testing.T) {
	gateway := new(jellyfinGateway)
	for _, path := range []string{"/Items/m2164/PlaybackInfo", "/Videos/m2164/stream", "/suxinvideo/native/hls?token=secret", "/Users/u1/Items"} {
		r := httptest.NewRequest("GET", "http://192.168.10.10:8601"+path, nil)
		r.RemoteAddr = "192.168.10.25:51234"
		w := httptest.NewRecorder()
		gateway.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous %s status=%d", path, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "http://192.168.10.10:8601/System/Info/Public", nil)
	r.RemoteAddr = "61.144.183.217:51234"
	w := httptest.NewRecorder()
	gateway.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("public network accepted status=%d", w.Code)
	}
}
func TestJellyfinEpisodeIdentityAndProxyBoundary(t *testing.T) {
	id := jellyfinEpisodeID(2164, "episode:193", "season-1")
	if id != jellyfinEpisodeID(2164, "episode:193", "season-1") {
		t.Fatal("episode ID changed")
	}
	if id == jellyfinEpisodeID(2164, "episode:193", "season-2") {
		t.Fatal("seasons collided")
	}
	if id == jellyfinEpisodeID(2164, "episode:194", "season-1") {
		t.Fatal("episodes collided")
	}
	vod, err := jellyfinVodID(id)
	if err != nil || vod != 2164 {
		t.Fatal("stable episode mapping failed")
	}
	for _, path := range []string{"/admin/suxinvideo/save", "/suxinvideo/app/v1/me", "/suxinvideo/native/../../admin", "/suxinvideo/native/..\\admin", "/resource/static/../../manifest"} {
		if jellyfinProxyPathAllowed(path) {
			t.Errorf("proxy accepted unsafe path %q", path)
		}
	}
	for _, path := range []string{"/suxinvideo/native/hls", "/suxinvideo/app/v1/media", "/suxinvideo/image"} {
		if !jellyfinProxyPathAllowed(path) {
			t.Errorf("proxy rejected media %q", path)
		}
	}
}

func TestJellyfinStrictUUIDTypedReversibleIDs(t *testing.T) {
	for _, kind := range []byte{'u', 'm', 's', 'e', 'n', 'l'} {
		for _, native := range []int64{1, 2164, 4294967295} {
			raw := jellyfinTypedGUID(kind, native, "version:episode")
			id, err := uuid.Parse(raw)
			if err != nil || id.Version() != 5 || id.Variant() != uuid.RFC4122 {
				t.Fatal("outward ID is not a strict UUID")
			}
			parsedKind, parsedNative, valid := jellyfinParseID(raw)
			if !valid || parsedKind != kind || parsedNative != native {
				t.Fatal("typed UUID did not preserve the CMS uint32 identity")
			}
			parsedKind, parsedNative, valid = jellyfinParseID(strings.ReplaceAll(raw, "-", ""))
			if !valid || parsedKind != kind || parsedNative != native {
				t.Fatal("Jellyfin compact GUID form was not accepted")
			}
		}
	}
	if _, err := uuid.Parse(jellyfinLibraryID(0)); err != nil {
		t.Fatal("all-media library is not a constant Guid")
	}
	if !jellyfinUserMatches("u71", 71) || jellyfinUserMatches(jellyfinUserID(72), 71) {
		t.Fatal("legacy account identity or UUID cross-member rejection failed")
	}
	if _, err := jellyfinVodID(jellyfinUserID(71)); err == nil {
		t.Fatal("member UUID accepted as a movie")
	}
	legacyEpisode := "e2164-" + appHash("\x00episode:193")[:24]
	if !jellyfinEpisodeMatches(2164, "episode:193", "vod:2164", legacyEpisode) || jellyfinEpisodeMatches(2164, "episode:194", "vod:2164", legacyEpisode) || jellyfinEpisodeMatches(2164, "episode:193", "season:2", legacyEpisode) {
		t.Fatal("implicit legacy version compatibility crossed film/episode/season identities")
	}
	legacySeason := "n2164-" + appHash("")[:16]
	if !jellyfinSeasonMatches(2164, "vod:2164", legacySeason) || jellyfinSeasonMatches(2164, "season:2", legacySeason) {
		t.Fatal("legacy season compatibility crossed explicit versions")
	}
	for _, raw := range []string{"u4294967296", "m-1", "library-bad", "00000000-0000-0000-0000-000000000001"} {
		if _, _, valid := jellyfinParseID(raw); valid {
			t.Fatal("unrelated/malformed Guid or overflowing legacy ID accepted")
		}
	}
}
func TestClientPublisherRejectsInvalidAPKBeforeSDK(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "invalid.apk")
	file, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	entry, _ := archive.Create("payload.txt")
	_, _ = entry.Write([]byte(strings.Repeat("untrusted", 1024)))
	archive.Close()
	file.Close()
	if _, _, _, _, _, _, _, err = inspectClientAPK(context.Background(), name); err == nil {
		t.Fatal("non-Android archive accepted")
	}
}
func TestClientPublisherInspectsBuiltAPK(t *testing.T) {
	file := os.Getenv("SUXINVIDEO_TEST_APK")
	if file == "" {
		t.Skip("set SUXINVIDEO_TEST_APK to validate a built signed Android artifact")
	}
	size, hash, pkg, signer, version, sdk, name, err := inspectClientAPK(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	if size < 1024 || len(hash) != 64 || len(signer) != 64 || version < 1 || sdk != 23 || name == "" || (pkg != "com.xiaoqi.video" && pkg != "com.xiaoqi.video.tv") {
		t.Fatalf("invalid native APK metadata: package=%s version=%d sdk=%d", pkg, version, sdk)
	}
}
func TestJellyfinRewritesLocalHTTPSOnly(t *testing.T) {
	gateway := new(jellyfinGateway)
	r := httptest.NewRequest("GET", "http://192.168.10.10:8601/", nil)
	for raw, want := range map[string]string{
		"/suxinvideo/native/hls?token=t":                          "http://192.168.10.10:8601/suxinvideo/native/hls?token=t",
		"https://xq.suxinwl.com:8600/suxinvideo/app/v1/media?q=x": "http://192.168.10.10:8601/suxinvideo/app/v1/media?q=x",
		"https://XQ.SUXINWL.COM:8600/suxinvideo/app/v1/media?q=x": "http://192.168.10.10:8601/suxinvideo/app/v1/media?q=x",
		"https://xq.suxinwl.com.cdn.example.org:8600/file.m3u8":   "https://xq.suxinwl.com.cdn.example.org:8600/file.m3u8",
		"https://xq.suxinwl.com:9443/file.m3u8":                   "https://xq.suxinwl.com:9443/file.m3u8",
		"https://127.0.0.1:8600/suxinvideo/app/v1/media?q=x":      "http://192.168.10.10:8601/suxinvideo/app/v1/media?q=x",
		"https://cdn.example.org/file.m3u8":                       "https://cdn.example.org/file.m3u8",
	} {
		if got := gateway.toLAN(r, raw); got != want {
			t.Errorf("LAN rewrite %q -> %q", raw, got)
		}
	}
}

func TestJellyfinMediaMatchingUsesGatewayBackend(t *testing.T) {
	gateway := &jellyfinGateway{backend: "https://media.example.org:8600"}
	r := httptest.NewRequest("GET", "http://192.168.10.10:8601/", nil)
	const raw = "https://media.example.org:8600/suxinvideo/app/v1/media?q=x"
	if got := gateway.toLAN(r, raw); got != "http://192.168.10.10:8601/suxinvideo/app/v1/media?q=x" {
		t.Fatalf("backend media URL was not rewritten: %s", got)
	}
	if gateway.localMediaHost("xq.suxinwl.com:8600", r.Host) {
		t.Fatal("media matching did not follow the gateway backend")
	}
}
