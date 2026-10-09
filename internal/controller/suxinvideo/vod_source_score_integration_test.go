package suxinvideo

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestVodSourceScoreCollectionIntegration(t *testing.T) {
	ctx := hongguoAuditFixtureContext(t)
	if err := execSQL(ctx, "INSERT INTO sx_collect_api(id,name,api_url,status,collect_auto,addtime) VALUES(902,'小柒隔离评分源','yqk://app',1,0,0)"); err != nil {
		t.Fatal(err)
	}
	title := "真实评分来源优先合并隔离验证"
	foreign := "第1集$https://fixture.invalid/rating.m3u8"
	hongguoAuditFixtureInsert(t, ctx, 3310, 9001, 901, "foreign-score", title, "2026", "大陆", "hnm3u8", foreign, 1)
	original := hongguoAuditFixtureRows(t, ctx, "sx_vod")
	other := row{"vod_id": "foreign-score", "vod_name": title, "vod_douban_score": "6.2"}
	if err := saveCollectedVodScore(ctx, 3310, 901, other); err != nil {
		t.Fatal(err)
	}
	native := row{"__hongguo": true, "vod_id": "400000001", "vod_name": title, "type_name": "短剧", "vod_year": "", "vod_area": "", "vod_douban_score": "8.5", "vod_play_from": "hongguo", "vod_play_url": "第1集$hongguo://400000001/410000001"}
	if state, err := upsertMacVod(ctx, 900, native); err != nil || state != 2 {
		t.Fatalf("native score update did not merge stable film: %v %d", err, state)
	}
	yqk := row{"vod_id": "12345", "vod_name": title, "vod_douban_score": "9.1"}
	if err := saveCollectedVodScore(ctx, 3310, 902, yqk); err != nil {
		t.Fatal(err)
	}
	other["vod_douban_score"] = "9.9"
	if err := saveCollectedVodScore(ctx, 3310, 901, other); err != nil {
		t.Fatal(err)
	}
	current, _ := one(ctx, "SELECT * FROM sx_vod WHERE id=3310")
	if gconv.Float64(current["score"]) != 9.1 {
		t.Fatal("higher auxiliary score replaced YQK preference")
	}
	if err := hydrateVodSourceScores(ctx, []row{current}); err != nil {
		t.Fatal(err)
	}
	if scoreSource(current) != "小柒" || scoreLabel(current) != "9.1" {
		t.Fatal("preferred score or provenance not displayed")
	}
	yqk["vod_douban_score"] = "0.0"
	if err := saveCollectedVodScore(ctx, 3310, 902, yqk); err != nil {
		t.Fatal(err)
	}
	current, _ = one(ctx, "SELECT * FROM sx_vod WHERE id=3310")
	if gconv.Float64(current["score"]) != 8.5 {
		t.Fatal("missing YQK did not fall back to real Hongguo score")
	}
	if err := execSQL(ctx, "UPDATE sx_collect_api SET status=0 WHERE id=900"); err != nil {
		t.Fatal(err)
	}
	if err := hydrateVodSourceScores(ctx, []row{current}); err != nil {
		t.Fatal(err)
	}
	if scoreSource(current) != "独立隔离源" || scoreLabel(current) != "9.9" {
		t.Fatal("disabled preferred source did not fall back")
	}
	// Only score and playback fields may change; origin, poster, status and
	// membership remain the independently stored original record.
	after := hongguoAuditFixtureRows(t, ctx, "sx_vod")
	hongguoAuditFixtureProtectedRows(t, original, after, map[int64]map[string]bool{3310: {"score": true, "play_from": true, "play_url": true}})
	records, err := all(ctx, "SELECT api_id,score FROM sx_vod_source_score WHERE vod_id=3310 ORDER BY api_id")
	if err != nil || len(records) != 3 {
		t.Fatal("ratings from every source were not retained")
	}
	// An unverified legacy numeric value never receives a fabricated collector.
	hongguoAuditFixtureInsert(t, ctx, 3311, 9001, 901, "alias-score", title, "2026", "大陆", "hnm3u8", foreign, 0)
	if err := execSQL(ctx, "INSERT INTO sx_vod_alias(vod_id,canonical_id,reason,created,updatetime) VALUES(3310,3310,'fixture',0,0),(3311,3310,'fixture',0,0)"); err != nil {
		t.Fatal(err)
	}
	if err := saveCollectedVodScore(ctx, 3311, 901, other); err != nil {
		t.Fatal(err)
	}
	alias, _ := one(ctx, "SELECT * FROM sx_vod WHERE id=3311")
	if gconv.Float64(alias["score"]) != 9.9 || gconv.Int(alias["status"]) != 0 {
		t.Fatal("verified alias did not share true score while retaining hidden status")
	}
	other["vod_douban_score"] = ""
	if err := saveCollectedVodScore(context.Background(), 3310, 901, other); err != nil {
		t.Fatal(err)
	}
	alias, _ = one(ctx, "SELECT * FROM sx_vod WHERE id=3311")
	if err := hydrateVodSourceScores(ctx, []row{alias}); err != nil {
		t.Fatal(err)
	}
	if scoreLabel(alias) != "暂无评分" || scoreSource(alias) != "" {
		t.Fatal("no valid enabled rating displayed an invented number")
	}
	// The background discovery already owns a validated detail. Its rating
	// is independent of failed media and rejects a changed film identity.
	if err := execSQL(ctx, "INSERT INTO sx_source_discovery(vod_id,run_token) VALUES(3310,'score-fixture') ON DUPLICATE KEY UPDATE run_token=VALUES(run_token)"); err != nil {
		t.Fatal(err)
	}
	current, _ = one(ctx, "SELECT * FROM sx_vod WHERE id=3310")
	job := sourceDiscoveryJob{id: 3310, token: "score-fixture", identity: discoveryFilmIdentity(current)}
	collector := row{"id": 901, "status": 1}
	result := discoveryProviderResult{CollectorID: 901, Error: "media failed", RatingItem: row{"vod_id": "foreign-score", "vod_name": title, "vod_douban_score": "7.4"}}
	beforeDiscovery := hongguoAuditFixtureRows(t, ctx, "sx_vod")
	backup := filepath.Join(hongguoAuditFixtureDirectory(t), "score-before.private.json")
	if err := backupSourceScores(ctx, []row{current}, backup); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Films  []row `json:"films"`
		Scores []row `json:"source_scores"`
	}
	if json.Unmarshal(body, &saved) != nil || len(saved.Films) != 2 || len(saved.Scores) != 3 {
		t.Fatal("score apply backup omitted existing alias/source rating values")
	}
	if err := saveDiscoveredVodScore(ctx, job, collector, result); err != nil {
		t.Fatal(err)
	}
	current, _ = one(ctx, "SELECT * FROM sx_vod WHERE id=3310")
	if gconv.Float64(current["score"]) != 7.4 {
		t.Fatal("existing discovered detail did not persist true rating")
	}
	afterDiscovery := hongguoAuditFixtureRows(t, ctx, "sx_vod")
	hongguoAuditFixtureProtectedRows(t, beforeDiscovery, afterDiscovery, map[int64]map[string]bool{3310: {"score": true}, 3311: {"score": true}})
	if err := execSQL(ctx, "UPDATE sx_vod SET year='2025' WHERE id=3310"); err != nil {
		t.Fatal(err)
	}
	result.RatingItem["vod_douban_score"] = "9.8"
	if err := saveDiscoveredVodScore(ctx, job, collector, result); !errors.Is(err, errDiscoveryChanged) {
		t.Fatal("stale discovery rating overwrote changed film")
	}
	current, _ = one(ctx, "SELECT * FROM sx_vod WHERE id=3310")
	if gconv.Float64(current["score"]) != 7.4 {
		t.Fatal("stale identity rating changed cached value")
	}
}

func TestVodSourceScoreConcurrentIntegration(t *testing.T) {
	ctx := hongguoAuditFixtureContext(t)
	if err := execSQL(ctx, "INSERT INTO sx_collect_api(id,name,api_url,status,collect_auto,addtime) VALUES(902,'小柒隔离评分源','yqk://app',1,0,0)"); err != nil {
		t.Fatal(err)
	}
	for _, group := range []struct {
		id    int64
		title string
	}{{4501, "跨组并发评分第一部隔离验证"}, {4511, "跨组并发评分第二部隔离验证"}} {
		hongguoAuditFixtureInsert(t, ctx, group.id, 9001, 901, gconv.String(group.id), group.title, "2026", "大陆", "hnm3u8", "第1集$https://fixture.invalid/score.m3u8", 1)
		hongguoAuditFixtureInsert(t, ctx, group.id+1, 9001, 900, gconv.String(group.id+1), group.title, "2026", "大陆", "hongguo", "第1集$hongguo://400000001/410000001", 0)
		if err := execSQL(ctx, "INSERT INTO sx_vod_alias(vod_id,canonical_id,reason,created,updatetime) VALUES(?,?,'fixture',0,0),(?,?,'fixture',0,0)", group.id, group.id, group.id+1, group.id); err != nil {
			t.Fatal(err)
		}
	}
	before := hongguoAuditFixtureRows(t, ctx, "sx_vod")
	var workers sync.WaitGroup
	failures := make(chan error, 8)
	for worker := 0; worker < 8; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			for repeat := 0; repeat < 16; repeat++ {
				id := int64(4501)
				title := "跨组并发评分第一部隔离验证"
				if (worker+repeat)%2 == 1 {
					id = 4511
					title = "跨组并发评分第二部隔离验证"
				}
				api := int64(901)
				score := "9.9"
				if worker%2 == 0 {
					api = 902
					score = "8.9"
				}
				if worker%3 == 0 {
					id++
				}
				if err := saveCollectedVodScore(ctx, id, api, row{"vod_id": "source-score", "vod_name": title, "vod_douban_score": score}); err != nil {
					failures <- err
					return
				}
			}
		}(worker)
	}
	workers.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	after := hongguoAuditFixtureRows(t, ctx, "sx_vod")
	allowed := map[int64]map[string]bool{}
	for _, film := range after {
		id := gconv.Int64(film["id"])
		allowed[id] = map[string]bool{"score": true}
		if gconv.Float64(film["score"]) != 8.9 {
			t.Fatal("parallel updates lost preferred score or crossed film groups")
		}
	}
	hongguoAuditFixtureProtectedRows(t, before, after, allowed)
	rows, err := all(ctx, "SELECT vod_id,api_id FROM sx_vod_source_score ORDER BY vod_id,api_id")
	if err != nil || len(rows) != 4 {
		t.Fatal("parallel group score writes lost provenance or created alias duplicates")
	}
}
