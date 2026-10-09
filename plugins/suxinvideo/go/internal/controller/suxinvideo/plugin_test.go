package suxinvideo

import (
	"context"
	"encoding/json"
	_ "github.com/suxinwl/GoSuxin/framework/contrib/nosql/redis"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestPaymentSignAndWXFields(t *testing.T) {
	fields := url.Values{"pid": {"1001"}, "out_trade_no": {"A-1"}, "money": {"9.90"}, "sign": {"ignored"}}
	if got := signedFields(fields, "secret", false); got != "031944df9557dac340d9b6fc967101b3" {
		t.Fatalf("unexpected epay signature: %s", got)
	}
	xmlText, err := wxRequest(url.Values{"body": {"A&B"}, "total_fee": {"990"}})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := wxFields([]byte(xmlText))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Get("body") != "A&B" || parsed.Get("total_fee") != "990" {
		t.Fatalf("XML round trip: %v", parsed)
	}
}
func TestPublicAddressGuard(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "10.1.2.3", "100.100.100.200", "169.254.169.254", "::1", "fc00::1"} {
		if publicIP(net.ParseIP(s)) {
			t.Fatalf("accepted private address %s", s)
		}
	}
	if !publicIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("rejected public address")
	}
}
func TestCollectorFakeDNSGuard(t *testing.T) {
	cases := []struct {
		host, ip           string
		collector, allowed bool
	}{
		{"example.com", "198.18.0.6", true, true},
		{"example.com", "198.18.0.6", false, false},
		{"198.18.0.6", "198.18.0.6", true, false},
		{"localhost", "198.18.0.6", true, false},
		{"server.internal", "198.18.0.6", true, false},
		{"example.com", "127.0.0.1", true, false},
		{"example.com", "169.254.169.254", true, false},
		{"example.com", "8.8.8.8", false, true},
	}
	for _, test := range cases {
		if got := allowedRemoteIP(test.host, net.ParseIP(test.ip), test.collector); got != test.allowed {
			t.Errorf("allowedRemoteIP(%q, %q, %v) = %v, want %v", test.host, test.ip, test.collector, got, test.allowed)
		}
	}
}

// Set SX_LIVE_COLLECT_URL to a configured source when checking this host's
// DNS/TUN setup. Regular tests remain deterministic and offline.
func TestLiveCollectorFetch(t *testing.T) {
	raw := os.Getenv("SX_LIVE_COLLECT_URL")
	if raw == "" {
		t.Skip("set SX_LIVE_COLLECT_URL for a live source check")
	}
	data, err := fetchMac(context.Background(), raw, url.Values{"ac": {"videolist"}, "pg": {"1"}, "h": {"24"}})
	if err != nil {
		t.Fatal(err)
	}
	if data.Code != 1 || len(data.List) == 0 {
		t.Fatalf("unexpected first page: code=%d items=%d", data.Code, len(data.List))
	}
}
func TestMacPayloadMixedNumberFormats(t *testing.T) {
	var payload macPayload
	if err := json.Unmarshal([]byte(`{"code":"1","total":20,"page":"1","pagecount":2,"list":[{"vod_id":17}]}`), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Code != 1 || payload.Total != 20 || payload.Page != 1 || payload.PageCount != 2 || len(payload.List) != 1 {
		t.Fatalf("decoded metadata incorrectly: %+v", payload)
	}
	if err := json.Unmarshal([]byte(`{"code":"oops"}`), &payload); err == nil {
		t.Fatal("accepted malformed numeric metadata")
	}
}
func TestMergePlayKeepsExistingEpisodes(t *testing.T) {
	from, play := mergePlay("lineA", "E1$https://a/1.m3u8", "lineA$$$lineB", "E1$https://a/1.m3u8#E2$https://a/2.m3u8$$$E1$https://b/1.m3u8")
	if from != "lineA$$$lineB" || strings.Count(play, "https://a/1.m3u8") != 1 || !strings.Contains(play, "https://a/2.m3u8") || !strings.Contains(play, "https://b/1.m3u8") {
		t.Fatalf("merged sources incorrectly: %q %q", from, play)
	}
}

func TestMergePlayReplacesSameEpisodeWithoutDuplicating(t *testing.T) {
	from, play := mergePlay(
		"lineA", "第01集$https://old.example/1.m3u8#第2集$https://old.example/2.m3u8",
		"lineA$$$lineB", "1$https://new.example/1.m3u8#第2集$https://new.example/2.m3u8$$$第1集$https://other.example/1.m3u8",
	)
	if from != "lineA$$$lineB" {
		t.Fatalf("merged line names = %q", from)
	}
	groups := strings.Split(play, "$$$")
	if len(groups) != 2 || len(strings.Split(groups[0], "#")) != 2 || len(strings.Split(groups[1], "#")) != 1 {
		t.Fatalf("episodes duplicated or sources lost: %q", play)
	}
	if strings.Contains(groups[0], "old.example") || !strings.Contains(groups[0], "new.example/1.m3u8") {
		t.Fatalf("stale media URLs were retained: %q", groups[0])
	}
}

func TestRewritePlaylistPreservesTagsAndRewritesResources(t *testing.T) {
	input := "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"keys/key.bin\"\n#EXTINF:5,\nsegment.ts\n"
	got := rewritePlaylist(input, "https://example.com/hls/master.m3u8", func(raw string) string {
		return "proxy(" + raw + ")"
	})
	if !strings.Contains(got, `URI="proxy(https://example.com/hls/keys/key.bin)"`) {
		t.Fatalf("key URI was not rewritten: %s", got)
	}
	if !strings.Contains(got, "\nproxy(https://example.com/hls/segment.ts)\n") {
		t.Fatalf("segment URI was not rewritten: %s", got)
	}
	if !strings.Contains(got, "#EXTM3U\n") || !strings.Contains(got, "#EXTINF:5,\n") {
		t.Fatalf("playlist tags changed: %s", got)
	}
}

func TestPreferredSourceChoosesDirectMedia(t *testing.T) {
	sources := []source{
		{Name: "parse", Episodes: []episode{{Name: "第一集", URL: "https://example.com/player.php?id=1"}}},
		{Name: "direct", Episodes: []episode{{Name: "第一集", URL: "https://cdn.example.com/one.m3u8?token=x"}}},
	}
	if got := preferredSource(sources); got != 1 {
		t.Fatalf("preferred source = %d, want 1", got)
	}
}
