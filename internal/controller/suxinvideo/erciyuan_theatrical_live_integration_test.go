package suxinvideo

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

// Opt-in live inspection is read-only by default. Explicit merge uses the
// normal locked collection update on one already identified local film; there
// is no insert path and no local film/category/user record is removed.
func TestErciyuanTheatricalLiveInspect(t *testing.T) {
	if os.Getenv("SUXIN_ERCIYUAN_THEATRICAL_INSPECT") != "1" {
		t.Skip("set SUXIN_ERCIYUAN_THEATRICAL_INSPECT=1 for native theatrical inspection")
	}
	t.Setenv("SUXIN_INTEGRATION", "1")
	ctx, cancel := context.WithTimeout(yqkIntegrationContext(t), 3*time.Minute)
	defer cancel()
	before, err := one(ctx, "SELECT * FROM sx_vod WHERE id=2164")
	if err != nil || before == nil {
		t.Fatal("reviewed existing film is unavailable")
	}
	kind, err := discoveryCategoryKind(ctx, gconv.Int64(before["type_id"]))
	if err != nil || discoveryNormalizeTitle(gconv.String(before["name"])) != "仙逆剧场版弑仙之战" || kind != "anime_movie" {
		t.Fatal("existing film no longer has the reviewed distinct theatrical identity")
	}
	collector, err := one(ctx, "SELECT * FROM sx_collect_api WHERE api_url=? ORDER BY id LIMIT 1", erciyuanSourceURL)
	if err != nil || collector == nil || gconv.Int(collector["status"]) != 1 {
		t.Fatal("native 二次元 source is unavailable or disabled")
	}
	apiID := gconv.Int64(collector["id"])
	target := discoveryTarget{ID: 2164, Name: gconv.String(before["name"]), Year: gconv.String(before["year"]), Area: gconv.String(before["area"]), Kind: kind, EpisodeKey: "movie:feature"}
	detail, err := fetchCollectSource(ctx, erciyuanSourceURL, url.Values{"ac": {"detail"}, "ids": {"64933"}})
	if err != nil || len(detail.List) != 1 || !discoveryErciyuanTheatricalMatch(target, detail.List[0]) {
		t.Fatal("fresh upstream detail does not independently identify the same theatrical film")
	}
	chosen, err := findNativeCollectionTarget(ctx, apiID, detail.List[0])
	if err != nil || chosen == nil || gconv.Int64(chosen["id"]) != 2164 {
		t.Fatalf("normal collection matching did not select exactly existing film2164: %v", err)
	}
	result := discoverCollector(ctx, target, collector)
	if len(result.Sources) != 1 || result.Sources[0].Code != "ecy_dd02" || len(result.Sources[0].Episodes) != 1 {
		t.Fatalf("fresh normal discovery did not yield the verified full feature: lines=%d error=%s", len(result.Sources), result.Error)
	}
	t.Log("READ-ONLY: native64933 exact theatrical title/category/synopsis accepted; normal matcher selects2164; ecy_dd02 complete feature media probe passed")
	if os.Getenv("SUXIN_ERCIYUAN_THEATRICAL_MERGE") != "1" {
		return
	}
	from, play := serializeDiscoverySources(result.Sources)
	release, err := collectionWrites.acquire(ctx, collectionVodWriteKeys(apiID, "64933", target.Name)...)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	fresh, err := one(ctx, "SELECT * FROM sx_vod WHERE id=2164")
	if err != nil || fresh == nil || discoveryFilmIdentity(fresh) != gconv.String(chosen["__yqk_identity"]) {
		t.Fatal("local film identity changed during inspection; refusing mutation")
	}
	backupDir := filepath.Join("data", "tmp", "ecy-theatrical-evidence")
	if err = os.MkdirAll(backupDir, 0700); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(backupDir, "before2164-"+time.Now().UTC().Format("20060102T150405.000000000")+".private.json")
	if err = hongguoAuditWriteJSON(backupPath, fresh, true); err != nil {
		t.Fatal("durable backup failed; refusing mutation")
	}
	chosen["__collection_source"] = "erciyuan"
	chosen["__collection_preserve_metadata"] = true
	var previousPlaylist row
	for round := 0; round < 2; round++ {
		if err = updateCollectedVod(ctx, chosen, apiID, "64933", from, play, "", ""); err != nil {
			t.Fatal(err)
		}
		after, e := one(ctx, "SELECT * FROM sx_vod WHERE id=2164")
		if e != nil || after == nil {
			t.Fatal("merged film disappeared")
		}
		for key, value := range fresh {
			if key != "play_from" && key != "play_url" && gconv.String(after[key]) != gconv.String(value) {
				t.Fatalf("existing metadata changed: %s", key)
			}
		}
		oldSources := playlist(fresh)
		newSources := playlist(after)
		byCode := map[string]source{}
		for _, src := range newSources {
			byCode[src.Code] = src
		}
		for _, src := range oldSources {
			if src.Code != "ecy_dd02" && !reflect.DeepEqual(src, byCode[src.Code]) {
				t.Fatalf("independent existing source changed: %s", src.Code)
			}
		}
		if len(byCode["ecy_dd02"].Episodes) != 1 {
			t.Fatal("complete native feature was not retained")
		}
		if round == 1 && (gconv.String(after["play_from"]) != gconv.String(previousPlaylist["play_from"]) || gconv.String(after["play_url"]) != gconv.String(previousPlaylist["play_url"])) {
			t.Fatal("repeated native merge was not idempotent")
		}
		previousPlaylist = after
	}
	t.Logf("MERGED: only verifiedecy_dd02 into existing2164 using locked normal update; metadata/independent lines preserved; durable backup=%s", backupPath)
}
