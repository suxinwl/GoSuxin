package suxinvideo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	xq "github.com/suxinwl/GoSuxin/internal/xiaoqiapp"
)

func TestYQKMarkerValidation(t *testing.T) {
	id, kind, ep, err := yqkMarkerParts("yqk://129431/1/27714278")
	if err != nil || id != "129431" || kind != "1" || ep != "27714278" {
		t.Fatal("valid marker rejected")
	}
	for _, raw := range []string{"https://129431/1/2", "yqk://129431:80/1/2", "yqk://user@129431/1/2", "yqk://129431/1/2?token=x", "yqk://129431/1/2#x", "yqk://129431/1/2/3", "yqk://129431/01/2", "yqk://129431/1/0", "yqk://1/1/9223372036854775808", "yqk://127.0.0.1/1/2"} {
		if _, _, _, err := yqkMarkerParts(raw); err == nil {
			t.Errorf("accepted invalid marker: %s", raw)
		}
	}
	if !isYQKSource(" YQK://APP ") || isYQKSource("yqk://app?url=http://127.0.0.1") {
		t.Fatal("collector marker must be exact")
	}
}

func TestYQKParameterValidation(t *testing.T) {
	for _, params := range []url.Values{{"ac": {"delete"}}, {"ac": {"detail"}}, {"ac": {"videolist"}, "pg": {"abc"}}, {"ac": {"videolist"}, "pg": {"0"}}, {"ac": {"videolist"}, "pg": {"-1"}}, {"ac": {"videolist"}, "pg": {"100001"}}, {"ac": {"videolist"}, "t": {"anime"}}} {
		if _, err := fetchYQK(context.Background(), params); err == nil {
			t.Fatal("invalid parameters reached upstream")
		}
	}
}

func TestYQKMetadataAndEpisodeConversion(t *testing.T) {
	vod := map[string]any{"vodId": 129431, "vodName": "仙逆", "year": 2023, "areaName": "中国大陆", "score": "9.2", "tagList": []string{"动画", "古装"}, "flags": "2023 / 国产动漫 / 大陆", "directorList": []map[string]any{{"vodWorkerName": "导演"}}, "playerList": []map[string]any{
		{"vodPlayerKind": 1, "playerName": "一起看APP", "epList": []map[string]any{{"epId": 4, "epName": "回顾特辑"}, {"epId": 3, "epName": "第02集"}, {"epId": 1, "epName": "EP01"}, {"epId": 2, "epName": "第01集"}, {"epId": 5, "epName": "第0集"}, {"epId": 6, "epName": "bad#label"}}},
		{"vodPlayerKind": 2, "playerName": "BD", "epList": []map[string]any{{"epId": 10, "epName": "第01集"}}},
		{"vodPlayerKind": 35, "playerName": "豆瓣资源", "epList": []map[string]any{{"epId": 11, "epName": "第01集"}}},
		{"vodPlayerKind": 999, "playerName": "未知", "epList": []map[string]any{{"epId": 12, "epName": "第01集"}}},
	}}
	item := yqkVodItem(vod, 0, "")
	if item["type_name"] != "国产动漫" || item["vod_douban_score"] != "9.2" || item["vod_class"] != "动画,古装" || item["vod_director"] != "导演" || !gconv.Bool(item["__yqk"]) {
		t.Fatal("actual metadata lost")
	}
	sources := playlist(row{"play_from": item["vod_play_from"], "play_url": item["vod_play_url"]})
	if len(sources) != 2 || sources[1].Code != "yqk_2" || len(sources[0].Episodes) != 4 {
		t.Fatal("excluded line, BD line or dedup policy incorrect")
	}
	for i, want := range []string{"第0集", "EP01", "第02集", "回顾特辑"} {
		if sources[0].Episodes[i].Name != want {
			t.Fatal("regular episodes must sort before extras and preserve identity")
		}
	}
	listed := yqkListItem(map[string]any{"vodId": 1, "vodName": "电影", "flags": "2026 / 动作片 / 中国大陆", "remark": "正片", "score": "8.1"}, 2, "电影")
	if listed["type_name"] != "动作片" || listed["vod_year"] != "2026" || listed["vod_area"] != "中国大陆" || listed["vod_play_url"] != "" {
		t.Fatal("list must expose source metadata without pretending it contains details")
	}
}

func TestYQKDetailMetadataIdentity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		detail map[string]any
		listed map[string]any
	}{
		{"different ID", map[string]any{"vodId": 1, "vodName": "仙逆"}, map[string]any{"vodId": 2, "vodName": "仙逆", "flags": "2023 / 国产动漫 / 大陆"}},
		{"different season", map[string]any{"vodId": 1, "vodName": "仙逆第二季"}, map[string]any{"vodId": 1, "vodName": "仙逆", "flags": "2023 / 国产动漫 / 大陆"}},
		{"conflicting year", map[string]any{"vodId": 1, "vodName": "仙逆", "year": 2024}, map[string]any{"vodId": 1, "vodName": "仙逆", "flags": "2023 / 国产动漫 / 大陆"}},
		{"conflicting region", map[string]any{"vodId": 1, "vodName": "仙逆", "areaName": "日本"}, map[string]any{"vodId": 1, "vodName": "仙逆", "flags": "2023 / 国产动漫 / 大陆"}},
		{"nonempty malformed year", map[string]any{"vodId": 1, "vodName": "仙逆", "year": "202x"}, map[string]any{"vodId": 1, "vodName": "仙逆", "flags": "2023 / 国产动漫 / 大陆"}},
		{"missing flags year", map[string]any{"vodId": 1, "vodName": "仙逆"}, map[string]any{"vodId": 1, "vodName": "仙逆", "flags": "未知 / 国产动漫 / 大陆"}},
		{"missing flags region", map[string]any{"vodId": 1, "vodName": "仙逆"}, map[string]any{"vodId": 1, "vodName": "仙逆", "flags": "2023 / 国产动漫 / 未知"}},
		{"missing flags type", map[string]any{"vodId": 1, "vodName": "仙逆"}, map[string]any{"vodId": 1, "vodName": "仙逆", "flags": "2023 / 未知 / 大陆"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := make(map[string]any, len(tc.detail))
			for key, value := range tc.detail {
				before[key] = value
			}
			if yqkApplyDetailMetadata(tc.detail, tc.listed) || !reflect.DeepEqual(before, tc.detail) {
				t.Fatal("unverified candidate changed detail metadata")
			}
		})
	}
}

func TestYQKDetailMetadataPreservesKnownFields(t *testing.T) {
	listed := map[string]any{"vodId": 129431, "vodName": "仙逆", "flags": "2023 / 国产动漫 / 大陆"}
	detail := map[string]any{"vodId": "129431", "vodName": "仙 逆", "year": 2023, "areaName": "中国大陆"}
	if !yqkApplyDetailMetadata(detail, listed) || detail["year"] != 2023 || detail["areaName"] != "中国大陆" {
		t.Fatal("verified flags replaced existing valid values")
	}
	converted := yqkVodItem(detail, 0, "")
	if converted["vod_year"] != "2023" || converted["vod_area"] != "中国大陆" || converted["type_name"] != "国产动漫" {
		t.Fatal("detail conversion lost known values or verified type")
	}
}

func TestYQKDetailMetadataSearchToDetail(t *testing.T) {
	listed := map[string]any{"vodId": 129431, "vodName": "仙逆", "flags": "2023 / 国产动漫 / 大陆"}
	search := yqkListItem(listed, 8, "动漫")
	for _, missing := range []any{nil, "", "未知", "unknown", 0} {
		detail := map[string]any{"vodId": 129431, "vodName": "仙 逆", "year": missing, "areaName": missing}
		if !yqkApplyDetailMetadata(detail, listed) {
			t.Fatalf("same-film search metadata rejected for missing value %v", missing)
		}
		converted := yqkVodItem(detail, 0, "")
		for _, key := range []string{"vod_year", "vod_area", "type_name"} {
			if converted[key] != search[key] {
				t.Fatalf("search-to-detail flow lost %s", key)
			}
		}
		// Conversion must also handle details carrying verified list flags
		// when the optional top-level SDK fields are absent.
		delete(detail, "year")
		delete(detail, "areaName")
		converted = yqkVodItem(detail, 0, "")
		if converted["vod_year"] != "2023" || converted["vod_area"] != "大陆" || converted["type_name"] != "国产动漫" {
			t.Fatal("verified flags did not fill missing adapter fields")
		}
	}
	conflicting := map[string]any{"vodId": 129431, "vodName": "仙逆", "year": 2024, "areaName": "日本", "flags": listed["flags"]}
	converted := yqkVodItem(conflicting, 0, "")
	if converted["vod_year"] != "2024" || converted["vod_area"] != "日本" || converted["type_name"] != "" {
		t.Fatal("conflicting flags supplied unverified metadata to adapter")
	}
}

func TestYQKUnknownAnimationTypeIsNotGuessed(t *testing.T) {
	for _, labels := range [][]string{{"第01集"}, {"第01集", "第02集"}} {
		var episodes []map[string]any
		for i, label := range labels {
			episodes = append(episodes, map[string]any{"epId": i + 1, "epName": label})
		}
		item := yqkVodItem(map[string]any{"vodId": 1, "vodName": "新动画", "year": 2026, "tagList": []string{"动画"}, "playerList": []map[string]any{{"vodPlayerKind": 1, "playerName": "一起看APP", "epList": episodes}}}, 0, "")
		if item["type_name"] != "" || item["type_id"] != 0 {
			t.Fatal("animation tags/episode count must not fabricate movie/series metadata")
		}
	}
}

func TestYQKExtraQualityTimeoutPreservesDefault(t *testing.T) {
	qualities := []map[string]any{{"vodResolution": 3, "canPlay": true}, {"vodResolution": 2, "canPlay": true}, {"vodResolution": 1, "canPlay": true}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	calls := 0
	variants := yqkResolveQualityURLs(ctx, qualities, 20*time.Millisecond, func(ctx context.Context, quality map[string]any) (xq.CMSMediaVariant, error) {
		calls++
		if gconv.Int(quality["vodResolution"]) == 3 {
			return xq.CMSMediaVariant{URL: "https://media.example/default.m3u8", Label: "超清"}, nil
		}
		<-ctx.Done()
		return xq.CMSMediaVariant{}, ctx.Err()
	})
	if len(variants) != 1 || variants[0].Label != "超清" || calls != 2 || ctx.Err() != nil {
		t.Fatal("slow optional quality discarded default or exhausted the main request")
	}
	variants = yqkResolveQualityURLs(ctx, qualities, time.Second, func(_ context.Context, quality map[string]any) (xq.CMSMediaVariant, error) {
		if gconv.Int(quality["vodResolution"]) == 3 {
			return xq.CMSMediaVariant{}, errors.New("default unavailable")
		}
		return xq.CMSMediaVariant{URL: "https://media.example/fallback.m3u8"}, nil
	})
	if len(variants) != 2 {
		t.Fatal("unavailable default prevented fresh fallback-quality resolution")
	}
}

func TestYQKExcludedSourcesAndGuestIdentity(t *testing.T) {
	for _, name := range []string{"豆瓣", "Douban", "DB", "黄豆资源", "huangdou", "黄果", "huangguo"} {
		if !yqkExcludedPlayer(name) {
			t.Errorf("did not exclude %s", name)
		}
	}
	if yqkExcludedPlayer("BD") || yqkExcludedPlayer("百度") {
		t.Fatal("Baidu must remain distinct from Douban")
	}
	collectors := []row{{"name": "豆瓣资源", "api_url": "https://dbzy5.com/api", "status": 0}}
	for _, raw := range []string{"https://vodcnd17.uvjtih.cn/path/index.m3u8", "https://vodcnd09.ajupf.com/path/index.m3u8", "https://185.92.188.176/path/index.m3u8"} {
		if yqkMediaPermitted(raw, collectors) {
			t.Fatal("disabled legacy Douban media revived through aggregate")
		}
	}
	if !yqkMediaPermitted("https://cdn.bdzy.example/path/index.m3u8", collectors) || !yqkMediaPermitted("https://other.uvjtih.cn/path", collectors) {
		t.Fatal("unrelated media must not inherit legacy host ownership")
	}
	collectors[0]["status"] = 1
	if !yqkMediaPermitted("https://vodcnd17.uvjtih.cn/path", collectors) {
		t.Fatal("enabled provider rejected")
	}
	common := yqkAnonymousCommon("cms-00112233445566778899aabbccddeeff")
	if common["udid"] != "cms-00112233445566778899aabbccddeeff" || common["deviceInfo"] != "GoCMS" {
		t.Fatal("guest identity ignored")
	}
	for _, key := range []string{"token", "userId", "sign", "requestId"} {
		if _, ok := common[key]; ok {
			t.Fatal("common fields contain session or fixed signing data")
		}
	}
}

type yqkTestTransport struct {
	body   string
	closed bool
}

func (t *yqkTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(t.body)), Header: http.Header{}, Request: r}, nil
}
func (t *yqkTestTransport) CloseIdleConnections() { t.closed = true }

func TestYQKBootstrapAndCancellation(t *testing.T) {
	for _, body := range []string{`[]`, `["http://unsafe.example/config.json"]`, `["https://user:pass@example.com/config.json"]`, `{"url":"https://example.com"}`} {
		_, err := yqkBootstrap(context.Background(), "https://invalid-config.example/"+url.PathEscape(body), &http.Client{Transport: &yqkTestTransport{body: body}})
		if err == nil {
			t.Fatal("invalid bootstrap accepted")
		}
	}
	transport := &yqkTestTransport{}
	yqkGuardTransport{base: transport}.CloseIdleConnections()
	if !transport.closed {
		t.Fatal("wrapped transport leaks idle connections")
	}
	for i := 0; i < cap(yqkPlaybackSlots); i++ {
		yqkPlaybackSlots <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(yqkPlaybackSlots); i++ {
			<-yqkPlaybackSlots
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if withYQK(ctx, true, nil) == nil {
		t.Fatal("pool acquisition ignored cancellation")
	}
	if len(yqkCollectSlots) != 0 {
		t.Fatal("playback occupies collection slots")
	}
	if err := yqkAPIError(errors.New("https://example.com/?secret=hidden")); strings.Contains(err.Error(), "secret") {
		t.Fatal("remote error leaked")
	}
}

// Optional real API smoke test. It writes only stable import markers and public
// metadata; no signed media URL, token, device ID or signature is logged/exported.
func TestYQKLiveProvider(t *testing.T) {
	if os.Getenv("SUXIN_YQK_LIVE") != "1" {
		t.Skip("set SUXIN_YQK_LIVE=1 for read-only upstream verification")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(filepath.Clean("../../..")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	classes, err := fetchYQK(ctx, url.Values{"ac": {"list"}})
	if err != nil || len(classes.Class) == 0 {
		t.Fatalf("classes: %v", err)
	}
	t.Logf("classes=%d", len(classes.Class))
	for page := 1; page <= 2; page++ {
		list, err := fetchYQK(ctx, url.Values{"ac": {"videolist"}, "pg": {gconv.String(page)}})
		if err != nil || len(list.List) == 0 {
			t.Fatalf("list page %d: %v", page, err)
		}
		t.Logf("list page=%d items=%d next=%t", page, len(list.List), list.PageCount > page)
	}
	keyword := os.Getenv("SUXIN_YQK_KEYWORD")
	if keyword == "" {
		keyword = "仙"
	}
	for page := 1; page <= 2; page++ {
		list, err := fetchYQK(ctx, url.Values{"ac": {"videolist"}, "wd": {keyword}, "pg": {gconv.String(page)}})
		if err != nil {
			t.Fatalf("search page %d: %v", page, err)
		}
		t.Logf("search page=%d items=%d next=%t", page, len(list.List), list.PageCount > page)
		if list.PageCount <= page {
			break
		}
	}
	id := os.Getenv("SUXIN_YQK_VOD")
	if id == "" {
		id = "129431"
	}
	detail, err := fetchYQK(ctx, url.Values{"ac": {"detail"}, "ids": {id}})
	if err != nil || len(detail.List) != 1 {
		t.Fatalf("detail: %v", err)
	}
	item := detail.List[0]
	if gconv.String(item["type_name"]) == "" || gconv.String(item["vod_year"]) == "" || gconv.String(item["vod_area"]) == "" {
		t.Fatal("actual metadata incomplete")
	}
	sources := playlist(row{"play_from": item["vod_play_from"], "play_url": item["vod_play_url"]})
	var selected episode
	for _, src := range sources {
		t.Logf("line=%s episodes=%d", src.Code, len(src.Episodes))
		if src.Code == "yqk_1" {
			for _, ep := range src.Episodes {
				if _, number := playbackEpisodeIdentity(ep.Name); number >= 0 {
					selected = ep
				}
			}
			if selected.URL == "" && len(src.Episodes) > 0 {
				selected = src.Episodes[0]
			}
		}
	}
	if selected.URL == "" {
		t.Fatal("APP default line absent")
	}
	media, err := resolveYQK(ctx, selected.URL)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(media.Variants) == 0 {
		t.Fatal("real qualities absent")
	}
	client := safeCollectorHTTPClient(15 * time.Second)
	defer client.CloseIdleConnections()
	probe := sourceHealthProbe{client: client, checkURL: safeCollectorURL}
	for _, variant := range media.Variants {
		if variant.Quality != 0 || len(variant.Key) != 0 {
			t.Fatal("APP enums/AES-HLS keys must not become pixel resolution/CENC keys")
		}
		probeCtx, probeCancel := context.WithTimeout(ctx, 20*time.Second)
		err := probe.media(probeCtx, variant.URL, 0, map[string]bool{})
		probeCancel()
		if err != nil {
			t.Fatalf("quality=%s media host=%s: %v", variant.Label, playbackHost(variant.URL), err)
		}
		t.Logf("quality=%s host=%s manifest+key+first-media=ok", variant.Label, playbackHost(variant.URL))
	}
	b, _ := json.MarshalIndent(map[string]any{"sample_only": true, "vod_id": id, "app_marker": selected.URL, "items": detail.List}, "", "  ")
	if err := os.WriteFile("data/tmp/yqk-live-import.json", b, 0600); err != nil {
		t.Fatal("cannot save stable import fixture")
	}
}
