package suxinvideo

import (
	"testing"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

func TestHongguoSubcategoryCollectionIntegration(t *testing.T) {
	ctx := hongguoAuditFixtureContext(t)
	if err := execSQL(ctx, "INSERT INTO sx_type(id,pid,name,sort,status) VALUES(9004,9001,'现代都市',0,1)"); err != nil {
		t.Fatal(err)
	}
	name := "这个神医会功夫：下山专治不服"
	hongguoAuditFixtureInsert(t, ctx, 9501, 9001, 900, "100000001", name, "", "", "hongguo", "第1集$hongguo://100000001/110000001#第2集$hongguo://100000001/110000002", 1)
	before := hongguoAuditFixtureRows(t, ctx, "sx_vod")
	item := ordinaryCollectionUnknownGenreItem()
	item["vod_id"], item["vod_name"] = "wj-verified", name
	item["vod_play_url"] = "第01集$https://fixture.invalid/wj-first.m3u8#第02集$https://fixture.invalid/wj-second.m3u8"
	for round := 0; round < 2; round++ {
		state, err := upsertMacVod(ctx, 901, item)
		if err != nil || state != 2 {
			t.Fatalf("verified modern-urban short subcategory did not merge existing HG record: state=%d err=%v", state, err)
		}
		current := hongguoAuditFixtureRows(t, ctx, "sx_vod")
		if len(current) != 1 || gconv.Int64(current[0]["id"]) != 9501 || gconv.Int64(current[0]["type_id"]) != 9001 {
			t.Fatal("subcategory collection inserted a duplicate or changed original ID/category")
		}
		allowed := map[int64]map[string]bool{9501: {"play_from": true, "play_url": true}}
		hongguoAuditFixtureProtectedRows(t, before, current, allowed)
		sources := playlist(current[0])
		if len(sources) != 2 || sources[0].Code != "hongguo" || sources[1].Code != "wjm3u8" || len(sources[0].Episodes) != 2 || len(sources[1].Episodes) != 2 || sources[0].Episodes[0].URL != "hongguo://100000001/110000001" {
			t.Fatal("native line lost or HTTP episodes duplicated/truncated")
		}
		if item["type_name"] != "现代都市" || item["vod_class"] != "现代都市" {
			t.Fatal("matching replaced original provider classification metadata")
		}
	}
	// A second same-name category under movies makes this genre ambiguous.
	// The local tree may not be used as proof that every incoming item is short.
	if err := execSQL(ctx, "INSERT INTO sx_type(id,pid,name,sort,status) VALUES(9005,9003,'现代都市',0,1)"); err != nil {
		t.Fatal(err)
	}
	native, err := one(ctx, "SELECT * FROM sx_vod WHERE id=9501")
	if err != nil {
		t.Fatal(err)
	}
	other := ordinaryCollectionUnknownGenreItem()
	other["vod_id"] = "another-provider-record"
	if compatible, err := ordinaryCollectionCompatible(ctx, native, 902, other); err != nil || compatible {
		t.Fatal("same-name genres belonging to movie and short trees permitted an automatic merge")
	}
}
