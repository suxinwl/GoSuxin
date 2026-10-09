package suxinvideo

import "testing"

func TestLegacyDoubanHostsAndUnknownSharedPlayerName(t *testing.T) {
	collectors := []row{
		{"id": 1, "api_url": "https://jszyapi.com/api.php", "status": 1},
		{"id": 4, "api_url": "https://caiji.dbzy5.com/api.php", "status": 0},
		{"id": 5, "api_url": "https://api.apibdzy.com/api.php", "status": 1},
	}
	players := map[string]row{"dbm3u8": {"name": "豆瓣m3u8", "status": 1}}
	for _, host := range []string{"vodcnd17.ajupf.com", "185.92.188.176:2100", "vodcnd17.uvjtih.cn"} {
		src := []source{{Code: "dbm3u8", Episodes: []episode{{Name: "530", URL: "http://" + host + "/media/index.m3u8"}}}}
		if got := availableSources(row{"api_id": 1}, src, players, collectors); len(got) != 0 {
			t.Fatalf("disabled legacy Douban CDN inherited the merged film's primary owner: %s", host)
		}
	}
	unknown := []source{{Code: "dbm3u8", Episodes: []episode{{Name: "1", URL: "https://unknown.example/index.m3u8"}}}}
	got := availableSources(row{"api_id": 1}, unknown, players, collectors)
	if len(got) != 1 || got[0].Name != "其它资源" {
		t.Fatalf("unknown shared CDN inherited a disabled player's display name: %+v", got)
	}
	baidu := []source{{Code: "dbm3u8", Episodes: []episode{{Name: "1", URL: "https://b3.bdzybf22.com/index.m3u8"}}}}
	got = availableSources(row{"api_id": 4}, baidu, players, collectors)
	if len(got) != 1 || got[0].Name != "百度资源" {
		t.Fatalf("legacy Douban attribution removed an enabled Baidu episode: %+v", got)
	}
	for _, host := range []string{"185.92.188.177", "vodcnd18.uvjtih.cn", "ajupf.com.evil.example"} {
		if legacyDoubanMediaHost(host) {
			t.Fatalf("legacy mapping guessed an unconfirmed host: %s", host)
		}
	}
}

func TestMergedSourcesUseTheirOwnCollectors(t *testing.T) {
	collectors := []row{
		{"id": 4, "api_url": "https://caiji.dbzy5.com", "status": 0},
		{"id": 9, "api_url": "hongguo://app", "status": 1},
		{"id": 10, "api_url": "4kvm://site", "status": 1},
		{"id": 16, "api_url": "https://api.xinlangapi.com/xinlangapi.php/provide/vod/", "status": 0},
	}
	sources := []source{
		{Code: "dbm3u8", Episodes: []episode{{Name: "1", URL: "https://vodcnd17.ajupf.com/a.m3u8"}}},
		{Code: "hongguo", Episodes: []episode{{Name: "1", URL: "hongguo://123/456"}}},
		{Code: "4kvm", Episodes: []episode{{Name: "1", URL: "4kvm://123/456"}}},
		{Code: "xlm3u8", Episodes: []episode{{Name: "1", URL: "https://play.example/play/a/index.m3u8"}}},
	}
	got := availableSources(row{"api_id": 4}, sources, nil, collectors)
	if len(got) != 2 || got[0].Code != "hongguo" || got[1].Code != "4kvm" {
		t.Fatalf("merged lines inherited the original collector's status: %+v", got)
	}
	collectors[1]["status"] = 0
	got = availableSources(row{"api_id": 10}, sources, nil, collectors)
	if len(got) != 1 || got[0].Code != "4kvm" {
		t.Fatalf("disabled merged native line remains visible: %+v", got)
	}
	// Native-only merged data must also resolve its actual provider.
	got = availableSources(row{"api_id": 4}, sources[2:3], nil, collectors)
	if len(got) != 1 {
		t.Fatal("active native-only line inherited an unrelated disabled collector")
	}
}

func TestCollectorOwnershipRequiresUnambiguousCode(t *testing.T) {
	collectors := []row{
		{"id": 1, "api_url": "https://one.example/api.php/provide/vod/from/customm3u8/at/json", "status": 0},
		{"id": 2, "api_url": "https://two.example/api.php/provide/vod/from/customm3u8/at/json", "status": 1},
	}
	src := []source{{Code: "customm3u8", Episodes: []episode{{Name: "1", URL: "https://cdn.example/a.m3u8"}}}}
	if got := availableSources(row{"api_id": 3}, src, nil, collectors); len(got) != 1 {
		t.Fatal("ambiguous merged ownership disabled a line")
	}
	if got := availableSources(row{"api_id": 1}, src, nil, collectors); len(got) != 0 {
		t.Fatal("known original collector status was ignored")
	}
	for _, raw := range []string{"https://api.apibdzy.com/api.php/provide/vod/from/dbm3u8/", "https://api.example/from/m3u8/", "https://xinlangapi.com.evil.example/api.php"} {
		if codes := collectorPlaybackCodes(raw); len(codes) != 0 {
			t.Fatalf("shared code or unrelated host claims a provider: %q -> %v", raw, codes)
		}
	}
}

func TestSharedDBM3U8NameUsesActualProvider(t *testing.T) {
	collectors := []row{
		{"id": 4, "name": "豆瓣资源", "api_url": "https://caiji.dbzy5.com", "status": 0},
		{"id": 5, "name": "百度资源", "api_url": "https://api.apibdzy.com", "status": 1},
	}
	players := map[string]row{"dbm3u8": {"name": "豆瓣m3u8", "status": 1}}
	baidu := episode{Name: "第01集", URL: "https://b3.bdzybf22.com/videos/202607/25/531002/index.m3u8"}
	douban := episode{Name: "第02集", URL: "https://vodcnd10.ajupf.com/20250816/abc/index.m3u8"}
	for _, primary := range []int{4, 5} {
		got := availableSources(row{"api_id": primary}, []source{{Code: "dbm3u8", Episodes: []episode{baidu, douban}}}, players, collectors)
		if len(got) != 1 || got[0].Name != "百度资源" || len(got[0].Episodes) != 1 || got[0].Episodes[0].URL != baidu.URL {
			t.Fatalf("Baidu inherited the disabled Douban name (primary=%d): %+v", primary, got)
		}
	}
	collectors[0]["status"] = 1
	got := availableSources(row{"api_id": 5}, []source{{Code: "dbm3u8", Episodes: []episode{baidu, douban}}}, players, collectors)
	if len(got) != 1 || got[0].Name != "豆瓣资源 / 百度资源" || len(got[0].Episodes) != 2 {
		t.Fatalf("mixed provider label is inaccurate: %+v", got)
	}
	collectors[1]["status"] = 0
	got = availableSources(row{"api_id": 5}, []source{{Code: "dbm3u8", Episodes: []episode{baidu, douban}}}, players, collectors)
	if len(got) != 1 || got[0].Name != "豆瓣资源" || len(got[0].Episodes) != 1 {
		t.Fatalf("disabled Baidu leaked into Douban line: %+v", got)
	}
}

func TestDisabledCollectorHidesOnlyItsEpisodes(t *testing.T) {
	collectors := []row{
		{"id": 4, "name": "豆瓣资源", "api_url": "https://caiji.dbzy5.com/api.php/provide/vod/", "status": 0},
		{"id": 5, "name": "百度资源", "api_url": "https://api.apibdzy.com/api.php/provide/vod/", "status": 1},
	}
	vod := row{"api_id": 4}
	sources := []source{{Code: "dbm3u8", Episodes: []episode{
		{Name: "1", URL: "https://vodcnd17.ajupf.com/a.m3u8"},
		{Name: "2", URL: "https://b3.bdzybf22.com/b.m3u8"},
	}}}
	got := availableSources(vod, sources, nil, collectors)
	if len(got) != 1 || len(got[0].Episodes) != 1 || got[0].Episodes[0].Name != "2" {
		t.Fatalf("disabled collector was not filtered by episode: %+v", got)
	}
	got = availableSources(vod, []source{{Code: "dbm3u8", Episodes: sources[0].Episodes[:1]}}, nil, collectors)
	if len(got) != 0 {
		t.Fatalf("disabled-only line is still visible: %+v", got)
	}
	collectors[0]["status"] = 1
	got = availableSources(vod, sources, nil, collectors)
	if len(got) != 1 || len(got[0].Episodes) != 2 {
		t.Fatalf("reenabling collector did not restore episodes: %+v", got)
	}
}

func TestDisabledPlayerAndMergedLine(t *testing.T) {
	vod := row{"api_id": 4}
	collectors := []row{{"id": 4, "name": "豆瓣资源", "api_url": "https://caiji.dbzy5.com", "status": 0}}
	sources := []source{
		{Code: "dbm3u8", Episodes: []episode{{Name: "1", URL: "https://vodcnd17.ajupf.com/a.m3u8"}}},
		{Code: "other", Episodes: []episode{{Name: "1", URL: "https://other.example/a.m3u8"}}},
	}
	got := availableSources(vod, sources, nil, collectors)
	if len(got) != 1 || got[0].Code != "other" {
		t.Fatalf("merged enabled line was removed: %+v", got)
	}
	players := map[string]row{"other": {"status": 0}}
	if got = availableSources(vod, sources, players, collectors); len(got) != 0 {
		t.Fatalf("disabled player is still visible: %+v", got)
	}
}

func TestDedupePageSourcesOnlyWhenStreamMatches(t *testing.T) {
	page := source{Code: "xlyun", Episodes: []episode{
		{Name: "1", URL: "https://play.example/play/first"},
		{Name: "2", URL: "https://play.example/play/second"},
	}}
	stream := source{Code: "xlm3u8", Episodes: []episode{
		{Name: "1", URL: "https://play.example/play/first/index.m3u8"},
		{Name: "2", URL: "https://play.example/play/second/index.m3u8"},
	}}
	got := dedupePageSources([]source{page, stream})
	if len(got) != 1 || got[0].Code != "xlm3u8" {
		t.Fatalf("duplicate HTML player page was not removed: %+v", got)
	}
	page.Parse = "m3u8:{url}/index.m3u8"
	if got = dedupePageSources([]source{page, stream}); len(got) != 1 || got[0].Code != "xlm3u8" {
		t.Fatalf("explicit identical HLS rule was not deduplicated: %+v", got)
	}
	page.Parse = "https://parser.example/?url={url}"
	if got = dedupePageSources([]source{page, stream}); len(got) != 2 {
		t.Fatalf("independent parser source was removed: %+v", got)
	}
	page.Parse = ""
	stream.Episodes[1].URL = "https://other.example/second/index.m3u8"
	got = dedupePageSources([]source{page, stream})
	if len(got) != 2 {
		t.Fatalf("different playback lines were incorrectly merged: %+v", got)
	}
}
