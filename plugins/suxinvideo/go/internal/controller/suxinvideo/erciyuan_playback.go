package suxinvideo

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/suxinwl/GoSuxin/internal/erciyuan"
	xq "github.com/suxinwl/GoSuxin/internal/xiaoqiapp"
)

type erciyuanPlaybackResolver interface {
	Resolve(context.Context, string) (erciyuan.Media, error)
}

// The playback client does not share mutable collection cursors or fixtures.
var erciyuanPlayClient erciyuanPlaybackResolver = erciyuan.NewClient(safeCollectorHTTPClient(20 * time.Second))

type erciyuanMediaResolver func(context.Context, string) (xq.CMSMedia, map[string]string, error)

func nativeErciyuanSeries(code, marker string) (string, error) {
	id, line, _, ok := erciyuan.ParseMarker(marker)
	if !ok || !erciyuan.ValidLineCode(code) || code != "ecy_"+line {
		return "", errors.New("二次元播放线路与分集不匹配")
	}
	return id, nil
}

func resolveErciyuanPlayback(ctx context.Context, marker string) (xq.CMSMedia, map[string]string, error) {
	return resolveErciyuanPlaybackWith(ctx, marker, erciyuanPlayClient)
}

func resolveErciyuanPlaybackWith(ctx context.Context, marker string, provider erciyuanPlaybackResolver) (xq.CMSMedia, map[string]string, error) {
	if _, _, _, ok := erciyuan.ParseMarker(marker); !ok || provider == nil {
		return xq.CMSMedia{}, nil, errors.New("二次元分集标识无效")
	}
	if err := ctx.Err(); err != nil {
		return xq.CMSMedia{}, nil, err
	}
	media, err := provider.Resolve(ctx, marker)
	if err != nil {
		// Upstream errors can contain signed media URLs. Keep the public error
		// local and leave the other lines available to the source selector.
		return xq.CMSMedia{}, nil, errors.New("二次元当前分集解析失败，请切换线路或稍后重试")
	}
	if !safePlayerAddress(media.URL) {
		return xq.CMSMedia{}, nil, errors.New("二次元返回了无效媒体地址")
	}
	headers := nativeMediaHeaders(media.Headers)
	return xq.CMSMedia{URL: media.URL, Referer: headers["Referer"]}, headers, nil
}

// Only media presentation headers are forwarded. A downloaded rule cannot
// replace transport/range/host headers or leak account credentials to a CDN.
func nativeMediaHeaders(input map[string]string) map[string]string {
	result := make(map[string]string)
	for key, value := range input {
		key = http.CanonicalHeaderKey(key)
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 4096 || strings.ContainsAny(value, "\r\n\x00") {
			continue
		}
		switch key {
		case "User-Agent", "Accept", "Accept-Language":
			result[key] = value
		case "Referer", "Origin":
			parsed, err := url.Parse(value)
			if err != nil || !safePlayerAddress(value) || parsed.Fragment != "" {
				continue
			}
			if key == "Origin" && (parsed.RawQuery != "" || parsed.Path != "" && parsed.Path != "/") {
				continue
			}
			result[key] = value
		}
	}
	return result
}

func applyNativeMediaHeaders(request *http.Request, headers map[string]string) {
	for key, value := range nativeMediaHeaders(headers) {
		request.Header.Set(key, value)
	}
	applyMediaReferer(request, request.Header.Get("Referer"))
}

func probeErciyuanDiscoveryWith(ctx context.Context, marker string, resolve erciyuanMediaResolver, client *http.Client, checkURL func(context.Context, string) error) error {
	if _, _, _, ok := erciyuan.ParseMarker(marker); !ok || resolve == nil {
		return errors.New("二次元分集标识无效")
	}
	media, headers, err := resolve(ctx, marker)
	if err != nil || len(media.Key) > 0 {
		return errors.New("二次元分集解析失败或格式暂不支持检测")
	}
	err = probeDiscoveryMediaHeaders(ctx, media, headers, client, checkURL)
	if !errors.Is(err, errSourceProbeTransient) || ctx.Err() != nil {
		return err
	}
	// Some native CDNs occasionally close the initial HLS connection with
	// EOF. Repeat this same guarded media probe once within its existing
	// deadline; malformed content and explicit HTTP failures are not retried.
	timer := time.NewTimer(250 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	return probeDiscoveryMediaHeaders(ctx, media, headers, client, checkURL)
}

func verifyErciyuanDiscoverySources(parent context.Context, input []source, filmID, episodeKey string, probe func(context.Context, string) error) []source {
	if probe == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	var sources []source
	seen := map[string]bool{}
	for _, src := range input {
		if len(sources) >= len(erciyuanPlayers) {
			break
		}
		if seen[src.Code] || strings.TrimSpace(src.Parse) != "" || !erciyuanAllowedSource(src) {
			continue
		}
		valid := true
		for _, ep := range src.Episodes {
			id, err := nativeErciyuanSeries(src.Code, ep.URL)
			if err != nil || id != filmID {
				valid = false
				break
			}
		}
		if valid {
			seen[src.Code] = true
			sources = append(sources, discoveryNormalizeSource(src))
		}
	}
	passed := make([]bool, len(sources))
	jobs := make(chan int, len(sources))
	for index := range sources {
		jobs <- index
	}
	close(jobs)
	var workers sync.WaitGroup
	for worker := 0; worker < 2; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				if ctx.Err() != nil {
					return
				}
				samples := discoverySampleURLs(sources[index], episodeKey)
				valid := len(samples) > 0
				lineCtx, lineCancel := context.WithTimeout(ctx, yqkDiscoveryProbeBudget(ctx, len(sources)-index, 2, 30*time.Second))
				for sampleIndex, marker := range samples {
					probeCtx, sampleCancel := context.WithTimeout(lineCtx, yqkDiscoveryProbeBudget(lineCtx, len(samples)-sampleIndex, 1, 12*time.Second))
					err := probe(probeCtx, marker)
					sampleCancel()
					if err != nil || lineCtx.Err() != nil || ctx.Err() != nil {
						valid = false
						break
					}
				}
				lineCancel()
				passed[index] = valid
			}
		}()
	}
	workers.Wait()
	result := make([]source, 0, len(sources))
	for index, src := range sources {
		if passed[index] {
			result = append(result, src)
		}
	}
	return result
}
