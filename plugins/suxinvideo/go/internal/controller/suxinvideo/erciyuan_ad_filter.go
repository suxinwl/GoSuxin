package suxinvideo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/suxinwl/GoSuxin/internal/mediaplaylist"
)

type erciyuanAdVerification struct {
	done    chan struct{}
	until   time.Time
	matched bool
}

type erciyuanAdVerifier struct {
	mu      sync.Mutex
	entries map[string]*erciyuanAdVerification
	slots   chan struct{}
}

var nativeErciyuanAds = &erciyuanAdVerifier{
	entries: make(map[string]*erciyuanAdVerification), slots: make(chan struct{}, 3),
}

// Hash verification is restricted to native 二次元 playback. Other source
// playlists use the ordinary no-I/O filter. Known reviewed resource paths are
// already removed there, so repeated playback does not fetch advertisement
// payloads. Only matching whole blocks are removed; timeout/error keeps them.
func filterErciyuanMediaPlaylist(ctx context.Context, raw, base string, headers map[string]string) mediaplaylist.Result {
	result := filterMediaPlaylist(ctx, raw, base)
	if setting(ctx, "player_ad_filter", "1") != "1" {
		return result
	}
	candidates := mediaplaylist.ErciyuanAdCandidates(result.Playlist, base)
	if len(candidates) == 0 {
		return result
	}
	verifyCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	verified := map[string]bool{}
	for _, block := range candidates {
		if nativeErciyuanAds.verify(verifyCtx, block, func(checkCtx context.Context, sample mediaplaylist.AdFingerprint) bool {
			return verifyErciyuanAdPayload(checkCtx, sample, headers, playbackHTTPClient, safeMediaURL)
		}) {
			for _, sample := range block {
				verified[sample.URL] = true
			}
		}
	}
	if len(verified) == 0 {
		return result
	}
	extra := mediaplaylist.FilterVerified(result.Playlist, base, nil, verified)
	extra.Removed += result.Removed
	extra.RemovedSeconds += result.RemovedSeconds
	return extra
}

// One cache entry covers all three ordered payloads. Pending callers share the
// same verification; no partial result authorizes deletion. The global slots
// bound concurrent payload reads across different requests, not just one film.
func (cache *erciyuanAdVerifier) verify(ctx context.Context, block mediaplaylist.AdBlock, check func(context.Context, mediaplaylist.AdFingerprint) bool) bool {
	if ctx.Err() != nil || check == nil {
		return false
	}
	var parts []string
	for _, sample := range block {
		parts = append(parts, sample.URL, sample.SHA256, strconv.FormatInt(sample.Bytes, 10))
	}
	key := strings.Join(parts, "\x00")
	now := time.Now()
	cache.mu.Lock()
	if entry := cache.entries[key]; entry != nil {
		if entry.until.IsZero() {
			cache.mu.Unlock()
			select {
			case <-ctx.Done():
				return false
			case <-entry.done:
				return entry.matched
			}
		}
		if now.Before(entry.until) {
			cache.mu.Unlock()
			return entry.matched
		}
		delete(cache.entries, key)
	}
	// Never evict an in-flight entry, which would let a second verification
	// start. Completed entries have bounded memory and short negative TTLs.
	for candidate, entry := range cache.entries {
		if !entry.until.IsZero() && now.After(entry.until) {
			delete(cache.entries, candidate)
		}
	}
	if len(cache.entries) >= 512 {
		var oldest string
		var until time.Time
		for candidate, entry := range cache.entries {
			if !entry.until.IsZero() && (oldest == "" || entry.until.Before(until)) {
				oldest, until = candidate, entry.until
			}
		}
		if oldest == "" {
			cache.mu.Unlock()
			return false
		}
		delete(cache.entries, oldest)
	}
	entry := &erciyuanAdVerification{done: make(chan struct{})}
	cache.entries[key] = entry
	cache.mu.Unlock()
	var passed [3]bool
	var work sync.WaitGroup
	for index, sample := range block {
		work.Add(1)
		go func(index int, sample mediaplaylist.AdFingerprint) {
			defer work.Done()
			select {
			case cache.slots <- struct{}{}:
				defer func() { <-cache.slots }()
			case <-ctx.Done():
				return
			}
			if ctx.Err() == nil {
				passed[index] = check(ctx, sample)
			}
		}(index, sample)
	}
	work.Wait()
	cache.mu.Lock()
	entry.matched = passed[0] && passed[1] && passed[2]
	ttl := time.Minute
	if entry.matched {
		ttl = 30 * time.Minute
	}
	entry.until = time.Now().Add(ttl)
	close(entry.done)
	cache.mu.Unlock()
	return entry.matched
}

func verifyErciyuanAdPayload(ctx context.Context, sample mediaplaylist.AdFingerprint, headers map[string]string, client *http.Client, checkURL func(context.Context, string) error) bool {
	if sample.Bytes <= 0 || sample.Bytes > 2<<20 || len(sample.SHA256) != 64 ||
		client == nil || checkURL == nil || checkURL(ctx, sample.URL) != nil {
		return false
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sample.URL, nil)
	if err != nil {
		return false
	}
	request.Header.Set("User-Agent", mediaUserAgent)
	applyNativeMediaHeaders(request, headers)
	guarded := *client // Reuse the transport and its pooled CDN connections.
	previousRedirect := client.CheckRedirect
	guarded.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return errors.New("advertisement verification redirect limit")
		}
		if err := checkURL(next.Context(), next.URL.String()); err != nil {
			return err
		}
		if previousRedirect != nil {
			return previousRedirect(next, via)
		}
		return nil
	}
	response, err := guarded.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > sample.Bytes ||
		response.Request == nil || response.Request.URL == nil || checkURL(ctx, response.Request.URL.String()) != nil {
		return false
	}
	// Length is exact and reads are streamed into SHA256; no decoded video,
	// credentials, content buffers or signed URLs enter logs or the database.
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(response.Body, sample.Bytes+1))
	return err == nil && n == sample.Bytes && hex.EncodeToString(hash.Sum(nil)) == sample.SHA256
}
