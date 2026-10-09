package suxinvideo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestHongguoAuditKindFollowsAncestryWithoutReplacingIndependentKinds(t *testing.T) {
	types := map[int64]row{
		1: {"name": "短剧", "pid": 0},
		2: {"name": "玄幻", "pid": 1},
		3: {"name": "电影", "pid": 0},
		4: {"name": "动漫", "pid": 0},
		5: {"name": "孤立标签", "pid": 0},
		6: {"name": "环甲", "pid": 7},
		7: {"name": "环乙", "pid": 6},
	}
	for _, tc := range []struct {
		name string
		film row
		want string
	}{
		{"short ancestor", row{"type_id": 2, "class": "玄幻"}, "short"},
		{"movie category wins over foreign tag", row{"type_id": 3, "class": "短剧"}, "movie"},
		{"anime category remains independent", row{"type_id": 4, "class": "短剧"}, "anime"},
		{"orphan explicit short class", row{"type_id": 5, "class": "短剧,玄幻"}, "short"},
		{"missing category class fallback", row{"type_id": 999, "class": "国产动漫"}, "anime"},
		{"unknown genre remains unknown", row{"type_id": 5, "class": "玄幻"}, ""},
		{"cyclic ancestry terminates", row{"type_id": 6, "class": "短剧"}, "short"},
		{"zero category", row{"type_id": 0}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, _ := json.Marshal(tc.film)
			if got := hongguoAuditKind(tc.film, types); got != tc.want {
				t.Fatalf("kind=%q, want %q", got, tc.want)
			}
			after, _ := json.Marshal(tc.film)
			if string(before) != string(after) {
				t.Fatal("audit classification mutated the stored metadata")
			}
		})
	}
	deep := map[int64]row{}
	for id := int64(1); id < 10; id++ {
		deep[id] = row{"name": "标签", "pid": id + 1}
	}
	deep[10] = row{"name": "电影", "pid": 0}
	if got := hongguoAuditKind(row{"type_id": 1, "class": "短剧"}, deep); got != "short" {
		t.Fatal("ancestry exceeded the bounded traversal before explicit class fallback")
	}
}

func hongguoAuditFixtureRow(id int64, year, area string) row {
	return row{
		"id": id, "name": "谁说乞丐不能逆袭", "api_id": 2, "api_vid": "foreign",
		"year": year, "area": area, "type_id": 1, "class": "短剧", "status": 1,
		"play_from": "hnm3u8", "play_url": "第1集$https://fixture.example/first.m3u8",
	}
}

func TestHongguoAuditGroupGateRejectsCrossSourceRemakeAmbiguity(t *testing.T) {
	types := map[int64]row{1: {"name": "短剧", "pid": 0}, 2: {"name": "电影", "pid": 0}}
	item := map[string]any{"vod_id": "123", "vod_name": "谁说乞丐不能逆袭", "type_name": "短剧", "vod_year": "", "vod_area": ""}
	for _, tc := range []struct {
		name string
		mode string
		want bool
	}{
		{"distinct release years", "year", true},
		{"distinct independent regions", "region", true},
		{"unproven second foreign record", "incomplete", true},
		{"different native drama", "other-native", true},
		{"same complete foreign identity", "complete", false},
		{"same native aliases with missing metadata", "bound-aliases", false},
		{"one eligible foreign record", "single", false},
		{"independent movie is not a short candidate", "movie", false},
		{"unrelated title is not a candidate", "title", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			donor := hongguoAuditFixtureRow(10, "", "")
			donor["api_id"], donor["api_vid"] = 9, "123"
			donor["play_from"], donor["play_url"] = "hongguo", "第1集$hongguo://123/11"
			a, b := hongguoAuditFixtureRow(20, "2026", "中国大陆"), hongguoAuditFixtureRow(30, "2026", "大陆")
			switch tc.mode {
			case "year":
				b["year"] = "2024"
			case "region":
				b["area"] = "日本"
			case "incomplete":
				b["year"], b["area"] = "", ""
			case "other-native":
				b["play_from"], b["play_url"] = "hongguo", "第1集$hongguo://999/22"
			case "bound-aliases":
				for _, film := range []row{a, b} {
					film["year"], film["area"] = "", ""
					film["play_from"], film["play_url"] = "hongguo", "第1集$hongguo://123/11"
				}
			case "movie":
				b["type_id"] = 2
			case "title":
				b["name"] = "谁说乞丐不能逆袭第二季"
			}
			group := []row{b, donor, a}
			if tc.mode == "single" {
				group = []row{donor, a}
			}
			before, _ := json.Marshal(group)
			if got := hongguoAuditGroupAmbiguous(group, 10, 9, "123", item, types); got != tc.want {
				t.Fatalf("ambiguous=%v, want %v", got, tc.want)
			}
			after, _ := json.Marshal(group)
			if string(before) != string(after) {
				t.Fatal("conflict gate changed candidate playback or metadata")
			}
		})
	}
}

func TestHongguoAuditSnapshotHashesProtectIdentityAndPlayback(t *testing.T) {
	film := hongguoAuditFixtureRow(20, "2026", "大陆")
	film["pic"], film["vip"], film["points"], film["updatetime"] = "keep.jpg", 1, 10, 123
	baseline := hongguoAuditRowHash(film)
	for _, field := range []string{"id", "api_id", "api_vid", "name", "class", "year", "area", "type_id", "status", "play_from", "play_url"} {
		t.Run(field, func(t *testing.T) {
			changed := row{}
			for key, value := range film {
				changed[key] = value
			}
			changed[field] = "changed"
			if hongguoAuditRowHash(changed) == baseline {
				t.Fatal("concurrent change to an audited identity/playback field escaped the snapshot hash")
			}
		})
	}
	unchanged := row{}
	for key, value := range film {
		unchanged[key] = value
	}
	unchanged["pic"], unchanged["vip"], unchanged["points"], unchanged["updatetime"] = "edited.jpg", 0, 20, 456
	if hongguoAuditRowHash(unchanged) != baseline {
		t.Fatal("fields the audit never writes unnecessarily invalidated the snapshot")
	}
	if hongguoAuditPlaylistHash(unchanged) != hongguoAuditPlaylistHash(film) {
		t.Fatal("playlist evidence was affected by unrelated metadata")
	}
	if hongguoAuditPlaylistHash(row{"play_from": "ab", "play_url": "c"}) == hongguoAuditPlaylistHash(row{"play_from": "a", "play_url": "bc"}) {
		t.Fatal("source/playlist boundary was ambiguous in the hash")
	}
	if !reflect.DeepEqual(film["pic"], "keep.jpg") {
		t.Fatal("hashing changed the original row")
	}
}

func TestHongguoAuditOutputDirectoryRejectsPathAndLinkEscapes(t *testing.T) {
	t.Chdir(t.TempDir())
	base, err := filepath.Abs(filepath.Join("data", "tmp"))
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{base, filepath.Dir(base), filepath.Join(base, "..", "published"), filepath.Join(filepath.Dir(base), "tmp-public", "audit")} {
		if got, err := hongguoAuditOutputDirectory(invalid); err == nil || got != "" {
			t.Fatalf("directory outside a private subdirectory accepted: %q", invalid)
		}
		if invalid != base && invalid != filepath.Dir(base) {
			if _, err := os.Stat(invalid); !os.IsNotExist(err) {
				t.Fatal("lexically rejected destination was created")
			}
		}
	}
	dir, err := hongguoAuditOutputDirectory(filepath.Join(base, "review", "nested"))
	if err != nil || dir != filepath.Join(base, "review", "nested") {
		t.Fatalf("private nested destination failed: %q %v", dir, err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("accepted private directory was not created: %v", err)
	}
	defaultDir, err := hongguoAuditOutputDirectory("")
	if err != nil {
		t.Fatal(err)
	}
	if rel, err := filepath.Rel(base, defaultDir); err != nil || !strings.HasPrefix(rel, "hongguo-audit"+string(filepath.Separator)) {
		t.Fatalf("default output escaped its private audit directory: %q", defaultDir)
	}
	outside := t.TempDir()
	link := filepath.Join(base, "publish-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Logf("symbolic-link boundary case unavailable on this host: %v", err)
		return
	}
	if got, err := hongguoAuditOutputDirectory(filepath.Join(link, "audit")); err == nil || got != "" {
		t.Fatal("directory link published private audit data outside data/tmp")
	}
}

func TestHongguoAuditBackupWriterPreservesPriorEvidenceAndReportsFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "before.private.json")
	original := []row{{"id": 20, "play_from": "hongguo", "play_url": "第1集$hongguo://123/11"}}
	if err := hongguoAuditWriteJSON(path, original, true); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stored []row
	if err := json.Unmarshal(before, &stored); err != nil || len(stored) != 1 || stored[0]["play_url"] != original[0]["play_url"] {
		t.Fatalf("backup was not complete valid JSON: %v", err)
	}
	if err := hongguoAuditWriteJSON(path, []row{{"id": 999}}, true); err == nil {
		t.Fatal("exclusive backup silently replaced prior evidence")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatal("failed exclusive backup changed the first snapshot")
	}
	reportPath := filepath.Join(dir, "report.safe.json")
	if err := hongguoAuditWriteJSON(reportPath, map[string]bool{"applied": false}, false); err != nil {
		t.Fatal(err)
	}
	if err := hongguoAuditWriteJSON(reportPath, map[string]bool{"applied": true}, false); err != nil {
		t.Fatal(err)
	}
	reportData, err := os.ReadFile(reportPath)
	if err != nil || !strings.Contains(string(reportData), `"applied": true`) || strings.Contains(string(reportData), `"applied": false`) {
		t.Fatal("final safe report was not replaced after applying")
	}
	if err := hongguoAuditWriteJSON(filepath.Join(dir, "missing", "before.private.json"), original, true); err == nil {
		t.Fatal("unwritable backup location reported success")
	}
	if err := hongguoAuditWriteJSON(filepath.Join(dir, "unsupported.private.json"), make(chan int), true); err == nil {
		t.Fatal("JSON encoding failure reported a durable valid backup")
	}
}
