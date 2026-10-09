package suxinvideo

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
)

// Explicit opt-in: an authenticated fixture owns only free loopback :9181.
// No runtime supervisor or CMS/database is started. Temporary cwd also isolates
// the engine secret and bridge files. Account ACLs are tested by the DB runner.
func TestLiveEngineControlledBridgeIntegration(t *testing.T) {
	if os.Getenv("SUXIN_LIVE_ENGINE_INTEGRATION") != "1" {
		t.Skip("set SUXIN_LIVE_ENGINE_INTEGRATION=1 with a free member engine port")
	}
	binary, err := ffmpegBinary()
	if err != nil {
		t.Fatal("FFmpeg is required for this integration gate")
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:9181")
	if err != nil {
		t.Fatal("controlled test refuses an occupied member engine port")
	}
	liveBridges.Lock()
	busy := len(liveBridges.items) != 0
	liveBridges.Unlock()
	if busy {
		listener.Close()
		t.Fatal("controlled test refuses existing media bridges")
	}
	t.Setenv("SUXIN_FFMPEG", binary)
	directory := t.TempDir()
	t.Chdir(directory)
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	command := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, binary, append([]string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y"}, args...)...)
		if output, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("controlled media generation/decode failed: %v %s", e, output)
		}
	}
	flv := filepath.Join(directory, "fixture.flv")
	mp4 := filepath.Join(directory, "fixture.mp4")
	command("-f", "lavfi", "-i", "testsrc2=size=160x90:rate=25", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100", "-t", "4", "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-g", "50", "-c:a", "aac", "-f", "flv", flv)
	command("-i", flv, "-t", "0.8", "-c", "copy", "-movflags", "+faststart", mp4)
	mp4Bytes, err := os.ReadFile(mp4)
	if err != nil || len(mp4Bytes) < 1024 {
		t.Fatal("finite MP4 fixture was not generated")
	}
	secret := liveEngineSecret()
	if len(secret) < 32 {
		t.Fatal("isolated engine credential was not generated")
	}
	const base = "http://127.0.0.1:9181/internal/media/"
	const flvPath = "/internal/media/11111111111111111111111111111111/index.flv"
	const mp4Path = "/internal/media/22222222222222222222222222222222/index.mp4"
	const hlsPath = "/internal/media/33333333333333333333333333333333/index.m3u8"
	const keyPath = "/internal/media/44444444444444444444444444444444/index.key"
	key := []byte("0123456789abcdef")
	keyFile := filepath.Join(directory, "key.bin")
	keyInfo := filepath.Join(directory, "key-info.txt")
	if err = os.WriteFile(keyFile, key, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(keyInfo, []byte("http://127.0.0.1:9181"+keyPath+"\n"+filepath.ToSlash(keyFile)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	command("-i", flv, "-c", "copy", "-hls_time", "2", "-hls_key_info_file", keyInfo, "-hls_segment_filename", filepath.Join(directory, "encrypted%d.ts"), filepath.Join(directory, "encrypted.m3u8"))
	manifest, err := os.ReadFile(filepath.Join(directory, "encrypted.m3u8"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(manifest), "\n") {
		if strings.HasSuffix(line, ".ts") {
			manifest = bytes.ReplaceAll(manifest, []byte(line), []byte(base+"55555555555555555555555555555555/"+line))
		}
	}
	var unauthorized, authenticated, encryptedSeen atomic.Int64
	upstream := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-IPTV-Secret") != secret {
			unauthorized.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		authenticated.Add(1)
		switch {
		case r.URL.Path == flvPath:
			w.Header().Set("Content-Type", "video/x-flv")
			w.WriteHeader(http.StatusOK)
			cmd := exec.CommandContext(r.Context(), binary, "-hide_banner", "-loglevel", "error", "-nostdin", "-re", "-stream_loop", "-1", "-i", flv, "-c", "copy", "-f", "flv", "pipe:1")
			cmd.Stdout = liveIntegrationFlushWriter{w}
			cmd.Stderr = io.Discard
			_ = cmd.Run()
		case r.URL.Path == mp4Path:
			w.Header().Set("Content-Type", "video/mp4")
			http.ServeContent(w, r, "fixture.mp4", time.Time{}, bytes.NewReader(mp4Bytes))
		case r.URL.Path == hlsPath:
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write(manifest)
		case r.URL.Path == keyPath:
			encryptedSeen.Add(1)
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(key)
		case strings.HasPrefix(r.URL.Path, "/internal/media/55555555555555555555555555555555/encrypted"):
			name := filepath.Base(r.URL.Path)
			if name != "encrypted0.ts" && name != "encrypted1.ts" && name != "encrypted2.ts" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			http.ServeFile(w, r, filepath.Join(directory, name))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})}
	go func() { _ = upstream.Serve(listener) }()
	t.Cleanup(func() {
		liveBridges.Lock()
		for id, bridge := range liveBridges.items {
			delete(liveBridges.items, id)
			_ = bridge.command.Process.Kill()
			bridge.release()
			_ = os.RemoveAll(bridge.directory)
		}
		liveBridges.Unlock()
		_ = upstream.Close()
	})
	stream := LiveStream{ID: 700001, SourceKind: "provider", ProviderKey: "member", ModuleKey: "controlled-fixture", AccessLevel: "member", SourceRevision: 1, URL: "http://127.0.0.1:9181" + flvPath, MediaType: "flv"}
	bridgeURL, err := liveBridgeAcquire(ctx, stream, LiveViewer{})
	if err != nil {
		t.Fatal("real FFmpeg FLV bridge failed", err)
	}
	sharedURL, err := liveBridgeAcquire(ctx, stream, LiveViewer{})
	if err != nil || sharedURL != bridgeURL {
		t.Fatal("the same permitted stream did not share its bridge")
	}
	initial, _, err := liveBridgeRead(bridgeURL)
	if err != nil {
		t.Fatal(err)
	}
	initialSequence := liveIntegrationSequence(t, initial)
	var window []byte
	for {
		window, _, err = liveBridgeRead(bridgeURL)
		if err != nil {
			t.Fatal(err)
		}
		if liveIntegrationSequence(t, window) > initialSequence {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("live HLS window did not advance")
		case <-time.After(250 * time.Millisecond):
		}
	}
	if bytes.Contains(window, []byte("#EXT-X-ENDLIST")) || bytes.Count(window, []byte("#EXTINF:")) > 6 {
		t.Fatal("live bridge lost the bounded moving HLS window")
	}
	segmentName := ""
	for _, line := range strings.Split(string(window), "\n") {
		if strings.HasSuffix(line, ".ts") {
			segmentName = line
			break
		}
	}
	segment, _, err := liveBridgeRead(strings.TrimSuffix(bridgeURL, "index.m3u8") + segmentName)
	if err != nil || len(segment) < 188 || segment[0] != 0x47 {
		t.Fatal("bridge returned no actual MPEG-TS packets")
	}
	segmentFile := filepath.Join(directory, "bridge.ts")
	if err = os.WriteFile(segmentFile, segment, 0600); err != nil {
		t.Fatal(err)
	}
	command("-i", segmentFile, "-t", "1", "-f", "null", "-")
	second := stream
	second.ID++
	if _, err = liveBridgeAcquire(ctx, second, LiveViewer{}); err != nil {
		t.Fatal("second authorized bridge failed", err)
	}
	third := second
	third.ID++
	if _, err = liveBridgeAcquire(ctx, third, LiveViewer{}); err == nil {
		t.Fatal("a third FFmpeg bridge escaped the host limit")
	}
	hls := stream
	hls.URL = "http://127.0.0.1:9181" + hlsPath
	hls.MediaType = "hls"
	body, manifestBase, err := liveManifest(ctx, hls, hls.URL)
	if err != nil {
		t.Fatal("controlled encrypted HLS manifest failed", err)
	}
	item := &liveMediaSession{Assets: map[string]liveAsset{}, Mode: "event_replay"}
	rewritten := rewritePlaylist(body, manifestBase, func(raw string) string { return item.link("controlled", raw, false) })
	if strings.Contains(rewritten, "127.0.0.1") || strings.Contains(rewritten, secret) || !strings.Contains(rewritten, "#EXT-X-KEY:") {
		t.Fatal("AES HLS exposed engine URLs or lost its key")
	}
	command("-headers", "X-IPTV-Secret: "+secret+"\r\n", "-i", hls.URL, "-t", "1", "-f", "null", "-")
	if encryptedSeen.Load() == 0 {
		t.Fatal("real HLS decoder did not fetch the authenticated AES key")
	}
	replay := stream
	replay.URL = "http://127.0.0.1:9181" + mp4Path
	replay.MediaType = "mp4"
	invalidRange := fmt.Sprintf("bytes=%d-", len(mp4Bytes)+1)
	rangeError, err := liveEngineRequest(ctx, replay, replay.URL, invalidRange, time.Second)
	if err != nil || rangeError.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatal("fixture did not reject an out-of-bounds MP4 Range")
	}
	rangeErrorBytes, _ := io.ReadAll(rangeError.Body)
	rangeError.Body.Close()
	front := mediaBinaryHTTPServer(t, func(group *ghttp.RouterGroup) {
		group.ALL("/replay", func(r *ghttp.Request) { liveFiniteMedia(r, item, replay, replay.URL) })
	})
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	for _, tc := range []struct {
		method, byteRange string
		status            int
		want              []byte
	}{
		{http.MethodGet, "", 200, mp4Bytes},
		{http.MethodGet, "bytes=0-1023", 206, mp4Bytes[:1024]},
		{http.MethodHead, "bytes=0-1023", 206, nil},
		{http.MethodGet, invalidRange, 416, rangeErrorBytes},
	} {
		req, _ := http.NewRequest(tc.method, front+"/replay", nil)
		req.Header.Set("Range", tc.byteRange)
		response, e := client.Do(req)
		if e != nil {
			t.Fatal("finite replay HTTP request failed", e)
		}
		result, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil || response.StatusCode != tc.status || !bytes.Equal(result, tc.want) {
			t.Fatalf("finite %s status=%d bytes=%d want=%d read=%v length=%d encoding=%q range=%q prefix=%x: binary response/Range changed", tc.method, response.StatusCode, len(result), len(tc.want), readErr, response.ContentLength, response.Header.Get("Content-Encoding"), response.Header.Get("Content-Range"), result[:min(len(result), 16)])
		}
		expectedRange := fmt.Sprintf("bytes 0-1023/%d", len(mp4Bytes))
		if tc.status == 416 {
			expectedRange = fmt.Sprintf("bytes */%d", len(mp4Bytes))
		}
		if tc.byteRange != "" && response.Header.Get("Content-Range") != expectedRange {
			t.Fatal("finite MP4 Range metadata changed")
		}
	}
	command("-i", mp4, "-f", "null", "-")
	if unauthorized.Load() != 0 || authenticated.Load() == 0 {
		t.Fatal("a fixture request omitted its internal credential")
	}
	liveBridges.Lock()
	for _, bridge := range liveBridges.items {
		bridge.touched = time.Now().Add(-61 * time.Second)
	}
	liveBridges.Unlock()
	idleDeadline := time.Now().Add(12 * time.Second)
	for {
		liveBridges.Lock()
		remaining := len(liveBridges.items)
		liveBridges.Unlock()
		if remaining == 0 {
			break
		}
		if time.Now().After(idleDeadline) {
			t.Fatal("idle bridge cleanup failed to release the bounded pipeline")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if _, _, err = liveBridgeRead(bridgeURL); err == nil {
		t.Fatal("an idle released bridge remained readable")
	}
	t.Log("real FLV→sliding HLS decoded; shared bridge limit 2; AES HLS decoded; finite MP4 full/Range/HEAD/416 exact; idle bridges released")
}

type liveIntegrationFlushWriter struct{ http.ResponseWriter }

func (w liveIntegrationFlushWriter) Write(value []byte) (int, error) {
	n, err := w.ResponseWriter.Write(value)
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
	return n, err
}

func liveIntegrationSequence(t *testing.T, body []byte) int {
	t.Helper()
	for _, line := range strings.Split(string(body), "\n") {
		if value, ok := strings.CutPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"); ok {
			sequence, err := strconv.Atoi(value)
			if err == nil {
				return sequence
			}
		}
	}
	t.Fatal("real FFmpeg bridge omitted HLS media sequence")
	return 0
}

func TestLiveFinitePlaylistRetainsLongSeekTargetsAndBounds(t *testing.T) {
	for _, mode := range []string{"catchup", "event_replay"} {
		t.Run(mode, func(t *testing.T) {
			item := &liveMediaSession{Mode: mode, Assets: map[string]liveAsset{}}
			var manifest strings.Builder
			manifest.WriteString("#EXTM3U\n#EXT-X-PLAYLIST-TYPE:VOD\n")
			for index := 0; index < 8000; index++ {
				fmt.Fprintf(&manifest, "#EXTINF:2,\npart%d.ts\n", index)
			}
			manifest.WriteString("#EXT-X-ENDLIST\n")
			for round := 0; round < 2; round++ {
				rewritten := rewritePlaylist(manifest.String(), "http://127.0.0.1:9181/internal/media/finite/index.m3u8", func(raw string) string { return item.link("finite", raw, false) })
				if len(item.Assets) != 8000 || strings.Contains(rewritten, "127.0.0.1") || !strings.Contains(rewritten, "#EXT-X-ENDLIST") {
					t.Fatal("long finite manifest lost its seek targets or opaque transport")
				}
				for _, index := range []int{0, 7999, 0} {
					link := item.link("finite", fmt.Sprintf("http://127.0.0.1:9181/internal/media/finite/part%d.ts", index), false)
					parsed, _ := url.Parse(link)
					asset, ok := item.Assets[parsed.Query().Get("asset")]
					if !ok || !strings.HasSuffix(asset.URL, fmt.Sprintf("/part%d.ts", index)) {
						t.Fatal("forward/back seek could not find its finite segment")
					}
				}
				for key, row := range item.Assets {
					row.Seen = time.Now().Add(-11 * time.Minute)
					item.Assets[key] = row
				}
			}
			for index := len(item.Assets); index < 50000; index++ {
				item.Assets[fmt.Sprintf("cap-%d", index)] = liveAsset{URL: "finite", Seen: time.Now()}
			}
			item.link("finite", "http://127.0.0.1:9181/internal/media/finite/cap.ts", false)
			if len(item.Assets) > 50000 {
				t.Fatal("finite asset budget grew beyond the supported manifest cap")
			}
		})
	}
}
