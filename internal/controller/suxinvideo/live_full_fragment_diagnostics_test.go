package suxinvideo

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This opt-in, read-only diagnostic uses the actual live transport and headers.
// It never creates/updates schema or health records and never prints source
// addresses, credentials, response bodies or URL-bearing error strings.
func TestLiveFullFragmentNetworkDiagnostics(t *testing.T) {
	if os.Getenv("SUXIN_LIVE_FULL_FRAGMENT_DIAGNOSTICS") != "1" {
		t.Skip("opt-in read-only live full-fragment transport diagnostics")
	}
	wd, err := os.Getwd()
	if err != nil || os.Chdir(filepath.Clean("../../..")) != nil {
		t.Fatal("project configuration directory unavailable")
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	streams, err := liveStreams(ctx, 63)
	if err != nil {
		t.Fatal("configured channel sources unavailable")
	}
	var source LiveStream
	for _, stream := range streams {
		if stream.ID == 64 {
			source = stream
			break
		}
	}
	if source.ID == 0 {
		t.Skip("configured diagnostic source is missing or disabled")
	}
	previous := liveHTTPClient
	t.Cleanup(func() { liveHTTPClient = previous })
	for _, timeout := range []time.Duration{12 * time.Second, 30 * time.Second} {
		client := *previous
		client.Timeout = timeout
		liveHTTPClient = &client
		body, base, manifestErr := liveManifest(ctx, source, source.URL)
		if manifestErr != nil {
			t.Logf("client_seconds=%d manifest_failed=true", int(timeout.Seconds()))
			continue
		}
		for depth := 0; strings.Contains(body, "#EXT-X-STREAM-INF:") && depth < 3; depth++ {
			variants := liveVariants(body, base)
			if len(variants) == 0 {
				break
			}
			body, base, manifestErr = liveManifest(ctx, source, variants[0].URL)
			if manifestErr != nil {
				break
			}
		}
		if manifestErr != nil {
			t.Logf("client_seconds=%d variant_manifest_failed=true", int(timeout.Seconds()))
			continue
		}
		baseURL, parseErr := url.Parse(base)
		if parseErr != nil {
			t.Fatal("manifest base address invalid")
		}
		fragments := []string{}
		for _, line := range strings.Split(body, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				fragments = append(fragments, line)
			}
		}
		if len(fragments) == 0 {
			t.Logf("client_seconds=%d fragments_missing=true", int(timeout.Seconds()))
			continue
		}
		for _, index := range []int{0, len(fragments) - 1} {
			relative, parseErr := url.Parse(fragments[index])
			if parseErr != nil {
				t.Fatal("fragment reference invalid")
			}
			started := time.Now()
			response, requestErr := liveRequest(ctx, source, baseURL.ResolveReference(relative).String(), "")
			if requestErr != nil {
				var timeoutErr net.Error
				t.Logf("client_seconds=%d fragment_index=%d request_failed=true seconds=%.3f timeout=%t", int(timeout.Seconds()), index, time.Since(started).Seconds(), errors.As(requestErr, &timeoutErr) && timeoutErr.Timeout())
				continue
			}
			headerSeconds := time.Since(started).Seconds()
			reader := bufio.NewReader(response.Body)
			_, peekErr := reader.Peek(16)
			data, readErr := io.ReadAll(io.LimitReader(reader, (64<<20)+1))
			response.Body.Close()
			var timeoutErr net.Error
			t.Logf("client_seconds=%d fragment_index=%d status=%d header_seconds=%.3f total_seconds=%.3f bytes=%d content_length=%d mime=%q peek_failed=%t read_failed=%t timeout=%t unexpected_eof=%t", int(timeout.Seconds()), index, response.StatusCode, headerSeconds, time.Since(started).Seconds(), len(data), response.ContentLength, response.Header.Get("Content-Type"), peekErr != nil, readErr != nil, errors.As(readErr, &timeoutErr) && timeoutErr.Timeout(), errors.Is(readErr, io.ErrUnexpectedEOF))
		}
	}
}
