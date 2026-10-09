package suxinvideo

import (
	"context"
	"net/http"
	"time"

	xq "github.com/suxinwl/GoSuxin/internal/xiaoqiapp"
)

// A cold provider can need more than two seconds for a legitimate master and
// child manifest. Keep a bounded six-second budget without fetching media bytes.
const appStartupManifestBudget = 6 * time.Second

// A valid manifest is enough to start the preferred native line. Candidate
// selection after failure still uses the full media probe; playback/media
// authorization and SSRF validation are independently enforced on each fetch.
func probeAppStartupMediaHeaders(ctx context.Context, media xq.CMSMedia, headers map[string]string, client *http.Client, checkURL func(context.Context, string) error) error {
	probeClient := *client
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	probeClient.Transport = discoveryRefererTransport{base: base, referer: media.Referer, headers: nativeMediaHeaders(headers)}
	probe := sourceHealthProbe{client: &probeClient, checkURL: checkURL, startup: true}
	return probe.media(ctx, media.URL, 0, map[string]bool{})
}
