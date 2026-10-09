package suxinvideo

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	xq "github.com/suxinwl/GoSuxin/internal/xiaoqiapp"
)

type erciyuanProbeFixtureTransport func(*http.Request) (*http.Response, error)

func (f erciyuanProbeFixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestErciyuanProbeRetriesOnlyTransientMediaWithinDeadline(t *testing.T) {
	for _, tc := range []struct {
		name              string
		status, calls     int
		body              string
		alwaysEOF, cancel bool
		success           bool
	}{
		{name: "one EOF", status: 206, calls: 2, body: "\x00\x00\x00\x18ftypisom", success: true},
		{name: "bounded repeated EOF", calls: 2, alwaysEOF: true},
		{name: "explicit not found", status: 404, calls: 1},
		{name: "malformed media", status: 200, calls: 1, body: "not a complete movie"},
		{name: "canceled transient", calls: 1, cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls, resolves := 0, 0
			client := &http.Client{Transport: erciyuanProbeFixtureTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				if tc.cancel {
					cancel()
					return nil, io.EOF
				}
				if tc.alwaysEOF || tc.name == "one EOF" && calls == 1 {
					return nil, io.EOF
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.body)), Request: request}, nil
			})}
			resolve := func(context.Context, string) (xq.CMSMedia, map[string]string, error) {
				resolves++
				return xq.CMSMedia{URL: "https://media.example/feature.mp4"}, nil, nil
			}
			err := probeErciyuanDiscoveryWith(ctx, "erciyuan://64933/dd02/0", resolve, client, func(context.Context, string) error { return nil })
			if (err == nil) != tc.success || calls != tc.calls || resolves != 1 {
				t.Fatalf("unbounded or inappropriate native retry: calls=%d resolves=%d error=%v", calls, resolves, err)
			}
			if tc.cancel && !errors.Is(err, context.Canceled) && !errors.Is(err, errSourceProbeTransient) {
				t.Fatalf("cancellation was replaced by another retry: %v", err)
			}
		})
	}
}
