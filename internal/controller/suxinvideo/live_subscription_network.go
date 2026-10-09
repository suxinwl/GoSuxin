package suxinvideo

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func liveOfficialSubscriptionURL(raw string) bool {
	for _, country := range []string{"cn", "hk", "mo", "tw"} {
		builtin := "https://iptv-org.github.io/iptv/countries/" + country + ".m3u"
		if raw == builtin || raw == liveOfficialSubscriptionFallback(builtin) {
			return true
		}
	}
	return false
}

// Public-suffix private hosting domains remain forbidden by the generic TUN
// exception. Only the two fixed, publicly maintained official catalog hosts
// may resolve through this machine's synthetic 198.18.0.0/15 range.
func liveOfficialSubscriptionIP(host string, ip net.IP) bool {
	return (strings.EqualFold(host, "iptv-org.github.io") || strings.EqualFold(host, "raw.githubusercontent.com")) && collectorFakeNet.Contains(ip)
}
func validateLiveSubscriptionURL(ctx context.Context, raw string) error {
	if err := safeCollectorURL(ctx, raw); err == nil {
		return nil
	}
	if !liveOfficialSubscriptionURL(raw) {
		return errors.New("订阅地址不是允许的公网地址")
	}
	u, _ := url.Parse(raw)
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, u.Hostname())
	if err != nil || len(addresses) == 0 {
		return errors.New("官方直播订阅地址无法解析")
	}
	for _, entry := range addresses {
		if !publicIP(entry.IP) && !liveOfficialSubscriptionIP(u.Hostname(), entry.IP) {
			return errors.New("官方订阅地址解析到了不允许的网络")
		}
	}
	return nil
}

func liveSubscriptionHTTPClient(timeout time.Duration) *http.Client {
	client := safeCollectorHTTPClient(timeout)
	transport := client.Transport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		if len(addresses) == 0 {
			return nil, errors.New("订阅地址无法解析")
		}
		for _, entry := range addresses {
			if !allowedRemoteIP(host, entry.IP, true) && !liveOfficialSubscriptionIP(host, entry.IP) {
				return nil, errors.New("不允许访问内网地址")
			}
		}
		connection, err := (&net.Dialer{Timeout: 8 * time.Second}).DialContext(ctx, network, net.JoinHostPort(addresses[0].IP.String(), port))
		if err != nil {
			return nil, err
		}
		return &idleReadConn{Conn: connection, idle: 30 * time.Second}, nil
	}
	client.Transport = transport
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return errors.New("订阅重定向次数过多")
		}
		if len(via) > 0 && liveOfficialSubscriptionURL(via[0].URL.String()) && !liveOfficialSubscriptionURL(next.URL.String()) {
			return errors.New("官方订阅不允许重定向到其他地址")
		}
		return validateLiveSubscriptionURL(next.Context(), next.URL.String())
	}
	return client
}
