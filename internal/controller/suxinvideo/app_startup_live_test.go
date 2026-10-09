package suxinvideo

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

// Explicitly enabled, read-only production probe. It never prints signed URLs,
// grants, provider credentials, or full database records, and creates no films.
func TestAppStartupLiveBudget(t *testing.T) {
	if os.Getenv("SUXIN_APP_STARTUP_LIVE") != "1" {
		t.Skip("set SUXIN_APP_STARTUP_LIVE=1 to compare live native startup probe budgets")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal("working directory unavailable")
	}
	if err = os.Chdir(filepath.Clean("../../..")); err != nil {
		t.Fatal("project configuration directory unavailable")
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	film, err := appVisibleFilm(ctx, 8576)
	if err != nil {
		t.Fatal("public sample film unavailable")
	}
	if _, err = AppAuthorizeFilm(ctx, film); err != nil {
		t.Skip("sample film is not authorized for anonymous playback")
	}
	sources, err := hydratePlayers(ctx, film, playlist(film))
	if err != nil {
		t.Fatal("sample film sources unavailable")
	}
	marker := ""
	for _, src := range sources {
		if src.Code != "yqk_1" || len(src.Episodes) == 0 {
			continue
		}
		if src.OwnerVodID > 0 && src.OwnerVodID != gconv.Int64(film["id"]) {
			owner, ownerErr := appVisibleFilm(ctx, src.OwnerVodID)
			if ownerErr != nil {
				t.Fatal("sample source owner unavailable")
			}
			if _, ownerErr = AppAuthorizeFilm(ctx, owner); ownerErr != nil {
				t.Skip("sample source owner requires member authorization")
			}
		}
		marker = src.Episodes[0].URL
		break
	}
	if marker == "" {
		t.Skip("sample first native line missing or disabled")
	}
	started := time.Now()
	media, err := sourceHealthResolveYQK(ctx, marker)
	if err != nil {
		t.Fatal("native sample resolution failed")
	}
	t.Logf("film=8576 provider_resolve=%s variants=%d", time.Since(started), len(media.Variants))
	for _, sample := range []struct {
		name    string
		budget  time.Duration
		startup bool
	}{{"startup_2s", 2 * time.Second, true}, {"startup_6s", 6 * time.Second, true}, {"full_6s", 6 * time.Second, false}} {
		base := playbackHTTPClient.Transport
		if base == nil {
			base = http.DefaultTransport
		}
		trace := &appStartupLiveTransport{base: base}
		client := *playbackHTTPClient
		client.Transport = trace
		probeCtx, stop := context.WithTimeout(ctx, sample.budget)
		started = time.Now()
		if sample.startup {
			err = probeAppStartupMediaHeaders(probeCtx, media, nil, &client, safeCollectorURL)
		} else {
			err = probeDiscoveryMediaHeaders(probeCtx, media, nil, &client, safeCollectorURL)
		}
		kind := "success"
		if err != nil {
			kind = "rejected"
			if errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
				kind = "deadline"
			} else if errors.Is(err, errSourceProbeTransient) {
				kind = "transient"
			} else if errors.Is(err, errSourceProbeUnsupported) {
				kind = "unsupported"
			}
		}
		stop()
		t.Logf("film=8576 mode=%s elapsed=%s result=%s", sample.name, time.Since(started), kind)
		trace.mu.Lock()
		for i, row := range trace.rows {
			t.Logf("mode=%s request=%d kind=%s status=%d elapsed=%s failed=%t", sample.name, i+1, row.kind, row.status, row.elapsed, row.failed)
		}
		trace.mu.Unlock()
	}
}

type appStartupLiveTransport struct {
	base http.RoundTripper
	mu   sync.Mutex
	rows []appStartupLiveRequest
}
type appStartupLiveRequest struct {
	kind    string
	status  int
	elapsed time.Duration
	failed  bool
}

func (p *appStartupLiveTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	started := time.Now()
	response, err := p.base.RoundTrip(r)
	kind := "media"
	if strings.HasSuffix(strings.ToLower(r.URL.Path), ".m3u8") {
		kind = "playlist"
	}
	status := 0
	if response != nil {
		status = response.StatusCode
	}
	p.mu.Lock()
	p.rows = append(p.rows, appStartupLiveRequest{kind: kind, status: status, elapsed: time.Since(started), failed: err != nil})
	p.mu.Unlock()
	return response, err
}
