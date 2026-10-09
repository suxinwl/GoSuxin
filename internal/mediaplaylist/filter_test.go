package mediaplaylist

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
)

func vod(lines ...string) string {
	return "#EXTM3U\n#EXT-X-TARGETDURATION:10\n" + strings.Join(lines, "\n") + "\n#EXT-X-ENDLIST\n"
}
func assertUnchanged(t *testing.T, raw string) {
	t.Helper()
	got := Filter(raw, "https://media.example/path/index.m3u8", nil)
	if got.Playlist != raw || got.Removed != 0 || got.RemovedSeconds != 0 {
		t.Fatalf("ambiguous or ordinary input changed: %+v", got)
	}
}
func TestNormalizeDomains(t *testing.T) {
	got, err := NormalizeDomains(" Ad.Example.com\r\nad.example.com\tother.example.org ")
	if err != nil || got != "ad.example.com\nother.example.org" {
		t.Fatalf("%q %v", got, err)
	}
	for _, raw := range []string{"", " \r\n\t"} {
		if got, err := NormalizeDomains(raw); err != nil || got != "" {
			t.Fatalf("empty: %q %v", got, err)
		}
	}
	for _, raw := range []string{"https://ad.example.com", "*.example.com", ".", ".example.com", "ad..com", "localhost", "127.0.0.1", "::1", "[::1]", "example.com:443", "example.com/path", "example.com?x", "example.com.", "-bad.com", "bad-.com", "ad.公司", "foo_bar.com", "example.123"} {
		if _, err := NormalizeDomains(raw); err == nil {
			t.Errorf("accepted invalid domain %q", raw)
		}
	}
	var many []string
	for i := 0; i < 65; i++ {
		many = append(many, fmt.Sprintf("ad%d.example.com", i))
	}
	if _, err := NormalizeDomains(strings.Join(many, "\n")); err == nil {
		t.Fatal("accepted over 64 domains")
	}
}
func TestCompleteCueInterval(t *testing.T) {
	raw := vod("#EXT-X-VERSION:3", "#EXTINF:5,", "479.ts",
		"#EXT-X-DISCONTINUITY", "#EXT-X-CUE-OUT:DURATION=14.866667",
		"#EXTINF:5,", "video2/slice/367/index0.ts", "#EXTINF:5,", "video2/slice/367/index1.ts",
		"#EXTINF:4.866667,", "video2/slice/367/index2.ts",
		"#EXT-X-CUE-IN", "#EXT-X-DISCONTINUITY", "#EXTINF:5,", "480.ts")
	got := Filter(raw, "https://media.example/1080/master.m3u8", nil)
	if got.Removed != 3 || math.Abs(got.RemovedSeconds-14.866667) > 1e-8 {
		t.Fatalf("%+v", got)
	}
	if strings.Contains(got.Playlist, "video2/") || strings.Contains(got.Playlist, "CUE-") {
		t.Fatal(got.Playlist)
	}
	if !strings.Contains(got.Playlist, "479.ts\n#EXT-X-DISCONTINUITY\n#EXTINF:5,\n480.ts") {
		t.Fatal(got.Playlist)
	}
	if strings.Count(got.Playlist, "#EXTINF:") != 2 {
		t.Fatal(got.Playlist)
	}
	again := Filter(got.Playlist, "", nil)
	if again.Playlist != got.Playlist || again.Removed != 0 {
		t.Fatal("filter is not idempotent")
	}
}

func TestCompletedEventCueInterval(t *testing.T) {
	// The APP labels its finalized movie EVENT, including a final ENDLIST.
	raw := vod("#EXT-X-VERSION:3", "#EXT-X-PLAYLIST-TYPE:EVENT", "#EXTINF:5,", "479.ts",
		"#EXT-X-DISCONTINUITY", "#EXT-X-CUE-OUT:DURATION=14.866667",
		"#EXTINF:5,", "video2/slice/367/index0.ts", "#EXTINF:5,", "video2/slice/367/index1.ts",
		"#EXTINF:4.866667,", "video2/slice/367/index2.ts",
		"#EXT-X-CUE-IN", "#EXT-X-DISCONTINUITY", "#EXTINF:5,", "480.ts")
	got := Filter(raw, "https://media.example/1080/master.m3u8", nil)
	if got.Removed != 3 || math.Abs(got.RemovedSeconds-14.866667) > 1e-8 || strings.Contains(got.Playlist, "video2/") {
		t.Fatalf("completed EVENT was not filtered: removed=%d seconds=%f", got.Removed, got.RemovedSeconds)
	}
	assertUnchanged(t, strings.ReplaceAll(raw, "#EXT-X-ENDLIST\n", ""))
}

func TestSavedAPPPlaylistFixture(t *testing.T) {
	// Opt-in read-only check of a local capture; never print signed media URLs.
	path := os.Getenv("SUXIN_HLS_SAVED_APP_FIXTURE")
	if path == "" {
		t.Skip("local fixture check is opt-in")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("cannot read local APP fixture")
	}
	got := Filter(string(raw), "https://media.example/1080/master.m3u8", nil)
	if got.Removed != 3 || math.Abs(got.RemovedSeconds-14.866667) > 1e-8 {
		t.Fatalf("saved APP fixture: removed=%d seconds=%f", got.Removed, got.RemovedSeconds)
	}
	t.Logf("saved APP fixture: removed=%d seconds=%.6f retained=%d", got.Removed, got.RemovedSeconds, strings.Count(got.Playlist, "#EXTINF:"))
}
func TestNoHeuristicRemovalAndByteIdentity(t *testing.T) {
	raw := vod("#EXT-X-VERSION:3", "#EXTINF:6.666667,", "clip1o.ts",
		"#EXT-X-DISCONTINUITY", "#EXTINF:8,", "https://cdn.bytegoofy.com/normal/adventure.ts?ad=1o&sign=example",
		"#EXTINF:0.5,", "https://media.example/video/advert/main.ts",
		"#EXTINF:5,", "https://media.example/xadjump/y.ts")
	assertUnchanged(t, raw)
	assertUnchanged(t, strings.ReplaceAll(raw, "\n", "\r\n"))
	assertUnchanged(t, strings.TrimSuffix(raw, "\n"))
}
func TestDomainsAndPathBoundaries(t *testing.T) {
	remove := []string{
		"https://ffzyad.com/a.ts", "https://sub.ffzyad.com/a.ts",
		"https://media.example/adjump/a.ts", "https://media.example/video/ad/a.ts",
		"https://ads.example.org/a.ts", "https://sub.ads.example.org/a.ts",
	}
	keep := []string{
		"https://ffzyad.com.example.net/a.ts", "https://notffzyad.com/a.ts",
		"https://media.example/file.ts?redirect=ffzyad.com", "https://media.example/adjumping/a.ts",
		"https://media.example/video/advert/a.ts", "https://media.example/video/ad",
		"https://evilads.example.org/a.ts", "https://ads.example.org.evil.net/a.ts",
	}
	lines := []string{}
	for _, uri := range append(append([]string{}, remove...), keep...) {
		lines = append(lines, "#EXTINF:5,", uri)
	}
	got := Filter(vod(lines...), "", []string{"ads.example.org"})
	if got.Removed != len(remove) {
		t.Fatalf("removed %d\n%s", got.Removed, got.Playlist)
	}
	for _, uri := range keep {
		if !strings.Contains(got.Playlist, uri) {
			t.Errorf("lost %s", uri)
		}
	}
	raw := vod("#EXTINF:5,", "//sub.ffzyad.com/ad.ts", "#EXTINF:5,", "/content.ts")
	if Filter(raw, "https://media.example/index.m3u8", nil).Removed != 1 {
		t.Fatal("protocol-relative domain not recognized")
	}
}
func TestConservativeUnsupportedAndMalformed(t *testing.T) {
	good := vod("#EXTINF:5,", "main.ts", "#EXT-X-CUE-OUT:5", "#EXTINF:5,", "promo.ts", "#EXT-X-CUE-IN", "#EXTINF:5,", "end.ts")
	cases := map[string]string{
		"live":               strings.ReplaceAll(good, "#EXT-X-ENDLIST\n", ""),
		"master":             "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=800000\nhttps://ffzyad.com/index.m3u8\n#EXT-X-ENDLIST\n",
		"ll":                 strings.Replace(good, "#EXTINF:5,", "#EXT-X-PART:DURATION=1,URI=\"part.ts\"\n#EXTINF:5,", 1),
		"unclosed":           strings.ReplaceAll(good, "#EXT-X-CUE-IN\n", ""),
		"unopened":           strings.ReplaceAll(good, "#EXT-X-CUE-OUT:5\n", ""),
		"nested":             strings.Replace(good, "#EXT-X-CUE-OUT:5", "#EXT-X-CUE-OUT:5\n#EXT-X-CUE-OUT:5", 1),
		"duration mismatch":  strings.Replace(good, "#EXT-X-CUE-OUT:5", "#EXT-X-CUE-OUT:50", 1),
		"unknown tag":        strings.Replace(good, "#EXTINF:5,", "#EXT-X-UNKNOWN:1\n#EXTINF:5,", 1),
		"daterange":          strings.Replace(good, "#EXTINF:5,", "#EXT-X-DATERANGE:ID=\"ad\",CLASS=\"commercial\"\n#EXTINF:5,", 1),
		"invalid inf":        strings.Replace(good, "#EXTINF:5,", "#EXTINF:NaN,", 1),
		"double inf":         strings.Replace(good, "#EXTINF:5,", "#EXTINF:5,\n#EXTINF:5,", 1),
		"sample aes":         strings.Replace(good, "#EXTINF:5,", "#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"key.bin\"\n#EXTINF:5,", 1),
		"invalid key":        strings.Replace(good, "#EXTINF:5,", "#EXT-X-KEY:METHOD=AES-128\n#EXTINF:5,", 1),
		"drm format":         strings.Replace(good, "#EXTINF:5,", "#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\",KEYFORMAT=\"com.apple.streamingkeydelivery\"\n#EXTINF:5,", 1),
		"implicit map IV":    strings.Replace(good, "#EXTINF:5,", "#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:5,", 1),
		"implicit map range": strings.Replace(good, "#EXTINF:5,", "#EXT-X-MAP:URI=\"init.mp4\",BYTERANGE=\"100\"\n#EXTINF:5,", 1),
		"all ads":            vod("#EXTINF:5,", "https://ffzyad.com/a.ts", "#EXTINF:5,", "https://ffzyad.com/b.ts"),
		"all cue":            vod("#EXT-X-CUE-OUT", "#EXTINF:5,", "ad.ts", "#EXT-X-CUE-IN"),
		"empty":              "#EXTM3U\n#EXT-X-TARGETDURATION:10\n#EXT-X-ENDLIST\n",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) { assertUnchanged(t, raw) })
	}
	if got := Filter(good, "https://media.example/index.m3u8", []string{"*"}); got.Playlist != good {
		t.Fatal("invalid domain configuration changed input")
	}
}
func TestOriginalAESSequenceAndDecryption(t *testing.T) {
	raw := vod("#EXT-X-MEDIA-SEQUENCE:20", "#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"",
		"#EXTINF:5,", "main0.ts", "#EXT-X-CUE-OUT:5", "#EXTINF:5,", "ad.ts", "#EXT-X-CUE-IN",
		"#EXTINF:5,", "main1.ts")
	got := Filter(raw, "", nil)
	if got.Removed != 1 || !strings.Contains(got.Playlist, "#EXT-X-VERSION:2") {
		t.Fatal(got.Playlist)
	}
	// Independently decrypt blocks encrypted using the original sequence. After
	// removing seq21, the second kept block must STILL use IV22, not IV21.
	key := []byte("0123456789abcdef")
	plain := []byte("video TS content")
	block, _ := aes.NewCipher(key)
	sequences := map[string]uint64{"main0.ts": 20, "main1.ts": 22}
	var currentIV []byte
	for _, line := range strings.Split(got.Playlist, "\n") {
		if strings.HasPrefix(line, "#EXT-X-KEY:") {
			_, iv, found := strings.Cut(line, ",IV=0x")
			if !found {
				t.Fatal("no explicit IV")
			}
			currentIV, _ = hex.DecodeString(iv)
		}
		if seq, kept := sequences[line]; kept {
			originalIV := make([]byte, 16)
			binary.BigEndian.PutUint64(originalIV[8:], seq)
			ciphertext := make([]byte, 16)
			cipher.NewCBCEncrypter(block, originalIV).CryptBlocks(ciphertext, plain)
			decrypted := make([]byte, 16)
			cipher.NewCBCDecrypter(block, currentIV).CryptBlocks(decrypted, ciphertext)
			if !bytes.Equal(decrypted, plain) {
				t.Fatalf("wrong IV for %s: %x", line, currentIV)
			}
		}
	}
}
func TestKeyAndMapStateRestoredAcrossRemoval(t *testing.T) {
	raw := vod("#EXT-X-VERSION:6", "#EXT-X-KEY:METHOD=AES-128,URI=\"first.key\",IV=0x1",
		"#EXT-X-MAP:URI=\"init-a.mp4\",BYTERANGE=\"100@0\"", "#EXTINF:5,", "first.m4s",
		"#EXT-X-CUE-OUT:5", "#EXT-X-KEY:METHOD=NONE", "#EXT-X-MAP:URI=\"ad-init.mp4\"",
		"#EXTINF:5,", "ad.m4s", "#EXT-X-KEY:METHOD=AES-128,URI=\"new.key\",IV=0x2",
		"#EXT-X-MAP:URI=\"init-b.mp4\"", "#EXT-X-KEY:METHOD=AES-128,URI=\"segment.key\"",
		"#EXT-X-CUE-IN", "#EXTINF:5,", "last.m4s")
	got := Filter(raw, "", nil)
	if got.Removed != 1 || strings.Contains(got.Playlist, "ad-init") {
		t.Fatal(got.Playlist)
	}
	want := "#EXT-X-KEY:METHOD=AES-128,URI=\"new.key\",IV=0x2\n#EXT-X-MAP:URI=\"init-b.mp4\"\n#EXT-X-KEY:METHOD=AES-128,URI=\"segment.key\",IV=0x00000000000000000000000000000002\n#EXTINF:5,\nlast.m4s"
	if !strings.Contains(got.Playlist, want) {
		t.Fatal(got.Playlist)
	}
	// Identical MAP URI with a different declared key must not be suppressed.
	raw = strings.ReplaceAll(raw, "init-b.mp4", "init-a.mp4")
	raw = strings.ReplaceAll(raw, "URI=\"init-a.mp4\",BYTERANGE=\"100@0\"", "URI=\"init-a.mp4\"")
	got = Filter(raw, "", nil)
	if strings.Count(got.Playlist, "#EXT-X-MAP:URI=\"init-a.mp4\"") != 2 {
		t.Fatal(got.Playlist)
	}
}
func TestByteRangeOriginalOffsets(t *testing.T) {
	raw := vod("#EXT-X-VERSION:4", "#EXTINF:5,", "#EXT-X-BYTERANGE:100@10", "all.ts",
		"#EXT-X-CUE-OUT:5", "#EXTINF:5,", "#EXT-X-BYTERANGE:50", "all.ts",
		"#EXT-X-CUE-IN", "#EXTINF:5,", "#EXT-X-BYTERANGE:200", "all.ts")
	got := Filter(raw, "", nil)
	if got.Removed != 1 || !strings.Contains(got.Playlist, "#EXT-X-BYTERANGE:200@160") {
		t.Fatal(got.Playlist)
	}
	for name, bad := range map[string]string{
		"no predecessor": strings.Replace(raw, "100@10", "100", 1),
		"changed URI":    strings.Replace(raw, "all.ts", "other.ts", 1),
		"overflow":       strings.Replace(raw, "100@10", "100@18446744073709551610", 1),
	} {
		t.Run(name, func(t *testing.T) { assertUnchanged(t, bad) })
	}
}
func TestVerifiedMaoyanFilesAndAESKeyRestoration(t *testing.T) {
	base := "https://play.maoyanplay.top/20260818/JMQDkGOL/hls/"
	lines := []string{"#EXT-X-MEDIA-SEQUENCE:100", "#EXT-X-KEY:METHOD=AES-128,URI=\"movie.key\""}
	for i := 0; i < 3; i++ {
		lines = append(lines, "#EXTINF:5,", fmt.Sprintf("main%d.ts", i),
			"#EXT-X-DISCONTINUITY", "#EXT-X-KEY:METHOD=NONE",
			"#EXTINF:5,", base+"9OG1QJKR.ts?expires=example",
			"#EXTINF:5,", base+"Dgaf3EqV.ts", "#EXTINF:3.2,", base+"Nr4MRgx5.ts",
			"#EXT-X-DISCONTINUITY", "#EXT-X-KEY:METHOD=AES-128,URI=\"movie.key\"")
	}
	lines = append(lines, "#EXTINF:5,", "last.ts")
	got := Filter(vod(lines...), "https://play.maoyanplay.top/film/index.m3u8", nil)
	if got.Removed != 9 || math.Abs(got.RemovedSeconds-39.6) > 1e-8 {
		t.Fatalf("%+v", got)
	}
	if strings.Contains(got.Playlist, "JMQDkGOL") || strings.Contains(got.Playlist, "METHOD=NONE") {
		t.Fatal(got.Playlist)
	}
	for _, seq := range []int{100, 104, 108, 112} {
		if !strings.Contains(got.Playlist, fmt.Sprintf("IV=0x%032x", seq)) {
			t.Fatal(got.Playlist)
		}
	}
	neighbors := []string{
		"https://other.example/20260818/JMQDkGOL/hls/9OG1QJKR.ts",
		"https://sub.play.maoyanplay.top/20260818/JMQDkGOL/hls/9OG1QJKR.ts",
		base + "other.ts", "https://play.maoyanplay.top/20260819/JMQDkGOL/hls/9OG1QJKR.ts",
		"https://play.maoyanplay.top/20260818/other/hls/9OG1QJKR.ts",
	}
	var keep []string
	for _, uri := range neighbors {
		keep = append(keep, "#EXTINF:5,", uri)
	}
	assertUnchanged(t, vod(keep...))
}

func TestVerifiedErciyuanCommercialOnly(t *testing.T) {
	const directory = "/2026-10-01/41491_YTojUj5qXO8reGCMct/3000k/hls/"
	const base = "https://vip17.jimxtc.com" + directory
	const first = "39eef675e8ec73b3b567661ff036fbc2.ts"
	const second = "fe29245fba20e2312e18b426fe063138.ts"
	const third = "4181392cff3e2cbdee21449b5d972063.ts"
	// Actual source durations and adjacent film files. Query strings do not
	// change the identity of the reviewed resource, and relative URIs resolve
	// against the final CDN playlist URL before classification.
	raw := vod("#EXT-X-DISCONTINUITY", "#EXTINF:4.0,", "7c436f334ff26d299734962fc8c5a8ca.ts",
		"#EXT-X-DISCONTINUITY", "#EXTINF:4.6,", first+"?expires=example",
		"#EXTINF:5.533,", base+second, "#EXTINF:0.233,", third,
		"#EXT-X-DISCONTINUITY", "#EXTINF:4.52,", "a244f5a37f97f577c8c6a594bfe897b9.ts")
	got := Filter(raw, base+"index.m3u8", nil)
	if got.Removed != 3 || math.Abs(got.RemovedSeconds-10.366) > 1e-8 {
		t.Fatalf("unexpected reviewed ad removal: %+v", got)
	}
	for _, filename := range []string{first, second, third} {
		if strings.Contains(got.Playlist, filename) {
			t.Fatalf("reviewed advertisement still present: %s", filename)
		}
	}
	if !strings.Contains(got.Playlist, "#EXTINF:4.0,") || !strings.Contains(got.Playlist, "#EXTINF:4.52,") ||
		strings.Count(got.Playlist, "#EXT-X-DISCONTINUITY") != 2 {
		t.Fatal("normal boundary clips or their timing were changed")
	}
	// A different host, film directory, bitrate, or nearby clip is not evidence
	// of an advertisement, including unrelated 10.366-second film shots.
	for _, uri := range []string{
		"https://other.example" + directory + first,
		"https://sub.vip17.jimxtc.com" + directory + first,
		strings.Replace(base+first, "41491_", "other_", 1),
		strings.Replace(base+first, "3000k", "2000k", 1),
		base + "normal-film.ts",
	} {
		input := vod("#EXT-X-DISCONTINUITY", "#EXTINF:10.366,", uri, "#EXT-X-DISCONTINUITY", "#EXTINF:4,", "normal.ts")
		if result := Filter(input, base+"index.m3u8", nil); result.Removed != 0 || result.Playlist != input {
			t.Fatalf("unreviewed clip changed: %s", uri)
		}
	}
}
