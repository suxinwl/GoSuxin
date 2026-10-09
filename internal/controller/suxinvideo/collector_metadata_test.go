package suxinvideo

import "testing"

func TestCollectedDetailMetadata(t *testing.T) {
	list := map[string]any{"vod_id": "12", "vod_name": "示例", "type_name": "电影", "vod_pic": "poster.jpg", "__yqk": true}
	detail := map[string]any{"vod_name": "示例详情", "type_name": "伦理片", "vod_year": "2026", "vod_pic": "", "vod_play_url": "第一集$https://example.com/1.m3u8"}
	item := collectedDetailItem(list, detail)
	if item["type_name"] != "伦理片" || item["vod_year"] != "2026" || item["vod_name"] != "示例详情" {
		t.Fatalf("detail category/identity metadata lost: %v", item)
	}
	if item["vod_id"] != "12" || item["vod_pic"] != "poster.jpg" || item["__yqk"] != true {
		t.Fatalf("missing detail fields erased list metadata: %v", item)
	}
	if list["type_name"] != "电影" || detail["vod_pic"] != "" {
		t.Fatal("merging changed the provider payload")
	}
}
