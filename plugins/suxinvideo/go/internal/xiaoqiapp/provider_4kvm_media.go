package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// These are the production HLS hostnames published by 4KVM. A host in the
// page's _pdf list is not automatically a working media server (some return
// an HTML parking page with status 200). Keep the API URL first and only
// change a known CDN hostname after validating the actual manifest.
func fourKVMKnownMediaHost(host string) bool {
	switch strings.ToLower(host) {
	case "oss.douyinbit.com", "myoss.douyinbit.top":
		return true
	default:
		return false
	}
}

// Apply the image-CDN header policy immediately before each actual request.
// Go's redirect handling creates a new Referer, and an
// existing client callback can add Origin, so a per-probe transport also
// enforces the policy. The shared catalog/API client's transport is unchanged.
type fourKVMMediaProbeTransport struct {
	base http.RoundTripper
}

func (transport fourKVMMediaProbeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	if fourKVMImageCDN(cloned.URL.Hostname()) {
		cloned.Header.Del("Referer")
		cloned.Header.Del("Origin")
	}
	return transport.base.RoundTrip(cloned)
}

func (d *Downloader) do4KVMProbeRequest(request *http.Request, timeout time.Duration) (*http.Response, error) {
	if _, allowed := fourKVMProbeURL(request.URL, request.URL.String()); !allowed {
		return nil, errors.New("4KVM probe target is not allowed")
	}
	ctx, cancel := context.WithTimeout(request.Context(), timeout)
	request = request.WithContext(ctx)
	release, err := d.limiter.acquire(ctx, request)
	if err != nil {
		cancel()
		return nil, err
	}
	client := *d.client
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = fourKVMMediaProbeTransport{base: base}
	previousCheck := client.CheckRedirect
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return errors.New("4KVM media redirected too many times")
		}
		if _, allowed := fourKVMProbeURL(next.URL, next.URL.String()); !allowed {
			return errors.New("4KVM media redirect target is not allowed")
		}
		if previousCheck != nil {
			if err := previousCheck(next, via); err != nil {
				return err
			}
			// Preserve the original callback's restrictions, while validating any
			// destination changes it makes before a redirected request is sent.
			if _, allowed := fourKVMProbeURL(next.URL, next.URL.String()); !allowed {
				return errors.New("4KVM media redirect target is not allowed")
			}
		}
		if fourKVMImageCDN(next.URL.Hostname()) {
			next.Header.Del("Referer")
			next.Header.Del("Origin")
		}
		return nil
	}
	response, err := client.Do(request)
	if err != nil {
		cancel()
		release()
		return nil, err
	}
	d.limiter.observe(request, response)
	response.Body = &limitedResponseBody{ReadCloser: response.Body, release: func() { cancel(); release() }}
	return response, nil
}

func (d *Downloader) fourKVMProbePlaylist(ctx context.Context, rawURL, referer string) (string, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", "", errors.New("4KVM media address is invalid")
	}
	request.Header.Set("User-Agent", userAgent)
	if !fourKVMImageCDN(request.URL.Hostname()) {
		request.Header.Set("Referer", referer)
	}
	response, err := d.do4KVMProbeRequest(request, 4*time.Second)
	if err != nil {
		return "", "", errors.New("4KVM media server did not respond")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", "", errors.New("4KVM media server returned an error")
	}
	const limit = 4 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || len(body) > limit {
		return "", "", errors.New("4KVM media manifest could not be read")
	}
	playlist := strings.TrimSpace(strings.TrimPrefix(string(body), "\ufeff"))
	if !strings.HasPrefix(playlist, "#EXTM3U") {
		return "", "", errors.New("4KVM media server returned an invalid manifest")
	}
	finalURL := rawURL
	if response.Request != nil && response.Request.URL != nil {
		finalURL = response.Request.URL.String()
	}
	return playlist, finalURL, nil
}

func fourKVMImageCDN(host string) bool {
	host = strings.ToLower(host)
	return host == "xhscdn.com" || strings.HasSuffix(host, ".xhscdn.com")
}

func fourKVMProbeURL(base *url.URL, raw string) (*url.URL, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, false
	}
	u = base.ResolveReference(u)
	if !isProviderHTTPMediaURL(u.String()) || u.Port() != "" {
		return nil, false
	}
	// Limit the additional probe to the CDN families used by production 4KVM.
	// Other media addresses remain available for the CMS's normal proxy checks.
	return u, fourKVMKnownMediaHost(u.Hostname()) || fourKVMImageCDN(u.Hostname())
}

var fourKVMKeyURI = regexp.MustCompile(`(?i)\bURI="([^"]+)"`)

// Validate at most one child manifest, one AES key and the first media chunk.
// Manifest status 200 alone cannot distinguish a working line from one whose
// segments are denied. PNG-wrapped TS is valid production 4KVM content; the
// CMS proxy removes that wrapper when forwarding it to the player.
func (d *Downloader) fourKVMProbeFirstChunk(ctx context.Context, playlist, rawURL, referer string) error {
	base, _ := url.Parse(rawURL)
	master, encrypted := strings.Contains(playlist, "#EXT-X-STREAM-INF:"), false
	keyURI, mediaURI := "", ""
	for _, raw := range strings.Split(playlist, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#EXT-X-KEY:") && strings.Contains(line, "METHOD=AES-128") {
			encrypted = true
			if match := fourKVMKeyURI.FindStringSubmatch(line); len(match) == 2 {
				keyURI = match[1]
			}
		}
		if mediaURI == "" && line != "" && !strings.HasPrefix(line, "#") {
			mediaURI = line
			break
		}
	}
	if mediaURI == "" {
		return errors.New("4KVM media manifest is empty")
	}
	mediaURL, allowed := fourKVMProbeURL(base, mediaURI)
	if !allowed {
		return nil
	}
	if master {
		child, finalURL, err := d.fourKVMProbePlaylist(ctx, mediaURL.String(), referer)
		if err != nil {
			return err
		}
		if strings.Contains(child, "#EXT-X-STREAM-INF:") {
			return errors.New("4KVM media manifest is nested too deeply")
		}
		return d.fourKVMProbeFirstChunk(ctx, child, finalURL, referer)
	}
	if keyURI != "" {
		if keyURL, ok := fourKVMProbeURL(base, keyURI); ok {
			key, err := d.fourKVMProbeChunk(ctx, keyURL, referer, 17)
			if err != nil || len(key) != 16 {
				return errors.New("4KVM media key is unavailable")
			}
		}
	} else if encrypted {
		return errors.New("4KVM media key is missing")
	}
	chunk, err := d.fourKVMProbeChunk(ctx, mediaURL, referer, 16384)
	if err != nil {
		return err
	}
	if len(chunk) == 0 || strings.HasPrefix(strings.ToLower(strings.TrimSpace(string(chunk))), "<html") || strings.HasPrefix(strings.ToLower(strings.TrimSpace(string(chunk))), "<!doctype") {
		return errors.New("4KVM media chunk is unavailable")
	}
	if encrypted {
		return nil
	}
	for offset := 0; offset+376 < len(chunk); offset++ {
		if chunk[offset] == 0x47 && chunk[offset+188] == 0x47 && chunk[offset+376] == 0x47 {
			return nil
		}
	}
	if len(chunk) >= 8 && (string(chunk[4:8]) == "ftyp" || string(chunk[4:8]) == "styp" || string(chunk[4:8]) == "moof") {
		return nil
	}
	return errors.New("4KVM media chunk has no supported video payload")
}

func (d *Downloader) fourKVMProbeChunk(ctx context.Context, address *url.URL, referer string, limit int) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address.String(), nil)
	if err != nil {
		return nil, errors.New("4KVM media chunk address is invalid")
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Range", "bytes=0-"+strconv.Itoa(limit-1))
	if !fourKVMImageCDN(address.Hostname()) {
		request.Header.Set("Referer", referer)
	}
	response, err := d.do4KVMProbeRequest(request, 3*time.Second)
	if err != nil {
		return nil, errors.New("4KVM media chunk did not respond")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, errors.New("4KVM media chunk was denied")
	}
	chunk, err := io.ReadAll(io.LimitReader(response.Body, int64(limit)))
	if err != nil {
		return nil, errors.New("4KVM media chunk could not be read")
	}
	return chunk, nil
}

func (d *Downloader) fourKVMSelectMedia(ctx context.Context, primary string, hosts []string, referer string) (providerMedia, error) {
	u, err := url.Parse(primary)
	if err != nil || !isProviderHTTPMediaURL(primary) {
		return providerMedia{}, errors.New("4KVM playback address is invalid")
	}
	media := providerMedia{URL: primary, Referer: referer}
	// Unknown future CDN URLs retain the exact provider address. Do not turn
	// arbitrary _pdf entries into server-side requests or rewrite their host.
	if !fourKVMKnownMediaHost(u.Hostname()) || u.Port() != "" {
		return media, nil
	}
	candidates := []string{primary}
	seen := map[string]bool{primary: true}
	for _, host := range hosts {
		if !fourKVMKnownMediaHost(host) {
			continue
		}
		if candidate := fourKVMReplaceHost(primary, host); candidate != "" && !seen[candidate] {
			seen[candidate] = true
			candidates = append(candidates, candidate)
		}
	}
	probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	for _, candidate := range candidates {
		if probeCtx.Err() != nil {
			break
		}
		playlist, finalURL, probeErr := d.fourKVMProbePlaylist(probeCtx, candidate, referer)
		if probeErr == nil {
			probeErr = d.fourKVMProbeFirstChunk(probeCtx, playlist, finalURL, referer)
		}
		if probeErr == nil {
			media.URL, media.Playlist, media.Duration = candidate, playlist, m3u8Duration(playlist)
			return media, nil
		}
	}
	if ctx.Err() != nil {
		return providerMedia{}, ctx.Err()
	}
	return providerMedia{}, errors.New("4KVM has no working media manifest")
}
