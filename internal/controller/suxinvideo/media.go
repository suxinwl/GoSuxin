package suxinvideo

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"golang.org/x/net/publicsuffix"
)

type Media struct{}

const mediaUserAgent = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.6 Mobile/15E148 Safari/604.1"

type AssetReq struct {
	g.Meta `path:"/asset" method:"get" noValApi:"1"`
	Theme  string `p:"theme"`
	File   string `p:"file"`
}
type AssetRes struct{}
type ProxyReq struct {
	g.Meta `path:"/proxy" method:"get" noValApi:"1"`
	URL    string `p:"url"`
	Exp    int64  `p:"exp"`
	Sig    string `p:"sig"`
}
type ProxyRes struct{}

var assetNames = map[string]string{"main.css": "static/css/main.css", "main.js": "static/js/main.js", "hls.js": "static/player/hls.js", "suxinplayer.js": "static/player/suxinplayer.js", "source-select.js": "static/player/source-select.js", "source-discovery.js": "static/player/source-discovery.js", "episode-next.js": "static/player/episode-next.js", "favicon.svg": "static/img/favicon.svg", "nopic.svg": "static/img/nopic.svg", "banner.svg": "static/img/banner.svg", "banner2.svg": "static/img/banner2.svg", "banner3.svg": "static/img/banner3.svg", "banner4.svg": "static/img/banner4.svg", "banner5.svg": "static/img/banner5.svg"}

func (*Media) Asset(ctx context.Context, req *AssetReq) (*AssetRes, error) {
	r := g.RequestFromCtx(ctx)
	file, ok := assetNames[req.File]
	if !ok {
		notFound(ctx)
		return &AssetRes{}, nil
	}
	path := filepath.Join("resource", "static", "suxinvideo", "themes", cleanTheme(req.Theme), filepath.FromSlash(file))
	if dir := os.Getenv("SUXIN_PLUGIN_PACKAGE"); dir != "" {
		packaged := filepath.Join(dir, "public", "suxinvideo", "themes", cleanTheme(req.Theme), filepath.FromSlash(file))
		if _, err := os.Stat(packaged); err == nil {
			path = packaged
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		notFound(ctx)
		return &AssetRes{}, nil
	}
	switch filepath.Ext(req.File) {
	case ".css":
		r.Response.Header().Set("Content-Type", "text/css; charset=utf-8")
	case ".js":
		r.Response.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	case ".svg":
		r.Response.Header().Set("Content-Type", "image/svg+xml")
	default:
		r.Response.Header().Set("Content-Type", "application/octet-stream")
	}
	r.Response.Header().Set("Cache-Control", "public, max-age=86400")
	r.Response.Write(data)
	return &AssetRes{}, nil
}
func signProxy(ctx context.Context, raw string, expires int64) string {
	key := setting(ctx, "proxy_secret", "")
	if key == "" {
		secret, _ := g.Cfg("app").Get(ctx, "app.SecretKey")
		key = secret.String()
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(fmt.Sprintf("%d:%s", expires, raw)))
	return hex.EncodeToString(mac.Sum(nil))
}
func proxyLink(ctx context.Context, raw string) string {
	exp := time.Now().Add(3 * time.Hour).Unix()
	return "/suxinvideo/proxy?url=" + url.QueryEscape(raw) + "&exp=" + fmt.Sprint(exp) + "&sig=" + signProxy(ctx, raw, exp)
}
func safeMediaURL(ctx context.Context, raw string) error {
	// Local TUN DNS may return an address in 198.18.0.0/15 for a public CDN.
	// The synthetic-address exception still rejects IP literals and private DNS.
	return safeRemoteURL(ctx, raw, true)
}

// Collector endpoints may resolve to 198.18.0.0/15 when a local TUN proxy
// assigns synthetic DNS addresses. Only registered public hostnames receive
// this exception; literal and genuinely private addresses remain blocked.
func safeCollectorURL(ctx context.Context, raw string) error {
	return safeRemoteURL(ctx, raw, true)
}

func safeRemoteURL(ctx context.Context, raw string, collector bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || !(u.Scheme == "http" || u.Scheme == "https") {
		return fmt.Errorf("播放地址无效")
	}
	if u.User != nil {
		return fmt.Errorf("播放地址无效")
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, u.Hostname())
	if err != nil || len(addresses) == 0 {
		return fmt.Errorf("播放地址无法解析")
	}
	for _, address := range addresses {
		if !allowedRemoteIP(u.Hostname(), address.IP, collector) {
			return fmt.Errorf("不允许访问内网地址")
		}
	}
	return nil
}

var collectorFakeNet = func() *net.IPNet {
	_, subnet, _ := net.ParseCIDR("198.18.0.0/15")
	return subnet
}()

func allowedRemoteIP(host string, ip net.IP, collector bool) bool {
	if publicIP(ip) {
		return true
	}
	if !collector || net.ParseIP(host) != nil || !collectorFakeNet.Contains(ip) {
		return false
	}
	_, icann := publicsuffix.PublicSuffix(strings.TrimSuffix(host, "."))
	if !icann {
		return false
	}
	_, err := publicsuffix.EffectiveTLDPlusOne(strings.TrimSuffix(host, "."))
	return err == nil
}

var blockedNets = func() []*net.IPNet {
	cidrs := []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4", "::/128", "::1/128", "fc00::/7", "fe80::/10", "ff00::/8"}
	result := make([]*net.IPNet, 0, len(cidrs))
	for _, s := range cidrs {
		_, n, _ := net.ParseCIDR(s)
		result = append(result, n)
	}
	return result
}()

func publicIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return false
	}
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return false
		}
	}
	return true
}

var hlsURI = regexp.MustCompile(`URI="([^"]+)"`)

type idleReadConn struct {
	net.Conn
	idle time.Duration
}

func (c *idleReadConn) Read(p []byte) (int, error) {
	if err := c.SetReadDeadline(time.Now().Add(c.idle)); err != nil {
		return 0, err
	}
	return c.Conn.Read(p)
}

func safeHTTPClient(timeout time.Duration) *http.Client {
	return safeHTTPClientPolicy(timeout, false)
}

func safeCollectorHTTPClient(timeout time.Duration) *http.Client {
	return safeHTTPClientPolicy(timeout, true)
}

func safeHTTPClientPolicy(timeout time.Duration, collector bool) *http.Client {
	transport := &http.Transport{ResponseHeaderTimeout: timeout, TLSHandshakeTimeout: timeout,
		ForceAttemptHTTP2: true, MaxIdleConns: 64, MaxIdleConnsPerHost: 8, IdleConnTimeout: time.Minute,
		DialContext: func(dialCtx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			addresses, err := net.DefaultResolver.LookupIPAddr(dialCtx, host)
			if err != nil {
				return nil, err
			}
			for _, entry := range addresses {
				if !allowedRemoteIP(host, entry.IP, collector) {
					return nil, fmt.Errorf("不允许访问内网地址")
				}
			}
			if len(addresses) == 0 {
				return nil, fmt.Errorf("播放地址无法解析")
			}
			connection, err := (&net.Dialer{Timeout: 8 * time.Second}).DialContext(dialCtx, network, net.JoinHostPort(addresses[0].IP.String(), port))
			if err != nil {
				return nil, err
			}
			return &idleReadConn{Conn: connection, idle: 30 * time.Second}, nil
		}}
	return &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(next *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return fmt.Errorf("重定向次数过多")
		}
		applyMediaReferer(next, next.Header.Get("Referer"))
		return safeRemoteURL(next.Context(), next.URL.String(), collector)
	}}
}

// Public image-CDN segments used by 4KVM reject a foreign Referer. Keep the
// provider's Referer for its API and other media hosts, including redirects.
func applyMediaReferer(request *http.Request, referer string) {
	if hostIs(strings.ToLower(request.URL.Hostname()), "xhscdn.com") {
		request.Header.Del("Referer")
		request.Header.Del("Origin")
		return
	}
	if referer != "" {
		request.Header.Set("Referer", referer)
	}
}

// Reuse CDN connections across segments instead of creating a new transport
// and repeating DNS/TLS setup for every request. Per-read deadlines remain on.
var playbackHTTPClient = func() *http.Client {
	client := safeCollectorHTTPClient(12 * time.Second)
	client.Timeout = 0
	return client
}()

func isMediaPlaylist(contentType, raw string) bool {
	u, err := url.Parse(raw)
	return strings.Contains(strings.ToLower(contentType), "mpegurl") ||
		(err == nil && strings.HasSuffix(strings.ToLower(u.Path), ".m3u8"))
}

func readMediaPlaylist(response *http.Response) (string, string, error) {
	data, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(data) > 4<<20 {
		return "", "", fmt.Errorf("播放清单读取失败或过大")
	}
	body := strings.TrimSpace(strings.TrimPrefix(string(data), "\ufeff"))
	if !strings.HasPrefix(body, "#EXTM3U") {
		return "", "", fmt.Errorf("源站未返回有效播放清单")
	}
	// Relative keys and segments belong to the final URL after redirects.
	return body, response.Request.URL.String(), nil
}

func (*Media) Proxy(ctx context.Context, req *ProxyReq) (*ProxyRes, error) {
	r := g.RequestFromCtx(ctx)
	if req.Exp < time.Now().Unix() || req.Exp > time.Now().Add(4*time.Hour).Unix() || !hmac.Equal([]byte(req.Sig), []byte(signProxy(ctx, req.URL, req.Exp))) {
		r.Response.WriteStatus(http.StatusForbidden)
		return &ProxyRes{}, nil
	}
	if err := safeMediaURL(ctx, req.URL); err != nil {
		badRequest(r, err.Error())
		return &ProxyRes{}, nil
	}
	upstream, err := http.NewRequestWithContext(ctx, http.MethodGet, req.URL, nil)
	if err != nil {
		return nil, err
	}
	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
		upstream.Header.Set("Range", rangeHeader)
	}
	upstream.Header.Set("User-Agent", mediaUserAgent)
	response, err := playbackHTTPClient.Do(upstream)
	if err != nil {
		r.Response.WriteStatus(http.StatusBadGateway, "视频源暂时不可用")
		return &ProxyRes{}, nil
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		r.Response.WriteStatus(http.StatusBadGateway, "视频源返回错误")
		return &ProxyRes{}, nil
	}
	contentType := response.Header.Get("Content-Type")
	if contentType != "" {
		r.Response.Header().Set("Content-Type", contentType)
	}
	if isMediaPlaylist(contentType, response.Request.URL.String()) || isMediaPlaylist("", req.URL) {
		body, base, err := readMediaPlaylist(response)
		if err != nil {
			r.Response.WriteStatus(http.StatusBadGateway, err.Error())
			return &ProxyRes{}, nil
		}
		r.Response.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		filtered := filterMediaPlaylist(ctx, body, base)
		mediaAdFilterHeaders(r.Response.Header(), filtered)
		r.Response.Write(rewritePlaylist(filtered.Playlist, base, func(raw string) string { return proxyLink(ctx, raw) }))
		return &ProxyRes{}, nil
	}
	// The host response middleware checks the GoFrame response buffer. Writing
	// directly to its underlying Writer leaves that buffer empty, causing a JSON
	// response to be appended to the media bytes after this handler returns.
	media, err := io.ReadAll(io.LimitReader(response.Body, (64<<20)+1))
	if err != nil || len(media) == 0 || len(media) > 64<<20 {
		r.Response.WriteStatus(http.StatusBadGateway, "视频分片读取失败")
		return &ProxyRes{}, nil
	}
	if contentRange := response.Header.Get("Content-Range"); contentRange != "" {
		r.Response.Header().Set("Content-Range", contentRange)
	}
	if response.StatusCode == http.StatusPartialContent {
		r.Response.WriteHeader(http.StatusPartialContent)
	}
	r.Response.Write(media)
	return &ProxyRes{}, nil
}
func rewritePlaylist(raw, baseURL string, link func(string) string) string {
	base, _ := url.Parse(baseURL)
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			lines[i] = hlsURI.ReplaceAllStringFunc(line, func(part string) string {
				raw := strings.Trim(part[4:], `"`)
				relative, e := url.Parse(raw)
				if e != nil {
					return part
				}
				return `URI="` + link(base.ResolveReference(relative).String()) + `"`
			})
			continue
		}
		relative, err := url.Parse(trimmed)
		if err != nil {
			continue
		}
		lines[i] = link(base.ResolveReference(relative).String())
	}
	return strings.Join(lines, "\n")
}
