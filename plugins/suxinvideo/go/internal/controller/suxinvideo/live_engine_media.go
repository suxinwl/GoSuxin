package suxinvideo

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
)

// Reuse a bounded loopback transport; creating one for each segment leaves an
// idle connection pool per request and grows memory during long broadcasts.
var liveEngineMediaTransport = &http.Transport{
	Proxy:                 nil,
	DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	ResponseHeaderTimeout: 12 * time.Second,
	IdleConnTimeout:       60 * time.Second,
	MaxIdleConns:          32,
	MaxIdleConnsPerHost:   16,
	MaxConnsPerHost:       64,
}

// Only authenticated managed-engine opaque media routes may access loopback.
// Imported addresses never use this exception. The adapter checks external DNS,
// every redirect and every playlist reference before fetching source media.
func liveEngineMediaURL(stream LiveStream, raw string) bool {
	if stream.SourceKind != "provider" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Fragment != "" {
		return false
	}
	origin := liveProviderOrigin(stream.ProviderKey)
	return origin != "" && u.Scheme+"://"+u.Host == origin && strings.HasPrefix(u.Path, "/internal/media/") && !strings.Contains(u.Path, "..") && !strings.Contains(strings.ToLower(u.EscapedPath()), "%2f")
}
func liveEngineRequest(ctx context.Context, stream LiveStream, raw, byteRange string, budget time.Duration) (*http.Response, error) {
	if !liveEngineMediaURL(stream, raw) {
		return nil, fmt.Errorf("引擎媒体地址无效")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("X-IPTV-Secret", liveEngineSecret())
	if byteRange != "" {
		request.Header.Set("Range", byteRange)
	}
	if budget == 0 {
		budget = 20 * time.Second
	}
	if budget < 0 {
		budget = 0
	}
	client := &http.Client{Timeout: budget, Transport: liveEngineMediaTransport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		response.Body.Close()
		return nil, fmt.Errorf("引擎媒体不允许跳转")
	}
	return response, nil
}

// Replay is a finite stream with Range; authorization is checked throughout long
// responses, as well as at each separate request. It never enters VOD caches.
func liveFiniteMedia(r *ghttp.Request, item *liveMediaSession, stream LiveStream, raw string) {
	// Raw streaming leaves the framework buffer empty. Stop outer JSON response
	// middleware after this handler, including HEAD and unsatisfiable Range.
	defer r.ExitAll()
	response, err := liveRequestWithBudget(r.Context(), stream, raw, r.Header.Get("Range"), -1)
	if err != nil {
		appWrite(r, nil, appError(502, "回放连接失败"))
		return
	}
	defer response.Body.Close()
	if response.StatusCode != 200 && response.StatusCode != 206 && response.StatusCode != 416 {
		appWrite(r, nil, appError(502, "回放暂时不可用"))
		return
	}
	for _, key := range []string{"Content-Length", "Content-Range", "Accept-Ranges"} {
		if value := response.Header.Get(key); value != "" {
			r.Response.Header().Set(key, value)
		}
	}
	r.Response.Header().Set("Content-Type", "video/mp4")
	// Flush only the tracked raw writer. Flushing an empty GoFrame buffer after
	// setting 206/416 injects its HTTP status text into the binary response.
	r.Response.Writer.WriteHeader(response.StatusCode)
	r.Response.Writer.Flush()
	if r.Method == "HEAD" {
		return
	}
	buffer := make([]byte, 32<<10)
	checked := time.Now()
	for {
		if time.Since(checked) > time.Second {
			if _, err = checkLiveSession(r.Context(), item); err != nil {
				return
			}
			checked = time.Now()
		}
		n, readErr := response.Body.Read(buffer)
		if n > 0 {
			if _, err = r.Response.Writer.Write(buffer[:n]); err != nil {
				return
			}
		}
		if readErr != nil {
			return
		}
	}
}
