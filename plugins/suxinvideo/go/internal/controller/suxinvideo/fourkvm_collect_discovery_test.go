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
	"testing"

	xq "github.com/suxinwl/GoSuxin/internal/xiaoqiapp"
)

type fourKVMCollectFixture struct {
	search func(context.Context, string, string) ([]xq.Drama, error)
	detail func(context.Context, string, string) (xq.Drama, []xq.Chapter, error)
}

func (f fourKVMCollectFixture) Search(ctx context.Context, provider, keyword string) ([]xq.Drama, error) {
	if f.search == nil {
		return nil, errors.New("unexpected search")
	}
	return f.search(ctx, provider, keyword)
}
func (f fourKVMCollectFixture) Detail(ctx context.Context, provider, id string) (xq.Drama, []xq.Chapter, error) {
	if f.detail == nil {
		return xq.Drama{}, nil, errors.New("unexpected detail")
	}
	return f.detail(ctx, provider, id)
}
func (fourKVMCollectFixture) List(context.Context, string, int, int) (xq.CMSPage, error) {
	return xq.CMSPage{}, errors.New("search incorrectly fell back to category listing")
}

func TestFourKVMCollectorUsesRealSearch(t *testing.T) {
	calls := 0
	bridge := fourKVMCollectFixture{search: func(_ context.Context, provider, keyword string) ([]xq.Drama, error) {
		calls++
		if provider != "4kvm" || keyword != "仙逆" {
			t.Fatalf("search parameters changed: %q %q", provider, keyword)
		}
		return []xq.Drama{{SourceID: "xn2023", Title: "仙逆", Category: "国产动漫", OnlineDate: "2023", Area: "中国大陆"}}, nil
	}}
	got, err := fetchNativeCollectSourceWith(context.Background(), "4kvm", url.Values{"ac": {"videolist"}, "pg": {"1"}, "wd": {" 仙逆 "}}, bridge)
	if err != nil || calls != 1 || got.Page != 1 || got.PageCount != 1 || len(got.List) != 1 || got.List[0]["vod_id"] != "xn2023" || got.List[0]["vod_area"] != "中国大陆" {
		t.Fatalf("real search metadata was not preserved: %+v, %v", got, err)
	}
	for _, provider := range []string{"native-other", "4kvm-other"} {
		if _, err := fetchNativeCollectSourceWith(context.Background(), provider, url.Values{"ac": {"videolist"}, "pg": {"1"}, "wd": {"仙逆"}}, bridge); err == nil {
			t.Fatalf("unsupported native search accepted: %s", provider)
		}
	}
	if calls != 1 {
		t.Fatal("another native provider called 4KVM search")
	}
	if _, err := fetchNativeCollectSourceWith(context.Background(), "4kvm", url.Values{"ac": {"videolist"}, "pg": {"2"}, "wd": {"仙逆"}}, bridge); err == nil {
		t.Fatal("an unsupported search page repeated page one")
	}
}

func TestFourKVMCollectorPrefersSourceDefaultWithoutMergingSpecials(t *testing.T) {
	chapter := func(title, line, dataID string, defaultLine bool) xq.Chapter {
		return xq.Chapter{Title: title, SourceLine: line, DefaultLine: defaultLine, VideoURL: "4kvm://xn2023?dataid=" + dataID + "&quality=1080"}
	}
	chapters := []xq.Chapter{
		chapter("第01集", "alternate", "1", false),
		chapter("第2集", "alternate", "2", false),
		chapter("第1集预告", "alternate", "3", false),
		chapter("正片", "alternate", "4", false),
		chapter("预告片", "alternate", "5", false),
		chapter("EP01", "primary", "11", true),
		chapter("第1集预告", "primary", "13", true),
		chapter("正片", "primary", "14", true),
		{SourceLine: "primary", DefaultLine: true, CurrentEpisode: json.RawMessage("3"), VideoURL: "4kvm://xn2023?dataid=15&quality=1080"},
		{SourceLine: "primary", DefaultLine: true, VideoURL: "4kvm://xn2023?dataid=16&quality=1080"},
		{Title: "第4集", SourceLine: "primary", DefaultLine: true, VideoURL: "https://signed.example/temporary.m3u8?token=ephemeral"},
	}
	got := fourKVMCollectEpisodes(chapters)
	want := []episode{
		{Name: "EP01", URL: "4kvm://xn2023?dataid=11&quality=1080"},
		{Name: "第2集", URL: "4kvm://xn2023?dataid=2&quality=1080"},
		{Name: "第3集", URL: "4kvm://xn2023?dataid=15&quality=1080"},
		{Name: "第1集预告", URL: "4kvm://xn2023?dataid=13&quality=1080"},
		{Name: "正片", URL: "4kvm://xn2023?dataid=14&quality=1080"},
		{Name: "预告片", URL: "4kvm://xn2023?dataid=5&quality=1080"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default line, missing episodes or special labels changed: %+v", got)
	}
	bridge := fourKVMCollectFixture{detail: func(context.Context, string, string) (xq.Drama, []xq.Chapter, error) {
		return xq.Drama{SourceID: "xn2023", Title: "仙逆"}, chapters, nil
	}}
	result, err := fetchNativeCollectSourceWith(context.Background(), "4kvm", url.Values{"ac": {"detail"}, "ids": {"xn2023"}}, bridge)
	if err != nil || len(result.List) != 1 || result.List[0]["vod_play_from"] != "4kvm" {
		t.Fatalf("CMS detail was not generated: %+v %v", result, err)
	}
	stored := fmt.Sprint(result.List[0]["vod_play_url"])
	if strings.Contains(stored, "signed.example") || strings.Count(stored, "dataid=") != len(want) {
		t.Fatal("duplicate episodes or temporary URLs would be persisted")
	}
}

func TestFourKVMDiscoverySupportsOnlyRegisteredNativeURL(t *testing.T) {
	for _, raw := range []string{fourKVMSourceURL, " " + fourKVMSourceURL + " ", "https://provider.example/api"} {
		if !discoveryCollectorSupported(raw) {
			t.Fatalf("supported source rejected: %s", raw)
		}
	}
	for _, raw := range []string{"4kvm://other", "4kvm://site/", "4kvm://site?host=private", "hongguo://other", "native://site", "https://name:password@provider.example"} {
		if discoveryCollectorSupported(raw) {
			t.Fatalf("unregistered source accepted: %s", raw)
		}
	}
}

func TestFourKVMDiscoveryEnrichesOnlyItsStoredRemoteIdentity(t *testing.T) {
	target := discoveryFixtureTarget()
	target.SourceAPIID, target.SourceAPIVID, target.Area = 10, "xn2023", ""
	collector := row{"id": 10, "status": 1, "api_url": fourKVMSourceURL}
	got := enrichDiscoveryTargetWith(context.Background(), target, collector, func(_ context.Context, raw string, values url.Values) (macPayload, error) {
		if raw != fourKVMSourceURL || values.Get("ac") != "detail" || values.Get("ids") != target.SourceAPIVID {
			t.Fatal("4KVM enrichment searched by guessed title instead of the stored remote identity")
		}
		item := discoveryFixtureItem()
		item["vod_id"] = target.SourceAPIVID
		return macPayload{Code: 1, List: []map[string]any{item}}, nil
	})
	if discoveryRegion(got.Area) != "cn" || got.Name != target.Name || got.Year != target.Year || got.Kind != target.Kind {
		t.Fatalf("confirmed original metadata was not preserved: %+v", got)
	}
	for _, cfg := range []row{{"id": 9, "status": 1, "api_url": fourKVMSourceURL}, {"id": 10, "status": 0, "api_url": fourKVMSourceURL}, {"id": 10, "status": 1, "api_url": "4kvm://other"}} {
		got = enrichDiscoveryTargetWith(context.Background(), target, cfg, func(context.Context, string, url.Values) (macPayload, error) {
			t.Fatal("unconfirmed native provider supplied target metadata")
			return macPayload{}, nil
		})
		if !reflect.DeepEqual(got, target) {
			t.Fatal("foreign or disabled metadata changed the discovery target")
		}
	}
}

func TestFourKVMDiscoveryRequiresStrictIdentityAndMedia(t *testing.T) {
	for _, tc := range []struct {
		name, field, value, failDataID string
		disabled, hidden               bool
		want                           bool
	}{
		{name: "matching regular episodes", want: true},
		{name: "different year", field: "vod_year", value: "2025"},
		{name: "missing region", field: "vod_area", value: ""},
		{name: "different kind", field: "type_name", value: "动画电影"},
		{name: "disabled collector", disabled: true},
		{name: "disabled player", hidden: true},
		{name: "first episode fails", failDataID: "1"},
		{name: "latest episode fails", failDataID: "160"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probes := []string{}
			deps := discoveryProviderDeps{
				fetch: func(_ context.Context, raw string, values url.Values) (macPayload, error) {
					if raw != fourKVMSourceURL || values.Get("ac") == "videolist" && values.Get("wd") != "仙逆" {
						t.Fatal("native discovery did not send real title search")
					}
					item := discoveryFixtureItem()
					item["vod_id"] = "xn2023"
					if values.Get("ac") == "detail" {
						if values.Get("ids") != "xn2023" {
							t.Fatal("detail request used a different remote film")
						}
						item["vod_play_from"] = "4kvm"
						item["vod_play_url"] = "第1集$4kvm://xn2023?dataid=1&quality=1080#第160集$4kvm://xn2023?dataid=160&quality=1080#预告片$4kvm://xn2023?dataid=999&quality=1080"
						if tc.field != "" {
							item[tc.field] = tc.value
						}
					}
					return macPayload{Code: 1, List: []map[string]any{item}}, nil
				},
				visible: func(_ context.Context, _ int64, sources []source) ([]source, error) {
					if tc.hidden {
						return nil, nil
					}
					return sources, nil
				},
				fourKVMProbe: func(_ context.Context, marker string) error {
					probes = append(probes, marker)
					if tc.failDataID != "" && strings.Contains(marker, "dataid="+tc.failDataID+"&") {
						return errors.New("token-bearing upstream error must not escape")
					}
					return nil
				},
				probe: func(context.Context, string) error { t.Fatal("native marker sent to HTTP probe"); return nil },
			}
			status := 1
			if tc.disabled {
				status = 0
				deps.fetch = func(context.Context, string, url.Values) (macPayload, error) {
					t.Fatal("disabled source was fetched")
					return macPayload{}, nil
				}
			}
			result := discoverCollectorWith(context.Background(), discoveryFixtureTarget(), row{"id": 10, "status": status, "api_url": fourKVMSourceURL}, deps)
			if tc.want {
				if len(result.Sources) != 1 || result.Error != "" || len(probes) != 2 || strings.Contains(probes[0]+probes[1], "dataid=999") {
					t.Fatalf("first/latest media was not required: %+v probes=%v", result, probes)
				}
				for _, ep := range result.Sources[0].Episodes {
					if !strings.HasPrefix(ep.URL, "4kvm://") {
						t.Fatal("discovery returned resolved temporary media instead of source marker")
					}
				}
			} else if len(result.Sources) != 0 || result.Error == "" || strings.Contains(result.Error, "token-bearing") {
				t.Fatalf("unproven film/media was accepted or error leaked: %+v", result)
			}
		})
	}
}

func TestFourKVMCompositeChapterMarker(t *testing.T) {
	for _, tc := range []struct {
		query string
		valid bool
	}{
		{"dataid=13772&quality=1080", true},
		{"dataid=13772&quality=1080&chapter=ch16yiwpu", true},
		{"dataid=13772&quality=1080&chapter=", false},
		{"dataid=13772&quality=1080&chapter=first&chapter=second", false},
		{"dataid=13772&chapter=https%3A%2F%2Fexample.org", false},
		{"dataid=13772&chapter=first%3Fevil%3D1", false},
		{"dataid=13772&chapter=" + strings.Repeat("a", 65), false},
	} {
		film, valid := fourKVMDiscoveryMarker("4kvm://ch16yiwpe?" + tc.query)
		if valid != tc.valid || valid && film != "ch16yiwpe" {
			t.Fatalf("marker contract failed for %s: film=%s valid=%t", tc.query, film, valid)
		}
	}
}

func TestFourKVMNativeProbeChecksPlaylistKeyAndSegment(t *testing.T) {
	for _, tc := range []struct {
		name, failPath string
	}{{name: "valid encrypted HLS"}, {name: "key fails", failPath: "/key"}, {name: "segment fails", failPath: "/segment.ts"}} {
		t.Run(tc.name, func(t *testing.T) {
			calls := map[string]int{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls[r.URL.Path]++
				if r.Header.Get("Referer") != "https://www.4kvm.net/" {
					t.Error("media probe lost the provider referer")
				}
				if r.URL.Path == tc.failPath {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				switch r.URL.Path {
				case "/master.m3u8":
					fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=100000\nchild.m3u8\n")
				case "/child.m3u8":
					fmt.Fprint(w, "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key\"\n#EXTINF:6,\nsegment.ts\n#EXT-X-ENDLIST\n")
				case "/key":
					_, _ = w.Write(make([]byte, 16))
				case "/segment.ts":
					_, _ = w.Write(make([]byte, 376))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			resolveCalls := 0
			resolve := func(_ context.Context, slug, marker string) (xq.CMSMedia, error) {
				resolveCalls++
				if slug != "movie123" || marker != "4kvm://movie123?dataid=39922&quality=1080" {
					t.Fatal("native marker or remote slug changed")
				}
				return xq.CMSMedia{URL: server.URL + "/master.m3u8", Referer: "https://www.4kvm.net/"}, nil
			}
			check := func(_ context.Context, raw string) error {
				if !strings.HasPrefix(raw, server.URL+"/") {
					return errors.New("unexpected media host")
				}
				return nil
			}
			err := probe4KVMDiscoveryWith(context.Background(), "4kvm://movie123?dataid=39922&quality=1080", resolve, server.Client(), check)
			if (err == nil) != (tc.failPath == "") || resolveCalls != 1 || calls["/master.m3u8"] != 1 || calls["/child.m3u8"] != 1 || calls["/key"] != 1 {
				t.Fatalf("native playlist/key validation failed: %v calls=%v", err, calls)
			}
			if tc.failPath != "/key" && calls["/segment.ts"] != 1 {
				t.Fatal("the actual media segment was never checked")
			}
			for _, invalid := range []string{"4kvm://movie123", "4kvm://movie123?dataid=0", "4kvm://../private?dataid=39922", "4kvm://movie123?dataid=39922&host=private", "https://www.4kvm.net/play/movie123"} {
				if err := probe4KVMDiscoveryWith(context.Background(), invalid, resolve, server.Client(), check); err == nil {
					t.Fatalf("invalid native marker resolved: %s", invalid)
				}
			}
			if resolveCalls != 1 {
				t.Fatal("invalid markers reached the remote resolver")
			}
		})
	}
}
