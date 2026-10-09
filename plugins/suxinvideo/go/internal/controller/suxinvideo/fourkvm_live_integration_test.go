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

// Explicit live acceptance updates only the verified 2023 mainland anime
// already stored as film 781. It creates no source or sample movie, and backs
// up the full original row before changing its playlist.
func TestFourKVMLiveMerge(t *testing.T) {
	if os.Getenv("SUXIN_4KVM_MERGE") != "1" {
		t.Skip("set SUXIN_4KVM_MERGE=1 to persist verified 4KVM playback on existing film 781")
	}
	t.Setenv("SUXIN_INTEGRATION", "1")
	ctx, cancel := context.WithTimeout(yqkIntegrationContext(t), 2*time.Minute)
	defer cancel()
	before, err := one(ctx, "SELECT * FROM sx_vod WHERE id=781")
	if err != nil || before == nil {
		t.Fatalf("existing film unavailable: %v", err)
	}
	kind, err := discoveryCategoryKind(ctx, gconv.Int64(before["type_id"]))
	if err != nil || gconv.String(before["name"]) != "仙逆" || gconv.String(before["year"]) != "2023" || discoveryRegion(gconv.String(before["area"])) != "cn" || kind != "anime" {
		t.Fatal("film 781 is no longer the confirmed 2023 mainland anime; refusing mutation")
	}
	collector, err := one(ctx, "SELECT id,name,api_url,status FROM sx_collect_api WHERE api_url=? ORDER BY id LIMIT 1", fourKVMSourceURL)
	if err != nil || collector == nil || gconv.Int(collector["status"]) != 1 {
		t.Fatal("official 4KVM source is missing or disabled")
	}
	apiID := gconv.Int64(collector["id"])
	if setting(ctx, "collect_dedup_title", "1") != "1" {
		t.Fatal("same-film merging is disabled; refusing to insert a duplicate film")
	}
	target := discoveryTarget{ID: 781, Name: gconv.String(before["name"]), Year: gconv.String(before["year"]), Area: gconv.String(before["area"]), Kind: kind}
	verified := discoverCollector(ctx, target, collector)
	if len(verified.Sources) != 1 || verified.Sources[0].Code != "4kvm" {
		t.Fatalf("production search/detail/first and latest episode validation failed: %s", verified.Error)
	}
	if len(verified.Sources[0].Episodes) == 0 {
		t.Fatal("verified production line has no episodes")
	}
	remoteID, valid := fourKVMDiscoveryMarker(verified.Sources[0].Episodes[0].URL)
	if !valid {
		t.Fatal("verified production line has no stable film marker")
	}
	detail, err := fetchCollectSource(ctx, fourKVMSourceURL, url.Values{"ac": {"detail"}, "ids": {remoteID}})
	if err != nil || len(detail.List) != 1 {
		t.Fatalf("production detail unavailable: %v", err)
	}
	item := detail.List[0]
	if gconv.String(item["vod_id"]) != remoteID || !discoveryMatches(target, item, apiID) || len(verified.Sources[0].Episodes) < 150 {
		t.Fatal("production metadata/episodes do not prove the same film identity")
	}
	item["vod_play_from"], item["vod_play_url"] = serializeDiscoverySources(verified.Sources)
	for index, ep := range playlist(row{"play_from": item["vod_play_from"], "play_url": item["vod_play_url"]})[0].Episodes {
		slug, ok := fourKVMDiscoveryMarker(ep.URL)
		if !ok || slug != remoteID {
			t.Fatalf("production episode %d does not belong to %s: parsed slug=%s valid=%t", index, remoteID, slug, ok)
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
	backupPath := filepath.Join(backupDir, "4kvm-before-vod781-"+time.Now().UTC().Format("20060102T150405.000000000")+".json")
	backup, err := json.MarshalIndent(before, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
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
			t.Fatalf("production collection did not update existing film: state=%d error=%v; backup=%s", state, e, backupPath)
		}
		after, e := one(ctx, "SELECT * FROM sx_vod WHERE id=781")
		if e != nil {
			t.Fatal(e)
		}
		for key, value := range before {
			if key != "play_from" && key != "play_url" && gconv.String(value) != gconv.String(after[key]) {
				t.Fatalf("film metadata/access changed: %s; backup=%s", key, backupPath)
			}
		}
		byCode := map[string]source{}
		for _, src := range playlist(after) {
			byCode[src.Code] = src
		}
		for _, src := range playlist(before) {
			if src.Code != "4kvm" {
				oldFrom, oldURL := serializeDiscoverySources([]source{src})
				newFrom, newURL := serializeDiscoverySources([]source{byCode[src.Code]})
				if oldFrom != newFrom || oldURL != newURL {
					t.Fatal("an existing independent playback source changed")
				}
			}
		}
		if len(byCode["4kvm"].Episodes) != len(verified.Sources[0].Episodes) {
			t.Fatal("4KVM episode list was truncated or duplicated")
		}
		if round == 1 && (gconv.String(after["play_from"]) != gconv.String(previous["play_from"]) || gconv.String(after["play_url"]) != gconv.String(previous["play_url"])) {
			t.Fatal("repeat collection changed the same playlist")
		}
		previous = after
	}
	countAfter, err := one(ctx, "SELECT COUNT(*) AS count FROM sx_vod WHERE name=?", before["name"])
	if err != nil || gconv.Int(countBefore["count"]) != gconv.Int(countAfter["count"]) {
		t.Fatal("production verification inserted a new film")
	}
	t.Logf("merged %d verified 4KVM episodes from %s into existing film 781 twice; all other fields/sources preserved; backup=%s", len(verified.Sources[0].Episodes), remoteID, backupPath)
}
