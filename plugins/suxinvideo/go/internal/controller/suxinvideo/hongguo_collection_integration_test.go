package suxinvideo

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

// Requires a disposable schema configured outside the running CMS. The opt-in
// gate is separate from tests that intentionally use development business data.
func TestHongguoConcurrentImportsKeepSingleStableFilmAndAllLines(t *testing.T) {
	if os.Getenv("SUXIN_HONGGUO_FIXTURE_INTEGRATION") != "1" {
		t.Skip("set SUXIN_HONGGUO_FIXTURE_INTEGRATION=1 with a disposable fixture database")
	}
	t.Setenv("SUXIN_INTEGRATION", "1")
	ctx, cancel := context.WithTimeout(yqkIntegrationContext(t), 30*time.Second)
	defer cancel()
	if setting(ctx, "collect_dedup_title", "1") != "1" {
		t.Fatal("fixture must enable same-film deduplication")
	}
	stamp := time.Now().UnixNano()
	name := fmt.Sprintf("红果短剧并发隔离验证%d", stamp)
	typeName := fmt.Sprintf("短剧隔离验证%d", stamp)
	typeResult, err := g.DB().Exec(ctx, "INSERT INTO sx_type(pid,name,sort,status) VALUES(0,?,50,1)", typeName)
	if err != nil {
		t.Fatal(err)
	}
	typeID, _ := typeResult.LastInsertId()
	defer execSQL(context.Background(), "DELETE FROM sx_type WHERE id=?", typeID)
	collectorResult, err := g.DB().Exec(ctx, "INSERT INTO sx_collect_api(name,api_url,status,collect_auto,addtime) VALUES(?,?,1,0,0)", name, hongguoSourceURL)
	if err != nil {
		t.Fatal(err)
	}
	apiID, _ := collectorResult.LastInsertId()
	defer execSQL(context.Background(), "DELETE FROM sx_collect_api WHERE id=?", apiID)
	filmResult, err := g.DB().Exec(ctx, "INSERT INTO sx_vod(type_id,api_id,api_vid,name,name_norm,year,area,pic,remarks,play_from,play_url,vip,points,status,addtime,updatetime) VALUES(?,2,'original',?,?,'2026','大陆','keep.jpg','keep','hnm3u8',?,1,9,1,1,2)",
		typeID, name, normalizeVodName(name), "第01集$https://example.test/independent.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	id, _ := filmResult.LastInsertId()
	defer execSQL(context.Background(), "DELETE FROM sx_vod WHERE name=?", name)
	start, results := make(chan struct{}), make(chan error, 8)
	for index := 0; index < 8; index++ {
		go func(index int) {
			<-start
			if index%2 == 0 {
				item := hongguoMatchingFixtureItem()
				item["vod_name"] = name
				state, err := upsertMacVod(ctx, apiID, item)
				if err == nil && state != 2 {
					err = fmt.Errorf("native update returned insert state %d", state)
				}
				results <- err
			} else {
				item := map[string]any{"vod_id": "parallel", "vod_name": name, "type_name": typeName, "vod_year": "2026", "vod_area": "中国大陆",
					"vod_play_from": "fixture-other", "vod_play_url": "第1集$https://example.test/second.m3u8"}
				state, err := upsertMacVod(ctx, apiID+10000, item)
				if err == nil && state != 2 {
					err = fmt.Errorf("foreign update returned insert state %d", state)
				}
				results <- err
			}
		}(index)
	}
	close(start)
	for index := 0; index < 8; index++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	films, err := all(ctx, "SELECT * FROM sx_vod WHERE name=?", name)
	if err != nil || len(films) != 1 || gconv.Int64(films[0]["id"]) != id {
		t.Fatal("concurrent native/foreign imports created another film or changed its ID")
	}
	film := films[0]
	if len(playlist(film)) != 3 || !strings.Contains(gconv.String(film["play_from"]), "hongguo") || gconv.Int64(film["type_id"]) != typeID || gconv.String(film["year"]) != "2026" || gconv.String(film["area"]) != "大陆" || gconv.String(film["pic"]) != "keep.jpg" || gconv.Int(film["vip"]) != 1 || gconv.Int(film["points"]) != 9 {
		t.Fatalf("merged native episodes lost independent resources, category, metadata or access: parsed_lines=%d sources=%s type_id=%v expected_type=%d year=%v(%T) area=%v(%T) pic=%v(%T) vip=%v points=%v", len(playlist(film)), film["play_from"], film["type_id"], typeID, film["year"], film["year"], film["area"], film["area"], film["pic"], film["pic"], film["vip"], film["points"])
	}
}
