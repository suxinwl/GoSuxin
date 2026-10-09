package app

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const proxySubscriptionMaxBytes = 8 << 20

var subscribedProxyURLPattern = regexp.MustCompile(`(?i)(?:https?|socks5h?)://[^\s"'<>]+`)

func validateProxySubscriptionURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Hostname() == "" || endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return errors.New("代理订阅地址必须是有效的 HTTP/HTTPS URL")
	}
	return nil
}

// fetchProxySubscription retrieves a subscription without routing the request
// through the proxy that it is about to configure.
func fetchProxySubscription(ctx context.Context, raw string, insecureTLS bool) (string, error) {
	if err := validateProxySubscriptionURL(raw); err != nil {
		return "", err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: insecureTLS} //nolint:gosec -- follows the application's TLS setting.
	client := &http.Client{Transport: transport, Timeout: 25 * time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(raw), nil)
	if err != nil {
		return "", fmt.Errorf("订阅地址无效: %w", err)
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", "text/plain, text/yaml, application/yaml, application/json, */*")
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("读取代理订阅失败: %w", publicError(err))
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("读取代理订阅失败: HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, proxySubscriptionMaxBytes+1))
	if err != nil {
		return "", fmt.Errorf("读取代理订阅失败: %w", publicError(err))
	}
	if len(body) > proxySubscriptionMaxBytes {
		return "", errors.New("代理订阅内容超过 8 MB 限制")
	}
	for _, candidate := range parseProxySubscription(body) {
		if endpoint, err := configuredProxy(candidate); err == nil && endpoint != nil {
			return endpoint.String(), nil
		}
	}
	return "", errors.New("代理订阅中没有可用的 HTTP/HTTPS/SOCKS5 节点")
}

func parseProxySubscription(body []byte) []string {
	seen := make(map[string]bool)
	var result []string
	add := func(raw string) {
		if candidate := normalizeSubscriptionProxy(raw); candidate != "" && !seen[candidate] {
			seen[candidate] = true
			result = append(result, candidate)
		}
	}
	var value any
	if json.Unmarshal(body, &value) == nil {
		collectJSONProxyCandidates(value, add)
	}
	collectClashProxyCandidates(string(body), add)
	for _, match := range subscribedProxyURLPattern.FindAllString(string(body), -1) {
		add(strings.TrimRight(match, ",;]})"))
	}
	decoded := decodeSubscriptionBody(body)
	if len(decoded) > 0 && string(decoded) != string(body) {
		for _, match := range subscribedProxyURLPattern.FindAllString(string(decoded), -1) {
			add(strings.TrimRight(match, ",;]})"))
		}
		collectClashProxyCandidates(string(decoded), add)
	}
	return result
}

func normalizeSubscriptionProxy(raw string) string {
	raw = strings.TrimSpace(strings.Trim(raw, "\"'`"))
	if raw == "" {
		return ""
	}
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Hostname() == "" || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Path != "" && endpoint.Path != "/" {
		return ""
	}
	switch strings.ToLower(endpoint.Scheme) {
	case "http", "https", "socks5", "socks5h":
		return endpoint.String()
	default:
		return ""
	}
}

func collectJSONProxyCandidates(value any, add func(string)) {
	switch current := value.(type) {
	case string:
		add(current)
	case []any:
		for _, item := range current {
			collectJSONProxyCandidates(item, add)
		}
	case map[string]any:
		for _, key := range []string{"url", "proxy", "proxy_url", "proxyUrl", "uri", "link"} {
			if value, ok := current[key].(string); ok {
				add(value)
			}
		}
		server := firstString(current, "server", "host", "address", "hostname")
		port := firstString(current, "port")
		if server != "" && port != "" {
			scheme := strings.ToLower(firstString(current, "type", "scheme", "protocol"))
			if scheme == "socks" {
				scheme = "socks5"
			}
			if scheme == "http" || scheme == "https" || scheme == "socks5" || scheme == "socks5h" {
				user := firstString(current, "username", "user")
				password := firstString(current, "password", "pass")
				candidate := scheme + "://" + server + ":" + port
				if user != "" {
					credentials := url.User(user)
					if password != "" {
						credentials = url.UserPassword(user, password)
					}
					parsed, _ := url.Parse(candidate)
					parsed.User = credentials
					candidate = parsed.String()
				}
				add(candidate)
			}
		}
		for _, item := range current {
			collectJSONProxyCandidates(item, add)
		}
	}
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		switch value := values[key].(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		case float64:
			return strconv.Itoa(int(value))
		case json.Number:
			return value.String()
		}
	}
	return ""
}

func collectClashProxyCandidates(body string, add func(string)) {
	var block map[string]string
	flush := func() {
		if len(block) == 0 {
			return
		}
		server, port := block["server"], block["port"]
		typ := strings.ToLower(block["type"])
		if typ == "socks" {
			typ = "socks5"
		}
		if server != "" && port != "" && (typ == "http" || typ == "https" || typ == "socks5" || typ == "socks5h") {
			candidate := typ + "://" + server + ":" + port
			if user := block["username"]; user != "" {
				parsed, _ := url.Parse(candidate)
				if password := block["password"]; password != "" {
					parsed.User = url.UserPassword(user, password)
				} else {
					parsed.User = url.User(user)
				}
				candidate = parsed.String()
			}
			add(candidate)
		}
		block = nil
	}
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "-") {
			flush()
			block = map[string]string{}
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
		}
		if block == nil {
			continue
		}
		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.Trim(strings.TrimSpace(parts[1]), "\"'")
		block[key] = value
	}
	flush()
}

func decodeSubscriptionBody(body []byte) []byte {
	text := strings.TrimSpace(string(body))
	if text == "" || strings.Contains(text, "\n") || strings.Contains(text, "://") {
		return nil
	}
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		decoded, err := encoding.DecodeString(text)
		if err == nil && len(decoded) > 0 {
			return decoded
		}
	}
	return nil
}
