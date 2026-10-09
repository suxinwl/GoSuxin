package suxinvideo

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

// These fixtures refuse every schema except the runner's generated disposable
// database, even if someone sets the opt-in flag against the development CMS.
func hongguoAuditFixtureContext(t *testing.T) context.Context {
	t.Helper()
	if os.Getenv("SUXIN_HONGGUO_AUDIT_FIXTURE_INTEGRATION") != "1" {
		t.Skip("run tools/test_hongguo_audit.py with a disposable database")
	}
	t.Setenv("SUXIN_INTEGRATION", "1")
	ctx := yqkIntegrationContext(t)
	db, err := one(ctx, "SELECT DATABASE() AS name")
	if err != nil || !regexp.MustCompile(`^suxin_hongguo_audit_verify_[0-9]+$`).MatchString(gconv.String(db["name"])) {
		t.Fatal("Hongguo audit fixture refuses a non-disposable database")
	}
	for _, table := range []string{"sx_vod_source_score", "sx_vod_alias", "sx_fav", "sx_play_record", "sx_comment", "sx_vod", "sx_collect_api", "sx_type"} {
		if err := execSQL(ctx, "DELETE FROM "+table); err != nil {
			t.Fatal(err)
		}
	}
	for _, sql := range []string{
		"INSERT INTO sx_type(id,pid,name,sort,status) VALUES(9001,0,'短剧',0,1),(9002,0,'剧情',0,1),(9003,0,'电影',0,1)",
		"INSERT INTO sx_collect_api(id,name,api_url,status,collect_auto,addtime) VALUES(900,'红果隔离源','hongguo://app',1,0,0),(901,'独立隔离源','https://fixture.invalid/api',1,0,0)",
	} {
		if err := execSQL(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	return ctx
}

func hongguoAuditFixtureInsert(t *testing.T, ctx context.Context, id, typeID, apiID int64, remote, name, year, area, from, play string, status int) {
	t.Helper()
	if err := execSQL(ctx, `INSERT INTO sx_vod(id,type_id,api_id,api_vid,name,name_norm,class,year,area,lang,remarks,score,director,actor,content,pic,play_from,play_url,vip,points,total_hits,status,addtime,updatetime) VALUES(?,?,?,?,?,?,'保留原标签',?,?,'国语','保留备注',8.1,'保留导演','保留演员','保留简介','keep-fixture.jpg',?,?,1,12,37,?,10,20)`, id, typeID, apiID, remote, name, normalizeVodName(name), year, area, from, play, status); err != nil {
		t.Fatal(err)
	}
}

func hongguoAuditFixtureRows(t *testing.T, ctx context.Context, table string) []row {
	t.Helper()
	result, err := all(ctx, "SELECT * FROM "+table+" ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func hongguoAuditFixtureDirectory(t *testing.T) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join("data", "tmp"), 0700); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(filepath.Join("data", "tmp"), "hongguo-audit-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func hongguoAuditFixtureProtectedRows(t *testing.T, before, after []row, allowed map[int64]map[string]bool) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatal("audit inserted or removed a business film")
	}
	for i, old := range before {
		current := after[i]
		id := gconv.Int64(old["id"])
		if gconv.Int64(current["id"]) != id {
			t.Fatal("audit changed stable film IDs")
		}
		for field, value := range old {
			if allowed[id][field] {
				continue
			}
			if gconv.String(value) != gconv.String(current[field]) {
				t.Fatalf("audit changed protected film %d field %s", id, field)
			}
		}
	}
}

func TestHongguoAuditReadOnlyAndApplyIntegration(t *testing.T) {
	ctx := hongguoAuditFixtureContext(t)
	title := "同名短剧合并独立隔离验证"
	native := "第1集$hongguo://100000001/110000001#第2集$hongguo://100000001/110000002"
	other := "第1集$https://fixture.invalid/independent.m3u8"
	hongguoAuditFixtureInsert(t, ctx, 1001, 9002, 900, "100000001", title, "", "", "hongguo", native, 0)
	hongguoAuditFixtureInsert(t, ctx, 1002, 9001, 901, "foreign-a", title, "2025", "中国大陆", "hnm3u8", other, 1)
	mixed := "第1集$hongguo://200000002/220000001#第2集$hongguo://200000003/230000001$$$" + other
	seasonalName := "原始红果错季重建隔离验证第二季"
	hongguoAuditFixtureInsert(t, ctx, 1003, 9002, 900, "200000002", seasonalName, "", "", "hongguo$$$hnm3u8", mixed, 0)
	if err := execSQL(ctx, "INSERT INTO sx_fav(user_id,vod_id,created) VALUES(71,1002,300)"); err != nil {
		t.Fatal(err)
	}
	if err := execSQL(ctx, "INSERT INTO sx_play_record(user_id,vod_id,episode,position,updated) VALUES(71,1002,1,42,301)"); err != nil {
		t.Fatal(err)
	}
	original := hongguoAuditFixtureRows(t, ctx, "sx_vod")
	favourites := hongguoAuditFixtureRows(t, ctx, "sx_fav")
	history := hongguoAuditFixtureRows(t, ctx, "sx_play_record")
	byID := map[int64]row{}
	for _, v := range original {
		byID[gconv.Int64(v["id"])] = v
	}
	dir := hongguoAuditFixtureDirectory(t)
	repairFile := filepath.Join(dir, "repair.private.json")
	repair := []hongguoAuditRepair{{ID: 1003, Name: seasonalName, APIID: 900, APIVID: "200000002", RemoteTitle: seasonalName, Verified: true, From: "hongguo", Play: "第1集$hongguo://200000002/220000001#第2集$hongguo://200000002/220000002", OldHash: hongguoAuditPlaylistHash(byID[1003])}}
	if err := hongguoAuditWriteJSON(repairFile, repair, false); err != nil {
		t.Fatal(err)
	}
	readOnly, err := RunHongguoAudit(ctx, HongguoAuditOptions{OutputDir: filepath.Join(dir, "readonly"), RepairFile: repairFile})
	if err != nil {
		t.Fatal(err)
	}
	if readOnly.Applied || readOnly.CategoryRepairs != 2 || readOnly.NativeRepairs != 1 || readOnly.ResourceCopies != 1 || readOnly.ChangedRecords != 3 {
		t.Fatalf("read-only scan did not plan controlled repairs: %#v", readOnly)
	}
	if !reflect.DeepEqual(original, hongguoAuditFixtureRows(t, ctx, "sx_vod")) {
		t.Fatal("default read-only audit changed business data")
	}
	if _, err := os.Stat(filepath.Join(dir, "readonly", "before.private.json")); !os.IsNotExist(err) {
		t.Fatal("read-only scan created an apply backup")
	}
	applied, err := RunHongguoAudit(ctx, HongguoAuditOptions{Apply: true, OutputDir: filepath.Join(dir, "apply"), RepairFile: repairFile})
	if err != nil {
		t.Fatal(err)
	}
	if !applied.Applied || applied.ChangedRecords != 3 {
		t.Fatal("controlled repairs were not applied")
	}
	after := hongguoAuditFixtureRows(t, ctx, "sx_vod")
	hongguoAuditFixtureProtectedRows(t, original, after, map[int64]map[string]bool{1001: {"type_id": true}, 1002: {"play_from": true, "play_url": true}, 1003: {"type_id": true, "play_from": true, "play_url": true}})
	got := map[int64]row{}
	for _, v := range after {
		got[gconv.Int64(v["id"])] = v
	}
	if gconv.Int64(got[1001]["type_id"]) != 9001 || gconv.Int64(got[1003]["type_id"]) != 9001 {
		t.Fatal("native legacy genre categories were not corrected")
	}
	if !strings.Contains(gconv.String(got[1002]["play_from"]), "hongguo") || !strings.Contains(gconv.String(got[1002]["play_url"]), other) {
		t.Fatal("adding Hongguo lost the independent original line")
	}
	series, valid := hongguoStoredSeries(got[1003])
	if !valid || series != "200000002" || !strings.Contains(gconv.String(got[1003]["play_url"]), other) {
		t.Fatal("native repair retained a wrong season or lost a foreign line")
	}
	if !reflect.DeepEqual(favourites, hongguoAuditFixtureRows(t, ctx, "sx_fav")) || !reflect.DeepEqual(history, hongguoAuditFixtureRows(t, ctx, "sx_play_record")) {
		t.Fatal("audit changed favourites or viewing history")
	}
	backupData, err := os.ReadFile(filepath.Join(dir, "apply", "before.private.json"))
	if err != nil {
		t.Fatal("apply did not create a durable private backup")
	}
	var backup []row
	if json.Unmarshal(backupData, &backup) != nil || len(backup) != 3 {
		t.Fatal("private backup did not contain the affected films and donor")
	}
	for _, v := range backup {
		if hongguoAuditRowHash(v) != hongguoAuditRowHash(byID[gconv.Int64(v["id"])]) {
			t.Fatal("backup did not capture original rows before mutation")
		}
	}
	safeData, err := os.ReadFile(filepath.Join(dir, "apply", "report.safe.json"))
	if err != nil || strings.Contains(string(safeData), "hongguo://") || strings.Contains(string(safeData), "https://fixture") {
		t.Fatal("safe report included a private playback URL")
	}
	again, err := RunHongguoAudit(ctx, HongguoAuditOptions{Apply: true, OutputDir: filepath.Join(dir, "repeat"), RepairFile: repairFile})
	if err != nil || again.ChangedRecords != 0 || again.ResourceCopies != 0 || again.NativeRepairs != 0 || again.CategoryRepairs != 0 {
		t.Fatalf("repeat audit was not idempotent: %v %#v", err, again)
	}
	if !reflect.DeepEqual(after, hongguoAuditFixtureRows(t, ctx, "sx_vod")) {
		t.Fatal("repeat audit changed business data")
	}
}

func TestHongguoAuditConflictAndRepairEvidenceIntegration(t *testing.T) {
	ctx := hongguoAuditFixtureContext(t)
	// Explicit remake years reject copies. Two unbound candidates with missing
	// independent metadata must also remain separate instead of picking the first.
	yearTitle := "明确发行年份冲突短剧隔离验证"
	native := func(series string) string { return "第1集$hongguo://" + series + "/330000001" }
	foreign := "第1集$https://fixture.invalid/retain.m3u8"
	hongguoAuditFixtureInsert(t, ctx, 2001, 9001, 900, "300000001", yearTitle, "2025", "大陆", "hongguo", native("300000001"), 1)
	hongguoAuditFixtureInsert(t, ctx, 2002, 9001, 901, "year-target", yearTitle, "2024", "大陆", "hnm3u8", foreign, 1)
	ambiguousTitle := "同名缺失资料多候选短剧隔离验证"
	hongguoAuditFixtureInsert(t, ctx, 2003, 9001, 900, "300000003", ambiguousTitle, "", "", "hongguo", native("300000003"), 1)
	hongguoAuditFixtureInsert(t, ctx, 2004, 9001, 901, "ambiguous-a", ambiguousTitle, "", "", "hnm3u8", foreign, 1)
	hongguoAuditFixtureInsert(t, ctx, 2005, 9001, 901, "ambiguous-b", ambiguousTitle, "", "", "xlm3u8", foreign, 0)
	mixedName := "未经验证的旧季修复隔离验证"
	badLine := "第1集$hongguo://300000006/360000001#第2集$hongguo://300000007/370000001"
	hongguoAuditFixtureInsert(t, ctx, 2006, 9001, 900, "300000006", mixedName, "", "", "hongguo", badLine, 1)
	before := hongguoAuditFixtureRows(t, ctx, "sx_vod")
	dir := hongguoAuditFixtureDirectory(t)
	repairPath := filepath.Join(dir, "stale.private.json")
	repair := []hongguoAuditRepair{{ID: 2006, Name: mixedName, APIID: 900, APIVID: "300000006", RemoteTitle: mixedName, Verified: true, From: "hongguo", Play: native("300000006"), OldHash: strings.Repeat("0", 64)}}
	if err := hongguoAuditWriteJSON(repairPath, repair, false); err != nil {
		t.Fatal(err)
	}
	result, err := RunHongguoAudit(ctx, HongguoAuditOptions{Apply: true, OutputDir: filepath.Join(dir, "apply"), RepairFile: repairPath})
	if err != nil {
		t.Fatal(err)
	}
	if result.ChangedRecords != 0 || result.ResourceCopies != 0 || result.NativeRepairs != 0 || result.PendingPairs < 3 {
		t.Fatalf("conflicting or ambiguous evidence permitted repair: %#v", result)
	}
	if !reflect.DeepEqual(before, hongguoAuditFixtureRows(t, ctx, "sx_vod")) {
		t.Fatal("conflict-only scan mutated existing resources")
	}
	foundStale, foundAmbiguous := false, false
	for _, entry := range result.Entries {
		for _, problem := range entry.Problems {
			if entry.ID == 2006 && strings.Contains(problem, "已变化") {
				foundStale = true
			}
			if entry.ID == 2003 && strings.Contains(problem, "多个") {
				foundAmbiguous = true
			}
		}
	}
	if !foundStale || !foundAmbiguous {
		t.Fatal("stale repair and unresolved candidates were not reported")
	}
}
