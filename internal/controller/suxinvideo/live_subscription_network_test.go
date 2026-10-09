package suxinvideo

import (
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestLiveOfficialTUNExceptionIsNarrow(t *testing.T) {
	for _, host := range []string{"iptv-org.github.io", "raw.githubusercontent.com"} {
		for _, ip := range []string{"198.18.0.1", "198.19.255.254"} {
			if !liveOfficialSubscriptionIP(host, net.ParseIP(ip)) {
				t.Fatal("exact official host TUN exception missing")
			}
		}
	}
	for _, host := range []string{"iptv-org.github.io.evil.example", "evil.githubusercontent.com", "198.18.0.1", "localhost"} {
		if liveOfficialSubscriptionIP(host, net.ParseIP("198.18.0.1")) {
			t.Fatal("arbitrary hostname/literal acquired official exception")
		}
	}
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.1.1", "169.254.1.1", "::1", "fc00::1", "8.8.8.8"} {
		if liveOfficialSubscriptionIP("raw.githubusercontent.com", net.ParseIP(ip)) {
			t.Fatal("non-TUN network acquired official exception")
		}
	}
	for _, raw := range []string{"https://raw.githubusercontent.com/evil/repo/master/live.m3u", "https://iptv-org.github.io/iptv/countries/hk.m3u?token=evil", "https://raw.githubusercontent.com:443/iptv-org/iptv/master/streams/hk.m3u", "https://198.18.0.1/live.m3u"} {
		if liveOfficialSubscriptionURL(raw) {
			t.Fatal("arbitrary URL acquired exact official exception")
		}
	}
	first, _ := url.Parse("https://iptv-org.github.io/iptv/countries/hk.m3u")
	client := liveSubscriptionHTTPClient(time.Second)
	for _, raw := range []string{"http://127.0.0.1/live.m3u", "https://example.com/catalog.m3u", "https://raw.githubusercontent.com/evil/live.m3u"} {
		next, _ := url.Parse(raw)
		if client.CheckRedirect(&http.Request{URL: next}, []*http.Request{{URL: first}}) == nil {
			t.Fatal("official download redirected outside its exact allowlist")
		}
	}
	if _, err := liveCleanHeaders(map[string]string{"Accept-Language": "zh-CN"}); err != nil {
		t.Fatal("supported Accept-Language header rejected")
	}
}
