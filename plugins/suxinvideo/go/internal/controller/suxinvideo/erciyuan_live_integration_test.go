package suxinvideo

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

// Opt-in acceptance against the configured development database. Preserve the
// existing film and every independent line; only append/update verified native
// 二次元 markers, and save the original row outside the distributable package.
func TestErciyuanLiveMerge(t *testing.T) {
	if os.Getenv("SUXIN_ERCIYUAN_MERGE") != "1" {
		t.Skip("set SUXIN_ERCIYUAN_MERGE=1 for reviewed live merge into existing film 781")
	}
	t.Setenv("SUXIN_INTEGRATION", "1")
	ctx, cancel := context.WithTimeout(yqkIntegrationContext(t), 4*time.Minute)
	defer cancel()
	before, err := one(ctx, "SELECT * FROM sx_vod WHERE id=781")
	if err != nil || before == nil {
		t.Fatalf("existing film unavailable: %v", err)
	}
	kind, err := discoveryCategoryKind(ctx, gconv.Int64(before["type_id"]))
	if err != nil || gconv.String(before["name"]) != "仙逆" || gconv.String(before["year"]) != "2023" || discoveryRegion(gconv.String(before["area"])) != "cn" || kind != "anime" {
		t.Fatal("existing film 781 no longer has the verified identity; refusing mutation")
	}
	if err = prepareErciyuanSource(ctx); err != nil {
		t.Fatal(err)
	}
	collector, err := one(ctx, "SELECT * FROM sx_collect_api WHERE api_url=? ORDER BY id LIMIT 1", erciyuanSourceURL)
	if err != nil || collector == nil || gconv.Int(collector["status"]) != 1 {
		t.Fatal("二次元 source is missing or disabled")
	}
	apiID := gconv.Int64(collector["id"])
	if setting(ctx, "collect_dedup_title", "1") != "1" {
		t.Fatal("same-film merging is disabled; refusing duplicate insertion")
	}
	classes, err := fetchCollectSource(ctx, erciyuanSourceURL, url.Values{"ac": {"list"}})
	if err != nil || len(classes.Class) != 6 {
		t.Fatalf("native categories failed: %v", err)
	}
	var listed map[string]any
	for page := 1; page <= 8; page++ {
		payload, e := fetchCollectSource(ctx, erciyuanSourceURL, url.Values{"ac": {"videolist"}, "t": {"2"}, "pg": {gconv.String(page)}})
		if e != nil {
			t.Fatalf("real category page %d failed: %v", page, e)
		}
		for _, item := range payload.List {
			if gconv.String(item["vod_id"]) == "35604" {
				listed = item
				break
			}
		}
		if listed != nil {
			t.Logf("verified 仙逆 list metadata on 国漫 page %d", page)
			break
		}
		if payload.PageCount <= page {
			break
		}
	}
	if listed == nil {
		t.Fatal("reviewed film not in bounded recent list pages; refusing metadata inference")
	}
	detail, err := fetchCollectSource(ctx, erciyuanSourceURL, url.Values{"ac": {"detail"}, "ids": {"35604"}})
	if err != nil || len(detail.List) != 1 {
		t.Fatalf("native detail unavailable: %v", err)
	}
	item := collectedDetailItem(listed, detail.List[0])
	target := discoveryTarget{ID: 781, Name: gconv.String(before["name"]), Year: gconv.String(before["year"]), Area: gconv.String(before["area"]), Kind: kind}
	if gconv.String(item["vod_id"]) != "35604" || !discoveryMatches(target, item, apiID) {
		t.Fatal("native list/detail do not prove the same film identity")
	}
	incoming := playlist(row{"play_from": item["vod_play_from"], "play_url": item["vod_play_url"]})
	if len(incoming) < 2 {
		t.Fatal("native detail returned insufficient independent playback lines")
	}
	for _, src := range incoming {
		if !erciyuanAllowedSource(src) {
			t.Fatal("native detail returned mismatched source markers")
		}
	}
	chosen, err := findNativeCollectionTarget(ctx, apiID, item)
	if err != nil || chosen == nil || gconv.Int64(chosen["id"]) != 781 {
		t.Fatal("production matching would target another film; refusing mutation")
	}
	backupDir := filepath.Join("data", "backups")
	if err = os.MkdirAll(backupDir, 0700); err != nil {
		t.Fatal(err)
	}
	backup, err := json.MarshalIndent(before, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(backupDir, "erciyuan-before-vod781-"+time.Now().UTC().Format("20060102T150405.000000000")+".json")
	if err = os.WriteFile(backupPath, backup, 0600); err != nil {
		t.Fatal(err)
	}
	countBefore, err := one(ctx, "SELECT COUNT(*) AS count FROM sx_vod WHERE name=?", before["name"])
	if err != nil {
		t.Fatal(err)
	}
	var previous row
	for round := 0; round < 2; round++ {
		state, e := upsertMacVod(ctx, apiID, item)
		if e != nil || state != 2 {
			t.Fatalf("native upsert did not update existing film: state=%d error=%v; backup=%s", state, e, backupPath)
		}
		after, e := one(ctx, "SELECT * FROM sx_vod WHERE id=781")
		if e != nil {
			t.Fatal(e)
		}
		for key, value := range before {
			if key != "play_from" && key != "play_url" && gconv.String(value) != gconv.String(after[key]) {
				t.Fatalf("existing film metadata/access changed: %s; backup=%s", key, backupPath)
			}
		}
		byCode := map[string]source{}
		for _, src := range playlist(after) {
			byCode[src.Code] = src
		}
		for _, src := range playlist(before) {
			if !erciyuanAllowedSource(src) {
				oldFrom, oldURL := serializeDiscoverySources([]source{src})
				newFrom, newURL := serializeDiscoverySources([]source{byCode[src.Code]})
				if oldFrom != newFrom || oldURL != newURL {
					t.Fatal("an independent existing source changed")
				}
			}
		}
		for _, src := range incoming {
			if len(byCode[src.Code].Episodes) != len(discoveryNormalizeSource(src).Episodes) {
				t.Fatalf("native episodes truncated or duplicated: %s", src.Code)
			}
		}
		if round == 1 && (gconv.String(after["play_from"]) != gconv.String(previous["play_from"]) || gconv.String(after["play_url"]) != gconv.String(previous["play_url"])) {
			t.Fatal("repeat collection changed the same playlist")
		}
		previous = after
	}
	countAfter, err := one(ctx, "SELECT COUNT(*) AS count FROM sx_vod WHERE name=?", before["name"])
	if err != nil || gconv.Int(countBefore["count"]) != gconv.Int(countAfter["count"]) {
		t.Fatal("live merge inserted a duplicate film")
	}
	t.Logf("merged %d 二次元 lines twice into existing film 781; all independent sources/metadata retained; backup=%s", len(incoming), backupPath)
}
