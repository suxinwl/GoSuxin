package suxinvideo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

func yqkIntegrationContext(t *testing.T) context.Context {
	t.Helper()
	if os.Getenv("SUXIN_INTEGRATION") != "1" {
		t.Skip("set SUXIN_INTEGRATION=1 for isolated development database fixtures")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(filepath.Clean("../../..")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	return context.Background()
}

// Explicitly enabled live acceptance: keep only verified playlist additions on
// existing film 781. No other title is inserted and the full row is backed up.
func TestYQKLiveMerge(t *testing.T) {
	if os.Getenv("SUXIN_YQK_MERGE") != "1" {
		t.Skip("set SUXIN_YQK_MERGE=1 to persist verified YQK lines on existing film 781")
	}
	t.Setenv("SUXIN_INTEGRATION", "1")
	ctx := yqkIntegrationContext(t)
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	before, err := one(ctx, "SELECT * FROM sx_vod WHERE id=781")
	if err != nil || before == nil {
		t.Fatalf("existing film unavailable: %v", err)
	}
	kind, err := discoveryCategoryKind(ctx, gconv.Int64(before["type_id"]))
	if err != nil || gconv.String(before["name"]) != "仙逆" || gconv.String(before["year"]) != "2023" || discoveryRegion(gconv.String(before["area"])) != "cn" || kind != "anime" {
		t.Fatal("film 781 is no longer the confirmed 2023 mainland anime; refusing mutation")
	}
	if err = prepareYQKSource(ctx); err != nil {
		t.Fatal(err)
	}
	collector, err := one(ctx, "SELECT id,status FROM sx_collect_api WHERE api_url=? ORDER BY id LIMIT 1", yqkSourceURL)
	if err != nil || collector == nil || gconv.Int(collector["status"]) != 1 {
		t.Fatal("aggregate source is unavailable or explicitly disabled")
	}
	apiID := gconv.Int64(collector["id"])
	data, err := fetchYQK(ctx, url.Values{"ac": {"detail"}, "ids": {"129431"}})
	if err != nil || len(data.List) != 1 {
		t.Fatalf("live detail failed: %v", err)
	}
	item := data.List[0]
	target := discoveryTarget{Name: gconv.String(before["name"]), Year: gconv.String(before["year"]), Area: gconv.String(before["area"]), Kind: kind}
	if !discoveryMatches(target, item, apiID) || gconv.String(item["vod_id"]) != "129431" {
		t.Fatal("live detail does not prove the same film identity")
	}
	incoming := playlist(row{"play_from": item["vod_play_from"], "play_url": item["vod_play_url"]})
	if len(incoming) < 2 {
		t.Fatal("live detail did not return multiple independent sources")
	}
	for _, src := range incoming {
		if !yqkAllowedSource(src) {
			t.Fatal("live detail returned an unknown source/marker")
		}
	}
	chosen, err := findYQKCollectionTarget(ctx, apiID, item)
	if err != nil || chosen == nil || gconv.Int64(chosen["id"]) != 781 {
		t.Fatal("collection would target another film; refusing mutation")
	}
	backupDir := filepath.Join("data", "backups")
	if err = os.MkdirAll(backupDir, 0700); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(backupDir, "yqk-before-vod781-"+time.Now().UTC().Format("20060102T150405.000000000")+".json")
	backup, err := json.MarshalIndent(before, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(backupPath, backup, 0600); err != nil {
		t.Fatal(err)
	}
	countBefore, err := one(ctx, "SELECT COUNT(*) AS count FROM sx_vod")
	if err != nil {
		t.Fatal(err)
	}
	var previous row
	for round := 0; round < 2; round++ {
		state, e := upsertMacVod(ctx, apiID, item)
		if e != nil || state != 2 {
			t.Fatalf("live collection did not update the existing film: state=%d error=%v; backup=%s", state, e, backupPath)
		}
		after, e := one(ctx, "SELECT * FROM sx_vod WHERE id=781")
		if e != nil {
			t.Fatal(e)
		}
		for key, value := range before {
			if key != "play_from" && key != "play_url" && !reflect.DeepEqual(value, after[key]) {
				t.Fatalf("film metadata/access changed: %s; backup=%s", key, backupPath)
			}
		}
		byCode := map[string]source{}
		for _, src := range playlist(after) {
			byCode[src.Code] = src
			if yqkAllowedCode(src.Code) {
				seen := map[string]bool{}
				for _, ep := range src.Episodes {
					key := discoveryEpisodeIdentity(ep)
					if seen[key] {
						t.Fatal("same-episode entries were duplicated")
					}
					seen[key] = true
				}
			}
		}
		for _, src := range playlist(before) {
			if !yqkAllowedCode(src.Code) && !reflect.DeepEqual(src, byCode[src.Code]) {
				t.Fatal("original independent source changed")
			}
		}
		for _, src := range incoming {
			if len(byCode[src.Code].Episodes) != len(discoveryNormalizeSource(src).Episodes) {
				t.Fatalf("aggregate episodes truncated: %s", src.Code)
			}
		}
		// one() retains *gvar.Var values; compare their contents, not the
		// independent wrappers allocated by each database query.
		if round == 1 && (gconv.String(after["play_from"]) != gconv.String(previous["play_from"]) || gconv.String(after["play_url"]) != gconv.String(previous["play_url"])) {
			t.Fatal("second live collection changed an identical playlist")
		}
		previous = after
	}
	countAfter, _ := one(ctx, "SELECT COUNT(*) AS count FROM sx_vod")
	if gconv.Int(countBefore["count"]) != gconv.Int(countAfter["count"]) {
		t.Fatal("live fixture inserted a new film")
	}
	t.Logf("merged %d independent YQK lines into existing film 781 twice; metadata preserved; backup=%s", len(incoming), backupPath)
}

func TestYQKCollectionMergeAndUpgradeIntegration(t *testing.T) {
	ctx := yqkIntegrationContext(t)
	suffix := fmt.Sprint(time.Now().UnixNano())
	res, err := g.DB().Exec(ctx, "INSERT INTO sx_collect_api(name,api_url,status,collect_auto,collect_hours) VALUES(?,?,1,0,12)", "yqk-fixture-"+suffix, yqkSourceURL)
	if err != nil {
		t.Fatal(err)
	}
	apiID, _ := res.LastInsertId()
	defer execSQL(ctx, "DELETE FROM sx_collect_api WHERE id=?", apiID)
	// Even an already disabled record must not be enabled by a later upgrade.
	if err = execSQL(ctx, "UPDATE sx_collect_api SET status=0 WHERE id=?", apiID); err != nil {
		t.Fatal(err)
	}
	before, _ := one(ctx, "SELECT COUNT(*) AS count FROM sx_collect_api WHERE api_url=?", yqkSourceURL)
	for i := 0; i < 2; i++ {
		if err = prepareYQKSource(ctx); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := one(ctx, "SELECT COUNT(*) AS count FROM sx_collect_api WHERE api_url=?", yqkSourceURL)
	api, _ := one(ctx, "SELECT status FROM sx_collect_api WHERE id=?", apiID)
	if gconv.Int(before["count"]) != gconv.Int(after["count"]) || gconv.Int(api["status"]) != 0 {
		t.Fatal("repeat upgrade duplicated or re-enabled the aggregate source")
	}
	if err = execSQL(ctx, "UPDATE sx_collect_api SET status=1 WHERE id=?", apiID); err != nil {
		t.Fatal(err)
	}
	categoryName := "国产动漫" + suffix
	res, err = g.DB().Exec(ctx, "INSERT INTO sx_type(name,pid,status) VALUES(?,0,1)", categoryName)
	if err != nil {
		t.Fatal(err)
	}
	typeID, _ := res.LastInsertId()
	defer execSQL(ctx, "DELETE FROM sx_type WHERE id=?", typeID)
	name := "yqk-film-fixture-" + suffix
	defer execSQL(ctx, "DELETE FROM sx_vod WHERE name=? OR name=?", name, name+"-empty")
	var ids []int64
	defer func() {
		for _, id := range ids {
			_ = execSQL(ctx, "DELETE FROM sx_vod WHERE id=?", id)
		}
	}()
	oldCode := "standalone_" + strings.Repeat("x", 60)
	for _, year := range []string{"2024", "2023"} {
		res, err = g.DB().Exec(ctx, "INSERT INTO sx_vod(type_id,api_id,api_vid,name,name_norm,year,area,status,vip,points,pic,play_from,play_url,addtime,updatetime) VALUES(?,0,?,?,?,?,'中国大陆',1,1,99,'/fixture.jpg',?,'第1集$https://fixture.example/old.m3u8',1,2)", typeID, year+suffix, name, normalizeVodName(name), year, oldCode)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		ids = append(ids, id)
	}
	var sources []source
	for _, code := range yqkPlaybackCodes() {
		sources = append(sources, yqkFixtureSource(strings.TrimPrefix(code, "yqk_")))
	}
	from, urls := serializeDiscoverySources(sources)
	item := map[string]any{"__yqk": true, "vod_id": "12", "vod_name": name, "vod_year": "2023", "vod_area": "大陆", "type_name": categoryName, "vod_play_from": from, "vod_play_url": urls}
	for i := 0; i < 2; i++ {
		state, e := upsertMacVod(ctx, apiID, item)
		if e != nil || state != 2 {
			t.Fatalf("same film was not merged idempotently: state=%d error=%v", state, e)
		}
	}
	vod, err := one(ctx, "SELECT * FROM sx_vod WHERE id=?", ids[1])
	if err != nil {
		t.Fatal(err)
	}
	merged := playlist(vod)
	if len(merged) != 19 || len(gconv.String(vod["play_from"])) <= 200 || merged[0].Code != oldCode || gconv.Int(vod["vip"]) != 1 || gconv.Int(vod["points"]) != 99 || gconv.String(vod["pic"]) != "/fixture.jpg" {
		t.Fatalf("aggregate lines truncated/duplicated or old data overwritten: lines=%d", len(merged))
	}
	remake, _ := one(ctx, "SELECT play_from FROM sx_vod WHERE id=?", ids[0])
	if gconv.String(remake["play_from"]) != oldCode {
		t.Fatal("same-name different-year remake was merged")
	}
	empty := map[string]any{"__yqk": true, "vod_id": "13", "vod_name": name + "-empty"}
	if _, err = upsertMacVod(ctx, apiID, empty); err == nil {
		t.Fatal("missing detail response inserted an empty film")
	}
	if err = execSQL(ctx, "UPDATE sx_collect_api SET status=0 WHERE id=?", apiID); err != nil {
		t.Fatal(err)
	}
	if _, err = upsertMacVod(ctx, apiID, item); err == nil {
		t.Fatal("disabled collector was allowed to update a film")
	}
}

func TestYQKFailedDetailJobIntegration(t *testing.T) {
	ctx := yqkIntegrationContext(t)
	if err := prepareCollectJobs(ctx); err != nil {
		t.Fatal(err)
	}
	active, err := one(ctx, "SELECT id FROM sx_collect_job WHERE status IN ('queued','running') LIMIT 1")
	if err != nil {
		t.Fatal(err)
	}
	if active != nil {
		t.Skip("another collection job is active")
	}
	res, err := g.DB().Exec(ctx, "INSERT INTO sx_collect_api(name,api_url,status,collect_auto,collect_hours) VALUES('yqk-detail-fixture',?,1,0,12)", yqkSourceURL)
	if err != nil {
		t.Fatal(err)
	}
	apiID, _ := res.LastInsertId()
	defer execSQL(ctx, "DELETE FROM sx_collect_api WHERE id=?", apiID)
	oldFetch, oldSave, oldPause := fetchCollectJobPage, saveCollectJobVod, pauseCollectJob
	defer func() { fetchCollectJobPage, saveCollectJobVod, pauseCollectJob = oldFetch, oldSave, oldPause }()
	fetchCollectJobPage = func(_ context.Context, _ string, params url.Values) (macPayload, error) {
		if params.Get("ac") == "detail" {
			return macPayload{}, errors.New("fixture detail unavailable")
		}
		return macPayload{Code: 1, Page: 1, PageCount: 1, List: []map[string]any{{"__yqk": true, "vod_id": "12", "vod_name": "fixture"}}}, nil
	}
	saves := 0
	saveCollectJobVod = func(context.Context, int64, map[string]any) (int, error) { saves++; return 1, nil }
	pauseCollectJob = func(context.Context) bool { return true }
	job, err := startCollectJob(ctx, "manual", apiID, 1, 0, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	id := gconv.Int64(job["id"])
	defer execSQL(ctx, "DELETE FROM sx_collect_job WHERE id=?", id)
	finished := waitCollectJob(t, ctx, id)
	if saves != 0 || gconv.Int(finished["failed"]) != 1 || gconv.Int(finished["added"]) != 0 || !strings.Contains(gconv.String(finished["error"]), "详情") {
		t.Fatalf("failed detail was reported as a saved film: saves=%d state=%v", saves, finished["status"])
	}
}
