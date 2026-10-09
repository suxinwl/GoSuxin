package suxinvideo

import (
	"context"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

const discoveryAnchorFixtureURL = "https://cdn.bdzy.example/feature/index.m3u8?signature=unchanged"

func discoveryAnchorFixture() (discoveryTarget, []source, []row) {
	target := discoveryTarget{ID: 2164, Name: "仙逆剧场版弑仙之战", Year: "2026", Kind: "anime_movie", SourceAPIID: 2, SourceAPIVID: "158030"}
	existing := []source{{Code: "dbm3u8", Episodes: []episode{{Name: "第01集", URL: discoveryAnchorFixtureURL}}}}
	collectors := []row{
		{"id": 2, "status": 1, "api_url": "https://maoyanapi.top/api.php/provide/vod"},
		{"id": 5, "status": 1, "api_url": "https://api.bdzyapi.com/api.php/provide/vod"},
		{"id": 25, "status": 1, "api_url": yqkSourceURL},
	}
	return target, existing, collectors
}
func discoveryAnchorItem() map[string]any {
	return map[string]any{"vod_id": "51347", "vod_name": "仙逆剧场版 弑仙之战", "vod_year": "2026", "vod_area": "中国大陆", "type_name": "国产动漫",
		"vod_play_from": "dbm3u8", "vod_play_url": "第01集$" + discoveryAnchorFixtureURL}
}

func TestDiscoveryRegionEnrichmentFromExactStoredURL(t *testing.T) {
	target, existing, collectors := discoveryAnchorFixture()
	var searches []string
	details := 0
	got := enrichDiscoveryFromStoredLines(context.Background(), target, existing, collectors, func(ctx context.Context, raw string, query url.Values) (macPayload, error) {
		if !strings.Contains(raw, "bdzyapi") {
			t.Fatal("queried original, native or unrelated provider")
		}
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 8*time.Second {
			t.Fatal("source lookup escaped 8-second bound")
		}
		if query.Get("ac") == "videolist" {
			searches = append(searches, query.Get("wd"))
			if query.Get("wd") != "仙逆剧场版 弑仙之战" {
				return macPayload{Code: 1}, nil
			}
			return macPayload{Code: 1, List: []map[string]any{discoveryAnchorItem()}}, nil
		}
		if query.Get("ac") != "detail" || query.Get("ids") != "51347" {
			t.Fatal("did not fetch exact candidate remote ID")
		}
		details++
		return macPayload{Code: 1, List: []map[string]any{discoveryAnchorItem()}}, nil
	})
	want := target
	want.Area = "中国大陆"
	if !reflect.DeepEqual(got, want) || details != 1 || !reflect.DeepEqual(searches, []string{target.Name, "仙逆剧场版 弑仙之战"}) {
		t.Fatalf("exact independent source evidence failed: target=%+v details=%d searches=%v", got, details, searches)
	}
}

func TestDiscoveryRegionEnrichmentRejectsUnprovenCandidates(t *testing.T) {
	for _, tc := range []struct{ name, field, value string }{
		{"different query", "vod_play_url", "第01集$https://cdn.bdzy.example/feature/index.m3u8?signature=changed"},
		{"different code", "vod_play_from", "ffm3u8"},
		{"different year", "vod_year", "2025"},
		{"missing year", "vod_year", ""},
		{"different full title", "vod_name", "仙逆剧场版第二季"},
		{"different kind", "type_name", "短剧"},
		{"missing kind", "type_name", ""},
		{"two chapters", "vod_play_url", "第01集$" + discoveryAnchorFixtureURL + "#第02集$https://cdn.bdzy.example/2.m3u8"},
		{"unknown region", "vod_area", "未知"},
		{"different detail ID", "vod_id", "999"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target, existing, collectors := discoveryAnchorFixture()
			got := enrichDiscoveryFromStoredLines(context.Background(), target, existing, collectors, func(_ context.Context, _ string, query url.Values) (macPayload, error) {
				item := discoveryAnchorItem()
				if query.Get("ac") == "detail" {
					item[tc.field] = tc.value
				}
				return macPayload{Code: 1, List: []map[string]any{item}}, nil
			})
			if !reflect.DeepEqual(got, target) {
				t.Fatalf("unproven evidence changed target: %+v", got)
			}
		})
	}
}
func TestDiscoveryRegionEnrichmentSkipsUntrustedOrUnneededLookups(t *testing.T) {
	target, existing, collectors := discoveryAnchorFixture()
	check := func(t *testing.T, target discoveryTarget, existing []source, collectors []row) {
		t.Helper()
		got := enrichDiscoveryFromStoredLines(context.Background(), target, existing, collectors, func(context.Context, string, url.Values) (macPayload, error) {
			t.Fatal("should not query a provider without an eligible existing anchor")
			return macPayload{}, nil
		})
		if !reflect.DeepEqual(got, target) {
			t.Fatal("target changed without permitted lookup")
		}
	}
	t.Run("known region", func(t *testing.T) { known := target; known.Area = "大陆"; check(t, known, existing, collectors) })
	t.Run("unknown year", func(t *testing.T) { unknown := target; unknown.Year = ""; check(t, unknown, existing, collectors) })
	t.Run("unknown kind", func(t *testing.T) { unknown := target; unknown.Kind = ""; check(t, unknown, existing, collectors) })
	t.Run("no existing line", func(t *testing.T) { check(t, target, nil, collectors) })
	t.Run("disabled collector", func(t *testing.T) {
		check(t, target, existing, []row{{"id": 5, "status": 0, "api_url": "https://api.bdzyapi.com/api.php/provide/vod"}})
	})
	t.Run("original collector already tried", func(t *testing.T) { other := target; other.SourceAPIID = 5; check(t, other, existing, collectors) })
	t.Run("yqk cannot confirm own data", func(t *testing.T) {
		check(t, target, []source{{Code: "yqk_1", Episodes: []episode{{Name: "HD", URL: "yqk://469365/1/100"}}}}, collectors)
	})
	t.Run("unsupported native", func(t *testing.T) {
		check(t, target, []source{{Code: "4kvm", Episodes: []episode{{Name: "HD", URL: "4kvm://movie?dataid=1"}}}}, []row{{"id": 24, "status": 1, "api_url": fourKVMSourceURL}})
	})
	t.Run("unknown ownership", func(t *testing.T) {
		check(t, target, []source{{Code: "unknownm3u8", Episodes: []episode{{Name: "HD", URL: "https://media.example/a.m3u8"}}}}, []row{{"id": 9, "status": 1, "api_url": "https://unknown.example/api.php"}})
	})
	t.Run("parser line", func(t *testing.T) {
		check(t, target, []source{{Code: "dbm3u8", Parse: "https://parser.example/?url=", Episodes: existing[0].Episodes}}, collectors)
	})
	t.Run("old douban shared code", func(t *testing.T) {
		check(t, target, []source{{Code: "dbm3u8", Episodes: []episode{{Name: "HD", URL: "https://vodcnd09.ajupf.com/old/index.m3u8"}}}}, collectors)
	})
}
func TestDiscoveryRegionEnrichmentFailureAndCancellation(t *testing.T) {
	target, existing, collectors := discoveryAnchorFixture()
	got := enrichDiscoveryFromStoredLines(context.Background(), target, existing, collectors, func(context.Context, string, url.Values) (macPayload, error) {
		return macPayload{}, errors.New("source unavailable")
	})
	if !reflect.DeepEqual(got, target) {
		t.Fatal("failed source lookup changed target")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got = enrichDiscoveryFromStoredLines(ctx, target, existing, collectors, func(context.Context, string, url.Values) (macPayload, error) {
		t.Fatal("cancelled work queried source")
		return macPayload{}, nil
	})
	if !reflect.DeepEqual(got, target) {
		t.Fatal("cancelled lookup changed target")
	}
}

func TestDiscoveryRegionEnrichmentRejectsSplitKnownMovie(t *testing.T) {
	target, existing, collectors := discoveryAnchorFixture()
	got := enrichDiscoveryFromStoredLines(context.Background(), target, existing, collectors, func(_ context.Context, _ string, query url.Values) (macPayload, error) {
		item := discoveryAnchorItem()
		item["type_name"] = "动画片"
		if query.Get("ac") == "detail" {
			item["vod_play_url"] = "第01集$" + discoveryAnchorFixtureURL + "#第02集$https://cdn.bdzy.example/part2.m3u8"
		}
		return macPayload{Code: 1, List: []map[string]any{item}}, nil
	})
	if !reflect.DeepEqual(got, target) {
		t.Fatal("same movie category bypassed split-release protection")
	}
}
