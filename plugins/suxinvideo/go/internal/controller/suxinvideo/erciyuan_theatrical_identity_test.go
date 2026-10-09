package suxinvideo

import (
	"context"
	"errors"
	"net/url"
	"reflect"
	"testing"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

func erciyuanTheatricalFixture() (discoveryTarget, map[string]any) {
	target := discoveryTarget{ID: 2164, Name: "仙逆剧场版弑仙之战", Year: "2026", Area: "大陆", Kind: "anime_movie"}
	item := map[string]any{"__erciyuan": true, "vod_id": "64933", "vod_name": "仙逆剧场版 弑仙之战", "type_name": "剧场版", "vod_content": "王林于雷之仙界遭遇宿敌绝命追杀，在生死绝境中展开一场惊天逆袭，以弱胜强，终成弑仙壮举。", "vod_play_from": "ecy_aa02$$$ecy_dd02$$$ecy_4k01", "vod_play_url": "第01集$erciyuan://64933/aa02/0#录屏版$erciyuan://64933/aa02/1#录屏$erciyuan://64933/aa02/2$$$HD中字$erciyuan://64933/dd02/0$$$第01集$erciyuan://64933/4k01/0#录屏版$erciyuan://64933/4k01/1"}
	return target, item
}

func TestErciyuanTheatricalRequiresDistinctFeatureEvidence(t *testing.T) {
	target, item := erciyuanTheatricalFixture()
	if discoveryMatches(target, item, 40) {
		t.Fatal("the ordinary matcher unexpectedly accepted missing year and region")
	}
	if !discoveryErciyuanTheatricalMatch(target, item) {
		t.Fatal("distinct native theatrical feature was rejected")
	}
	sources, valid := erciyuanTheatricalFeatureSources(target.Name, item)
	if !valid || len(sources) != 1 || sources[0].Code != "ecy_dd02" || sources[0].Episodes[0].URL != "erciyuan://64933/dd02/0" {
		t.Fatalf("recording variants were confused with a complete feature: %+v", sources)
	}
	for _, tc := range []struct {
		name, field string
		value       any
	}{
		{"other theatrical film", "vod_name", "仙逆剧场版 神临之战"},
		{"television series", "vod_name", "仙逆"},
		{"not native", "__erciyuan", false},
		{"broad anime category", "type_name", "国漫"},
		{"series category", "type_name", "电视剧"},
		{"missing synopsis", "vod_content", ""},
		{"title only synopsis", "vod_content", "仙逆剧场版弑仙之战"},
		{"contradictory year", "vod_year", "2025"},
		{"contradictory region", "vod_area", "日本"},
		{"foreign feature ID", "vod_play_url", "HD中字$erciyuan://52675/dd02/0"},
		{"real numbered series", "vod_play_url", "第01集$erciyuan://64933/dd02/0#第02集$erciyuan://64933/dd02/1"},
		{"ordinary HTTP source", "vod_play_url", "HD中字$https://media.example/feature.mp4"},
		{"recording only", "vod_play_url", "录屏版$erciyuan://64933/dd02/0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, bad := erciyuanTheatricalFixture()
			if tc.field == "vod_play_url" {
				bad["vod_play_from"] = "ecy_dd02"
			}
			bad[tc.field] = tc.value
			if discoveryErciyuanTheatricalMatch(target, bad) {
				t.Fatal("incomplete or conflicting identity was accepted")
			}
		})
	}
	for _, name := range []string{"仙逆剧场版", "仙逆剧场版二", "仙逆剧场版弑仙之战预告"} {
		badTarget, bad := erciyuanTheatricalFixture()
		badTarget.Name, bad["vod_name"] = name, name
		if discoveryErciyuanTheatricalMatch(badTarget, bad) {
			t.Fatalf("nondistinct or partial title accepted: %s", name)
		}
	}
	for _, kind := range []string{"anime", "movie", "short", ""} {
		wrong := target
		wrong.Kind = kind
		if discoveryErciyuanTheatricalMatch(wrong, item) {
			t.Fatalf("target kind %s was replaced by incoming category", kind)
		}
	}
}

func TestErciyuanTheatricalDiscoveryProbesOnlyFeature(t *testing.T) {
	target, item := erciyuanTheatricalFixture()
	target.EpisodeKey = "movie:feature"
	var probed []string
	deps := discoveryProviderDeps{
		fetch: func(_ context.Context, _ string, query url.Values) (macPayload, error) {
			if query.Get("ac") == "detail" {
				return macPayload{Code: 1, List: []map[string]any{item}}, nil
			}
			return macPayload{Code: 1, List: []map[string]any{{"vod_id": "64933", "vod_name": item["vod_name"], "type_name": "剧场版"}}}, nil
		},
		visible:       func(_ context.Context, _ int64, sources []source) ([]source, error) { return sources, nil },
		erciyuanProbe: func(_ context.Context, marker string) error { probed = append(probed, marker); return nil },
	}
	collector := row{"id": 40, "name": "二次元", "status": 1, "api_url": erciyuanSourceURL}
	result := discoverCollectorWith(context.Background(), target, collector, deps)
	if len(result.Sources) != 1 || result.Sources[0].Code != "ecy_dd02" || !reflect.DeepEqual(probed, []string{"erciyuan://64933/dd02/0"}) {
		t.Fatalf("unverified or ambiguous feature reached discovery: sources=%+v probe=%v", result.Sources, probed)
	}
	deps.erciyuanProbe = func(context.Context, string) error { return errors.New("controlled media failure") }
	if got := discoverCollectorWith(context.Background(), target, collector, deps); len(got.Sources) != 0 || got.Error == "" {
		t.Fatal("failed native media was merged")
	}
	deps.fetch = func(_ context.Context, _ string, query url.Values) (macPayload, error) {
		if query.Get("ac") == "detail" {
			t.Fatal("ambiguous theatrical search fetched a candidate detail")
		}
		return macPayload{Code: 1, List: []map[string]any{{"vod_id": "64933", "vod_name": item["vod_name"], "type_name": "剧场版"}, {"vod_id": "64934", "vod_name": item["vod_name"], "type_name": "剧场版"}}}, nil
	}
	if got := discoverCollectorWith(context.Background(), target, collector, deps); len(got.Sources) != 0 || got.Error == "" {
		t.Fatal("distinct native IDs for the same incomplete theatrical title were merged")
	}
}

func TestErciyuanTheatricalSearchSpacingFallback(t *testing.T) {
	target, item := erciyuanTheatricalFixture()
	var searched []string
	deps := discoveryProviderDeps{
		fetch: func(_ context.Context, _ string, query url.Values) (macPayload, error) {
			if query.Get("ac") == "detail" {
				return macPayload{Code: 1, List: []map[string]any{item}}, nil
			}
			searched = append(searched, query.Get("wd"))
			if query.Get("wd") == "仙逆剧场版 弑仙之战" {
				return macPayload{Code: 1, List: []map[string]any{{"vod_id": "64933", "vod_name": item["vod_name"], "type_name": "剧场版"}}}, nil
			}
			return macPayload{Code: 1}, nil
		},
		visible:       func(_ context.Context, _ int64, sources []source) ([]source, error) { return sources, nil },
		erciyuanProbe: func(context.Context, string) error { return nil },
	}
	collector := row{"id": 40, "status": 1, "api_url": erciyuanSourceURL}
	result := discoverCollectorWith(context.Background(), target, collector, deps)
	if len(result.Sources) != 1 || !reflect.DeepEqual(searched, []string{"仙逆剧场版弑仙之战", "仙逆剧场版 弑仙之战"}) {
		t.Fatalf("native title spacing fallback failed: searches=%v lines=%d", searched, len(result.Sources))
	}
	searched = nil
	target.Kind = "anime"
	if result = discoverCollectorWith(context.Background(), target, collector, deps); len(result.Sources) != 0 || len(searched) != 1 {
		t.Fatal("theatrical fallback widened to anime television")
	}
	for _, name := range []string{"仙逆", "仙逆剧场版", "仙逆剧场版神临之战预告", "仙逆剧场版二"} {
		if erciyuanTheatricalSearchKeyword(name) != "" {
			t.Fatalf("partial title produced theatrical search variant: %s", name)
		}
	}
}

func TestErciyuanTheatricalCollectionPreservesIdentityAndRejectsRemakes(t *testing.T) {
	target, item := erciyuanTheatricalFixture()
	deps := collectionMatchDeps{target: func(_ context.Context, vod row, _ int64, _ string) (discoveryTarget, error) {
		copy := target
		copy.ID = gconv.Int64(vod["id"])
		copy.Year = gconv.String(vod["year"])
		copy.Area = gconv.String(vod["area"])
		return copy, nil
	}}
	primary := row{"id": 2164, "api_id": 4, "api_vid": "old", "name": target.Name, "year": "2026", "area": "大陆"}
	got, err := chooseCollectionTarget(context.Background(), 40, item, []row{primary}, deps)
	if err != nil || got == nil || gconv.Int64(got["id"]) != 2164 || !gconv.Bool(got["__collection_preserve_metadata"]) || !gconv.Bool(got["__erciyuan_theatrical"]) {
		t.Fatalf("narrow matching did not preserve existing identity: %+v %v", got, err)
	}
	if primary["__erciyuan_theatrical"] != nil {
		t.Fatal("candidate row was mutated")
	}
	otherNative := row{"id": 2164, "name": target.Name, "year": "2026", "area": "大陆", "play_from": "ecy_dd02", "play_url": "HD中字$erciyuan://52675/dd02/0"}
	if got, err = chooseCollectionTarget(context.Background(), 40, item, []row{otherNative}, deps); got != nil || !errors.Is(err, errErciyuanTheatricalAmbiguous) {
		t.Fatal("different established native film ID was overwritten")
	}
	for _, extra := range []row{{"id": 2165, "name": target.Name, "year": "2025", "area": "大陆"}, {"id": 2165, "name": target.Name, "year": "2026", "area": "日本"}, {"id": 2165, "name": target.Name, "year": "", "area": "大陆"}} {
		got, err = chooseCollectionTarget(context.Background(), 40, item, []row{primary, extra}, deps)
		if got != nil || !errors.Is(err, errErciyuanTheatricalAmbiguous) {
			t.Fatalf("ambiguous local remake chosen: %+v %v", got, err)
		}
	}
	bound := row{"id": 6934, "api_id": 40, "api_vid": "64933", "name": target.Name, "year": "2026", "area": "日本", "play_from": "ecy_dd02", "play_url": "HD中字$erciyuan://64933/dd02/0"}
	if got, err = chooseCollectionTarget(context.Background(), 40, item, []row{primary, bound}, deps); err != nil || got == nil || gconv.Int64(got["id"]) != 2164 || gconv.String(got["area"]) != "大陆" {
		t.Fatalf("known native alias polluted independent foreign metadata: %+v %v", got, err)
	}
}
