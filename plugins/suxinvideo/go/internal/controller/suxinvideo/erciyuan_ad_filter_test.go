package suxinvideo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/suxinwl/GoSuxin/internal/mediaplaylist"
)

type erciyuanAdRoundTrip func(*http.Request) (*http.Response, error)

func (f erciyuanAdRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestErciyuanAdPayloadRequiresFullHashAndBoundedLength(t *testing.T) {
	body := "verified payload"
	hash := sha256.Sum256([]byte(body))
	sample := mediaplaylist.AdFingerprint{URL: "https://vip17.jimxtc.com/clip.ts", Bytes: int64(len(body)), SHA256: hex.EncodeToString(hash[:])}
	allow := func(context.Context, string) error { return nil }
	for _, item := range []struct {
		name, body string
		status     int
		want       bool
	}{
		{"full match", body, 200, true}, {"same length different content", "another payload!", 200, false},
		{"short", body[:4], 200, false}, {"oversized", body + "x", 200, false},
		{"partial", body, 206, false}, {"upstream error", body, 403, false},
	} {
		t.Run(item.name, func(t *testing.T) {
			client := &http.Client{Transport: erciyuanAdRoundTrip(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Referer") != "https://source.example/" || r.Header.Get("Authorization") != "" || r.Header.Get("Range") != "" {
					t.Fatal("verification leaked credentials or discarded presentation headers")
				}
				return &http.Response{StatusCode: item.status, Body: io.NopCloser(strings.NewReader(item.body)), ContentLength: -1, Request: r}, nil
			})}
			got := verifyErciyuanAdPayload(context.Background(), sample, map[string]string{"Referer": "https://source.example/", "Authorization": "secret"}, client, allow)
			if got != item.want {
				t.Fatalf("verification=%t want%t", got, item.want)
			}
		})
	}
	called := false
	client := &http.Client{Transport: erciyuanAdRoundTrip(func(*http.Request) (*http.Response, error) { called = true; return nil, errors.New("blocked") })}
	deny := func(context.Context, string) error { return errors.New("private host") }
	if verifyErciyuanAdPayload(context.Background(), sample, nil, client, deny) || called {
		t.Fatal("URL policy rejection reached the network")
	}
	privateURL, _ := url.Parse("http://127.0.0.1/private.ts")
	if playbackHTTPClient.CheckRedirect(&http.Request{URL: privateURL}, nil) == nil {
		t.Fatal("verification transport allowed private redirects")
	}
	var redirectCalls atomic.Int32
	redirectClient := &http.Client{Transport: erciyuanAdRoundTrip(func(r *http.Request) (*http.Response, error) {
		redirectCalls.Add(1)
		if r.URL.Hostname() == "127.0.0.1" {
			t.Fatal("public-to-private redirect reached the network")
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"http://127.0.0.1/private.ts"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	guard := func(_ context.Context, address string) error {
		parsed, _ := url.Parse(address)
		if parsed.Hostname() == "127.0.0.1" {
			return errors.New("private redirect")
		}
		return nil
	}
	if verifyErciyuanAdPayload(context.Background(), sample, nil, redirectClient, guard) || redirectCalls.Load() != 1 {
		t.Fatal("per-hop verification policy did not stop a private redirect")
	}
	// The shallow client copy must keep its owner's redirect policy as well.
	redirectClient.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("owner forbids redirect") }
	if verifyErciyuanAdPayload(context.Background(), sample, nil, redirectClient, allow) {
		t.Fatal("verification bypassed the existing redirect policy")
	}
	timeoutClient := &http.Client{Transport: erciyuanAdRoundTrip(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	if verifyErciyuanAdPayload(timeoutCtx, sample, nil, timeoutClient, allow) || time.Since(started) > time.Second {
		t.Fatal("stalled verification ignored the bounded deadline")
	}
}

func TestErciyuanAdCacheWholeBlockSingleflightAndConcurrency(t *testing.T) {
	cache := &erciyuanAdVerifier{entries: map[string]*erciyuanAdVerification{}, slots: make(chan struct{}, 3)}
	block := mediaplaylist.AdBlock{{URL: "a", SHA256: "hash-a"}, {URL: "b", SHA256: "hash-b"}, {URL: "c", SHA256: "hash-c"}}
	var calls, active, peak atomic.Int32
	check := func(ctx context.Context, sample mediaplaylist.AdFingerprint) bool {
		calls.Add(1)
		n := active.Add(1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		defer active.Add(-1)
		select {
		case <-ctx.Done():
			return false
		case <-time.After(15 * time.Millisecond):
			return sample.URL != "false-c"
		}
	}
	var jobs sync.WaitGroup
	for i := 0; i < 20; i++ {
		jobs.Add(1)
		go func() {
			defer jobs.Done()
			if !cache.verify(context.Background(), block, check) {
				t.Error("same block was not verified")
			}
		}()
	}
	jobs.Wait()
	if calls.Load() != 3 || peak.Load() > 3 || !cache.verify(context.Background(), block, check) || calls.Load() != 3 {
		t.Fatalf("singleflight/cache/concurrency violated: calls%d peak%d", calls.Load(), peak.Load())
	}
	for i := 0; i < 8; i++ {
		copy := block
		copy[0].URL += strings.Repeat("x", i+1)
		jobs.Add(1)
		go func(block mediaplaylist.AdBlock) { defer jobs.Done(); cache.verify(context.Background(), block, check) }(copy)
	}
	jobs.Wait()
	if peak.Load() > 3 {
		t.Fatalf("global payload concurrency=%d", peak.Load())
	}
	block[2].URL = "false-c"
	if cache.verify(context.Background(), block, check) {
		t.Fatal("two verified clips removed a partially matched normal-film block")
	}
	before := calls.Load()
	if cache.verify(context.Background(), block, check) || calls.Load() != before {
		t.Fatal("negative cache repeated failed verification")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	block[0].URL = "uncached"
	if cache.verify(canceled, block, check) {
		t.Fatal("canceled request authorized advertisement deletion")
	}
}
