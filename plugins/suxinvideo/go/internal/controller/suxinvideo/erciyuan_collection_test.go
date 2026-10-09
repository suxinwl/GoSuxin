package suxinvideo

import (
	"context"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/suxinwl/GoSuxin/internal/erciyuan"
)

type erciyuanCollectionFixture struct {
	category, page, hours int
	search                string
	detailID              string
	fail                  bool
}

func (f *erciyuanCollectionFixture) Categories(context.Context) ([]erciyuan.Category, error) {
	return []erciyuan.Category{{ID: 1, Name: "日漫"}, {ID: 2, Name: "国漫"}, {ID: 3, Name: "剧场版"}, {ID: 4, Name: "经典番剧"}, {ID: 21, Name: "特摄"}, {ID: 27, Name: "动态漫画"}}, nil
}
func (f *erciyuanCollectionFixture) List(_ context.Context, category, page, hours int) (erciyuan.Page, error) {
	f.category, f.page, f.hours = category, page, hours
	return erciyuan.Page{Items: []map[string]any{{"vod_id": 35604, "vod_name": "仙逆", "vod_year": "2023", "vod_area": "中国大陆", "type_id": 2, "type_name": "国漫"}}, Total: 2177, Page: page, PageCount: 91}, nil
}
func (f *erciyuanCollectionFixture) Search(_ context.Context, keyword string) ([]map[string]any, error) {
	f.search = keyword
	return []map[string]any{{"vod_id": 35604, "vod_name": "仙逆", "type_name": "国漫"}}, nil
}
func (f *erciyuanCollectionFixture) Detail(_ context.Context, id string) (erciyuan.Detail, error) {
	f.detailID = id
	if f.fail {
		return erciyuan.Detail{}, errors.New("fixture upstream timeout")
	}
	return erciyuan.Detail{Item: map[string]any{"vod_id": 35604, "vod_name": "仙逆", "vod_year": "2023", "vod_area": "中国大陆", "type_id": 2, "type_name": "国漫"}, Lines: []erciyuan.Line{
		{Code: "aa03", Name: "AA-03[电信]", Episodes: []erciyuan.Episode{{Name: "第01集", URL: "erciyuan://35604/aa03/0"}, {Name: "第02集", URL: "erciyuan://35604/aa03/2"}}},
		{Code: "4k01", Name: "4K-01", Episodes: []erciyuan.Episode{{Name: "第70集", URL: "erciyuan://35604/4k01/0"}}},
	}}, nil
}

func TestErciyuanCollectionClassAndPagingAdapter(t *testing.T) {
	f := &erciyuanCollectionFixture{}
	classes, err := fetchErciyuanCollectSourceWith(context.Background(), url.Values{"ac": {"list"}}, f)
	if err != nil || classes.Code != 1 || len(classes.Class) != 6 || classes.Class[4]["type_id"] != 21 || classes.Class[5]["type_id"] != 27 {
		t.Fatalf("real source category IDs were lost: %+v %v", classes, err)
	}
	list, err := fetchErciyuanCollectSourceWith(context.Background(), url.Values{"ac": {"videolist"}, "t": {"2"}, "pg": {"2"}, "h": {"12"}}, f)
	if err != nil || f.category != 2 || f.page != 2 || f.hours != 12 || list.Page != 2 || list.Total != 2177 || list.PageCount != 91 || list.List[0]["__erciyuan"] != true || list.List[0]["vod_year"] != "2023" {
		t.Fatalf("metadata/paging lost: %+v %+v %v", f, list, err)
	}
	if nativeProvider(erciyuanSourceURL) != "erciyuan" || isErciyuanSource(erciyuanSourceURL+"?url=private") {
		t.Fatal("unregistered native source URL accepted")
	}
	for _, query := range []url.Values{
		{"ac": {"videolist"}, "t": {"5"}, "pg": {"1"}},
		{"ac": {"videolist"}, "t": {"2"}, "pg": {"0"}},
		{"ac": {"videolist"}, "h": {"721"}, "pg": {"1"}},
		{"ac": {"delete"}},
		{"ac": {"videolist"}, "wd": {"仙逆"}, "pg": {"2"}},
	} {
		if _, err := fetchErciyuanCollectSourceWith(context.Background(), query, f); err == nil {
			t.Fatalf("invalid native operation accepted: %v", query)
		}
	}
}

func TestErciyuanCollectionKeepsStableMarkerAndLineEpisodeIndex(t *testing.T) {
	f := &erciyuanCollectionFixture{}
	detail, err := fetchErciyuanCollectSourceWith(context.Background(), url.Values{"ac": {"detail"}, "ids": {"35604"}}, f)
	if err != nil || f.detailID != "35604" || len(detail.List) != 1 {
		t.Fatalf("detail failed: %+v %v", detail, err)
	}
	item := detail.List[0]
	if item["vod_play_from"] != "ecy_aa03$$$ecy_4k01" || item["vod_play_url"] != "第01集$erciyuan://35604/aa03/0#第02集$erciyuan://35604/aa03/2$$$第70集$erciyuan://35604/4k01/0" {
		t.Fatalf("episode IDs or line provenance changed: %+v", item)
	}
	if strings.Contains(item["vod_play_url"].(string), "temporary") || strings.Contains(item["vod_play_url"].(string), "signed") {
		t.Fatal("temporary upstream media URL persisted")
	}
	f.fail = true
	if failed, err := fetchErciyuanCollectSourceWith(context.Background(), url.Values{"ac": {"detail"}, "ids": {"35604"}}, f); err == nil || len(failed.List) != 0 {
		t.Fatal("failed detail produced a film placeholder")
	}
	if requiredCollectPlaylistError(erciyuanSourceURL, map[string]any{"vod_name": "仙逆"}) == nil {
		t.Fatal("collector can insert a native film without its playlist")
	}
}

func TestErciyuanCollectedPlaylistRejectsForeignFilmLineAndSource(t *testing.T) {
	collector := row{"api_url": erciyuanSourceURL, "status": 1}
	for _, input := range []struct{ from, play string }{
		{"ecy_aa03", "第01集$erciyuan://35604/aa03/0"},
		{"ecy_4k01", "第70集$erciyuan://35604/4k01/0"},
	} {
		if _, _, err := validateErciyuanCollectedPlaylist(collector, "35604", input.from, input.play); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{
		"erciyuan://35604/aa03/-1", "erciyuan://35604/aa03/01", "erciyuan://35604/unknown/0", "erciyuan://35604/aa03/10000",
		"erciyuan://35604/aa03/0?url=http://private", "erciyuan://user@35604/aa03/0", "erciyuan://0/aa03/0", "erciyuan://35604/aa03/%30",
	} {
		if _, _, _, err := erciyuanCollectionMarker(raw); err == nil {
			t.Fatalf("unsafe marker accepted: %s", raw)
		}
	}
	for _, input := range []struct{ from, play string }{
		{"ecy_aa03", "第01集$erciyuan://99/aa03/0"},
		{"ecy_aa03", "第01集$erciyuan://35604/dd02/0"},
		{"ecy_unknown", "第01集$erciyuan://35604/aa03/0"},
		{"ecy_aa03$$$ecy_aa03", "第01集$erciyuan://35604/aa03/0$$$第02集$erciyuan://35604/aa03/1"},
		{"ecy_aa03", "第01集$https://cdn.example/a.m3u8"},
	} {
		if _, _, err := validateErciyuanCollectedPlaylist(collector, "35604", input.from, input.play); err == nil {
			t.Fatalf("foreign/malformed source accepted: %+v", input)
		}
	}
	if _, _, err := validateErciyuanCollectedPlaylist(row{"api_url": erciyuanSourceURL, "status": 0}, "35604", "ecy_aa03", "第01集$erciyuan://35604/aa03/0"); err == nil {
		t.Fatal("disabled collector remains able to import")
	}
}

func TestErciyuanCategoriesReuseAnimeAndScheduledEmptyPagesContinue(t *testing.T) {
	types := []row{{"id": 1, "pid": 0, "name": "电影"}, {"id": 2, "pid": 1, "name": "日本动漫"}, {"id": 3, "pid": 0, "name": "动漫"}, {"id": 24, "pid": 3, "name": "日本动漫"}, {"id": 15, "pid": 3, "name": "国产动漫"}, {"id": 44, "pid": 24, "name": "特摄"}}
	if chooseErciyuanType(types, 1) != 24 || chooseErciyuanType(types, 2) != 15 || chooseErciyuanType(types, 21) != 44 || chooseErciyuanType(types, 27) != 0 {
		t.Fatal("source category IDs collided with local navigation")
	}
	if h, one := erciyuanCollectJobScope("scheduled", erciyuanSourceURL, 12, true); h != 12 || one {
		t.Fatal("scheduled collector stops after the first anime category")
	}
	if h, one := erciyuanCollectJobScope("manual", erciyuanSourceURL, 0, true); h != 0 || !one {
		t.Fatal("manual only-this-page behavior changed")
	}
	if collectJobPageFinished(erciyuanSourceURL, 1, macPayload{PageCount: 6}, false) || !collectJobPageFinished(erciyuanSourceURL, 6, macPayload{PageCount: 6}, false) {
		t.Fatal("empty recent anime page ends scanning other source categories")
	}
}

func TestErciyuanCollectionTransactionPreservesIndependentResourcesAndAccess(t *testing.T) {
	original := row{"id": 781, "api_id": 10, "api_vid": "original", "name": "仙逆", "year": "2023", "area": "中国大陆", "type_id": 15,
		"vip": 1, "points": 10, "status": 1, "pic": "existing-poster.jpg", "remarks": "更新至160集", "updatetime": 123,
		"play_from": "hnm3u8", "play_url": "第01集$https://cdn.example/old.m3u8"}
	existing := fourKVMFixtureRowCopy(original)
	existing["__yqk_identity"], existing["__collection_source"] = discoveryFilmIdentity(original), "erciyuan"
	update := collectedVodUpdate{existing: existing, apiID: 20, from: "ecy_aa03", play: "第01集$erciyuan://35604/aa03/0",
		remarks: "wrong source metadata", preparedPic: "wrong-poster.jpg", preserveMetadata: true, pictureReady: true,
		needsPicture: func(row) bool { return false }}
	tx := &fourKVMCollectionFixtureTX{film: fourKVMFixtureRowCopy(original), collector: row{"api_url": erciyuanSourceURL, "status": 1}}
	if err := update.apply(tx); err != nil || tx.writes != 1 {
		t.Fatalf("native playlist merge failed: %v", err)
	}
	for key, value := range original {
		if key != "play_from" && key != "play_url" && !reflect.DeepEqual(value, tx.film[key]) {
			t.Fatalf("cross-source collection changed %s", key)
		}
	}
	if !strings.Contains(tx.film["play_from"].(string), "ecy_aa03") || !strings.Contains(tx.film["play_from"].(string), "hnm3u8") {
		t.Fatal("existing independent source was lost")
	}
	for _, query := range tx.queries {
		if !strings.Contains(query, "FOR UPDATE") {
			t.Fatal("source or film was not locked during the playlist write")
		}
	}
	tx = &fourKVMCollectionFixtureTX{film: fourKVMFixtureRowCopy(original), collector: row{"api_url": erciyuanSourceURL, "status": 0}}
	if err := update.apply(tx); err == nil || tx.writes != 0 {
		t.Fatal("collector disabled during merge still wrote a playlist")
	}
}
