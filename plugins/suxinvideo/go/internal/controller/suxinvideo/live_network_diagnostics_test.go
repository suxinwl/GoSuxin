package suxinvideo

import (
	"context"
	"golang.org/x/net/publicsuffix"
	"net"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"
)

// Opt-in diagnostics for the two fixed public URLs only. Never log playlist
// bodies, stream URLs, source headers, private configuration or credentials.
func TestLiveOfficialNetworkDiagnostics(t *testing.T) {
	if os.Getenv("SUXIN_LIVE_NETWORK_DIAGNOSTICS") != "1" {
		t.Skip("opt-in official live subscription networking diagnostics")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	for _, address := range []string{"https://iptv-org.github.io/iptv/countries/hk.m3u", "https://raw.githubusercontent.com/iptv-org/iptv/master/streams/hk.m3u"} {
		u, _ := url.Parse(address)
		addresses, dnsErr := net.DefaultResolver.LookupIPAddr(ctx, u.Hostname())
		allowed := true
		for _, a := range addresses {
			allowed = allowed && allowedRemoteIP(u.Hostname(), a.IP, true)
		}
		_, icann := publicsuffix.PublicSuffix(u.Hostname())
		fakeOnly := len(addresses) > 0
		for _, a := range addresses {
			fakeOnly = fakeOnly && collectorFakeNet.Contains(a.IP)
		}
		t.Logf("official host=%s DNS error=%v allowed=%t TUN-only=%t ICANN-suffix=%t", u.Hostname(), dnsErr, allowed, fakeOnly, icann)
		if err := validateLiveSubscriptionURL(ctx, address); err != nil {
			t.Logf("official host=%s URL validation error=%v", u.Hostname(), err)
			continue
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		req.Header.Set("User-Agent", mediaUserAgent)
		response, err := liveSubscriptionHTTPClient(20 * time.Second).Do(req)
		if err != nil {
			t.Logf("official host=%s transport error=%v", u.Hostname(), err)
			continue
		}
		content, _, readErr := readMediaPlaylist(response)
		response.Body.Close()
		t.Logf("official host=%s HTTP status=%d playlist error=%v", u.Hostname(), response.StatusCode, readErr)
		if readErr == nil {
			items, _, parseErr := liveParseImport(content, "中国香港")
			t.Logf("official host=%s import count=%d parse error=%v", u.Hostname(), len(items), parseErr)
		}
	}
	content, err := liveFetchSubscription(ctx, "https://iptv-org.github.io/iptv/countries/hk.m3u")
	t.Logf("builtin fallback bytes=%d error=%v", len(content), err)
	if err != nil {
		t.Fatal("official subscription fetch still failing")
	}
}
