package suxinvideo

import "testing"

func TestLiveChineseDisplayAndStableBackupLabels(t *testing.T) {
	for id, expected := range map[string]string{
		"Jade.hk@HD": "翡翠台", "PhoenixChineseChannel.hk": "凤凰中文台",
		"PhoenixInfoNewsChannel.hk": "凤凰资讯台", "PhoenixHongKongChannel.hk": "凤凰香港台",
		"BeijingSatelliteTV.cn": "北京卫视", "CCTV1.cn": "CCTV-1 综合",
		"EBCNews.tw": "东森新闻台", "LotusTV.mo": "澳门莲花卫视",
		"TVBPlus.hk": "TVB Plus（82台）",
	} {
		if got := liveDisplayName(id, "Original English"); got != expected {
			t.Fatalf("%s mapped to %q", id, got)
		}
	}
	if liveDisplayName("unknown.example", "Custom Channel") != "Custom Channel" {
		t.Fatal("unknown station received an invented official name")
	}
	for _, group := range []string{"News", "Movies;Series", "Education;Lifestyle;Science"} {
		translated := liveGroupDisplayName(group)
		if translated == group || liveGroupIdentity(group) != liveGroupIdentity(translated) {
			t.Fatal("translated category changed identity", group)
		}
	}
	items, _, err := liveParseImport("#EXTM3U\n#EXTINF:-1 tvg-id=\"Jade.hk\" group-title=\"News\",Jade (720p)\nhttps://example.com/live.m3u8\n", "")
	if err != nil || len(items) != 1 || items[0].Name != "翡翠台" || items[0].Quality != "720p" || items[0].Group != "新闻" {
		t.Fatal("localization lost upstream resolution or group", err)
	}
	first := row{"name": "Same English Name", "line_no": 1, "quality": "720p", "manual_edited": 0, "health": "healthy"}
	second := row{"name": "Same English Name", "line_no": 2, "quality": "1080p", "manual_edited": 0, "health": "unavailable"}
	if liveStreamDisplayName(first) != "线路1 · 720p" || liveStreamDisplayName(second) != "备用2 · 1080p" {
		t.Fatal("backup display names are not distinct")
	}
	second["health"] = "healthy"
	second["priority"] = 1000
	if liveStreamDisplayName(second) != "备用2 · 1080p" {
		t.Fatal("health/priority changed the stable backup number")
	}
	second["manual_edited"] = 1
	second["name"] = "管理员专用线路"
	if liveStreamDisplayName(second) != "管理员专用线路" {
		t.Fatal("manual source name was overridden")
	}
}
