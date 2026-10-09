// Package erciyuan implements the reviewed original-client read protocol for
// the 二次元 source. It never downloads or executes source JavaScript.
package erciyuan

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const primaryConfig = "https://sansan999.oss-accelerate.aliyuncs.com/xcy.txt"
const maxResponseBytes = 8 << 20

type Category struct {
	ID   int
	Name string
}
type Page struct {
	Items                  []map[string]any
	Total, Page, PageCount int
}
type Detail struct {
	Item  map[string]any
	Lines []Line
}
type Line struct {
	Code, Name string
	Episodes   []Episode
}
type Episode struct{ Name, URL string }
type Media struct {
	URL     string
	Headers map[string]string
}

// Client caches only the public API base and category/page metadata. Every
// Resolve re-reads the current episode URL; expiring CDN links are never stored.
type Client struct {
	http          *http.Client
	mu            sync.Mutex
	base          string
	baseUntil     time.Time
	loading       chan struct{}
	categories    []Category
	categoryUntil time.Time
	firstPages    map[int]cachedPage
	metadata      map[string]map[string]any
}
type cachedPage struct {
	page  Page
	until time.Time
}

func NewClient(client *http.Client) *Client {
	if client == nil {
		client = &http.Client{Transport: defaultTransport(), Timeout: 25 * time.Second}
	}
	copyClient := *client
	if copyClient.Timeout == 0 || copyClient.Timeout > 30*time.Second {
		copyClient.Timeout = 25 * time.Second
	}
	priorRedirect := copyClient.CheckRedirect
	copyClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("二次元上游重定向次数过多")
		}
		if err := validRequestURL(req.URL); err != nil {
			return err
		}
		if priorRedirect != nil {
			return priorRedirect(req, via)
		}
		return nil
	}
	return &Client{http: &copyClient, firstPages: map[int]cachedPage{}, metadata: map[string]map[string]any{}}
}

var deniedNetworks = func() []*net.IPNet {
	var ranges []*net.IPNet
	for _, raw := range []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4", "::/128", "::1/128", "fc00::/7", "fe80::/10", "ff00::/8"} {
		_, block, _ := net.ParseCIDR(raw)
		ranges = append(ranges, block)
	}
	return ranges
}()
var syntheticDNS = func() *net.IPNet { _, block, _ := net.ParseCIDR("198.18.0.0/15"); return block }()

func publicIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return false
	}
	for _, block := range deniedNetworks {
		if block.Contains(ip) {
			return false
		}
	}
	return true
}
func allowedDialIP(host string, ip net.IP) bool {
	if publicIP(ip) {
		return true
	}
	// This workstation's TUN DNS synthesizes 198.18/15 for public names.
	// Never allow such IP literals or unreviewed names, nor any real LAN IP.
	if net.ParseIP(host) != nil || !syntheticDNS.Contains(ip) {
		return false
	}
	switch strings.ToLower(host) {
	case "sansan999.oss-accelerate.aliyuncs.com", "sh13.fannaz.top", "json.xyzhenqin.top":
		return true
	}
	return false
}
func defaultTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Requests use their reviewed origins directly. A caller needing its own
	// proxy can pass an explicit, policy-controlled http.Client.
	transport.Proxy = nil
	transport.ResponseHeaderTimeout = 15 * time.Second
	transport.TLSHandshakeTimeout = 10 * time.Second
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("二次元上游地址无效")
		}
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(addresses) == 0 {
			return nil, errors.New("二次元上游地址解析失败")
		}
		for _, candidate := range addresses {
			if !allowedDialIP(host, candidate.IP) {
				return nil, errors.New("二次元上游解析到受限地址")
			}
		}
		return (&net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, network, net.JoinHostPort(addresses[0].IP.String(), port))
	}
	return transport
}

// Upstream request targets are a narrow, reviewed set. Parser URLs from detail
// responses cannot direct server-side requests to arbitrary hosts or paths.
func validRequestURL(u *url.URL) error {
	if u == nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Fragment != "" {
		return errors.New("二次元上游地址不合法")
	}
	host := strings.ToLower(u.Hostname())
	verifiedParser := u.Scheme == "http" && host == "183.131.206.132" && u.Path == "/json5.php"
	if u.Port() != "" && !((u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") || (verifiedParser && u.Port() == "9999")) {
		return errors.New("二次元上游端口不受支持")
	}
	allowed := (u.Scheme == "https" && (host == "sansan999.oss-accelerate.aliyuncs.com" || host == "sh13.fannaz.top" || host == "json.xyzhenqin.top")) ||
		verifiedParser
	if !allowed {
		return errors.New("二次元上游地址不在已验证范围内")
	}
	return nil
}

func (c *Client) read(ctx context.Context, address string, params url.Values) ([]byte, error) {
	u, err := url.Parse(address)
	if err != nil {
		return nil, errors.New("二次元上游地址无效")
	}
	if err = validRequestURL(u); err != nil {
		return nil, err
	}
	query := u.Query()
	for key, values := range params {
		query[key] = append([]string(nil), values...)
	}
	u.RawQuery = query.Encode()
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errors.New("二次元请求创建失败")
	}
	req.Header.Set("User-Agent", "okhttp/4.12.0")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("二次元上游连接失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("二次元上游返回 HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, errors.New("二次元上游读取失败")
	}
	if len(raw) > maxResponseBytes {
		return nil, errors.New("二次元上游响应超出限制")
	}
	return raw, nil
}

func launchParams(now time.Time) (url.Values, error) {
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return nil, errors.New("二次元请求随机参数生成失败")
	}
	nonce := hex.EncodeToString(nonceBytes)
	ts := strconv.FormatInt(now.Unix(), 10)
	message := "v1|10000|selfOperated|" + originalSigner + "|" + ts + "|" + nonce
	mac := hmac.New(sha256.New, originalKey[:])
	_, _ = mac.Write([]byte(message))
	return url.Values{"version": {"10000"}, "channel": {"selfOperated"}, "sign": {originalSigner}, "ts": {ts}, "nonce": {nonce}, "sig": {hex.EncodeToString(mac.Sum(nil))}}, nil
}

func (c *Client) bootstrap(ctx context.Context) (string, error) {
	for {
		c.mu.Lock()
		if c.base != "" && time.Now().Before(c.baseUntil) {
			base := c.base
			c.mu.Unlock()
			return base, nil
		}
		if c.loading != nil {
			ready := c.loading
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-ready:
				continue
			}
		}
		ready := make(chan struct{})
		c.loading = ready
		c.mu.Unlock()
		base, err := c.loadConfig(ctx)
		c.mu.Lock()
		if err == nil {
			c.base = base
			c.baseUntil = time.Now().Add(30 * time.Minute)
		}
		c.loading = nil
		close(ready)
		c.mu.Unlock()
		return base, err
	}
}

func (c *Client) loadConfig(ctx context.Context) (string, error) {
	params, err := launchParams(time.Now())
	if err != nil {
		return "", err
	}
	raw, err := c.read(ctx, primaryConfig, params)
	if err != nil {
		return "", err
	}
	launch := strings.Trim(strings.TrimSpace(string(raw)), "\"")
	u, err := url.Parse(launch)
	if err != nil || validRequestURL(u) != nil || u.Path != "/app-config/launch/json" {
		return "", errors.New("二次元启动配置地址不受支持")
	}
	params, err = launchParams(time.Now())
	if err != nil {
		return "", err
	}
	raw, err = c.read(ctx, launch, params)
	if err != nil {
		return "", err
	}
	document, err := decodeDocument(raw)
	if err != nil {
		return "", err
	}
	config, ok := document.(map[string]any)
	if !ok {
		return "", errors.New("二次元启动配置格式错误")
	}
	if nested, ok := config["data"].(map[string]any); ok {
		config = nested
	}
	if granted, ok := config["access_granted"].(bool); !ok || !granted {
		return "", errors.New("二次元上游未授予普通接口访问")
	}
	base := strings.TrimRight(stringValue(config["base_url"]), "/")
	u, err = url.Parse(base)
	if err != nil || validRequestURL(u) != nil || u.Scheme != "https" || u.RawQuery != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("二次元影片接口地址不受支持")
	}
	return base, nil
}

func (c *Client) api(ctx context.Context, path string, params url.Values) (map[string]any, error) {
	base, err := c.bootstrap(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := c.read(ctx, base+path, params)
	if err != nil {
		return nil, err
	}
	doc, err := decodeDocument(raw)
	if err != nil {
		return nil, err
	}
	object, ok := doc.(map[string]any)
	if !ok {
		return nil, errors.New("二次元接口响应格式错误")
	}
	if _, exists := object["code"]; exists {
		code := intValue(object["code"])
		if code != 0 && code != 1 && code != 200 {
			return nil, fmt.Errorf("二次元接口返回错误代码 %d", code)
		}
	}
	return object, nil
}

func stringValue(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return string(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	}
	return ""
}
func intValue(v any) int { n, _ := strconv.Atoi(stringValue(v)); return n }
func objectRows(v any) []map[string]any {
	values, ok := v.([]any)
	if !ok {
		return nil
	}
	rows := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if row, ok := value.(map[string]any); ok {
			rows = append(rows, row)
		}
	}
	return rows
}
func validID(id string) bool {
	if len(id) < 1 || len(id) > 12 {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	n, err := strconv.ParseInt(id, 10, 64)
	return err == nil && n > 0
}

func validateMedia(address string) error {
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return errors.New("二次元未返回有效媒体地址")
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.ContainsAny(host, " \t\r\n") {
		return errors.New("二次元媒体地址不合法")
	}
	if ip := net.ParseIP(host); ip != nil && !publicIP(ip) {
		return errors.New("二次元媒体地址不合法")
	}
	if strings.Contains(strings.ToLower(u.Path), "advideolp") {
		return errors.New("二次元当前分集返回广告素材，请切换线路")
	}
	return nil
}

func cloneRow(row map[string]any) map[string]any {
	copy := make(map[string]any, len(row))
	for key, value := range row {
		copy[key] = value
	}
	return copy
}
func (c *Client) remember(rows []map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, row := range rows {
		id := stringValue(row["vod_id"])
		if !validID(id) {
			continue
		}
		old := c.metadata[id]
		if old == nil {
			old = map[string]any{}
		}
		for key, value := range row {
			if value != nil && stringValue(value) != "" {
				old[key] = value
			}
		}
		c.metadata[id] = old
	}
	// Collection reads details immediately after its page, so retain a bounded
	// working set instead of holding an unbounded full-site catalog in memory.
	for len(c.metadata) > 8192 {
		for id := range c.metadata {
			delete(c.metadata, id)
			break
		}
	}
}
