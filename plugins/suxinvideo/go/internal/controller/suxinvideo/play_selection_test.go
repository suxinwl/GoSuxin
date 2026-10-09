package suxinvideo

import (
	"strings"
	"testing"
)

func TestPlaybackProviderGenrePreference(t *testing.T) {
	for _, test := range []struct {
		name              string
		shortDrama, anime bool
		code, label       string
	}{
		{"short drama", true, false, "hongguo", "红果"},
		{"anime", false, true, "erciyuan", "二次元"},
		{"short animated drama", true, true, "hongguo", "红果"},
		{"other genres", false, false, "yqk", "小柒APP / 小柒"},
	} {
		t.Run(test.name, func(t *testing.T) {
			code, label := playbackPreferredProvider(test.shortDrama, test.anime)
			if code != test.code || label != test.label {
				t.Fatalf("preference=(%q,%q), want (%q,%q)", code, label, test.code, test.label)
			}
		})
	}
	for _, label := range []string{"动漫", "国产动漫", "日韩动漫", "日本动漫", "动画", "漫剧", "动漫电影", "动画电影", "动画片"} {
		if !playbackCategoryHasChannel(label, 8) {
			t.Errorf("anime channel category missed: %q", label)
		}
	}
	for _, label := range []string{"电影", "短剧", "电视剧", "纪录片", "动作片", "综艺", "体育"} {
		if playbackCategoryHasChannel(label, 8) {
			t.Errorf("non-anime category gained anime preference: %q", label)
		}
	}
	for _, test := range []struct {
		class string
		want  bool
	}{
		{"anime", true}, {"ANIME", true}, {"动作,动画", true}, {"奇幻、动漫", true},
		{"电影/动漫电影", true}, {"电视剧,剧情", false}, {"剧情片", false},
		{"动作片", false}, {"体育", false}, {"", false},
	} {
		if got := animePlaybackMetadata(row{"class": test.class}); got != test.want {
			t.Errorf("anime metadata %q=%v, want %v", test.class, got, test.want)
		}
	}
}

func TestFreshShortDramaCompilationCanPreferHongguoFirstChapter(t *testing.T) {
	original := []source{{Code: "hnm3u8", Episodes: []episode{{Name: "全集完结", URL: "https://cdn.example/compilation.m3u8"}}}}
	visible := []playSource{
		{Code: "hnm3u8", Episodes: []playEpisode{{Name: "全集完结", Key: "label:全集完结", Number: -1, PSrc: "https://cdn.example/compilation.m3u8"}}},
		{Code: "hongguo", Episodes: []playEpisode{{Name: "第01集", Key: "episode:1", Number: 1, URL: "hongguo://55/1", PSrc: "/suxinvideo/native/resolve?line=hongguo"}, {Name: "第02集", Key: "episode:2", Number: 2, URL: "hongguo://55/2", PSrc: "/suxinvideo/native/resolve?line=hongguo"}}},
	}
	req := &PlayReq{Line: "hnm3u8", Episode: 0}
	key, fallback := shortDramaCompilationStart(req, original, visible, 0, "label:全集完结", true, 0)
	if key != "episode:1" || fallback == nil || fallback.Code != "hnm3u8" || fallback.Key != "label:全集完结" {
		t.Fatalf("fresh compilation did not offer a separate chapter with its original fallback: %q, %#v", key, fallback)
	}
	if visible[0].Episodes[0].Key != "label:全集完结" || visible[1].Episodes[1].Key != "episode:2" {
		t.Fatal("compilation or chapter identities were rewritten")
	}
	for _, test := range []struct {
		name     string
		req      *PlayReq
		position int64
		short    bool
	}{
		{"resume compilation", req, 15, true}, {"invalid negative resume", req, -1, true},
		{"other index", &PlayReq{Line: "hnm3u8", Episode: 1}, 0, true},
		{"unrelated missing line", &PlayReq{Line: "missing", Episode: 0}, 0, true},
		{"other genre", req, 0, false}, {"nil request", nil, 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			key, fallback := shortDramaCompilationStart(test.req, original, visible, 0, "label:全集完结", test.short, test.position)
			if key != "label:全集完结" || fallback != nil {
				t.Fatalf("unsafe fresh-start conversion: %q, %#v", key, fallback)
			}
		})
	}
	for _, label := range []string{"全集", "全集完结", " 全集 完结 "} {
		raw := []source{{Code: "hnm3u8", Episodes: []episode{{Name: label}}}}
		origin, _ := playbackEpisodeIdentity(label)
		if key, fallback := shortDramaCompilationStart(req, raw, visible, 0, origin, true, 0); key != "episode:1" || fallback == nil {
			t.Errorf("explicit complete-series label not accepted: %q", label)
		}
	}
	for _, label := range []string{"正片", "第01集", "预告", "大结局", "高清", "全集预告", "全集（上）", "全集解说", "全1-98集"} {
		raw := []source{{Code: "hnm3u8", Episodes: []episode{{Name: label}}}}
		origin, _ := playbackEpisodeIdentity(label)
		if key, fallback := shortDramaCompilationStart(req, raw, visible, 0, origin, true, 0); key != origin || fallback != nil {
			t.Errorf("ambiguous label converted into separate chapter: %q", label)
		}
	}
	multi := []source{{Code: "hnm3u8", Episodes: []episode{{Name: "全集完结"}, {Name: "预告"}}}}
	if key, fallback := shortDramaCompilationStart(req, multi, visible, 0, "label:全集完结", true, 0); key != "label:全集完结" || fallback != nil {
		t.Fatal("multi-entry collection was treated as a single full-series compilation")
	}
	for _, candidate := range []playSource{
		{Code: "ecy_aa03", Episodes: visible[1].Episodes},
		{Code: "hongguo", Episodes: visible[1].Episodes[1:]},
		{Code: "hongguo", Episodes: []playEpisode{{Name: "第01集", Key: "episode:1", Number: 1, URL: "https://unrelated.example/a.m3u8", PSrc: "https://unrelated.example/a.m3u8"}}},
		{Code: "hongguo", Episodes: []playEpisode{{Name: "第01集", Key: "episode:1", Number: 1, URL: "hongguo://55/1"}}},
	} {
		if key, fallback := shortDramaCompilationStart(req, original, []playSource{visible[0], candidate}, 0, "label:全集完结", true, 0); key != "label:全集完结" || fallback != nil {
			t.Fatal("unavailable/unrelated/unverified provider first chapter gained preference")
		}
	}
	if key, fallback := shortDramaCompilationStart(&PlayReq{Episode: 0}, original, visible, 0, "label:全集完结", true, 0); key != "episode:1" || fallback == nil {
		t.Fatal("normal default entry into a compilation cannot prefer the first chapter")
	}
}

func TestPlaybackChineseMessagesRemainIntact(t *testing.T) {
	if strings.Contains(playThemeBody, "???") {
		t.Fatal("playback UI contains an encoding replacement string")
	}
	for _, label := range []string{"重试播放", "片源解析超时", "原线路不可用，已切换同一集", "该线路没有对应分集"} {
		if !strings.Contains(playThemeBody, label) {
			t.Errorf("playback message missing or corrupted: %s", label)
		}
	}
}

func TestPlaybackEpisodeIdentity(t *testing.T) {
	for _, label := range []string{"第1集", "第01集", "第 001 集", "EP01", "ep1", "E01", "e001", "01", "1"} {
		key, number := playbackEpisodeIdentity(label)
		if key != "episode:1" || number != 1 {
			t.Errorf("equivalent numbered episode %q: %q, %d", label, key, number)
		}
	}
	for _, label := range []string{"第1集上", "第1集下", "第1集预告", "第01集预告", "第1季第1集", "第2季第1集", "S01E01", "S02E01", "EP01-02", "01(上)", "1.5", "HD1080P", "2026-01-01", "第1期"} {
		key, number := playbackEpisodeIdentity(label)
		if key == "episode:1" || number != -1 {
			t.Errorf("qualified label must not collapse into episode one: %q => %q, %d", label, key, number)
		}
	}
	for _, pair := range [][2]string{{"第1集上", "第1集下"}, {"S01E01", "S02E01"}, {"第1集预告", "第1集"}, {"HD", "HD中字"}, {"", "1"}} {
		a, _ := playbackEpisodeIdentity(pair[0])
		b, _ := playbackEpisodeIdentity(pair[1])
		if a == b {
			t.Errorf("different episodes must remain separate: %q, %q", pair[0], pair[1])
		}
	}
}

func TestMovieFeatureMatching(t *testing.T) {
	const title = "仙逆剧场版弑仙之战"
	for _, label := range []string{title, "HD国语", "第01集", "正片", "HD中字", "1080P", "蓝光", "完整版", "EP01"} {
		if !movieFeatureIdentity(title, label) {
			t.Errorf("feature label not recognized: %s", label)
		}
	}
	for _, label := range []string{"", "预告片", "HD预告", "花絮", "试看", "加长版", "第01集预告", "第02集", "上集", "HD下部", "第1集上", "第一季", "S01E01", "HD(下)", "完整版合集", "删减版", "trailer", "另一部电影"} {
		if movieFeatureIdentity(title, label) {
			t.Errorf("unrelated/split content must not match a movie: %s", label)
		}
	}
	if movieFeatureIdentity("某电影预告片", "HD") {
		t.Fatal("a preview record must not be treated as a full movie")
	}
	makeSources := func() []playSource {
		return []playSource{
			{Episodes: []playEpisode{{Name: title, Key: "label:" + title, Number: -1}}},
			{Episodes: []playEpisode{{Name: "HD国语", Key: "label:HD国语", Number: -1}}},
			{Episodes: []playEpisode{{Name: "第01集", Key: "episode:1", Number: 1}}},
			{Episodes: []playEpisode{{Name: "预告", Key: "label:预告", Number: -1}}},
		}
	}
	series := makeSources()
	alignMoviePlaybackSources(title, false, series)
	if series[0].Episodes[0].Key == series[2].Episodes[0].Key {
		t.Fatal("ordinary series must retain strict episode matching")
	}
	movie := makeSources()
	alignMoviePlaybackSources(title, true, movie)
	for i := 0; i < 3; i++ {
		if movie[i].Episodes[0].Key != "movie:feature" || movie[i].Episodes[0].Number != -1 {
			t.Errorf("single-feature line %d did not normalize", i)
		}
	}
	if movie[3].Episodes[0].Key == "movie:feature" {
		t.Fatal("preview incorrectly became a single feature")
	}
	multi := append(makeSources(), playSource{Episodes: []playEpisode{{Name: "第01集", Key: "episode:1", Number: 1}, {Name: "第02集", Key: "episode:2", Number: 2}}})
	alignMoviePlaybackSources(title, true, multi)
	if multi[2].Episodes[0].Key != "episode:1" || multi[4].Episodes[0].Key != "episode:1" {
		t.Fatal("first part of a split release was treated as a full movie")
	}
	for _, name := range []string{"动画片", "电影", "剧情片", "动作片"} {
		if !moviePlaybackCategories[name] {
			t.Errorf("missing movie category %s", name)
		}
	}
	for _, name := range []string{"动漫", "剧集", "短剧", "纪录片", "动画", "电影解说"} {
		if moviePlaybackCategories[name] {
			t.Errorf("ambiguous category treated as movie: %s", name)
		}
	}
}

func TestMovieEndedFirstEpisodeLabel(t *testing.T) {
	for _, label := range []string{"第1集完结", "第01集完结"} {
		if key, _ := moviePlaybackEpisodeKey("仙逆剧场版 弑仙之战", label, true); key != "movie:feature" {
			t.Fatalf("single complete theatrical release lost movie identity: %s", label)
		}
		if key, _ := moviePlaybackEpisodeKey("仙逆", label, false); key == "movie:feature" {
			t.Fatal("a television episode cannot gain movie identity")
		}
	}
	for _, label := range []string{"第2集完结", "第1集上完结", "第1集预告", "全集完结"} {
		if movieFeatureIdentity("影片", label) {
			t.Fatalf("ambiguous or partial release was treated as a feature: %s", label)
		}
	}
	if moviePlaybackAlignmentAllowed("影片", true, []source{{Episodes: []episode{{Name: "第1集完结"}, {Name: "第2集"}}}}) {
		t.Fatal("multiple television episodes cannot align as one movie")
	}
}

func TestStablePlaySourceSelection(t *testing.T) {
	sources := []source{
		{Code: "line-a", Episodes: []episode{{URL: "https://cdn.example/a.m3u8"}}},
		{Code: "line-c", Episodes: []episode{{URL: "https://cdn.example/c.m3u8"}}},
	}
	if got := playSourceIndex(sources, "line-c", 2, true); got != 1 {
		t.Fatalf("stable line must survive removal of an earlier source: %d", got)
	}
	if got := playSourceIndex(sources, "line-a", 1, true); got != 0 {
		t.Fatalf("line code must win over stale numeric index: %d", got)
	}
	if got := playSourceIndex(sources, "removed", 1, true); got != preferredSource(sources) {
		t.Fatalf("removed line must use default instead of stale index: %d", got)
	}
	if got := playSourceIndex(sources, "", 1, true); got != 1 {
		t.Fatalf("legacy numeric source links remain supported: %d", got)
	}
	if got := playSourceIndex(sources, "", -1, true); got != preferredSource(sources) {
		t.Fatalf("invalid legacy index must use default: %d", got)
	}
	if got := playSourceIndex(nil, "", 0, false); got != 0 {
		t.Fatalf("empty source list must remain safely renderable: %d", got)
	}
}

func movieAlignmentFixture(labels ...[]string) ([]source, []playSource) {
	var original []source
	var views []playSource
	for _, line := range labels {
		raw, view := source{}, playSource{}
		for _, label := range line {
			key, number := playbackEpisodeIdentity(label)
			raw.Episodes = append(raw.Episodes, episode{Name: label})
			view.Episodes = append(view.Episodes, playEpisode{Name: label, Key: key, Number: number})
		}
		original = append(original, raw)
		views = append(views, view)
	}
	return original, views
}

func TestMovieVersionsAlignWithoutHidingWorkingAlternatives(t *testing.T) {
	const title = "示例电影"
	original, views := movieAlignmentFixture([]string{"HD"}, []string{"TC", "HD"}, []string{"正片"}, []string{"720P"}, []string{"HD国语", "HD中字"}, []string{"预告片", "正片", "花絮"})
	allowed := moviePlaybackAlignmentAllowed(title, true, original)
	if !allowed {
		t.Fatal("complete quality/edition choices were mistaken for a split release")
	}
	alignMoviePlaybackSources(title, allowed, views)
	for i, src := range views {
		if len(src.Episodes) != len(original[i].Episodes) {
			t.Fatal("alignment must preserve all stored alternatives and their indices")
		}
		for j, ep := range src.Episodes {
			if ep.Name == "预告片" || ep.Name == "花絮" {
				if ep.Key == "movie:feature" {
					t.Fatal("supplement became a full movie")
				}
				continue
			}
			if ep.Key != "movie:feature" || ep.Number != -1 {
				t.Errorf("line %d item %d did not align with the feature: %s", i, j, ep.Name)
			}
			if key, _ := moviePlaybackEpisodeKey(title, original[i].Episodes[j].Name, allowed); key != ep.Key {
				t.Fatal("original request and rendered candidate identities differ")
			}
		}
	}
	_, series := movieAlignmentFixture([]string{"HD"}, []string{"正片"})
	alignMoviePlaybackSources(title, false, series)
	if series[0].Episodes[0].Key == series[1].Episodes[0].Key {
		t.Fatal("unknown/non-movie categories must not cross-match labels")
	}
}

func TestMovieAlignmentPreservesHiddenSplitEvidence(t *testing.T) {
	for _, split := range [][]string{{"第01集", "第02集"}, {"EP01", "EP02"}, {"01", "02"}, {"上集", "下集"}, {"HD(上)", "HD(下)"}, {"正片上", "正片下"}, {"Part1", "Part2"}, {"S01E01"}, {"第02集"}, {"版本甲", "版本乙"}, {"HD", "未知版本"}} {
		original, visible := movieAlignmentFixture([]string{"HD"}, []string{"正片"}, split)
		// Simulate a disabled or health-hidden line: only its raw evidence
		// remains, so the visible list alone would wrongly look unambiguous.
		allowed := moviePlaybackAlignmentAllowed("影片", true, original)
		if allowed {
			t.Errorf("split/ambiguous evidence ignored: %#v", split)
		}
		visible = visible[:2]
		alignMoviePlaybackSources("影片", allowed, visible)
		if visible[0].Episodes[0].Key == "movie:feature" || visible[1].Episodes[0].Key == "movie:feature" {
			t.Fatal("hidden split release was reinterpreted as a whole movie")
		}
	}
	original, _ := movieAlignmentFixture([]string{"1917"}, []string{"HD", "TC"})
	if !moviePlaybackAlignmentAllowed("1917", true, original) {
		t.Fatal("numeric film title treated as numbered installments")
	}
	if moviePlaybackAlignmentAllowed("影片预告片", true, original) {
		t.Fatal("trailer record treated as a full movie")
	}
}

func TestMovieDiscoveryRetainsQualityVersionsDuringMerge(t *testing.T) {
	existing := []source{
		{Code: "variants", Episodes: []episode{{Name: "TC", URL: "https://cdn.example/tc-old.m3u8"}, {Name: "HD", URL: "https://cdn.example/hd-old.m3u8"}}},
		{Code: "single", Episodes: []episode{{Name: "HD国语", URL: "https://cdn.example/old.m3u8"}}},
	}
	incoming := []source{
		{Code: "variants", Episodes: []episode{{Name: "TC", URL: "https://cdn.example/tc-new.m3u8"}, {Name: "HD", URL: "https://cdn.example/hd-new.m3u8"}}},
		{Code: "single", Episodes: []episode{{Name: "正片", URL: "https://cdn.example/new.m3u8"}}},
		{Code: "new-versions", Episodes: []episode{{Name: "TC", URL: "https://cdn.example/tc.m3u8"}, {Name: "HD", URL: "https://cdn.example/hd.m3u8"}}},
	}
	merged, added, updated := mergeDiscoveryFilmPlaylists(existing, incoming, "示例电影", true)
	if added != 1 || updated != 2 || len(merged) != 3 || len(merged[0].Episodes) != 2 || len(merged[1].Episodes) != 1 || len(merged[2].Episodes) != 2 {
		t.Fatal("movie merge collapsed editions or duplicated a single-feature label")
	}
	for i, want := range []string{"TC", "HD"} {
		if merged[0].Episodes[i].Name != want || merged[0].Episodes[i].URL != incoming[0].Episodes[i].URL {
			t.Fatal("an edition URL/label/index was overwritten by another edition")
		}
	}
	if merged[1].Episodes[0].Name != "HD国语" || merged[1].Episodes[0].URL != incoming[1].Episodes[0].URL {
		t.Fatal("unrelated multi-version line disabled safe single-feature refresh")
	}
	_, added, updated = mergeDiscoveryFilmPlaylists(merged, incoming, "示例电影", true)
	if added != 0 || updated != 0 {
		t.Fatal("repeated version merge must be a no-op")
	}
}
