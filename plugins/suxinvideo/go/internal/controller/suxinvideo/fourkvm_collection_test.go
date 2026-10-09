package suxinvideo

import (
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/suxinwl/GoSuxin/framework/container/gvar"
	"github.com/suxinwl/GoSuxin/framework/database/gdb"
)

func TestFourKVMCollectionRequiresSourceOwnedMarkers(t *testing.T) {
	collector := row{"api_url": fourKVMSourceURL, "status": 1}
	marker := "第01集$4kvm://xn2023?dataid=11&quality=1080"
	from, play, err := validateFourKVMCollectedPlaylist(collector, "xn2023", "4kvm", marker)
	if err != nil || from != "4kvm" || play != marker {
		t.Fatalf("valid source-owned marker rejected: %v", err)
	}
	for _, tc := range []struct {
		name, id, from, play string
		collector            row
	}{
		{name: "disabled", id: "xn2023", from: "4kvm", play: marker, collector: row{"api_url": fourKVMSourceURL, "status": 0}},
		{name: "foreign API", id: "xn2023", from: "4kvm", play: marker, collector: row{"api_url": "https://other.example/api", "status": 1}},
		{name: "unregistered native URL", id: "xn2023", from: "4kvm", play: marker, collector: row{"api_url": "4kvm://site?url=private", "status": 1}},
		{name: "other film slug", id: "other2023", from: "4kvm", play: marker, collector: collector},
		{name: "truncated remote ID", id: strings.Repeat("a", 33), from: "4kvm", play: marker, collector: collector},
		{name: "temporary media address", id: "xn2023", from: "4kvm", play: "第01集$https://cdn.example/signed.m3u8", collector: collector},
		{name: "arbitrary marker parameters", id: "xn2023", from: "4kvm", play: marker + "&url=https://private.example", collector: collector},
		{name: "empty data ID", id: "xn2023", from: "4kvm", play: "第01集$4kvm://xn2023?dataid=0", collector: collector},
		{name: "mixed namespaces", id: "xn2023", from: "4kvm$$$hnm3u8", play: marker + "$$$第01集$https://cdn.example/video.m3u8", collector: collector},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := validateFourKVMCollectedPlaylist(tc.collector, tc.id, tc.from, tc.play); err == nil {
				t.Fatal("unowned or malformed playlist accepted")
			}
		})
	}
}

func TestFourKVMCollectionBindsFilmAndKeepsDistinctChapterSlugs(t *testing.T) {
	collector := row{"api_url": fourKVMSourceURL, "status": 1}
	play := "第01集$4kvm://ch16yiwpe?dataid=11&quality=1080&chapter=ch16yiwpe#第02集$4kvm://ch16yiwpe?dataid=12&quality=1080&chapter=ch16yiwpu"
	from, normalized, err := validateFourKVMCollectedPlaylist(collector, "ch16yiwpe", "4kvm", play)
	if err != nil || from != "4kvm" || normalized != play {
		t.Fatalf("legitimate chapters with different playback slugs were rejected: %v", err)
	}
	for _, invalid := range []string{
		// A real chapter page is not interchangeable with the parent film ID.
		"第02集$4kvm://ch16yiwpu?dataid=12&quality=1080&chapter=ch16yiwpu",
		"第02集$4kvm://otherfilm?dataid=12&quality=1080&chapter=ch16yiwpu",
		"第02集$4kvm://ch16yiwpe?dataid=12&chapter=https%3A%2F%2Fprivate.example",
		"第02集$4kvm://ch16yiwpe?dataid=12&chapter=ch16yiwpu&chapter=other",
	} {
		if _, _, err := validateFourKVMCollectedPlaylist(collector, "ch16yiwpe", "4kvm", invalid); err == nil {
			t.Fatal("forged parent or malformed chapter identity reached collection")
		}
	}
}

func TestFourKVMCollectionMatchesFilmIdentity(t *testing.T) {
	film := row{"name": "仙逆", "year": "2023", "area": "中国大陆", "api_id": 10, "api_vid": "standalone"}
	for _, tc := range []struct {
		name, year, area, kind string
		known, want            bool
	}{
		{name: "same film", year: "2023", area: "China", kind: "国产动漫", want: true},
		{name: "same title remake", year: "2025", area: "中国大陆", kind: "国产动漫"},
		{name: "missing remote metadata", year: "", area: "", kind: "国产动漫"},
		{name: "missing remote kind", year: "2023", area: "中国大陆", kind: ""},
		{name: "different region", year: "2023", area: "中国香港", kind: "国产动漫"},
		{name: "different format", year: "2023", area: "中国大陆", kind: "动画电影"},
		{name: "known remote missing metadata", known: true, want: true},
		{name: "known remote remake conflict", known: true, year: "2025", area: "中国大陆", kind: "国产动漫"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			local := fourKVMFixtureRowCopy(film)
			if tc.known {
				local["api_id"], local["api_vid"] = 20, "xn2023"
			}
			item := map[string]any{"vod_id": "xn2023", "vod_name": "仙逆", "vod_year": tc.year, "vod_area": tc.area, "type_name": tc.kind}
			if got := fourKVMCollectionCompatible(local, "anime", item, 20); got != tc.want {
				t.Fatalf("same-title or trusted-ID metadata policy changed: got %v want %v", got, tc.want)
			}
		})
	}
}

// A transaction fixture executes the real locked update logic without any
// database configuration or business data mutation. It accepts only parameter
// bound updates and applies precisely the columns the production query writes.
type fourKVMCollectionFixtureTX struct {
	gdb.TX
	film, collector row
	writes          int
	queries         []string
}

func fourKVMFixtureRowCopy(input row) row {
	copy := row{}
	for key, value := range input {
		copy[key] = value
	}
	return copy
}

func (tx *fourKVMCollectionFixtureTX) GetOne(query string, _ ...any) (gdb.Record, error) {
	tx.queries = append(tx.queries, query)
	var input row
	switch {
	case strings.Contains(query, "FROM sx_vod WHERE"):
		input = tx.film
	case strings.Contains(query, "FROM sx_collect_api WHERE"):
		input = tx.collector
	default:
		return nil, errors.New("unexpected transaction read")
	}
	if input == nil {
		return nil, nil
	}
	result := gdb.Record{}
	for key, value := range input {
		result[key] = gvar.New(value)
	}
	return result, nil
}

func (tx *fourKVMCollectionFixtureTX) Exec(query string, args ...any) (sql.Result, error) {
	if !strings.HasPrefix(query, "UPDATE sx_vod SET ") {
		return nil, errors.New("unexpected transaction mutation")
	}
	set, where, ok := strings.Cut(strings.TrimPrefix(query, "UPDATE sx_vod SET "), " WHERE ")
	fields := strings.Split(set, ",")
	if !ok || where != "id=?" || len(args) != len(fields)+1 {
		return nil, errors.New("update must remain parameter bound")
	}
	for i, field := range fields {
		if !strings.HasSuffix(field, "=?") {
			return nil, errors.New("unbound update column")
		}
		tx.film[strings.TrimSuffix(field, "=?")] = args[i]
	}
	tx.writes++
	return nil, nil
}

func TestFourKVMCollectionTransactionPreservesAccessAndPrimaryMetadata(t *testing.T) {
	original := row{"id": 781, "api_id": 10, "api_vid": "original", "name": "仙逆", "year": "2023", "area": "中国大陆", "type_id": 33,
		"vip": 1, "points": 10, "status": 1, "pic": "existing-poster.jpg", "remarks": "更新至160集", "updatetime": 123,
		"play_from": "hnm3u8", "play_url": "第01集$https://cdn.example/old.m3u8"}
	existing := fourKVMFixtureRowCopy(original)
	existing["__yqk_identity"], existing["__collection_source"] = discoveryFilmIdentity(original), "4kvm"
	update := collectedVodUpdate{existing: existing, apiID: 20, from: "4kvm", play: "第01集$4kvm://xn2023?dataid=11&quality=1080",
		remarks: "wrong source description", preparedPic: "wrong-poster.jpg", preserveMetadata: true, pictureReady: true,
		needsPicture: func(row) bool { return false }}
	tx := &fourKVMCollectionFixtureTX{film: fourKVMFixtureRowCopy(original), collector: row{"api_url": fourKVMSourceURL, "status": 1}}
	if err := update.apply(tx); err != nil || tx.writes != 1 {
		t.Fatalf("strict native playlist merge failed: %v", err)
	}
	for key, value := range original {
		if key != "play_from" && key != "play_url" && !reflect.DeepEqual(value, tx.film[key]) {
			t.Fatalf("cross-source collection changed %s", key)
		}
	}
	if !strings.Contains(tx.film["play_from"].(string), "4kvm") || !strings.Contains(tx.film["play_from"].(string), "hnm3u8") {
		t.Fatal("existing independent line was lost or native line was not merged")
	}
	for _, query := range tx.queries {
		if !strings.Contains(query, "FOR UPDATE") {
			t.Fatal("film or native source was not locked through the write")
		}
	}
	for _, tc := range []struct {
		name      string
		collector row
		changed   bool
	}{
		{name: "disabled during collection", collector: row{"api_url": fourKVMSourceURL, "status": 0}},
		{name: "source changed during collection", collector: row{"api_url": "https://other.example/api", "status": 1}},
		{name: "film identity changed during collection", collector: row{"api_url": fourKVMSourceURL, "status": 1}, changed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := &fourKVMCollectionFixtureTX{film: fourKVMFixtureRowCopy(original), collector: tc.collector}
			if tc.changed {
				fixture.film["year"] = "2025"
			}
			if err := update.apply(fixture); err == nil || fixture.writes != 0 {
				t.Fatal("changed identity or source survived the locked validation")
			}
		})
	}
}
