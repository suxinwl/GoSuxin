package suxinvideo

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	xq "github.com/suxinwl/GoSuxin/internal/xiaoqiapp"
)

func TestHongguoCollectorUsesRealKeywordSearch(t *testing.T) {
	bridge := fourKVMCollectFixture{search: func(_ context.Context, provider, keyword string) ([]xq.Drama, error) {
		if provider != "hongguo" || keyword != "冲喜赘婿竟是绝世神医" {
			t.Fatal("Hongguo search dispatched to a different provider")
		}
		return []xq.Drama{{SourceID: "7684943327614995518", Title: keyword, Category: "玄幻", TotalEpisode: "98"}}, nil
	}}
	result, err := fetchNativeCollectSourceWith(context.Background(), "hongguo", url.Values{"ac": {"videolist"}, "pg": {"1"}, "wd": {"冲喜赘婿竟是绝世神医"}}, bridge)
	if err != nil || len(result.List) != 1 || result.List[0]["type_name"] != "短剧" || result.List[0]["vod_id"] != "7684943327614995518" {
		t.Fatalf("Hongguo search was not normalized into a short-drama result: %+v error=%v", result, err)
	}
	for _, query := range []url.Values{{"ac": {"videolist"}, "pg": {"2"}, "wd": {"冲喜赘婿竟是绝世神医"}}, {"ac": {"videolist"}, "pg": {"1"}, "wd": {strings.Repeat("剧", 81)}}} {
		if _, err := fetchNativeCollectSourceWith(context.Background(), "hongguo", query, bridge); err == nil {
			t.Fatal("unsupported Hongguo search parameters accepted")
		}
	}
}

func TestHongguoDiscoveryExactShortIdentityAndNativeBinding(t *testing.T) {
	target := discoveryTarget{Name: "冲喜赘婿竟是绝世神医", Kind: "short", Year: "2026", Area: "中国大陆"}
	collector := row{"id": 9, "status": 1, "api_url": hongguoSourceURL}
	for _, mode := range []string{"working", "different-title", "movie", "contradictory-year", "foreign-series", "latest-fails", "short-title"} {
		t.Run(mode, func(t *testing.T) {
			current := target
			if mode == "movie" {
				current.Kind = "movie"
			}
			if mode == "short-title" {
				current.Name = "神医"
			}
			probes := 0
			deps := discoveryProviderDeps{
				fetch: func(_ context.Context, _ string, values url.Values) (macPayload, error) {
					item := map[string]any{"vod_id": "123", "vod_name": current.Name, "type_name": "短剧"}
					if mode == "different-title" {
						item["vod_name"] = "冲喜赘婿竟是绝世神医第二季"
					}
					if mode == "contradictory-year" {
						item["vod_year"] = "2025"
					}
					if values.Get("ac") == "detail" {
						item["vod_play_from"] = "hongguo"
						item["vod_play_url"] = "第1集$hongguo://123/11#第2集$hongguo://123/12"
						if mode == "foreign-series" {
							item["vod_play_url"] = "第1集$hongguo://999/11#第2集$hongguo://123/12"
						}
					}
					return macPayload{Code: 1, List: []map[string]any{item}}, nil
				},
				visible: func(_ context.Context, _ int64, sources []source) ([]source, error) { return sources, nil },
				hongguoProbe: func(_ context.Context, marker string) error {
					probes++
					if mode == "latest-fails" && strings.HasSuffix(marker, "/12") {
						return errors.New("unavailable")
					}
					return nil
				},
			}
			got := discoverCollectorWith(context.Background(), current, collector, deps)
			if mode == "working" {
				if len(got.Sources) != 1 || got.Sources[0].Code != "hongguo" || len(got.Sources[0].Episodes) != 2 || probes != 2 || got.Error != "" {
					t.Fatalf("verified short-drama source was not returned: %+v probes=%d", got, probes)
				}
			} else if len(got.Sources) != 0 || got.Error == "" {
				t.Fatalf("unverified/different native source accepted (%s): %+v", mode, got)
			}
		})
	}
	valid := source{Code: "hongguo", Episodes: []episode{{Name: "第1集", URL: "hongguo://123/11"}}}
	if !discoverySourceAllowed(valid, collector, nil) || discoverySourceAllowed(valid, row{"id": 5, "api_url": "https://foreign.example/api"}, nil) {
		t.Fatal("Hongguo native ownership did not follow its own collector")
	}
	for _, marker := range []string{"hongguo://123/11?token=secret", "hongguo://123/11/", "hongguo://0/11", "hongguo://user@123/11", "hongguo://123/11#other"} {
		bad := valid
		bad.Episodes = []episode{{URL: marker}}
		if discoverySourceAllowed(bad, collector, nil) {
			t.Fatal("invalid native marker accepted")
		}
	}
}

func TestHongguoProbeChecksExtensionlessContainerAndDecode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "" || r.Header.Get("Referer") != "https://hongguoduanju.com/" {
			t.Error("probe omitted source headers or byte range")
		}
		w.Header().Set("Content-Type", "video/mp4")
		w.Write([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0, 0, 0, 0})
	}))
	defer server.Close()
	check := func(_ context.Context, raw string) error {
		if raw != server.URL+"/extensionless" {
			return errors.New("unapproved address")
		}
		return nil
	}
	for _, failDecode := range []bool{false, true} {
		decoded := 0
		resolve := func(_ context.Context, series, video string) (xq.CMSMedia, error) {
			if series != "123" || video != "hongguo-cenc://11" {
				t.Fatal("resolver identity changed")
			}
			return xq.CMSMedia{URL: server.URL + "/extensionless", Referer: "https://hongguoduanju.com/", Key: make([]byte, 16)}, nil
		}
		decode := func(_ context.Context, media xq.CMSMedia) error {
			decoded++
			if failDecode {
				return errors.New("bad key")
			}
			return nil
		}
		err := probeHongguoDiscoveryWith(context.Background(), "hongguo://123/11", resolve, server.Client(), check, decode)
		if decoded != 1 || (err != nil) != failDecode {
			t.Fatalf("invalid decrypt result accepted: decode=%d fail=%v err=%v", decoded, failDecode, err)
		}
	}
}

func TestDiscoveryCategoryPreferenceKeepsAllCollectors(t *testing.T) {
	collectors := []row{{"id": 1, "api_url": "https://api.example"}, {"id": 9, "api_url": hongguoSourceURL}, {"id": 25, "api_url": yqkSourceURL}, {"id": 30, "api_url": erciyuanSourceURL}}
	for kind, expected := range map[string]int64{"short": 9, "anime": 30, "anime_movie": 30, "movie": 25} {
		got := discoveryPrioritizeCollectors(collectors, 1, kind)
		if got[0]["id"] != int(expected) || len(got) != len(collectors) || collectors[0]["id"] != 1 {
			t.Fatalf("category collector priority mutated/dropped inventory: kind=%s got=%v", kind, got)
		}
	}
}
