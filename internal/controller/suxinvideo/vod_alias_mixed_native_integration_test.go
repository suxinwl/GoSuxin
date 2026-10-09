package suxinvideo

import (
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"testing"
)

func TestVodAliasMixedNativeIntegration(t *testing.T) {
	ctx := hongguoAuditFixtureContext(t)
	if err := execSQL(ctx, "INSERT INTO sx_type(id,pid,name,sort,status) VALUES(9004,0,'电视剧',0,1),(9006,0,'重生民国',0,1)"); err != nil {
		t.Fatal(err)
	}
	native := "第1集$hongguo://100000001/110000001"
	otherNative := "第1集$hongguo://200000002/220000001"
	title := "批准未知题材混合线路"
	hongguoAuditFixtureInsert(t, ctx, 1001, 9006, 900, "100000001", title, "2026", "", "hongguo$$$hnm3u8", native+"$$$全集$https://fixture.invalid/mixed.m3u8", 1)
	hongguoAuditFixtureInsert(t, ctx, 1002, 9001, 901, "foreign-a", title+"！", "2026", "", "hongguo", native, 1)
	hongguoAuditFixtureInsert(t, ctx, 1003, 9004, 901, "tv-a", title, "2013", "", "hongguo", native, 1)
	hongguoAuditFixtureInsert(t, ctx, 1004, 9006, 900, "200000002", title+"未知版本", "2026", "", "hongguo", otherNative, 1)
	if err := execSQL(ctx, "UPDATE sx_vod SET vip=0,points=0"); err != nil {
		t.Fatal(err)
	}
	if err := execSQL(ctx, "INSERT INTO sx_vod_alias(vod_id,canonical_id) VALUES(1001,1001),(1002,1001),(1003,1001),(1004,1001)"); err != nil {
		t.Fatal(err)
	}
	original := hongguoAuditFixtureRows(t, ctx, "sx_vod")
	members, err := vodAliasMembers(ctx, 1001)
	if err != nil {
		t.Fatal(err)
	}
	categories := map[int64]row{9001: {"id": 9001, "name": "短剧", "pid": 0}, 9004: {"id": 9004, "name": "电视剧", "pid": 0}, 9006: {"id": 9006, "name": "重生民国", "pid": 0}}
	confirmed := map[string]bool{"100000001": true}
	keys := map[int64]string{}
	for _, member := range members {
		member["__alias_kind"] = vodAliasMemberKind(member, categories, confirmed)
		keys[gconv.Int64(member["id"])] = vodAliasVersionKey(member)
	}
	if keys[1001] != keys[1002] || keys[1001] == keys[1003] || keys[1001] == keys[1004] {
		t.Fatal("orphan genre evidence crossed an explicit TV format or another native series")
	}
	view, err := hydratePlayers(ctx, original[0], playlist(original[0]))
	if err != nil {
		t.Fatal(err)
	}
	var primary, television, unknown string
	for _, src := range view {
		switch src.OwnerVodID {
		case 1001:
			primary = src.VersionKey
		case 1003:
			television = src.VersionKey
		case 1004:
			unknown = src.VersionKey
		}
	}
	if primary != keys[1001] || television != keys[1003] || unknown != keys[1004] {
		t.Fatal("real source hydration did not keep the matching short-drama version and separate native formats")
	}
	hongguoAuditFixtureProtectedRows(t, original, hongguoAuditFixtureRows(t, ctx, "sx_vod"), nil)
}
