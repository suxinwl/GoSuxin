package suxinvideo

import (
	"testing"
)

func TestCollectedSourceScoreRejectsMissingAndInventedScales(t *testing.T) {
	for _, raw := range []any{nil, "", 0, "0.0", "NaN", "Inf", "20", "4/5", "约8分", "热度8.8", "暂无评分", -1} {
		if value, _ := collectedSourceScore(row{"vod_douban_score": raw}); value != 0 {
			t.Fatalf("unrated/unsupported scale accepted: %v", raw)
		}
	}
	for _, raw := range []any{"8.7", 8.7, " 8.7分 "} {
		if value, field := collectedSourceScore(row{"vod_douban_score": raw}); value != 8.7 || field != "vod_douban_score" {
			t.Fatalf("true ten-point score rejected: %v", raw)
		}
	}
	if scoreLabel(row{"score": "0.0"}) != "暂无评分" || scoreSource(row{"score": "0.0"}) != "" {
		t.Fatal("zero score is shown as a numeric rating")
	}
	if scoreSource(row{"score": 8.1}) != "已有评分" {
		t.Fatal("legacy value falsely attributed to a named upstream")
	}
}
func TestVodSourceScorePrioritizesRealEnabledSources(t *testing.T) {
	scores := []row{{"score": 9.9, "api_url": "https://fixture.invalid", "name": "辅助", "status": 1, "updated": 100}, {"score": 8.2, "api_url": hongguoSourceURL, "status": 1, "updated": 90}, {"score": 8.7, "api_url": yqkSourceURL, "status": 1, "updated": 80}}
	if value := chooseVodSourceScore(scores); sourceScoreName(value) != "小柒" {
		t.Fatal("auxiliary higher score replaced the preferred true YQK rating")
	}
	scores[2]["score"] = 0.0
	if value := chooseVodSourceScore(scores); sourceScoreName(value) != "红果" {
		t.Fatal("unrated YQK did not fall back to Hongguo")
	}
	scores[1]["status"] = 0
	if value := chooseVodSourceScore(scores); sourceScoreName(value) != "辅助" {
		t.Fatal("disabled Hongguo rating did not fall back to an enabled source")
	}
}
