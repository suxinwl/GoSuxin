package suxinvideo

import (
	"reflect"
	"testing"
)

func TestVodAliasVersionAndOwnerIsolation(t *testing.T) {
	base := row{"id": 1, "name": "同名短剧", "year": "2024", "__alias_canonical": 1, "__alias_kind": "short"}
	other := row{"id": 2, "name": "同名短剧！", "year": "2026", "__alias_canonical": 1, "__alias_kind": "short"}
	if vodAliasVersionKey(base) != vodAliasVersionKey(other) {
		t.Fatal("approved punctuation and short year variants cannot share source selection")
	}
	series := row{"id": 3, "name": "同名短剧", "__alias_canonical": 1, "__alias_kind": "series"}
	if vodAliasVersionKey(base) == vodAliasVersionKey(series) {
		t.Fatal("short drama and TV episodes became interchangeable")
	}
	oldMovie := row{"id": 31, "name": "同名电影", "year": "2013", "__alias_canonical": 1, "__alias_kind": "movie"}
	newMovie := row{"id": 32, "name": "同名电影", "year": "2025", "__alias_canonical": 1, "__alias_kind": "movie"}
	if vodAliasVersionKey(oldMovie) == vodAliasVersionKey(newMovie) {
		t.Fatal("explicit non-short remake years became interchangeable")
	}
	first := row{"id": 4, "name": "同名短剧第1季", "__alias_canonical": 1, "__alias_kind": "short"}
	firstChinese := row{"id": 5, "name": "同名短剧第一季", "__alias_canonical": 1, "__alias_kind": "short"}
	second := row{"id": 6, "name": "同名短剧第二季", "__alias_canonical": 1, "__alias_kind": "short"}
	if vodAliasVersionKey(first) != vodAliasVersionKey(firstChinese) || vodAliasVersionKey(first) == vodAliasVersionKey(second) || vodAliasVersionKey(base) == vodAliasVersionKey(second) {
		t.Fatal("explicit seasons and unspecified episodes are not isolated")
	}
	if vodAliasSeasonNumber("十二") != 12 || vodAliasSeasonNumber("二十一") != 21 {
		t.Fatal("Chinese season numbers were not parsed")
	}
	original := source{Code: "hongguo", Name: "红果", Episodes: []episode{{Name: "第01集", URL: "hongguo://100000001/110000001"}}}
	secondSource := source{Code: "hongguo", Name: "红果", Episodes: []episode{{Name: "第一话", URL: "hongguo://200000002/220000002"}}}
	before := []source{original, secondSource}
	composed := composeVodAliasSources(1, []row{base, second}, map[int64][]source{1: {original}, 6: {secondSource}})
	if len(composed) != 2 || composed[0].Code != "hongguo" || composed[1].Code != "alias_6_hongguo" || composed[1].OwnerVodID != 6 || composed[1].BaseCode != "hongguo" {
		t.Fatal("stable native owner and UI source references were lost")
	}
	if composed[0].VersionKey == composed[1].VersionKey || !reflect.DeepEqual(composed[1].Episodes, secondSource.Episodes) || vodAliasOwnerSource(composed[1]).Code != "hongguo" {
		t.Fatal("seasons were concatenated or raw labels changed")
	}
	if !reflect.DeepEqual(before, []source{original, secondSource}) {
		t.Fatal("view composition changed raw source records")
	}
}

func TestVodAliasMemberAccess(t *testing.T) {
	member := row{"id": 8, "status": 1, "vip": 0, "points": 0}
	if !vodAliasMemberAccessible(member, nil, nil, 100) {
		t.Fatal("free member is inaccessible")
	}
	member["vip"] = 1
	if vodAliasMemberAccessible(member, nil, nil, 100) || vodAliasMemberAccessible(member, row{"vip_expire": 100}, nil, 100) {
		t.Fatal("VIP member leaked to anonymous or expired user")
	}
	member["points"] = 12
	user := row{"vip_expire": 101}
	if vodAliasMemberAccessible(member, user, map[int64]bool{7: true}, 100) || !vodAliasMemberAccessible(member, user, map[int64]bool{8: true}, 100) {
		t.Fatal("purchase from another original ID bypassed access control")
	}
	member["status"] = 0
	if vodAliasMemberAccessible(member, user, map[int64]bool{8: true}, 100) {
		t.Fatal("disabled group member leaked resources")
	}
}

func TestVodAliasMixedNativeFormatEvidence(t *testing.T) {
	categories := map[int64]row{1: {"id": 1, "name": "重生民国", "pid": 0}, 2: {"id": 2, "name": "电视剧", "pid": 0}, 3: {"id": 3, "name": "电影", "pid": 0}}
	member := row{"type_id": 1, "play_from": "hongguo$$$hnm3u8", "play_url": "第1集$hongguo://100000001/110000001$$$全集$https://fixture.invalid/title.m3u8"}
	confirmed := map[string]bool{"100000001": true}
	if vodAliasMemberKind(member, categories, confirmed) != "short" {
		t.Fatal("valid native Hongguo with an unknown retained genre could not share approved short-drama versions")
	}
	member["type_id"] = 2
	if vodAliasMemberKind(member, categories, confirmed) != "series" {
		t.Fatal("explicit TV format was replaced by a native short-drama assumption")
	}
	member["type_id"] = 3
	if vodAliasMemberKind(member, categories, confirmed) != "movie" {
		t.Fatal("explicit movie format was replaced by a native short-drama assumption")
	}
	member["type_id"] = 1
	if vodAliasMemberKind(member, categories, map[string]bool{"200000002": true}) != "" {
		t.Fatal("another native season was accepted as proof for this unknown member")
	}
	member["class"] = "动漫"
	if vodAliasMemberKind(member, categories, confirmed) != "" {
		t.Fatal("explicit non-short metadata was overridden by a shared native series")
	}
	delete(member, "class")
	member["play_url"] = "第1集$hongguo://100000001/110000001#第2集$hongguo://200000002/220000001$$$全集$https://fixture.invalid/title.m3u8"
	if vodAliasMemberKind(member, categories, confirmed) != "" {
		t.Fatal("mixed remote series were accepted as independent format evidence")
	}
}
