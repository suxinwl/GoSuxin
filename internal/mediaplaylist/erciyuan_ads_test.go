package mediaplaylist

import (
	"strings"
	"testing"
)

func candidatePlaylist(count int) string {
	var lines []string
	for i := 0; i < count; i++ {
		lines = append(lines, "#EXT-X-DISCONTINUITY", "#EXTINF:4.6,", "a.ts",
			"#EXTINF:5.533,", "b.ts", "#EXTINF:0.233,", "c.ts")
	}
	lines = append(lines, "#EXT-X-DISCONTINUITY", "#EXTINF:5,", "film.ts")
	return vod(lines...)
}

func TestErciyuanCandidatesRequireCompleteReviewedCDNBlock(t *testing.T) {
	const base = "https://vip17.jimxtc.com/another-film/index.m3u8"
	raw := candidatePlaylist(3)
	blocks := ErciyuanAdCandidates(raw, base)
	if len(blocks) != 2 || blocks[0][0].Bytes != 1601760 ||
		blocks[0][0].URL != "https://vip17.jimxtc.com/another-film/a.ts" || blocks[0][2].Bytes != 166380 {
		t.Fatalf("incorrect candidate bounds/resources: %+v", blocks)
	}
	// Candidate discovery alone changes no bytes, even with identical durations.
	if result := FilterVerified(raw, base, nil, nil); result.Playlist != raw || result.Removed != 0 {
		t.Fatal("a duration-only candidate removed normal footage")
	}
	for name, input := range map[string]string{
		"live":           strings.ReplaceAll(raw, "#EXT-X-ENDLIST\n", ""),
		"master":         "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=3000000\nvariant.m3u8\n",
		"wrong duration": strings.ReplaceAll(raw, "5.533", "5.534"),
		"no start":       strings.ReplaceAll(raw, "#EXT-X-DISCONTINUITY\n", ""),
		"four clips":     strings.ReplaceAll(raw, "c.ts\n", "c.ts\n#EXTINF:5,\nmain.ts\n"),
		"encrypted":      strings.ReplaceAll(raw, "#EXTINF:4.6,", "#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\n#EXTINF:4.6,"),
		"map":            strings.ReplaceAll(raw, "#EXTINF:4.6,", "#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:4.6,"),
		"range":          strings.ReplaceAll(raw, "#EXTINF:4.6,", "#EXTINF:4.6,\n#EXT-X-BYTERANGE:10@0"),
		"foreign CDN":    strings.ReplaceAll(raw, "b.ts", "https://other.example/b.ts"),
		"wrong folder":   strings.ReplaceAll(raw, "b.ts", "other/b.ts"),
	} {
		t.Run(name, func(t *testing.T) {
			if got := ErciyuanAdCandidates(input, base); len(got) != 0 {
				t.Fatalf("ambiguous candidate accepted: %+v", got)
			}
		})
	}
	for _, invalid := range []string{"https://other.example/index.m3u8", "https://sub.vip17.jimxtc.com/index.m3u8", "ftp://vip17.jimxtc.com/index.m3u8"} {
		if len(ErciyuanAdCandidates(raw, invalid)) != 0 {
			t.Fatal("unreviewed origin accepted")
		}
	}
}

func TestVerifiedErciyuanPathsSkipPayloadReads(t *testing.T) {
	for _, dir := range []struct{ folder, first, second, third string }{
		{"2026-10-01/41491_YTojUj5qXO8reGCMct", "39eef675e8ec73b3b567661ff036fbc2.ts", "fe29245fba20e2312e18b426fe063138.ts", "4181392cff3e2cbdee21449b5d972063.ts"},
		{"2026-09-27/40945_DK_c95_43k7DfvzlrT", "8954ad7b8ad06fa9a423119287456cfd.ts", "a3e12149d4deb23609c0d49c1d1b8ef9.ts", "b1661f6de2c31e2f56185960bf44c819.ts"},
	} {
		base := "https://vip17.jimxtc.com/" + dir.folder + "/3000k/hls/index.m3u8"
		raw := vod("#EXT-X-DISCONTINUITY", "#EXTINF:4.6,", dir.first, "#EXTINF:5.533,", dir.second,
			"#EXTINF:0.233,", dir.third, "#EXT-X-DISCONTINUITY", "#EXTINF:4,", "film.ts")
		if got := Filter(raw, base, nil); got.Removed != 3 {
			t.Fatal("confirmed resources did not use the fast no-I/O policy")
		}
		if len(ErciyuanAdCandidates(raw, base)) != 0 {
			t.Fatal("known resources caused unnecessary payload checks")
		}
	}
}
