package suxinvideo

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

func collectionMatchingFixtureRow(id int64, year, area, kind string) row {
	return row{"id": id, "name": "仙逆剧场版弑仙之战", "year": year, "area": area, "__kind": kind, "api_id": 2, "api_vid": "158030", "type_id": 94,
		"play_from": "mym3u8", "play_url": "正片$https://play.maoyan.example/movie/index.m3u8", "vip": 1, "points": 9, "pic": "keep.jpg", "remarks": "旧备注", "updatetime": 7}
}
func collectionMatchingFixtureItem() map[string]any {
	return map[string]any{"__yqk": true, "vod_id": "469365", "vod_name": "仙逆剧场版 弑仙之战", "vod_year": "2026", "vod_area": "大陆", "type_name": "国产动漫",
		"vod_play_from": "yqk_1$$$yqk_8", "vod_play_url": "HD$yqk://469365/1/100$$$第01集$yqk://469365/8/101"}
}
func collectionMatchingFixtureDeps(enrich func(context.Context, discoveryTarget, row) (discoveryTarget, error)) collectionMatchDeps {
	return collectionMatchDeps{
		target: func(_ context.Context, vod row, apiID int64, remoteID string) (discoveryTarget, error) {
			target := discoveryTarget{ID: gconv.Int64(vod["id"]), Name: gconv.String(vod["name"]), Year: gconv.String(vod["year"]), Area: gconv.String(vod["area"]), Kind: gconv.String(vod["__kind"])}
			for _, src := range playlist(vod) {
				for _, ep := range src.Episodes {
					id, _, _, err := yqkMarkerParts(ep.URL)
					if err == nil && id == remoteID {
						target.SourceAPIID, target.SourceAPIVID = apiID, remoteID
					}
				}
			}
			return target, nil
		},
		enrich: enrich,
	}
}
func TestCollectionMatchingAnchoredAreaPreservesOriginalID(t *testing.T) {
	old := collectionMatchingFixtureRow(2164, "2026", "", "anime_movie")
	duplicate := collectionMatchingFixtureRow(3021, "2026", "大陆", "anime")
	duplicate["name"], duplicate["api_id"], duplicate["api_vid"] = "仙逆剧场版 弑仙之战", 25, "469365"
	calls := 0
	got, err := chooseCollectionTarget(context.Background(), 25, collectionMatchingFixtureItem(), []row{duplicate, old}, collectionMatchingFixtureDeps(func(_ context.Context, target discoveryTarget, vod row) (discoveryTarget, error) {
		calls++
		if gconv.Int64(vod["id"]) != 2164 {
			t.Fatal("complete duplicate caused network enrichment")
		}
		target.Area = "中国大陆"
		return target, nil
	}))
	if err != nil || gconv.Int64(got["id"]) != 2164 || calls != 1 || got["__collection_area"] != "中国大陆" || got["__yqk_identity"] != discoveryFilmIdentity(old) {
		t.Fatalf("did not preserve independently verified original film: got=%v err=%v calls=%d", got, err, calls)
	}
	if old["area"] != "" || old["__collection_area"] != nil || duplicate["api_id"] != 25 {
		t.Fatal("selection mutated stored rows")
	}
}
func TestCollectionMatchingCompleteIdentityNeedsNoNetwork(t *testing.T) {
	old := collectionMatchingFixtureRow(2164, "2026", "中国大陆", "anime_movie")
	got, err := chooseCollectionTarget(context.Background(), 25, collectionMatchingFixtureItem(), []row{old}, collectionMatchingFixtureDeps(func(context.Context, discoveryTarget, row) (discoveryTarget, error) {
		t.Fatal("complete identity should match locally")
		return discoveryTarget{}, nil
	}))
	if err != nil || gconv.Int64(got["id"]) != 2164 || got["__collection_area"] != nil {
		t.Fatalf("%v %v", got, err)
	}
}
func TestCollectionMatchingUnprovenRegionDefersDuplicateInsert(t *testing.T) {
	old := collectionMatchingFixtureRow(2164, "2026", "", "anime_movie")
	deps := collectionMatchingFixtureDeps(func(_ context.Context, target discoveryTarget, _ row) (discoveryTarget, error) { return target, nil })
	got, err := chooseCollectionTarget(context.Background(), 25, collectionMatchingFixtureItem(), []row{old}, deps)
	if got != nil || !errors.Is(err, errCollectionIdentityIncomplete) {
		t.Fatalf("unproven existing film permitted duplicate insert: %v %v", got, err)
	}
	duplicate := collectionMatchingFixtureRow(3021, "2026", "大陆", "anime")
	duplicate["api_id"], duplicate["api_vid"] = 25, "469365"
	got, err = chooseCollectionTarget(context.Background(), 25, collectionMatchingFixtureItem(), []row{old, duplicate}, deps)
	if err != nil || gconv.Int64(got["id"]) != 3021 {
		t.Fatal("unproven older row blocked a proven existing remote-ID update")
	}
}
func TestCollectionMatchingRejectsIdentityConflicts(t *testing.T) {
	for _, tc := range []struct{ name, field, value string }{
		{"different year", "vod_year", "2025"}, {"different country", "vod_area", "日本"},
		{"short drama", "type_name", "短剧"}, {"television", "type_name", "国产剧"}, {"season title", "vod_name", "仙逆剧场版弑仙之战第二季"},
		{"split feature", "vod_play_url", "第01集$yqk://469365/1/100#第02集$yqk://469365/1/101"},
		{"unknown label", "vod_play_url", "资源1$yqk://469365/1/100"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := collectionMatchingFixtureRow(2164, "2026", "大陆", "anime_movie")
			item := collectionMatchingFixtureItem()
			item[tc.field] = tc.value
			got, err := chooseCollectionTarget(context.Background(), 25, item, []row{old}, collectionMatchingFixtureDeps(func(context.Context, discoveryTarget, row) (discoveryTarget, error) {
				t.Fatal("explicit conflict reached enrichment")
				return discoveryTarget{}, nil
			}))
			if got != nil || err != nil {
				t.Fatalf("conflict was selected: %v %v", got, err)
			}
			// The same remote ID still cannot waive explicit conflicting metadata.
			old["api_id"], old["api_vid"] = 25, "469365"
			got, err = chooseCollectionTarget(context.Background(), 25, item, []row{old}, collectionMatchingFixtureDeps(nil))
			if got != nil || err != nil {
				t.Fatal("pinned remote ID overrode explicit identity conflict")
			}
		})
	}
}
func TestCollectionMatchingUnicodeTitleAndPunctuation(t *testing.T) {
	item := collectionMatchingFixtureItem()
	old := collectionMatchingFixtureRow(2164, "2026", "大陆", "anime_movie")
	old["name"] = " 仙逆\u3000剧场版\t弑仙之战\u200b "
	got, err := chooseCollectionTarget(context.Background(), 25, item, []row{old}, collectionMatchingFixtureDeps(nil))
	if err != nil || got == nil {
		t.Fatalf("Unicode whitespace produced duplicate: %v", err)
	}
	old["name"] = "仙逆：剧场版弑仙之战"
	got, err = chooseCollectionTarget(context.Background(), 25, item, []row{old}, collectionMatchingFixtureDeps(nil))
	if err != nil || got != nil {
		t.Fatal("coarse name_norm erased meaningful title punctuation")
	}
}
func TestCollectionMatchingTrustedMarkerFillsOnlyMissingRegion(t *testing.T) {
	for _, missing := range []string{"", "未知", "其他", "其它"} {
		old := collectionMatchingFixtureRow(2164, "2026", missing, "anime_movie")
		old["play_from"], old["play_url"] = "yqk_1", "HD$yqk://469365/1/100"
		got, err := chooseCollectionTarget(context.Background(), 25, collectionMatchingFixtureItem(), []row{old}, collectionMatchingFixtureDeps(func(context.Context, discoveryTarget, row) (discoveryTarget, error) {
			t.Fatal("stored exact remote identity should not need network")
			return discoveryTarget{}, nil
		}))
		if err != nil || got["__collection_area"] != "大陆" {
			t.Fatalf("known marker lost its own region (%q): %v %v", missing, got, err)
		}
	}
}
func TestCollectionMatchingLockedAreaFillKeepsMetadataAndResources(t *testing.T) {
	original := collectionMatchingFixtureRow(2164, "2026", "", "anime_movie")
	existing := fourKVMFixtureRowCopy(original)
	existing["__yqk_identity"] = discoveryFilmIdentity(original)
	existing["__collection_area"] = "中国大陆"
	existing["__yqk_movie"] = true
	tx := &fourKVMCollectionFixtureTX{film: fourKVMFixtureRowCopy(original), collector: row{"api_url": yqkSourceURL, "status": 1}}
	update := collectedVodUpdate{existing: existing, apiID: 25, from: "yqk_1", play: "HD$yqk://469365/1/100",
		preserveMetadata: true, needsPicture: func(row) bool { return false }}
	if err := update.apply(tx); err != nil {
		t.Fatal(err)
	}
	if tx.film["area"] != "中国大陆" || len(playlist(tx.film)) != 2 || tx.writes != 1 {
		t.Fatalf("area or original resources lost: %v", tx.film)
	}
	for _, key := range []string{"id", "name", "year", "type_id", "api_id", "api_vid", "vip", "points", "pic", "remarks", "updatetime"} {
		if !reflect.DeepEqual(tx.film[key], original[key]) {
			t.Fatalf("overwrote preserved metadata %s", key)
		}
	}
	for _, tc := range []struct {
		name, field string
		value       any
	}{{"concurrent country", "area", "日本"}, {"concurrent year", "year", "2025"}, {"concurrent identity", "name", "another film"}} {
		t.Run(tc.name, func(t *testing.T) {
			changed := fourKVMFixtureRowCopy(original)
			changed[tc.field] = tc.value
			race := &fourKVMCollectionFixtureTX{film: changed, collector: row{"api_url": yqkSourceURL, "status": 1}}
			if err := update.apply(race); err == nil || race.writes != 0 {
				t.Fatal("stale identity overwrote concurrent edit")
			}
		})
	}
}

func TestCollectionMatchingBoundAliasesKeepOldURLsWithoutLooseMerges(t *testing.T) {
	primary := collectionMatchingFixtureRow(2164, "2026", "大陆", "anime_movie")
	owned := collectionMatchingFixtureRow(3021, "2026", "大陆", "anime")
	owned["api_id"], owned["api_vid"] = 25, "469365"
	marked := collectionMatchingFixtureRow(3022, "2026", "大陆", "anime_movie")
	marked["play_from"], marked["play_url"] = "yqk_1", "HD$yqk://469365/1/100"
	remake := fourKVMFixtureRowCopy(owned)
	remake["id"], remake["year"] = 3023, "2025"
	foreignRegion := fourKVMFixtureRowCopy(owned)
	foreignRegion["id"], foreignRegion["area"] = 3024, "日本"
	unbound := collectionMatchingFixtureRow(3025, "2026", "大陆", "anime_movie")
	otherRemote := fourKVMFixtureRowCopy(owned)
	otherRemote["id"], otherRemote["api_vid"] = 3026, "other"
	got, err := collectionBoundAliases(context.Background(), primary, 25, collectionMatchingFixtureItem(),
		[]row{primary, owned, marked, remake, foreignRegion, unbound, otherRemote}, collectionMatchingFixtureDeps(nil))
	if err != nil || len(got) != 2 || gconv.Int64(got[0]["id"]) != 3021 || gconv.Int64(got[1]["id"]) != 3022 {
		t.Fatalf("duplicate sync was too broad or lost proved aliases: %v %v", got, err)
	}
	for _, alias := range got {
		if !gconv.Bool(alias["__collection_preserve_metadata"]) || gconv.String(alias["__yqk_identity"]) == "" {
			t.Fatal("alias sync could overwrite metadata or skip transaction identity guard")
		}
	}
	if owned["__collection_preserve_metadata"] != nil || marked["__yqk_identity"] != nil {
		t.Fatal("alias selection mutated original records")
	}
}

func TestCollectionMatchingBoundAliasLockedMergePreservesAccess(t *testing.T) {
	alias := collectionMatchingFixtureRow(3021, "2026", "大陆", "anime")
	alias["api_id"], alias["api_vid"] = 25, "469365"
	alias["play_from"], alias["play_url"] = "yqk_1", "HD$yqk://469365/1/100"
	original := fourKVMFixtureRowCopy(alias)
	alias["__yqk_identity"], alias["__yqk_movie"] = discoveryFilmIdentity(alias), true
	alias["__collection_preserve_metadata"] = true
	tx := &fourKVMCollectionFixtureTX{film: fourKVMFixtureRowCopy(original), collector: row{"api_url": yqkSourceURL, "status": 1}}
	update := collectedVodUpdate{existing: alias, apiID: 25, from: "yqk_1$$$yqk_8", play: "HD$yqk://469365/1/102$$$第01集$yqk://469365/8/101",
		remarks: "不应覆盖", preparedPic: "wrong.jpg", preserveMetadata: true, needsPicture: func(row) bool { return false }}
	if err := update.apply(tx); err != nil {
		t.Fatal(err)
	}
	if len(playlist(tx.film)) != 2 || playlist(tx.film)[0].Episodes[0].URL != "yqk://469365/1/102" {
		t.Fatal("alias did not receive renewed native episodes")
	}
	for _, key := range []string{"id", "name", "year", "area", "type_id", "api_id", "api_vid", "vip", "points", "pic", "remarks", "updatetime"} {
		if !reflect.DeepEqual(tx.film[key], original[key]) {
			t.Fatalf("alias update overwrote %s", key)
		}
	}
}

func TestCollectionMatchingSameMovieIdentityPreservesSplitProviderRelease(t *testing.T) {
	// Real provider166227: the movie is already categorized as an animated
	// film; BF has always distributed it as two parts while APP offers HD.
	// Only a cross-kind anime -> feature inference needs the single-film proof.
	old := collectionMatchingFixtureRow(3057, "2025", "大陆", "anime_movie")
	old["name"], old["api_id"], old["api_vid"] = "仙逆剧场版神临之战", 25, "166227"
	old["play_from"] = "yqk_1$$$yqk_27"
	old["play_url"] = "HD$yqk://166227/1/100$$$第1集$yqk://166227/27/201#第2集$yqk://166227/27/202"
	item := map[string]any{"__yqk": true, "vod_id": "166227", "vod_name": old["name"], "vod_year": "2025", "vod_area": "大陆", "type_name": "动画片",
		"vod_play_from": "yqk_1$$$yqk_27", "vod_play_url": "HD$yqk://166227/1/101$$$第1集$yqk://166227/27/211#第2集$yqk://166227/27/212"}
	got, err := chooseCollectionTarget(context.Background(), 25, item, []row{old}, collectionMatchingFixtureDeps(func(context.Context, discoveryTarget, row) (discoveryTarget, error) {
		t.Fatal("a known same-film identity must not require independent metadata lookup")
		return discoveryTarget{}, nil
	}))
	if err != nil || got == nil || gconv.Int64(got["id"]) != 3057 {
		t.Fatalf("known split release was wrongly rejected: %v %v", got, err)
	}
	tx := &fourKVMCollectionFixtureTX{film: fourKVMFixtureRowCopy(old), collector: row{"api_url": yqkSourceURL, "status": 1}}
	update := collectedVodUpdate{existing: got, apiID: 25, from: gconv.String(item["vod_play_from"]), play: gconv.String(item["vod_play_url"]),
		preserveMetadata: true, needsPicture: func(row) bool { return false }}
	if err = update.apply(tx); err != nil {
		t.Fatal(err)
	}
	sources := playlist(tx.film)
	if len(sources) != 2 || sources[0].Episodes[0].URL != "yqk://166227/1/101" || len(sources[1].Episodes) != 2 ||
		sources[1].Episodes[0].Name != "第1集" || sources[1].Episodes[0].URL != "yqk://166227/27/211" ||
		sources[1].Episodes[1].Name != "第2集" || sources[1].Episodes[1].URL != "yqk://166227/27/212" {
		t.Fatal("refresh lost, duplicated or collapsed the established provider parts")
	}
	for _, key := range []string{"id", "name", "year", "area", "type_id", "api_id", "api_vid", "vip", "points"} {
		if !reflect.DeepEqual(tx.film[key], old[key]) {
			t.Fatalf("split refresh changed identity/access field %s", key)
		}
	}
	item["type_name"] = "国产动漫"
	got, err = chooseCollectionTarget(context.Background(), 25, item, []row{old}, collectionMatchingFixtureDeps(nil))
	if err != nil || got != nil {
		t.Fatal("same remote ID waived the stricter cross-kind split protection")
	}
	item["type_name"], item["vod_year"] = "动画片", "2026"
	got, err = chooseCollectionTarget(context.Background(), 25, item, []row{old}, collectionMatchingFixtureDeps(nil))
	if err != nil || got != nil {
		t.Fatal("known split release waived explicit conflicting year")
	}
}
