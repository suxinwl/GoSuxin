package suxinvideo

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func ordinaryCollectionCategoryFixture() map[int64]row {
	return map[int64]row{
		6:  {"id": 6, "name": "短剧", "pid": 0},
		28: {"id": 28, "name": "现代都市", "pid": 6},
		2:  {"id": 2, "name": "电影", "pid": 0},
		3:  {"id": 3, "name": "电视剧", "pid": 0},
	}
}

func ordinaryCollectionUnknownGenreItem() map[string]any {
	return map[string]any{"vod_id": "ordinary-123", "vod_name": "这个神医会功夫：下山专治不服", "type_name": "现代都市", "vod_class": "现代都市", "vod_year": "2026", "vod_area": "中国大陆",
		"vod_play_from": "wjm3u8", "vod_play_url": "第1集$https://fixture.invalid/first.m3u8", "vod_pic": "incoming.jpg", "vod_remarks": "全88集"}
}

func ordinaryCollectionCategoryNativeFixture() row {
	return row{"id": 5833, "type_id": 6, "api_id": 9, "api_vid": "100000001", "name": "这个神医会功夫：下山专治不服", "year": "", "area": "", "play_from": "hongguo", "play_url": "第1集$hongguo://100000001/110000001"}
}

func TestOrdinaryCollectionUnknownSubcategoryUsesOnlyCompleteShortAncestors(t *testing.T) {
	for _, mode := range []string{"short-parent", "nested-short-parent", "same-name-two-short-parents", "no-matching-category", "movie-parent", "unknown-parent", "broken-parent", "cyclic-parent", "conflicting-known-ancestors", "same-name-movie-and-short", "same-name-series-and-short", "same-name-unknown-and-short"} {
		t.Run(mode, func(t *testing.T) {
			categories, item := ordinaryCollectionCategoryFixture(), ordinaryCollectionUnknownGenreItem()
			original := fourKVMFixtureRowCopy(item)
			want := mode == "short-parent" || mode == "nested-short-parent" || mode == "same-name-two-short-parents"
			switch mode {
			case "nested-short-parent":
				categories[28]["pid"] = 29
				categories[29] = row{"id": 29, "name": "都市题材", "pid": 6}
			case "same-name-two-short-parents":
				categories[29] = row{"id": 29, "name": "现代都市", "pid": 6}
			case "no-matching-category":
				delete(categories, 28)
			case "movie-parent":
				categories[28]["pid"] = 2
			case "unknown-parent":
				categories[28]["pid"] = 0
			case "broken-parent":
				categories[28]["pid"] = 999
			case "cyclic-parent":
				categories[28]["pid"], categories[6]["pid"] = 6, 28
			case "conflicting-known-ancestors":
				categories[6]["pid"] = 2
			case "same-name-movie-and-short":
				categories[29] = row{"id": 29, "name": "现代都市", "pid": 2}
			case "same-name-series-and-short":
				categories[29] = row{"id": 29, "name": "现代都市", "pid": 3}
			case "same-name-unknown-and-short":
				categories[29] = row{"id": 29, "name": "现代都市", "pid": 0}
			}
			matched := ordinaryCollectionMatchItemWithCategories(item, categories)
			if want && matched["type_name"] != "短剧" || !want && matched["type_name"] != "现代都市" {
				t.Fatal("unknown genre was guessed or an independent short ancestry was ignored")
			}
			if !reflect.DeepEqual(item, original) {
				t.Fatal("matching normalization mutated the incoming category or metadata")
			}
			for key, value := range item {
				if key != "type_name" && !reflect.DeepEqual(value, matched[key]) {
					t.Fatalf("matching override rewrote %s", key)
				}
			}
		})
	}
}

func TestOrdinaryCollectionShortAncestorNormalizationKeepsExplicitFormats(t *testing.T) {
	categories := ordinaryCollectionCategoryFixture()
	for _, name := range []string{"电影", "电视剧", "国产动漫", "短剧", "剧情", ""} {
		item := ordinaryCollectionUnknownGenreItem()
		item["type_name"] = name
		categories[28]["name"] = name
		matched := ordinaryCollectionMatchItemWithCategories(item, categories)
		if matched["type_name"] != name {
			t.Fatalf("explicit/empty format %q was replaced by a local ancestor", name)
		}
	}
}

func TestOrdinaryCollectionPerItemCategorySnapshotIsLazyAndShared(t *testing.T) {
	item, native := ordinaryCollectionUnknownGenreItem(), ordinaryCollectionCategoryNativeFixture()
	var reads, kinds int
	matcher := newOrdinaryCollectionMatcher(12, item)
	matcher.categoryKind = func(context.Context, int64) (string, error) { kinds++; return "short", nil }
	matcher.loadCategories = func(context.Context) ([]row, error) {
		reads++
		var rows []row
		for _, category := range ordinaryCollectionCategoryFixture() {
			rows = append(rows, category)
		}
		return rows, nil
	}
	// An ordinary HTTP candidate retains the historic unknown-format policy
	// without loading the complete category tree.
	ordinary := fourKVMFixtureRowCopy(native)
	ordinary["play_from"], ordinary["play_url"] = "hnm3u8", "第1集$https://fixture.invalid/original.m3u8"
	if got, err := matcher.compatible(context.Background(), ordinary); err != nil || !got || reads != 0 {
		t.Fatal("unrelated ordinary matching eagerly loaded or widened the category tree")
	}
	for round := 0; round < 3; round++ {
		if got, err := matcher.compatible(context.Background(), native); err != nil || !got {
			t.Fatalf("mapped ordinary short import failed to match native film: %v", err)
		}
	}
	if reads != 1 || kinds != 2 || item["type_name"] != "现代都市" || matcher.matchingItem["type_name"] != "短剧" {
		t.Fatal("one upsert repeated full-tree queries or mutated the incoming genre")
	}
	wrong := fourKVMFixtureRowCopy(native)
	wrong["type_id"] = 2
	if got, err := matcher.compatible(context.Background(), wrong); err != nil || got {
		t.Fatal("mapped incoming short genre overrode an explicit movie candidate")
	}
	for _, field := range []string{"name", "year", "area"} {
		wrong := fourKVMFixtureRowCopy(native)
		wrong[field] = map[string]string{"name": "这个神医会功夫：下山专治不服第二季", "year": "2025", "area": "日本"}[field]
		if got, err := matcher.compatible(context.Background(), wrong); err != nil || got {
			t.Fatalf("local category mapping waived explicit %s identity conflict", field)
		}
	}
}

func TestOrdinaryCollectionSubcategoryReadErrorDefersInsteadOfDuplicating(t *testing.T) {
	var reads int
	matcher := newOrdinaryCollectionMatcher(12, ordinaryCollectionUnknownGenreItem())
	matcher.categoryKind = func(context.Context, int64) (string, error) { return "short", nil }
	errRead := errors.New("category fixture unavailable")
	matcher.loadCategories = func(context.Context) ([]row, error) { reads++; return nil, errRead }
	for round := 0; round < 2; round++ {
		if got, err := matcher.compatible(context.Background(), ordinaryCollectionCategoryNativeFixture()); got || !errors.Is(err, errRead) {
			t.Fatal("failed category proof allowed a new duplicate or hid its read error")
		}
	}
	if reads != 1 {
		t.Fatal("failed category snapshot was repeatedly queried for one item")
	}
}
