package suxinvideo

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

func TestVodAliasMaintenanceIntegration(t *testing.T) {
	ctx := hongguoAuditFixtureContext(t)
	for _, sql := range []string{
		"INSERT INTO sx_type(id,pid,name,sort,status) VALUES(9004,0,'电视剧',0,1)",
		"UPDATE sx_player SET status=1 WHERE code IN ('hongguo','hnm3u8','wjm3u8')",
	} {
		if err := execSQL(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	native := "第01集$hongguo://100000001/110000001#第02集$hongguo://100000001/110000002"
	nativeSecond := "第一话$hongguo://200000002/220000001"
	foreign := "第1集$https://fixture.invalid/foreign.m3u8"
	hongguoAuditFixtureInsert(t, ctx, 1001, 9001, 900, "100000001", "批准同名短剧", "", "", "hongguo", native, 1)
	hongguoAuditFixtureInsert(t, ctx, 1002, 9001, 901, "foreign-a", "批准同名短剧", "2026", "中国大陆", "wjm3u8", foreign, 1)
	hongguoAuditFixtureInsert(t, ctx, 1003, 9001, 900, "200000002", "批准同名短剧第二季", "", "", "hongguo", nativeSecond, 1)
	hongguoAuditFixtureInsert(t, ctx, 1004, 9004, 901, "tv-a", "批准同名短剧", "2013", "中国大陆", "hnm3u8", foreign, 1)
	hongguoAuditFixtureInsert(t, ctx, 1005, 9001, 901, "not-approved", "排除独立短剧", "2026", "中国大陆", "hnm3u8", foreign, 1)
	if err := execSQL(ctx, "UPDATE sx_vod SET vip=0,points=0 WHERE id IN(1001,1002,1003,1005)"); err != nil {
		t.Fatal(err)
	}
	if err := execSQL(ctx, "INSERT INTO sx_fav(user_id,vod_id,created) VALUES(71,1002,300)"); err != nil {
		t.Fatal(err)
	}
	if err := execSQL(ctx, "INSERT INTO sx_play_record(user_id,vod_id,episode,position,updated) VALUES(71,1003,1,42,301)"); err != nil {
		t.Fatal(err)
	}
	original := hongguoAuditFixtureRows(t, ctx, "sx_vod")
	favourites, history := hongguoAuditFixtureRows(t, ctx, "sx_fav"), hongguoAuditFixtureRows(t, ctx, "sx_play_record")
	dir := hongguoAuditFixtureDirectory(t)
	reportFile, variantsFile := filepath.Join(dir, "input.safe.json"), filepath.Join(dir, "variants.safe.json")
	reportInput := map[string]any{"entries": []any{map[string]any{"id": 1001, "name": "批准同名短剧", "candidate_details": []any{map[string]any{"id": 1002, "status": "待核实"}, map[string]any{"id": 1004, "status": "待核实"}, map[string]any{"id": 1005, "status": "身份匹配"}}}}}
	variantsInput := []any{map[string]any{"hongguo_id": 1001, "candidate_id": 1003, "hongguo_title": "批准同名短剧", "candidate_title": "批准同名短剧第二季", "status": "待核实；完整标题不同，未自动合并"}}
	for name, value := range map[string]any{reportFile: reportInput, variantsFile: variantsInput} {
		if err := hongguoAuditWriteJSON(name, value, false); err != nil {
			t.Fatal(err)
		}
	}
	options := VodAliasOptions{ReportFile: reportFile, VariantsFile: variantsFile, OutputDir: filepath.Join(dir, "dryrun")}
	dry, err := RunVodAliasMaintenance(ctx, options)
	if err != nil || dry.Applied || len(dry.Groups) != 1 || len(dry.Groups[0].IDs) != 4 {
		t.Fatalf("explicit group dryrun failed: %v", err)
	}
	aliases, err := all(ctx, "SELECT * FROM sx_vod_alias")
	if err != nil || len(aliases) != 0 || !reflect.DeepEqual(original, hongguoAuditFixtureRows(t, ctx, "sx_vod")) {
		t.Fatal("dryrun wrote film data or aliases")
	}
	options.Apply, options.OutputDir = true, filepath.Join(dir, "apply")
	applied, err := RunVodAliasMaintenance(ctx, options)
	if err != nil || !applied.Applied || applied.ChangedMembers != 4 {
		t.Fatalf("apply failed: %v", err)
	}
	if !reflect.DeepEqual(original, hongguoAuditFixtureRows(t, ctx, "sx_vod")) || !reflect.DeepEqual(favourites, hongguoAuditFixtureRows(t, ctx, "sx_fav")) || !reflect.DeepEqual(history, hongguoAuditFixtureRows(t, ctx, "sx_play_record")) {
		t.Fatal("alias installation changed films, favourites or history")
	}
	backup, err := os.ReadFile(filepath.Join(dir, "apply", "before-attempt-01.private.json"))
	if err != nil {
		t.Fatal(err)
	}
	var proof struct {
		Films   []row `json:"films"`
		Aliases []row `json:"aliases"`
	}
	if json.Unmarshal(backup, &proof) != nil || len(proof.Films) != 4 || len(proof.Aliases) != 0 {
		t.Fatal("private backup is incomplete or includes unrelated film")
	}
	safe, err := os.ReadFile(filepath.Join(dir, "apply", "report.safe.json"))
	if err != nil || strings.Contains(string(safe), "hongguo://") || strings.Contains(string(safe), "fixture.invalid") {
		t.Fatal("safe report exposed source URLs")
	}
	options.OutputDir = filepath.Join(dir, "repeat")
	repeat, err := RunVodAliasMaintenance(ctx, options)
	if err != nil || repeat.ChangedMembers != 0 {
		t.Fatal("repeated approved group creation was not idempotent")
	}
	vod, err := one(ctx, "SELECT * FROM sx_vod WHERE id=1001")
	if err != nil {
		t.Fatal(err)
	}
	sources, err := hydratePlayers(ctx, vod, playlist(vod))
	if err != nil || len(sources) != 3 {
		t.Fatalf("anonymous group source view did not omit locked TV member: %v count=%d", err, len(sources))
	}
	byOwner := map[int64]source{}
	for _, src := range sources {
		byOwner[src.OwnerVodID] = src
	}
	if byOwner[1001].Code != "hongguo" || byOwner[1003].Code != "alias_1003_hongguo" || byOwner[1001].VersionKey == byOwner[1003].VersionKey || byOwner[1001].VersionKey != byOwner[1002].VersionKey || byOwner[1004].Code != "" {
		t.Fatal("raw owner, same-version sharing or independent season isolation is wrong")
	}
	if !reflect.DeepEqual(byOwner[1003].Episodes, playlist(row{"play_from": "hongguo", "play_url": nativeSecond})[0].Episodes) {
		t.Fatal("another season's raw episode labels changed")
	}
	listed, err := all(ctx, "SELECT id FROM sx_vod WHERE "+publicVodListingCondition(ctx, "")+" ORDER BY id")
	if err != nil || len(listed) != 2 || gconv.Int64(listed[0]["id"]) != 1001 {
		t.Fatal("lists did not collapse aliases")
	}
	if err := execSQL(ctx, "UPDATE sx_vod SET status=0 WHERE id=1001"); err != nil {
		t.Fatal(err)
	}
	listed, err = all(ctx, "SELECT id FROM sx_vod WHERE "+publicVodListingCondition(ctx, "")+" ORDER BY id")
	if err != nil || len(listed) != 2 || gconv.Int64(listed[0]["id"]) != 1002 {
		t.Fatal("disabled canonical hid the remaining public member")
	}
	if err := execSQL(ctx, "UPDATE sx_vod SET status=1 WHERE id=1001"); err != nil {
		t.Fatal(err)
	}
	update := map[string]any{"vod_id": "foreign-a", "vod_name": "批准同名短剧", "type_name": "短剧", "vod_year": "2024", "vod_area": "中国大陆", "vod_play_from": "wjm3u8", "vod_play_url": "第1集$https://fixture.invalid/updated.m3u8", "vod_remarks": "should not replace editorial remarks"}
	result, err := upsertMacVod(ctx, 901, update)
	if err != nil || result != 2 {
		t.Fatalf("bound approved member rejected later metadata conflict: %v", err)
	}
	after := hongguoAuditFixtureRows(t, ctx, "sx_vod")
	hongguoAuditFixtureProtectedRows(t, original, after, map[int64]map[string]bool{1002: {"play_from": true, "play_url": true}})
	if len(after) != 5 || !strings.Contains(gconv.String(after[1]["play_url"]), "updated.m3u8") || gconv.String(after[2]["play_url"]) != nativeSecond {
		t.Fatal("collection duplicated a group or overwrote another season")
	}
	// Maintenance and ongoing collection scoring must use the same film-first
	// lock order. Two maintenance calls also verify their dedicated named lock
	// does not leave a pooled session holding it after a successful return.
	start := make(chan struct{})
	failures := make(chan error, 3)
	var workers sync.WaitGroup
	for index := 1; index <= 2; index++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-start
			parallel := options
			parallel.OutputDir = filepath.Join(dir, fmt.Sprintf("parallel-%d", index))
			result, e := RunVodAliasMaintenance(ctx, parallel)
			if e != nil {
				failures <- e
				return
			}
			if !result.Applied || result.ChangedMembers != 0 {
				failures <- errors.New("parallel maintenance changed an existing group")
			}
		}(index)
	}
	workers.Add(1)
	go func() {
		defer workers.Done()
		<-start
		for index := 0; index < 16; index++ {
			if e := saveCollectedVodScore(ctx, 1002, 901, map[string]any{"vod_id": "foreign-a", "vod_name": "批准同名短剧", "vod_score": 8.8}); e != nil {
				failures <- e
				return
			}
		}
	}()
	close(start)
	workers.Wait()
	close(failures)
	for failure := range failures {
		t.Fatalf("maintenance/scoring lock order failed: %v", failure)
	}
	final := hongguoAuditFixtureRows(t, ctx, "sx_vod")
	hongguoAuditFixtureProtectedRows(t, after, final, map[int64]map[string]bool{1001: {"score": true}, 1002: {"score": true}, 1003: {"score": true}, 1004: {"score": true}})
	for index := 1; index <= 2; index++ {
		if _, e := os.Stat(filepath.Join(dir, fmt.Sprintf("parallel-%d", index), "before-attempt-01.private.json")); e != nil {
			t.Fatal("maintenance retry evidence was not independently backed up")
		}
	}
}
