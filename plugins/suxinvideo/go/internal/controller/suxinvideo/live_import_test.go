package suxinvideo

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLiveImportM3UChannelIdentityAndHeaders(t *testing.T) {
	raw := `#EXTM3U
#EXTINF:-1 tvg-id="Jade.hk" tvg-logo="https://cdn.example/jade.png" group-title="Hong Kong, TV",TVB Jade (720p)
#EXTVLCOPT:http-user-agent=ExamplePlayer
#EXTHTTP:{"Authorization":"Bearer private"}
https://cdn.example/jade/index.m3u8
#EXTINF:-1 tvg-id="Jade.hk" group-title="香港",Jade
https://backup.example/jade.m3u8|Referer=https%3A%2F%2Fexample.com
#EXTINF:-1 tvg-id="GoldenJade.hk",Golden Jade
https://cdn.example/golden.m3u8
#EXTINF:-1 tvg-id="jade.hk",Duplicate URL
https://cdn.example/jade/index.m3u8
`
	items, preview, err := liveParseImport(raw, "中国香港")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || preview.Channels != 2 || preview.Streams != 3 {
		t.Fatalf("wrong deduplication: %+v", preview)
	}
	if items[0].Name != "翡翠台" || items[2].Name != "黄金翡翠台" || items[0].Group != "Hong Kong, TV" {
		t.Fatalf("wrong channel mapping: %+v", items)
	}
	if items[0].Headers["User-Agent"] != "ExamplePlayer" || items[1].Headers["Referer"] != "https://example.com" {
		t.Fatal("player header directives were lost")
	}
	if preview.Sample[0].Headers["Authorization"] == "Bearer private" {
		t.Fatal("preview exposed credential")
	}
	if liveChannelIdentity(items[0]) == liveChannelIdentity(items[2]) {
		t.Fatal("Jade and Golden Jade must remain distinct")
	}
}

func TestLiveImportTXTGroupsAndConservativeValidation(t *testing.T) {
	items, preview, err := liveParseImport("香港,#genre#\n翡翠台,https://cdn.example/jade.m3u8\n翡翠台,https://backup.example/jade.m3u8\n错误,file:///secret\n", "")
	if err != nil || len(items) != 2 || preview.Channels != 1 || items[0].Group != "香港" || len(preview.Warnings) != 1 {
		t.Fatalf("TXT parser: items=%+v preview=%+v err=%v", items, preview, err)
	}
	for _, raw := range []string{"<html>error</html>", "错误,rtsp://example.com/live", "错误,http://user:pass@example.com/live"} {
		if _, _, err := liveParseImport(raw, ""); err == nil {
			t.Fatalf("accepted unsupported input %q", raw)
		}
	}
	if _, _, err := liveParseImport(strings.Repeat("x", liveImportLimit+1), ""); err == nil {
		t.Fatal("oversized list accepted")
	}
	items, _, err = liveParseImport("#EXTM3U\n#EXTINF:-1 tvg-id=\"CCTV1.cn\",CCTV 1\nhttps://cdn.example/cctv1.m3u8\n", "")
	if err != nil || items[0].Name != "CCTV-1 综合" {
		t.Fatalf("CCTV alias not applied: %+v %v", items, err)
	}
}

func TestLiveHeadersRedactionAndBlankPreserve(t *testing.T) {
	original := map[string]string{"Authorization": "Bearer secret", "Cookie": "sid=secret", "User-Agent": "first"}
	merged, err := liveMergeHeaders(original, map[string]string{"authorization": "", "Cookie": "••••••", "User-Agent": "second"})
	if err != nil || merged["Authorization"] != original["Authorization"] || merged["Cookie"] != original["Cookie"] || merged["User-Agent"] != "second" {
		t.Fatalf("secret preservation failed: %v", err)
	}
	public := liveRedactHeaders(merged)
	if public["Authorization"] == original["Authorization"] || public["Cookie"] == original["Cookie"] {
		t.Fatal("credential was not redacted")
	}
	if _, err := liveCleanHeaders(map[string]string{"User-Agent": "a\r\nX-Evil: yes"}); err == nil {
		t.Fatal("header injection accepted")
	}
	if _, err := liveCleanHeaders(map[string]string{"Host": "127.0.0.1"}); err == nil {
		t.Fatal("unrestricted request header accepted")
	}
	stream := LiveStream{ID: 1, URL: "https://private.example/secret", Headers: original}
	encoded, err := json.Marshal(stream)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private.example") || strings.Contains(string(encoded), "secret") {
		t.Fatal("public stream JSON leaked upstream credentials")
	}
}

func TestLiveOfficialFallbackAndEditionNormalization(t *testing.T) {
	for _, country := range []string{"cn", "hk", "mo", "tw"} {
		fallback := liveOfficialSubscriptionFallback("https://iptv-org.github.io/iptv/countries/" + country + ".m3u")
		if fallback != "https://raw.githubusercontent.com/iptv-org/iptv/master/streams/"+country+".m3u" {
			t.Fatal("official country fallback missing")
		}
	}
	for _, raw := range []string{"http://iptv-org.github.io/iptv/countries/hk.m3u", "https://evil.example/iptv/countries/hk.m3u", "https://iptv-org.github.io/iptv/countries/hk.m3u?redirect=evil", "https://iptv-org.github.io.evil.example/iptv/countries/hk.m3u", "https://iptv-org.github.io/iptv/countries/us.m3u"} {
		if liveOfficialSubscriptionFallback(raw) != "" {
			t.Fatal("unrelated subscription received official fallback")
		}
	}
	items, preview, err := liveParseImport("#EXTM3U\n#EXTINF:-1 tvg-id=\"Jade.hk@HD\",Jade HD\nhttps://cdn.example/hd.m3u8\n#EXTINF:-1 tvg-id=\"Jade.hk@SD\",Jade SD\nhttps://cdn.example/sd.m3u8\n#EXTINF:-1 tvg-id=\"GoldenJade.hk@HD\",Golden Jade HD\nhttps://cdn.example/golden.m3u8\n", "")
	if err != nil || preview.Channels != 2 || preview.Streams != 3 || items[0].TVGID != "Jade.hk" || items[0].Name != "翡翠台" || items[0].Quality != "HD" || items[2].Name != "黄金翡翠台" {
		t.Fatalf("channel edition normalization failed: %v", err)
	}
}
