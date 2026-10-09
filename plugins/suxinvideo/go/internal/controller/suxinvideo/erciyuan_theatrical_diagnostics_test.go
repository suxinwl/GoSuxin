package suxinvideo

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/suxinwl/GoSuxin/internal/erciyuan"
	xq "github.com/suxinwl/GoSuxin/internal/xiaoqiapp"
)

type erciyuanTheatricalTrace struct {
	base     http.RoundTripper
	mu       sync.Mutex
	requests []map[string]any
}

func (trace *erciyuanTheatricalTrace) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := trace.base.RoundTrip(request)
	info := map[string]any{"host": request.URL.Hostname(), "range": request.Header.Get("Range")}
	if response != nil {
		info["status"] = response.StatusCode
		info["type"] = response.Header.Get("Content-Type")
	}
	if err != nil {
		info["error"] = err.Error()
	}
	trace.mu.Lock()
	trace.requests = append(trace.requests, info)
	trace.mu.Unlock()
	return response, err
}

func TestErciyuanTheatricalMediaDiagnostics(t *testing.T) {
	if os.Getenv("SUXIN_ERCIYUAN_THEATRICAL_INSPECT") != "1" {
		t.Skip("native read-only diagnostic is opt-in")
	}
	t.Setenv("SUXIN_INTEGRATION", "1")
	ctx, cancel := context.WithTimeout(yqkIntegrationContext(t), 80*time.Second)
	defer cancel()
	all := []map[string]any{}
	for attempt := 0; attempt < 3; attempt++ {
		client := safeCollectorHTTPClient(20 * time.Second)
		trace := &erciyuanTheatricalTrace{base: client.Transport}
		client.Transport = trace
		provider := erciyuan.NewClient(client)
		attemptCtx, attemptCancel := context.WithTimeout(ctx, 20*time.Second)
		media, err := provider.Resolve(attemptCtx, "erciyuan://64933/dd02/0")
		info := map[string]any{"attempt": attempt + 1, "resolved": err == nil, "probe_pass": false}
		if err != nil {
			info["resolve_error"] = err.Error()
		} else {
			info["media_url"] = media.URL
			probeErr := probeDiscoveryMediaHeaders(attemptCtx, xq.CMSMedia{URL: media.URL, Referer: media.Headers["Referer"]}, media.Headers, client, safeMediaURL)
			info["probe_pass"] = probeErr == nil
			if probeErr != nil {
				info["probe_error"] = probeErr.Error()
			}
		}
		attemptCancel()
		trace.mu.Lock()
		info["requests"] = trace.requests
		trace.mu.Unlock()
		all = append(all, info)
		t.Logf("READ-ONLY attempt%d resolved=%t media_probe=%v requests=%d", attempt+1, err == nil, info["probe_pass"], len(trace.requests))
	}
	for attempt := 0; attempt < 3; attempt++ {
		client := safeCollectorHTTPClient(12 * time.Second)
		trace := &erciyuanTheatricalTrace{base: client.Transport}
		client.Transport = trace
		attemptCtx, attemptCancel := context.WithTimeout(ctx, 12*time.Second)
		probeErr := probeErciyuanDiscoveryWith(attemptCtx, "erciyuan://64933/dd02/0", resolveErciyuanPlayback, client, safeMediaURL)
		attemptCancel()
		info := map[string]any{"production_probe_attempt": attempt + 1, "probe_pass": probeErr == nil, "requests": trace.requests}
		if probeErr != nil {
			info["probe_error"] = probeErr.Error()
		}
		all = append(all, info)
		t.Logf("READ-ONLY exact production attempt%d media_probe=%t requests=%d", attempt+1, probeErr == nil, len(trace.requests))
	}
	dir := filepath.Join("data", "tmp", "ecy-theatrical-evidence")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := hongguoAuditWriteJSON(filepath.Join(dir, "media-diagnostics.private.json"), all, false); err != nil {
		t.Fatal(err)
	}
}
