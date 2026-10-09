package suxinvideo

import (
	"context"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	xq "github.com/suxinwl/GoSuxin/internal/xiaoqiapp"
)

func hongguoMatchingFixtureItem() map[string]any {
	return map[string]any{"__hongguo": true, "vod_id": "7684943327614995518", "vod_name": "冲喜赘婿竟是绝世神医", "type_name": "短剧", "vod_year": "", "vod_area": "",
		"vod_play_from": "hongguo", "vod_play_url": "第1集$hongguo://7684943327614995518/11#第2集$hongguo://7684943327614995518/12"}
}

func hongguoMatchingFixtureRow(id int64) row {
	return row{"id": id, "name": "冲喜赘婿竟是绝世神医", "year": "2026", "area": "大陆", "type_id": 15, "__kind": "short", "api_id": 2, "api_vid": "foreign",
		"play_from": "hnm3u8", "play_url": "第01集$https://cdn.example/old.m3u8", "class": "现代言情", "vip": 1, "points": 10, "status": 1, "pic": "keep.jpg", "remarks": "keep", "updatetime": 123}
}

func hongguoMatchingFixtureDeps() collectionMatchDeps {
	return collectionMatchDeps{target: func(_ context.Context, vod row, apiID int64, remoteID string) (discoveryTarget, error) {
		return hongguoCollectionTargetWithKind(vod, gconv.String(vod["__kind"]), apiID, remoteID), nil
	}}
}

type hongguoCatalogFixture struct {
	fourKVMCollectFixture
}

func (hongguoCatalogFixture) List(_ context.Context, provider string, category, page int) (xq.CMSPage, error) {
	if provider != "hongguo" || category != 1 || page != 1 {
		return xq.CMSPage{}, errors.New("wrong native list arguments")
	}
	return xq.CMSPage{Dramas: []xq.Drama{{SourceID: "7684943327614995518", Title: "冲喜赘婿竟是绝世神医", Category: "玄幻", OnlineDate: ""}}, HasMore: true}, nil
}

func TestHongguoCatalogAlwaysUsesShortTaxonomyWithoutInventingMetadata(t *testing.T) {
	list, err := fetchNativeCollectSourceWith(context.Background(), "hongguo", url.Values{"ac": {"videolist"}, "t": {"1"}, "pg": {"1"}}, hongguoCatalogFixture{})
	if err != nil || len(list.List) != 1 || list.PageCount != 2 {
		t.Fatalf("native list failed: %v %v", list, err)
	}
	item := list.List[0]
	if item["type_name"] != "短剧" || item["vod_class"] != "玄幻" || item["__hongguo"] != true || item["vod_year"] != "" || item["vod_area"] != "" {
		t.Fatal("native genre became a main category or missing identity was fabricated")
	}
	other := cmsProviderVodItem("4kvm", xq.Drama{Category: "国产动漫"})
	if other["type_name"] != "国产动漫" || other["__hongguo"] != nil {
		t.Fatal("Hongguo taxonomy contaminated another provider")
	}
}

func TestHongguoMatchingPreservesExactForeignShortWithoutMetadataInference(t *testing.T) {
	item, original := hongguoMatchingFixtureItem(), hongguoMatchingFixtureRow(13446)
	got, err := chooseHongguoCollectionTarget(context.Background(), 9, item, []row{original}, hongguoMatchingFixtureDeps())
	if err != nil || gconv.Int64(got["id"]) != 13446 || got["__collection_source"] != "hongguo" || got["__yqk_identity"] != discoveryFilmIdentity(original) {
		t.Fatalf("confirmed long-title short drama not merged: %v %v", got, err)
	}
	if original["__yqk_identity"] != nil || item["vod_year"] != "" || item["vod_area"] != "" || got["__collection_area"] != nil {
		t.Fatal("matching mutated rows or inferred missing metadata")
	}
}

func TestHongguoMatchingRejectsConflictsShortTitlesAndForeignNativeIdentity(t *testing.T) {
	for _, mode := range []string{"short-title", "movie", "anime", "unknown-kind", "punctuation", "season", "year", "region", "different-native", "mixed-native", "other-owned-native"} {
		t.Run(mode, func(t *testing.T) {
			item, original := hongguoMatchingFixtureItem(), hongguoMatchingFixtureRow(13446)
			switch mode {
			case "short-title":
				original["name"], item["vod_name"] = "绝世神医", "绝世神医"
			case "movie", "anime":
				original["__kind"] = mode
			case "unknown-kind":
				original["__kind"], original["class"] = "", "玄幻"
			case "punctuation":
				item["vod_name"] = "冲喜赘婿：竟是绝世神医"
			case "season":
				item["vod_name"] = "冲喜赘婿竟是绝世神医第二季"
			case "year":
				item["vod_year"] = "2025"
			case "region":
				item["vod_area"] = "日本"
			case "different-native":
				original["play_from"], original["play_url"] = "hongguo", "第1集$hongguo://999/11"
			case "mixed-native":
				original["play_from"], original["play_url"] = "hongguo", "第1集$hongguo://7684943327614995518/11#第2集$hongguo://999/12"
			case "other-owned-native":
				original["api_id"], original["api_vid"] = 9, "999"
			}
			got, err := chooseHongguoCollectionTarget(context.Background(), 9, item, []row{original}, hongguoMatchingFixtureDeps())
			if got != nil || mode == "short-title" && !errors.Is(err, errHongguoIdentityIncomplete) || mode != "short-title" && err != nil {
				t.Fatalf("unproved/remake identity selected: got=%v err=%v", got, err)
			}
		})
	}
}

func TestHongguoMatchingAcceptsShortTitleOnlyWithCompleteOrPinnedIdentity(t *testing.T) {
	for _, mode := range []string{"complete", "owned-legacy-genre", "stored-marker-unknown-kind", "explicit-class-fallback"} {
		t.Run(mode, func(t *testing.T) {
			item, original := hongguoMatchingFixtureItem(), hongguoMatchingFixtureRow(1)
			if mode != "explicit-class-fallback" {
				original["name"], item["vod_name"] = "绝世神医", "绝世神医"
			}
			switch mode {
			case "complete":
				item["vod_year"], item["vod_area"] = "2026", "中国大陆"
			case "owned-legacy-genre":
				original["api_id"], original["api_vid"], original["__kind"] = 9, item["vod_id"], "movie"
			case "stored-marker-unknown-kind":
				original["play_from"], original["play_url"], original["__kind"] = item["vod_play_from"], item["vod_play_url"], ""
			case "explicit-class-fallback":
				original["__kind"], original["class"] = "", "短剧,玄幻"
			}
			got, err := chooseHongguoCollectionTarget(context.Background(), 9, item, []row{original}, hongguoMatchingFixtureDeps())
			if err != nil || got == nil || got["type_id"] != original["type_id"] {
				t.Fatalf("independent/pinned identity not respected: %v %v", got, err)
			}
		})
	}
}

func TestHongguoMatchingAliasesRequireIndependentIdentityAndReportRemakes(t *testing.T) {
	item := hongguoMatchingFixtureItem()
	first, second := hongguoMatchingFixtureRow(1), hongguoMatchingFixtureRow(2)
	for _, mode := range []string{"same-proven-identity", "same-bound-missing-metadata", "different-year", "different-region", "unproven-second"} {
		t.Run(mode, func(t *testing.T) {
			a, b := fourKVMFixtureRowCopy(first), fourKVMFixtureRowCopy(second)
			want := mode == "same-proven-identity" || mode == "same-bound-missing-metadata"
			switch mode {
			case "same-bound-missing-metadata":
				for _, film := range []row{a, b} {
					film["play_from"], film["play_url"], film["year"], film["area"] = item["vod_play_from"], item["vod_play_url"], "0", ""
				}
			case "different-year":
				b["year"] = "2025"
			case "different-region":
				b["area"] = "日本"
			case "unproven-second":
				b["year"], b["area"] = "", ""
			}
			got, err := chooseHongguoCollectionTarget(context.Background(), 9, item, []row{b, a}, hongguoMatchingFixtureDeps())
			if !want {
				if got != nil || !errors.Is(err, errHongguoIdentityAmbiguous) {
					t.Fatalf("ambiguous/remake group merged: %v %v", got, err)
				}
				return
			}
			if err != nil || gconv.Int64(got["id"]) != 1 {
				t.Fatalf("oldest existing ID not preserved: %v %v", got, err)
			}
			aliases, ok := got["__collection_aliases"].([]row)
			if !ok || len(aliases) != 1 || gconv.Int64(aliases[0]["id"]) != 2 || aliases[0]["__collection_preserve_metadata"] != true {
				t.Fatal("proven aliases lost their stable ID/metadata")
			}
		})
	}
}

func TestHongguoPlaylistRejectsForeignOrMalformedMarkers(t *testing.T) {
	item := hongguoMatchingFixtureItem()
	collector := row{"api_url": hongguoSourceURL, "status": 1}
	from, play, err := validateHongguoCollectedPlaylist(collector, gconv.String(item["vod_id"]), "hongguo", gconv.String(item["vod_play_url"]))
	if err != nil || from != "hongguo" || play != item["vod_play_url"] {
		t.Fatalf("valid native playlist rejected: %v", err)
	}
	for _, invalid := range []string{"第1集$hongguo://999/11", "第1集$hongguo://7684943327614995518/11?token=secret", "第1集$hongguo://0/11", "第1集$https://media.example/file.mp4", "第1集$hongguo://7684943327614995518/11#broken", "$hongguo://7684943327614995518/11"} {
		if _, _, err := validateHongguoCollectedPlaylist(collector, gconv.String(item["vod_id"]), "hongguo", invalid); err == nil {
			t.Fatal("invalid or foreign native marker accepted")
		}
	}
	for _, disabled := range []row{nil, {"api_url": hongguoSourceURL, "status": 0}, {"api_url": "https://foreign.example/api", "status": 1}} {
		if _, _, err := validateHongguoCollectedPlaylist(disabled, gconv.String(item["vod_id"]), from, play); err == nil {
			t.Fatal("disabled/foreign collector accepted native markers")
		}
	}
}

func TestHongguoLockedMergeKeepsOriginalIDMetadataAndEpisodeIndices(t *testing.T) {
	original := hongguoMatchingFixtureRow(13446)
	oldFrom := "hnm3u8$$$hongguo"
	oldPlay := "第01集$https://cdn.example/old.m3u8$$$第02集$hongguo://7684943327614995518/12#第01集$hongguo://7684943327614995518/11"
	original["play_from"], original["play_url"] = oldFrom, oldPlay
	existing := fourKVMFixtureRowCopy(original)
	existing["__yqk_identity"], existing["__collection_source"] = discoveryFilmIdentity(original), "hongguo"
	update := collectedVodUpdate{existing: existing, apiID: 9, from: "hongguo", play: "第1集$hongguo://7684943327614995518/111#第2集$hongguo://7684943327614995518/12#第3集$hongguo://7684943327614995518/13",
		remarks: "foreign remarks", preparedPic: "foreign.jpg", preserveMetadata: true, pictureReady: true, needsPicture: func(row) bool { return false }}
	tx := &fourKVMCollectionFixtureTX{film: fourKVMFixtureRowCopy(original), collector: row{"api_url": hongguoSourceURL, "status": 1}}
	for round := 0; round < 2; round++ {
		if err := update.apply(tx); err != nil {
			t.Fatal(err)
		}
		for key, value := range original {
			if key != "play_from" && key != "play_url" && !reflect.DeepEqual(value, tx.film[key]) {
				t.Fatalf("cross-provider merge overwrote %s", key)
			}
		}
		sources := playlist(tx.film)
		if len(sources) != 2 || sources[0].Code != "hnm3u8" || sources[0].Episodes[0].URL != "https://cdn.example/old.m3u8" || len(sources[1].Episodes) != 3 || sources[1].Episodes[0].Name != "第02集" || sources[1].Episodes[1].Name != "第01集" || !strings.HasSuffix(sources[1].Episodes[1].URL, "/111") {
			t.Fatal("independent source, labels or stable episode indices changed")
		}
	}
	for _, query := range tx.queries {
		if !strings.Contains(query, "FOR UPDATE") {
			t.Fatal("native collector and film must remain locked through the write")
		}
	}
	for _, mode := range []string{"disabled", "collector-changed", "identity-changed", "native-series-changed"} {
		t.Run(mode, func(t *testing.T) {
			fixture := &fourKVMCollectionFixtureTX{film: fourKVMFixtureRowCopy(original), collector: row{"api_url": hongguoSourceURL, "status": 1}}
			switch mode {
			case "disabled":
				fixture.collector["status"] = 0
			case "collector-changed":
				fixture.collector["api_url"] = "https://other.example/api"
			case "identity-changed":
				fixture.film["year"] = "2025"
			case "native-series-changed":
				fixture.film["play_url"] = strings.ReplaceAll(oldPlay, "7684943327614995518", "999")
			}
			if err := update.apply(fixture); err == nil || fixture.writes != 0 {
				t.Fatal("concurrent source/identity change was overwritten")
			}
		})
	}
}

func TestHongguoOwnedMalformedLegacyLineReplacedOnlyForPinnedExactFilm(t *testing.T) {
	item, original := hongguoMatchingFixtureItem(), hongguoMatchingFixtureRow(1311)
	original["api_id"], original["api_vid"] = 9, item["vod_id"]
	original["play_from"], original["play_url"] = "hongguo$$$hnm3u8", "第1集$hongguo://999/11#第2集$hongguo://7684943327614995518/12$$$第01集$https://cdn.example/old.m3u8"
	selected, err := chooseHongguoCollectionTarget(context.Background(), 9, item, []row{original}, hongguoMatchingFixtureDeps())
	if err != nil || selected == nil {
		t.Fatalf("pinned original film cannot repair its old native line: %v", err)
	}
	update := collectedVodUpdate{existing: selected, apiID: 9, from: "hongguo", play: gconv.String(item["vod_play_url"]), preserveMetadata: true, needsPicture: func(row) bool { return false }}
	tx := &fourKVMCollectionFixtureTX{film: fourKVMFixtureRowCopy(original), collector: row{"api_url": hongguoSourceURL, "status": 1}}
	if err := update.apply(tx); err != nil {
		t.Fatal(err)
	}
	sources := playlist(tx.film)
	series, valid := hongguoStoredSeries(tx.film)
	if len(sources) != 2 || sources[0].Code != "hongguo" || len(sources[0].Episodes) != 2 || !valid || series != item["vod_id"] || sources[1].Episodes[0].URL != "https://cdn.example/old.m3u8" {
		t.Fatal("legacy repair appended another native drama or removed an independent source")
	}
	for _, mode := range []string{"title-conflict", "native-id-conflict", "year-conflict"} {
		t.Run(mode, func(t *testing.T) {
			conflict := fourKVMFixtureRowCopy(original)
			switch mode {
			case "title-conflict":
				conflict["name"] = "冲喜赘婿竟是绝世神医第二季"
			case "native-id-conflict":
				conflict["api_vid"] = "999"
			case "year-conflict":
				copyItem := fourKVMFixtureRowCopy(item)
				copyItem["vod_year"] = "2025"
				item = copyItem
			}
			got, err := chooseHongguoCollectionTarget(context.Background(), 9, item, []row{conflict}, hongguoMatchingFixtureDeps())
			if got != nil || err != nil {
				t.Fatalf("another movie/remake permitted native repair: %v %v", got, err)
			}
		})
	}
}

func TestHongguoSymmetricForeignCollectionDoesNotDuplicateNativeShortFilm(t *testing.T) {
	original, item := hongguoMatchingFixtureRow(1311), hongguoMatchingFixtureItem()
	original["api_id"], original["api_vid"], original["play_from"], original["play_url"] = 9, item["vod_id"], item["vod_play_from"], item["vod_play_url"]
	original["year"], original["area"] = "", ""
	foreign := map[string]any{"__yqk": true, "vod_id": "123456", "vod_name": item["vod_name"], "type_name": "短剧", "vod_year": "2026", "vod_area": "大陆", "vod_play_from": "yqk_1", "vod_play_url": "第1集$yqk://123456/1/123"}
	deps := hongguoMatchingFixtureDeps()
	deps.enrich = func(context.Context, discoveryTarget, row) (discoveryTarget, error) {
		t.Fatal("native identity must not request or fabricate foreign metadata")
		return discoveryTarget{}, nil
	}
	got, err := chooseCollectionTarget(context.Background(), 25, foreign, []row{original}, deps)
	if err != nil || got == nil || got["id"] != original["id"] || got["__collection_preserve_metadata"] != true || got["__collection_area"] != nil || original["area"] != "" {
		t.Fatalf("foreign short import duplicated/rewrote native film: %v %v", got, err)
	}
	for _, mode := range []string{"movie", "anime", "series", "short-name", "different-season", "mixed-native", "explicit-region", "explicit-year", "different-native-remake"} {
		t.Run(mode, func(t *testing.T) {
			local, remote := fourKVMFixtureRowCopy(original), fourKVMFixtureRowCopy(foreign)
			candidates := []row{local}
			switch mode {
			case "movie", "anime", "series":
				local["__kind"] = mode
			case "short-name":
				local["name"], remote["vod_name"] = "神医", "神医"
			case "different-season":
				remote["vod_name"] = gconv.String(remote["vod_name"]) + "第二季"
			case "mixed-native":
				local["play_url"] = "第1集$hongguo://999/1#第2集$hongguo://7684943327614995518/12"
			case "explicit-region":
				local["area"] = "日本"
			case "explicit-year":
				local["year"] = "2025"
			case "different-native-remake":
				other := fourKVMFixtureRowCopy(local)
				other["id"], other["api_vid"], other["play_url"] = 1361, "999", "第1集$hongguo://999/1"
				candidates = append(candidates, other)
			}
			got, err := chooseCollectionTarget(context.Background(), 25, remote, candidates, hongguoMatchingFixtureDeps())
			if got != nil || mode == "different-native-remake" && !errors.Is(err, errHongguoIdentityAmbiguous) || mode != "different-native-remake" && err != nil {
				t.Fatalf("unproven reverse identity matched: %v %v", got, err)
			}
		})
	}
}

func TestHongguoExistingNativeLineTightensOnlyForeignHTTPIdentity(t *testing.T) {
	original, native := hongguoMatchingFixtureRow(1311), hongguoMatchingFixtureItem()
	original["api_id"], original["api_vid"], original["play_from"], original["play_url"] = 9, native["vod_id"], native["vod_play_from"], native["vod_play_url"]
	original["year"], original["area"] = "", ""
	foreign := map[string]any{"vod_id": "foreign", "vod_name": original["name"], "type_name": "短剧", "vod_year": "2026", "vod_area": "大陆"}
	if !ordinaryCollectionMatches(original, "short", 2, foreign) {
		t.Fatal("distinctive exact native short title rejected a foreign short line")
	}
	original["name"], foreign["vod_name"] = "神医", "神医"
	if ordinaryCollectionMatches(original, "short", 2, foreign) {
		t.Fatal("missing native metadata merged an ambiguous short title")
	}
	original["api_id"], original["api_vid"] = 2, "foreign"
	if !ordinaryCollectionMatches(original, "short", 2, foreign) {
		t.Fatal("originally pinned HTTP film cannot update its own lines")
	}
	original["api_id"], original["api_vid"], original["year"], original["area"] = 9, native["vod_id"], "2026", "大陆"
	if !ordinaryCollectionMatches(original, "short", 2, foreign) {
		t.Fatal("complete independently stored identity rejected a short title")
	}
}
