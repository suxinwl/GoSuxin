package suxinvideo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func discoveryFixtureItem() map[string]any {
	return map[string]any{"vod_id": "12", "vod_name": "仙逆", "vod_year": "2023", "vod_area": "大陆", "type_name": "国产动漫"}
}

func discoveryFixtureTarget() discoveryTarget {
	return discoveryTarget{ID: 781, Name: "仙逆", Year: "2023", Area: "中国大陆", Kind: "anime", SourceAPIID: 4}
}

func TestDiscoveryStrictFilmIdentity(t *testing.T) {
	target := discoveryFixtureTarget()
	if !discoveryMatches(target, discoveryFixtureItem(), 2) {
		t.Fatal("same 2023 mainland anime was rejected")
	}
	for _, tc := range []struct {
		name, field, value string
	}{
		{"remake", "vod_year", "2025"},
		{"short drama", "type_name", "古装仙侠"},
		{"animated film", "type_name", "动画电影"},
		{"movie", "type_name", "剧情片"},
		{"season", "vod_name", "仙逆第二季"},
		{"special", "vod_name", "仙逆特别篇"},
		{"region", "vod_area", "日本"},
		{"missing year", "vod_year", ""},
		{"missing region", "vod_area", "未知"},
		{"missing kind", "type_name", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := discoveryFixtureItem()
			item[tc.field] = tc.value
			if discoveryMatches(target, item, 2) {
				t.Fatalf("different or unproven identity was accepted: %+v", item)
			}
		})
	}
	item := discoveryFixtureItem()
	item["vod_name"] = " 仙\t逆 "
	if !discoveryMatches(target, item, 2) {
		t.Fatal("insignificant title whitespace changed identity")
	}
	target.SourceAPIID, target.SourceAPIVID = 2, "12"
	item["vod_year"] = ""
	if !discoveryMatches(target, item, 2) || discoveryMatches(target, item, 3) {
		t.Fatal("missing metadata exception did not require exact remote identity")
	}
	item["vod_year"] = "2025"
	if discoveryMatches(target, item, 2) {
		t.Fatal("exact remote ID allowed an explicit metadata contradiction")
	}
}

func TestDiscoveryEnrichesOnlyConfirmedOriginalIdentity(t *testing.T) {
	target := discoveryFixtureTarget()
	target.SourceAPIID, target.SourceAPIVID, target.Area = 2, "12", ""
	collector := row{"id": 2, "status": 1, "api_url": "https://original.example"}
	fetch := func(_ context.Context, _ string, query url.Values) (macPayload, error) {
		if query.Get("ac") != "detail" || query.Get("ids") != "12" {
			t.Fatal("original metadata was searched by name rather than stored remote ID")
		}
		return macPayload{Code: 1, List: []map[string]any{discoveryFixtureItem()}}, nil
	}
	got := enrichDiscoveryTargetWith(context.Background(), target, collector, fetch)
	if discoveryRegion(got.Area) != "cn" || got.Year != target.Year || got.Name != target.Name || got.Kind != target.Kind {
		t.Fatalf("missing region was not independently enriched: %+v", got)
	}
	for _, tc := range []struct{ field, value string }{{"vod_id", "99"}, {"vod_name", "仙逆第二季"}, {"vod_year", "2025"}, {"type_name", "古装仙侠"}} {
		got = enrichDiscoveryTargetWith(context.Background(), target, collector, func(context.Context, string, url.Values) (macPayload, error) {
			item := discoveryFixtureItem()
			item[tc.field] = tc.value
			return macPayload{Code: 1, List: []map[string]any{item}}, nil
		})
		if !reflect.DeepEqual(got, target) {
			t.Fatalf("contradictory original metadata changed target (%s): %+v", tc.field, got)
		}
	}
	for _, cfg := range []row{{"id": 3, "status": 1, "api_url": "https://foreign.example"}, {"id": 2, "status": 0, "api_url": "https://original.example"}, {"id": 3, "status": 1, "api_url": "hongguo://app"}} {
		got = enrichDiscoveryTargetWith(context.Background(), target, cfg, func(context.Context, string, url.Values) (macPayload, error) {
			t.Fatal("foreign, disabled or unsupported provider attempted enrichment")
			return macPayload{}, nil
		})
		if !reflect.DeepEqual(got, target) {
			t.Fatal("untrusted metadata altered target")
		}
	}
	got = enrichDiscoveryTargetWith(context.Background(), target, collector, func(context.Context, string, url.Values) (macPayload, error) {
		return macPayload{}, context.DeadlineExceeded
	})
	if !reflect.DeepEqual(got, target) {
		t.Fatal("failed original lookup changed target")
	}
}

func TestDiscoveryEpisodeOrderAndSamples(t *testing.T) {
	src := source{Code: " test ", Episodes: []episode{
		{Name: "回顾特辑", URL: "https://cdn.example/extra.m3u8"},
		{Name: "第160集", URL: "https://cdn.example/last.m3u8"},
		{Name: "第\t001\t集", URL: " https://cdn.example/first.m3u8\t"},
		{Name: "EP01", URL: "https://cdn.example/duplicate.m3u8"},
		{Name: "0", URL: "https://cdn.example/zero.m3u8"},
		{Name: "第2集", URL: "https://cdn.example/current.m3u8"},
		{Name: "第2集预告", URL: "https://cdn.example/preview.m3u8"},
	}}
	src = discoveryNormalizeSource(src)
	if len(src.Episodes) != 6 || src.Code != "test" || src.Episodes[0].Name != "0" || src.Episodes[1].URL != "https://cdn.example/first.m3u8" || src.Episodes[4].Name != "回顾特辑" {
		t.Fatalf("numeric order, zero, whitespace or extras were mishandled: %+v", src)
	}
	samples := discoverySampleURLs(src, "episode:2")
	want := []string{"https://cdn.example/zero.m3u8", "https://cdn.example/last.m3u8", "https://cdn.example/current.m3u8"}
	if !reflect.DeepEqual(samples, want) {
		t.Fatalf("sampled extras or missed current/last episode: %v", samples)
	}
	if got := discoverySampleURLs(src, "episode:99"); len(got) != 0 {
		t.Fatal("line missing current episode was certified")
	}
}

func TestDiscoveryRequiresFirstAndLatestMedia(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusForbidden, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var server *httptest.Server
			calls := map[string]int{}
			transport := make([]byte, 188*3)
			transport[0], transport[188], transport[376] = 0x47, 0x47, 0x47
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls[r.URL.Path]++
				switch r.URL.Path {
				case "/api.php":
					item := discoveryFixtureItem()
					if r.URL.Query().Get("ac") == "videolist" {
						if r.URL.Query().Get("wd") != "仙逆" || r.URL.Query().Get("pg") != "1" {
							t.Error("search did not use exact requested title/page")
						}
					} else if r.URL.Query().Get("ac") != "detail" || r.URL.Query().Get("ids") != "12" {
						t.Error("unexpected detail query")
					}
					item["vod_play_from"] = "testm3u8"
					item["vod_play_url"] = "回顾特辑$" + server.URL + "/extra.m3u8#第160集$" + server.URL + "/last.m3u8#EP01$" + server.URL + "/first.m3u8"
					_ = json.NewEncoder(w).Encode(macPayload{Code: 1, List: []map[string]any{item}})
				case "/first.m3u8", "/last.m3u8":
					if r.URL.Path == "/last.m3u8" && status != http.StatusOK {
						w.WriteHeader(status)
						return
					}
					fmt.Fprint(w, "#EXTM3U\n#EXTINF:6,\nvideo.ts\n#EXT-X-ENDLIST\n")
				case "/video.ts":
					_, _ = w.Write(transport)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			probe := healthTestProbe(server)
			deps := discoveryProviderDeps{
				fetch: func(ctx context.Context, raw string, query url.Values) (macPayload, error) {
					req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw+"?"+query.Encode(), nil)
					resp, err := server.Client().Do(req)
					if err != nil {
						return macPayload{}, err
					}
					defer resp.Body.Close()
					var result macPayload
					err = json.NewDecoder(resp.Body).Decode(&result)
					return result, err
				},
				visible: func(_ context.Context, _ int64, sources []source) ([]source, error) { return sources, nil },
				probe:   func(ctx context.Context, raw string) error { return probe.media(ctx, raw, 0, map[string]bool{}) },
			}
			result := discoverCollectorWith(context.Background(), discoveryFixtureTarget(), row{"id": 2, "status": 1, "api_url": server.URL + "/api.php"}, deps)
			if status == http.StatusOK {
				if len(result.Sources) != 1 || result.Error != "" || result.Sources[0].Episodes[0].Name != "EP01" {
					t.Fatalf("verified regular line not returned: %+v", result)
				}
			} else if len(result.Sources) != 0 || result.Error == "" {
				t.Fatalf("one working episode certified a failing latest episode: %+v", result)
			}
			if calls["/first.m3u8"] != 1 || calls["/last.m3u8"] != 1 || calls["/extra.m3u8"] != 0 || calls["/video.ts"] == 0 {
				t.Fatalf("did not inspect real regular media: %+v", calls)
			}
		})
	}
}

func TestDiscoveryDisabledNativeAndSanitizedFailure(t *testing.T) {
	deps := discoveryProviderDeps{fetch: func(context.Context, string, url.Values) (macPayload, error) {
		return macPayload{}, errors.New("request https://secret.example/x?token=private failed")
	}}
	for _, collector := range []row{{"id": 9, "status": 1, "api_url": "hongguo://app"}, {"id": 10, "status": 1, "api_url": "4kvm://site"}, {"id": 4, "status": 0, "api_url": "https://api.example"}, {"id": 2, "status": 1, "api_url": "https://api.example"}} {
		result := discoverCollectorWith(context.Background(), discoveryFixtureTarget(), collector, deps)
		if result.Error == "" || len(result.Sources) != 0 || strings.Contains(result.Error, "secret") || strings.Contains(result.Error, "token") {
			t.Fatalf("unsupported/failed collector returned unsafe result: %+v", result)
		}
	}
}

func TestDiscoveryBoundsCandidatesAndSkipsDisabledPlayers(t *testing.T) {
	details, probes := 0, 0
	deps := discoveryProviderDeps{
		fetch: func(_ context.Context, _ string, values url.Values) (macPayload, error) {
			if values.Get("ac") == "detail" {
				details++
				item := discoveryFixtureItem()
				item["vod_id"] = values.Get("ids")
				item["vod_play_from"] = "testm3u8"
				item["vod_play_url"] = "第1集$https://cdn.example/first.m3u8"
				return macPayload{Code: 1, List: []map[string]any{item}}, nil
			}
			items := make([]map[string]any, 50)
			for i := range items {
				items[i] = discoveryFixtureItem()
				items[i]["vod_id"] = fmt.Sprint(i + 1)
			}
			return macPayload{Code: 1, List: items}, nil
		},
		visible: func(_ context.Context, id int64, sources []source) ([]source, error) {
			return availableSources(row{"api_id": id}, sources, map[string]row{"testm3u8": {"status": 0}}, []row{{"id": id, "status": 1}}), nil
		},
		probe: func(context.Context, string) error { probes++; return nil },
	}
	got := discoverCollectorWith(context.Background(), discoveryFixtureTarget(), row{"id": 2, "status": 1, "api_url": "https://api.example"}, deps)
	if details != 2 || probes != 0 || len(got.Sources) != 0 {
		t.Fatalf("candidate bounds or disabled player ignored: details=%d probes=%d result=%+v", details, probes, got)
	}
	deps.visible = func(_ context.Context, _ int64, sources []source) ([]source, error) { return sources, nil }
	deps.probe = func(context.Context, string) error { return context.DeadlineExceeded }
	got = discoverCollectorWith(context.Background(), discoveryFixtureTarget(), row{"id": 2, "status": 1, "api_url": "https://api.example"}, deps)
	if len(got.Sources) != 0 {
		t.Fatal("timed-out media probe returned a verified line")
	}
}

func TestDiscoveryDoesNotSearchBeyondFirstFortyResults(t *testing.T) {
	details := 0
	deps := discoveryProviderDeps{fetch: func(_ context.Context, _ string, values url.Values) (macPayload, error) {
		if values.Get("ac") == "detail" {
			details++
			return macPayload{}, nil
		}
		items := make([]map[string]any, 41)
		for i := range items {
			items[i] = discoveryFixtureItem()
			items[i]["vod_id"] = fmt.Sprint(i + 1)
			if i < 40 {
				items[i]["vod_name"] = "仙逆第二季"
			}
		}
		return macPayload{Code: 1, List: items}, nil
	}}
	got := discoverCollectorWith(context.Background(), discoveryFixtureTarget(), row{"id": 2, "status": 1, "api_url": "https://api.example"}, deps)
	if details != 0 || len(got.Sources) != 0 {
		t.Fatalf("search exceeded bounded first page results: details=%d result=%+v", details, got)
	}
}

func TestDiscoveryTheatricalAnimeNeedsFeatureEvidence(t *testing.T) {
	target := discoveryTarget{Name: "仙逆剧场版弑仙之战", Year: "2026", Area: "中国大陆", Kind: "anime_movie", SourceAPIID: 2}
	item := func() map[string]any {
		return map[string]any{"vod_id": "469365", "vod_name": "仙逆剧场版 弑仙之战", "vod_year": "2026", "vod_area": "大陆", "type_name": "国产动漫", "vod_play_from": "yqk_1$$$yqk_8", "vod_play_url": "HD$yqk://469365/1/100$$$第01集$yqk://469365/8/101"}
	}
	if !discoveryMatches(target, item(), 25) {
		t.Fatal("proven same theatrical anime rejected by broad category")
	}
	search := item()
	delete(search, "vod_play_from")
	delete(search, "vod_play_url")
	if !discoveryCompatible(target, search, 25, true) || discoveryMatches(target, search, 25) {
		t.Fatal("search may fetch details, but cannot certify film type without full playlist")
	}
	for _, tc := range []struct{ name, field, value string }{
		{"different year", "vod_year", "2025"}, {"missing year", "vod_year", ""},
		{"different region", "vod_area", "日本"}, {"missing region", "vod_area", ""},
		{"short drama", "type_name", "短剧"}, {"live series", "type_name", "国产剧"},
		{"two numeric episodes", "vod_play_url", "第01集$https://media.example/1.m3u8#第02集$https://media.example/2.m3u8"},
		{"split movie", "vod_play_url", "上篇$https://media.example/1.m3u8#下篇$https://media.example/2.m3u8"},
		{"only trailer", "vod_play_url", "预告$https://media.example/1.m3u8"},
		{"unknown single label", "vod_play_url", "资源1$https://media.example/1.m3u8"},
		{"single episode two", "vod_play_url", "第02集$https://media.example/2.m3u8"},
		{"different title", "vod_name", "仙逆剧场版第二季"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := item()
			candidate[tc.field] = tc.value
			if discoveryMatches(target, candidate, 25) {
				t.Fatalf("unsafe theatrical match: %s", tc.name)
			}
			if tc.field == "vod_play_url" && discoveryCompatible(target, candidate, 25, true) {
				t.Fatal("allowMissing bypassed complete-detail feature evidence")
			}
		})
	}
	for _, title := range []string{"仙逆", "仙逆特别篇", "仙逆剧场版TV版", "仙逆剧场版第二季", "仙逆剧场版电视版"} {
		t.Run(title, func(t *testing.T) {
			other := target
			other.Name = title
			candidate := item()
			candidate["vod_name"] = title
			if discoveryCompatible(other, candidate, 25, true) || discoveryMatches(other, candidate, 25) {
				t.Fatal("non-theatrical, TV or season naming bypassed type check")
			}
		})
	}
	missingRegion := target
	missingRegion.Area = ""
	if discoveryMatches(missingRegion, item(), 25) {
		t.Fatal("candidate supplied its own missing target-region evidence")
	}
	original := target
	original.Kind = "anime"
	regular := item()
	regular["type_name"] = "动画片"
	if discoveryMatches(original, regular, 25) {
		t.Fatal("broadened converse mismatch without known movie target")
	}
}

func TestDiscoveryTheatricalSearchRequiresVerifiedDetail(t *testing.T) {
	target := discoveryTarget{Name: "测试剧场版", Year: "2026", Area: "大陆", Kind: "anime_movie"}
	for _, detailLabel := range []string{"HD", "第02集"} {
		t.Run(detailLabel, func(t *testing.T) {
			details, probes := 0, 0
			deps := discoveryProviderDeps{
				fetch: func(_ context.Context, _ string, query url.Values) (macPayload, error) {
					item := map[string]any{"vod_id": "12", "vod_name": target.Name, "vod_year": target.Year, "vod_area": target.Area, "type_name": "国产动漫"}
					if query.Get("ac") == "detail" {
						details++
						item["vod_play_from"] = "testm3u8"
						item["vod_play_url"] = detailLabel + "$https://media.example/feature.m3u8"
					}
					return macPayload{Code: 1, List: []map[string]any{item}}, nil
				},
				visible: func(_ context.Context, _ int64, sources []source) ([]source, error) { return sources, nil },
				probe:   func(context.Context, string) error { probes++; return nil },
			}
			got := discoverCollectorWith(context.Background(), target, row{"id": 2, "status": 1, "api_url": "https://api.example"}, deps)
			if details != 1 {
				t.Fatal("broad anime search candidate was never detailed")
			}
			if detailLabel == "HD" {
				if len(got.Sources) != 1 || probes != 1 {
					t.Fatalf("single verified feature not returned: sources=%d probes=%d", len(got.Sources), probes)
				}
			} else if len(got.Sources) != 0 || probes != 0 {
				t.Fatal("second episode certified as a feature")
			}
		})
	}
}

func TestYQKDiscoveryFairBudgetsReachLaterWorkingSources(t *testing.T) {
	kinds := []string{"1", "2", "3", "5", "8", "12"}
	var sources []source
	for _, kind := range kinds {
		sources = append(sources, source{Code: yqkPlayerCode(kind), Episodes: []episode{
			{Name: "第01集", URL: "yqk://12/" + kind + "/100"},
			{Name: "第02集", URL: "yqk://12/" + kind + "/101"},
		}})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 900*time.Millisecond)
	defer cancel()
	var mu sync.Mutex
	visited := map[string]int{}
	got := verifyYQKDiscoverySources(ctx, sources, "", func(probeCtx context.Context, marker string) error {
		_, kind, _, err := yqkMarkerParts(marker)
		if err != nil {
			return err
		}
		mu.Lock()
		visited[kind]++
		mu.Unlock()
		if kind == "1" || kind == "2" || kind == "3" {
			<-probeCtx.Done()
			return probeCtx.Err()
		}
		if kind == "12" && strings.HasSuffix(marker, "/101") {
			return errors.New("latest episode unavailable")
		}
		return nil
	})
	if len(got) != 2 || got[0].Code != "yqk_5" || got[1].Code != "yqk_8" {
		t.Fatalf("slow leading sources starved later verified lines, or failed last episode accepted: codes=%v", got)
	}
	for _, kind := range []string{"5", "8", "12"} {
		if visited[kind] != 2 {
			t.Fatalf("later source %s first/latest not both attempted: %d", kind, visited[kind])
		}
	}
}
func TestYQKDiscoverySingleFilmSampleDeduplicated(t *testing.T) {
	calls := 0
	src := source{Code: "yqk_1", Episodes: []episode{{Name: "HD", URL: "yqk://12/1/100"}}}
	got := verifyYQKDiscoverySources(context.Background(), []source{src}, "", func(context.Context, string) error { calls++; return nil })
	if calls != 1 || len(got) != 1 {
		t.Fatalf("same first/last sample fetched twice: calls=%d", calls)
	}
}
func TestYQKDiscoveryAggregateDeadlineDiffersFromSingleProvider(t *testing.T) {
	for _, tc := range []struct {
		address          string
		minimum, maximum time.Duration
	}{
		{yqkSourceURL, 50 * time.Second, 60 * time.Second},
		{"https://api.example", 15 * time.Second, 20 * time.Second},
	} {
		var providerTime, fetchTime time.Duration
		deps := discoveryProviderDeps{
			fetch: func(ctx context.Context, _ string, query url.Values) (macPayload, error) {
				deadline, _ := ctx.Deadline()
				fetchTime = time.Until(deadline)
				item := discoveryFixtureItem()
				item["vod_play_from"], item["vod_play_url"] = "yqk_1", "第1集$yqk://12/1/100"
				return macPayload{Code: 1, List: []map[string]any{item}}, nil
			},
			visible: func(ctx context.Context, _ int64, _ []source) ([]source, error) {
				deadline, _ := ctx.Deadline()
				providerTime = time.Until(deadline)
				return nil, nil
			},
		}
		discoverCollectorWith(context.Background(), discoveryFixtureTarget(), row{"id": 2, "status": 1, "api_url": tc.address}, deps)
		if providerTime < tc.minimum || providerTime > tc.maximum {
			t.Fatalf("wrong aggregate provider deadline: %s %s", tc.address, providerTime)
		}
		if fetchTime <= 0 || fetchTime > 12*time.Second {
			t.Fatalf("search/detail escaped request bound: %s", fetchTime)
		}
	}
}
