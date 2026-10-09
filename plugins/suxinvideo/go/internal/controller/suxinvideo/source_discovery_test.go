package suxinvideo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/suxinwl/GoSuxin/framework/contrib/drivers/mysql"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

func TestSourceDiscoveryTerminalIdentityMessage(t *testing.T) {
	complete := discoveryTarget{Year: "2026", Area: "中国大陆", Kind: "anime_movie"}
	base := "已完成片源搜索，可用线路已同步"
	if got := discoveryTerminalMessage(complete, "done", base); got != base {
		t.Fatalf("complete metadata should retain the result: %s", got)
	}
	incomplete := complete
	incomplete.Area = ""
	for _, status := range []string{"done", "error"} {
		got := discoveryTerminalMessage(incomplete, status, base)
		if !strings.HasPrefix(got, base) || !strings.Contains(got, "影片地区信息不完整，暂无法匹配其它资源站") {
			t.Fatalf("missing region must explain strict cross-source rejection without hiding original-source results: %s", got)
		}
	}
	incomplete.Year, incomplete.Area, incomplete.Kind = "未知", "其它", "unknown"
	if got := discoveryTerminalMessage(incomplete, "done", base); !strings.Contains(got, "影片年份、地区、类型信息不完整") {
		t.Fatalf("invalid metadata must identify all missing fields: %s", got)
	}
	if got := discoveryTerminalMessage(incomplete, "disabled", base); got != base {
		t.Fatalf("disabled state must retain its actionable message: %s", got)
	}
}

func TestSourceDiscoveryEpisodeIdentityMatchesPlayback(t *testing.T) {
	one := discoveryEpisodeIdentity(episode{Name: "第001集"})
	for _, name := range []string{"\tEP01\t", "E01", "01", "第1集"} {
		if got := discoveryEpisodeIdentity(episode{Name: name}); got != one {
			t.Errorf("equivalent episode %q differs: %s != %s", name, got, one)
		}
	}
	old := []source{{Code: "same", Episodes: []episode{{Name: "第1集", URL: "https://example.com/episode.m3u8"}}}}
	for _, name := range []string{"第1期", "第1话", "第1集上", "第1集下", "S01E01", "预告1"} {
		want, _ := playbackEpisodeIdentity(name)
		if got := discoveryEpisodeIdentity(episode{Name: name}); got != want || got == one {
			t.Errorf("qualified label %q lost playback identity: %s", name, got)
		}
		incoming := []source{{Code: "same", Episodes: []episode{{Name: name, URL: "https://example.com/other.m3u8"}}}}
		merged, _, _ := mergeDiscoveryPlaylists(old, incoming)
		if len(merged[0].Episodes) != 2 || merged[0].Episodes[0].URL != old[0].Episodes[0].URL {
			t.Errorf("qualified label %q overwrote a regular episode: %#v", name, merged)
		}
	}
}

func TestSourceDiscoveryMergeKeepsPositions(t *testing.T) {
	existing := []source{{Code: "one", Episodes: []episode{{Name: "第01集", URL: "https://a.example/old.m3u8"}, {Name: "第3集", URL: "https://a.example/3.m3u8"}}}}
	incoming := []source{
		{Code: "one", Episodes: []episode{{Name: "EP01", URL: "https://b.example/new.m3u8"}, {Name: "第2集", URL: "https://b.example/2.m3u8"}, {Name: "E01", URL: "https://b.example/renewed.m3u8"}}},
		{Code: "two", Episodes: []episode{{Name: "预告", URL: "https://b.example/trailer.m3u8"}, {Name: "第03集", URL: "https://b.example/3.m3u8"}, {Name: "EP1", URL: "https://b.example/1.m3u8"}, {Name: "第1集", URL: "https://b.example/1new.m3u8"}}},
	}
	merged, added, updated := mergeDiscoveryPlaylists(existing, incoming)
	if added != 1 || updated != 1 || len(merged) != 2 || len(merged[0].Episodes) != 3 {
		t.Fatalf("wrong merge counts: added=%d updated=%d %#v", added, updated, merged)
	}
	if merged[0].Episodes[0].Name != "第01集" || merged[0].Episodes[0].URL != "https://b.example/renewed.m3u8" || merged[0].Episodes[1].Name != "第3集" || merged[0].Episodes[2].Name != "第2集" {
		t.Fatal("existing episode indices/labels changed or renewed URLs duplicated")
	}
	if merged[1].Episodes[0].Name != "EP1" || merged[1].Episodes[1].Name != "第03集" || merged[1].Episodes[2].Name != "预告" {
		t.Fatal("new line must have numeric episodes first, preserving extras")
	}
	if existing[0].Episodes[0].URL != "https://a.example/old.m3u8" {
		t.Fatal("merge mutated its snapshot")
	}
	again, a, u := mergeDiscoveryPlaylists(merged, incoming)
	if a != 0 || u != 0 || len(again[0].Episodes) != 3 {
		t.Fatalf("repeat discovery duplicated entries: %d %d %#v", a, u, again)
	}
	if discoveryHash(again) != discoveryHash(merged) {
		t.Fatal("repeated discovery changed the final playlist")
	}
}

func TestSourceDiscoveryMovieLabels(t *testing.T) {
	for _, labels := range [][2]string{{"仙逆剧场版弑仙之战", "HD国语"}, {"HD国语", "正片"}, {"第01集", "仙逆剧场版弑仙之战"}} {
		old := []source{{Code: "movie", Episodes: []episode{{Name: labels[0], URL: "https://example.com/old.m3u8"}}}}
		fresh := []source{{Code: "movie", Episodes: []episode{{Name: labels[1], URL: "https://example.com/new.m3u8"}}}}
		merged, a, u := mergeDiscoveryFilmPlaylists(old, fresh, "仙逆剧场版弑仙之战", true)
		if a != 0 || u != 1 || len(merged[0].Episodes) != 1 || merged[0].Episodes[0].Name != labels[0] || merged[0].Episodes[0].URL != fresh[0].Episodes[0].URL {
			t.Fatalf("equivalent full-feature labels duplicated: %#v", merged)
		}
	}
	for _, label := range []string{"第1集上", "第1集下", "S01E01", "预告", "第2集", "导演剪辑版"} {
		old := []source{{Code: "movie", Episodes: []episode{{Name: "正片", URL: "https://example.com/old.m3u8"}}}}
		fresh := []source{{Code: "movie", Episodes: []episode{{Name: label, URL: "https://example.com/new.m3u8"}}}}
		merged, _, _ := mergeDiscoveryFilmPlaylists(old, fresh, "影片", true)
		if len(merged[0].Episodes) != 2 || merged[0].Episodes[0].URL != old[0].Episodes[0].URL {
			t.Errorf("qualified edition must not overwrite feature: %s", label)
		}
	}
}

func TestDiscoveryPrioritizesAggregateWithoutDroppingCollectors(t *testing.T) {
	collectors := []row{{"id": 3, "api_url": "https://ff.example/api"}, {"id": 2, "api_url": "https://original.example/api"}, {"id": 25, "api_url": yqkSourceURL}, {"id": 5, "api_url": "https://bd.example/api"}}
	got := discoveryPrioritizeCollectors(collectors, 2)
	for i, want := range []int64{25, 2, 3, 5} {
		if gconv.Int64(got[i]["id"]) != want {
			t.Fatal("aggregate and original must get early slots while all other collectors retain order")
		}
	}
	if gconv.Int64(collectors[0]["id"]) != 3 || len(got) != len(collectors) {
		t.Fatal("priority must not mutate, duplicate or drop the collector inventory")
	}
}

func TestSourceDiscoveryDueAndOwnership(t *testing.T) {
	record := row{"lease_until": int64(200), "next_check": int64(500), "collector_fingerprint": "old", "status": "running"}
	if discoveryDue(record, "new", 100) {
		t.Fatal("new settings must not create a second concurrent job")
	}
	record["lease_until"] = 0
	if discoveryDue(record, "old", 300) || !discoveryDue(record, "new", 300) || !discoveryDue(record, "old", 500) {
		t.Fatal("cooldown/fingerprint invalidation failed")
	}
	record["status"] = "disabled"
	if !discoveryDue(record, "old", 300) {
		t.Fatal("re-enabling discovery should allow a new job")
	}
	if discoveryCooldownMinutes("1") != 15 || discoveryCooldownMinutes("360") != 360 || discoveryCooldownMinutes("5000") != 1440 {
		t.Fatal("discovery interval limits were ignored")
	}
	baidu := row{"id": 5, "api_url": "https://api.apibdzy.com/api.php/provide/vod/"}
	douban := row{"id": 4, "api_url": "https://caiji.dbzy5.com/api.php/provide/vod/", "status": 0}
	if !discoverySourceAllowed(source{Code: "dbm3u8", Episodes: []episode{{URL: "https://cdn.bdzy.com/1.m3u8"}}}, baidu, []row{baidu, douban}) {
		t.Fatal("known active Baidu line was rejected")
	}
	for _, raw := range []string{"https://vodcnd09.ajupf.com/1.m3u8", "https://unknown.example/1.m3u8"} {
		if discoverySourceAllowed(source{Code: "dbm3u8", Episodes: []episode{{URL: raw}}}, baidu, []row{baidu, douban}) {
			t.Fatal("shared source code revived disabled/unknown origin")
		}
	}
	my := row{"id": 2, "api_url": "https://api.maoyanapi.top/api.php/provide/vod/"}
	if discoverySourceAllowed(source{Code: "hnm3u8", Episodes: []episode{{URL: "https://cdn.example/1.m3u8"}}}, my, []row{my}) {
		t.Fatal("collector attempted to overwrite another source's independent code")
	}
	locked := sourceDiscoveryState{Status: "done"}
	data, _ := json.Marshal(locked)
	if strings.Contains(string(data), `"sources"`) {
		t.Fatal("locked response must omit source data")
	}
	empty := []playSource{}
	locked.Sources = &empty
	data, _ = json.Marshal(locked)
	if !strings.Contains(string(data), `"sources":[]`) {
		t.Fatal("an unlocked empty snapshot must explicitly return an empty list")
	}
}

func waitSourceDiscoveryTest(t *testing.T, ctx context.Context, id int64) row {
	t.Helper()
	until := time.Now().Add(10 * time.Second)
	for time.Now().Before(until) {
		state, err := one(ctx, "SELECT * FROM sx_source_discovery WHERE vod_id=?", id)
		if err != nil {
			t.Fatal(err)
		}
		if state != nil && gconv.String(state["status"]) != "queued" && gconv.String(state["status"]) != "running" {
			return state
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("discovery %d did not finish", id)
	return nil
}

func TestSourceDiscoveryIntegration(t *testing.T) {
	if os.Getenv("SUXIN_INTEGRATION") != "1" {
		t.Skip("set SUXIN_INTEGRATION=1 to use isolated development database fixtures")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(filepath.Clean("../../..")); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	ctx := context.Background()
	if setting(ctx, "source_discovery_enable", "1") != "1" {
		t.Skip("automatic discovery is disabled by the administrator")
	}
	if setting(ctx, "player_parse", "") != "" {
		t.Skip("direct discovery is intentionally disabled for global parsers")
	}
	if err = execSQL(ctx, sourceDiscoveryTable); err != nil {
		t.Fatal(err)
	}
	insert, err := g.DB().Exec(ctx, "INSERT INTO sx_type(name,pid,status) VALUES('国产动漫',0,1)")
	if err != nil {
		t.Fatal(err)
	}
	typeID, _ := insert.LastInsertId()
	defer execSQL(ctx, "DELETE FROM sx_type WHERE id=?", typeID)
	insert, err = g.DB().Exec(ctx, "INSERT INTO sx_collect_api(name,api_url,status,collect_auto,collect_hours) VALUES(?,?,1,0,12)", "discovery-fixture", "https://example.test/api/from/testdiscovery/")
	if err != nil {
		t.Fatal(err)
	}
	collectorID, _ := insert.LastInsertId()
	defer execSQL(ctx, "DELETE FROM sx_collect_api WHERE id=?", collectorID)
	var ids []int64
	defer func() {
		for _, id := range ids {
			_ = execSQL(ctx, "DELETE FROM sx_source_discovery WHERE vod_id=?", id)
			_ = execSQL(ctx, "DELETE FROM sx_vod WHERE id=?", id)
		}
	}()
	newFilm := func(suffix int) int64 {
		res, e := g.DB().Exec(ctx, "INSERT INTO sx_vod(type_id,api_id,api_vid,name,name_norm,year,area,status,play_from,play_url,addtime,updatetime) VALUES(?,?,?,?,?,'2023','中国大陆',1,'testdiscovery',?,1,2)", typeID, collectorID, fmt.Sprint(suffix), fmt.Sprintf("discovery-fixture-%d-%d", time.Now().UnixNano(), suffix), "fixture", "第01集$https://example.test/old.m3u8")
		if e != nil {
			t.Fatal(e)
		}
		id, _ := res.LastInsertId()
		ids = append(ids, id)
		return id
	}
	oldProvider, oldList, oldEnrich, oldUser := discoveryProvider, sourceDiscoveryList, sourceDiscoveryEnrich, sourceDiscoveryUser
	defer func() {
		discoveryProvider, sourceDiscoveryList, sourceDiscoveryEnrich, sourceDiscoveryUser = oldProvider, oldList, oldEnrich, oldUser
	}()
	sourceDiscoveryList = func(ctx context.Context) ([]row, string, error) {
		collector, e := one(ctx, "SELECT id,name,api_url,status FROM sx_collect_api WHERE id=?", collectorID)
		return []row{collector}, discoveryHash(collector), e
	}
	sourceDiscoveryEnrich = func(_ context.Context, target discoveryTarget, _ row) discoveryTarget { return target }
	sourceDiscoveryUser = func(context.Context) (row, error) { return nil, nil }
	gate := make(chan struct{})
	defer func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
	}()
	var calls, active, maximum atomic.Int32
	discoveryProvider = func(ctx context.Context, _ discoveryTarget, collector row) discoveryProviderResult {
		calls.Add(1)
		current := active.Add(1)
		for previous := maximum.Load(); current > previous && !maximum.CompareAndSwap(previous, current); previous = maximum.Load() {
		}
		defer active.Add(-1)
		select {
		case <-gate:
		case <-ctx.Done():
			return discoveryProviderResult{CollectorID: collectorID, Error: "cancelled"}
		}
		return discoveryProviderResult{CollectorID: collectorID, Sources: []source{{Code: "testdiscovery", Episodes: []episode{{Name: "EP01", URL: "https://example.test/new.m3u8"}, {Name: "第02集", URL: "https://example.test/2.m3u8"}}}}}
	}
	id := newFilm(0)
	var requests sync.WaitGroup
	for i := 0; i < 12; i++ {
		requests.Add(1)
		go func() { defer requests.Done(); _, _ = startSourceDiscovery(ctx, id) }()
	}
	requests.Wait()
	until := time.Now().Add(3 * time.Second)
	for calls.Load() < 1 && time.Now().Before(until) {
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() != 1 {
		t.Fatalf("simultaneous visitors started %d jobs for one film", calls.Load())
	}
	if err = execSQL(ctx, "UPDATE sx_vod SET play_from='testdiscovery$$$manualsrc',play_url=?,points=7 WHERE id=?", "第01集$https://example.test/old.m3u8$$$第1集$https://example.test/manual.m3u8", id); err != nil {
		t.Fatal(err)
	}
	close(gate)
	state := waitSourceDiscoveryTest(t, ctx, id)
	if gconv.String(state["status"]) != "done" || gconv.Int(state["updated"]) != 1 || gconv.Int(state["checked"]) != 1 || gconv.Int64(state["next_check"]) < time.Now().Unix()+14*60 {
		t.Fatalf("incorrect completion/cooldown: %#v", state)
	}
	vod, _ := one(ctx, "SELECT * FROM sx_vod WHERE id=?", id)
	if gconv.String(vod["play_from"]) != "testdiscovery$$$manualsrc" || !strings.Contains(gconv.String(vod["play_url"]), "manual.m3u8") || gconv.Int(vod["points"]) != 7 || gconv.Int64(vod["updatetime"]) != 2 {
		t.Fatal("discovery clobbered a concurrent source/access/metadata update")
	}
	if len(playlist(vod)[0].Episodes) != 2 || playlist(vod)[0].Episodes[0].Name != "第01集" {
		t.Fatal("renewed URL produced duplicate episodes or changed an existing index")
	}
	_, _ = startSourceDiscovery(ctx, id)
	if calls.Load() != 1 {
		t.Fatal("successful cooldown did not suppress a repeated visit")
	}
	locked, err := sourceDiscoverySnapshot(ctx, id, "")
	if err != nil || locked.Sources != nil || locked.SourceRevision != "" {
		t.Fatalf("paid content leaked to anonymous discovery: %#v %v", locked, err)
	}
	_ = execSQL(ctx, "UPDATE sx_vod SET points=0 WHERE id=?", id)
	snapshot, err := sourceDiscoverySnapshot(ctx, id, "")
	if err != nil || snapshot.Sources == nil || len(snapshot.SourceRevision) != 64 {
		t.Fatalf("unlocked source snapshot missing: %#v %v", snapshot, err)
	}
	unchanged, err := sourceDiscoverySnapshot(ctx, id, snapshot.SourceRevision)
	if err != nil || unchanged.Sources != nil || unchanged.SourceRevision != snapshot.SourceRevision {
		t.Fatal("unchanged polling must omit repeated signed source payload")
	}
	urls := discoveryURLs(ctx, id)
	parsed, _ := url.Parse(urls.Status)
	params := parsed.Query()
	exp := gconv.Int64(params.Get("exp"))
	if !validDiscoverySignature(ctx, id, exp, params.Get("sig")) || validDiscoverySignature(ctx, id+1, exp, params.Get("sig")) || validDiscoverySignature(ctx, id, time.Now().Unix()-1, params.Get("sig")) {
		t.Fatal("discovery status authorization did not bind film and expiration")
	}
	// Direct commit checks prove that identity/source changes are re-read under lock.
	collector, _ := one(ctx, "SELECT id,name,api_url,status FROM sx_collect_api WHERE id=?", collectorID)
	vod, _ = one(ctx, "SELECT * FROM sx_vod WHERE id=?", id)
	job := sourceDiscoveryJob{id: id, token: "fixture-token", identity: discoveryFilmIdentity(vod)}
	_ = execSQL(ctx, "UPDATE sx_source_discovery SET run_token=? WHERE vod_id=?", job.token, id)
	result := discoveryProviderResult{CollectorID: collectorID, Sources: []source{{Code: "testdiscovery", Episodes: []episode{{Name: "第1集", URL: "https://example.test/should-not-write.m3u8"}}}}}
	_ = execSQL(ctx, "UPDATE sx_vod SET name=CONCAT(name,'-changed') WHERE id=?", id)
	if _, _, _, err = mergeDiscoveredSources(ctx, job, collector, result); !errors.Is(err, errDiscoveryChanged) {
		t.Fatalf("changed film identity was accepted: %v", err)
	}
	vod, _ = one(ctx, "SELECT * FROM sx_vod WHERE id=?", id)
	job.identity = discoveryFilmIdentity(vod)
	_ = execSQL(ctx, "UPDATE sx_collect_api SET status=0 WHERE id=?", collectorID)
	if _, _, _, err = mergeDiscoveredSources(ctx, job, collector, result); !errors.Is(err, errDiscoveryChanged) {
		t.Fatalf("disabled collector was accepted: %v", err)
	}
	_ = execSQL(ctx, "UPDATE sx_collect_api SET status=1 WHERE id=?", collectorID)
	// More than ten distinct visitors remain bounded to two active films/eight queued.
	gate = make(chan struct{})
	maximum.Store(0)
	var admitted []int64
	busy := 0
	for i := 1; i <= 12; i++ {
		otherID := newFilm(i)
		response, e := startSourceDiscovery(ctx, otherID)
		if e != nil {
			t.Fatal(e)
		}
		if response.Status == "busy" {
			busy++
		} else {
			admitted = append(admitted, otherID)
		}
	}
	if busy == 0 || len(admitted) > 10 || len(sourceDiscoveryWorkers.jobs) > 8 {
		t.Fatalf("queue was not bounded: admitted=%d busy=%d", len(admitted), busy)
	}
	close(gate)
	for _, otherID := range admitted {
		waitSourceDiscoveryTest(t, ctx, otherID)
	}
	if maximum.Load() > 2 {
		t.Fatalf("global movie concurrency exceeded two: %d", maximum.Load())
	}
}
