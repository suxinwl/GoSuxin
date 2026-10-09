package suxinvideo

import (
	"context"
	"net/url"
	"testing"
)

func TestVodSourceScoreDiscoveryRequiresUniqueFilmNotWorkingMedia(t *testing.T) {
	for _, tc := range []struct {
		name                string
		ambiguous, conflict bool
		want                bool
	}{
		{"unique identity despite failed media", false, false, true},
		{"two remote editions", true, false, false},
		{"contradictory remake", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			deps := discoveryProviderDeps{
				fetch: func(_ context.Context, _ string, q url.Values) (macPayload, error) {
					requests++
					item := discoveryFixtureItem()
					if q.Get("ac") == "detail" {
						item["vod_id"] = q.Get("ids")
						item["vod_douban_score"] = "8.8"
						item["vod_play_from"] = "testm3u8"
						item["vod_play_url"] = "第1集$https://fixture.invalid/first.m3u8"
						if tc.conflict {
							item["vod_year"] = "2025"
						}
						return macPayload{List: []map[string]any{item}}, nil
					}
					items := []map[string]any{item}
					if tc.ambiguous {
						other := discoveryFixtureItem()
						other["vod_id"] = "13"
						items = append(items, other)
					}
					return macPayload{List: items}, nil
				},
				visible: func(_ context.Context, _ int64, sources []source) ([]source, error) { return sources, nil },
				probe:   func(context.Context, string) error { return context.DeadlineExceeded },
			}
			got := discoverCollectorWith(context.Background(), discoveryFixtureTarget(), row{"id": 2, "status": 1, "api_url": "https://fixture.invalid/api"}, deps)
			if (got.RatingItem != nil) != tc.want {
				t.Fatal("rating attached to unproven or ambiguous edition")
			}
			if len(got.Sources) != 0 || got.Error == "" {
				t.Fatal("failed media was certified as playable")
			}
			if tc.want && (len(got.RatingItem) != 3 || requests != 2) {
				t.Fatal("rating used an additional request or retained media/private detail fields")
			}
		})
	}
}
