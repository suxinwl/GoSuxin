package suxinvideo

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

func parallelIntegrationContext(t *testing.T) context.Context {
	t.Helper()
	if os.Getenv("SUXIN_PARALLEL_INTEGRATION") != "1" {
		t.Skip("requires a separate parallel integration database")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(filepath.Clean("../../..")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	ctx := context.Background()
	database, err := one(ctx, "SELECT DATABASE() name")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(gconv.String(database["name"])), "parallel") {
		t.Fatal("parallel fixtures refuse to modify the real CMS database; database name must contain 'parallel'")
	}
	return ctx
}

func TestCollectParallelLifecycleIntegration(t *testing.T) {
	ctx := parallelIntegrationContext(t)
	if err := prepareCollectJobs(ctx); err != nil {
		t.Fatal(err)
	}
	if err := execSQL(ctx, "UPDATE sx_collect_api SET collect_auto=0"); err != nil {
		t.Fatal(err)
	}
	if err := execSQL(ctx, "INSERT INTO sx_config(`key`,`value`) VALUES('collect_source_concurrency','3') ON DUPLICATE KEY UPDATE `value`='3'"); err != nil {
		t.Fatal(err)
	}
	oldFetch, oldSave, oldPause := fetchCollectJobPage, saveCollectJobVod, pauseCollectJob
	t.Cleanup(func() { fetchCollectJobPage, saveCollectJobVod, pauseCollectJob = oldFetch, oldSave, oldPause })
	gate := make(chan struct{})
	entered := make(chan string, 100)
	var mutex sync.Mutex
	active, maximum := 0, 0
	calls := map[string]int{}
	urls := []string{}
	ids := []int64{}
	for index := 0; index < 6; index++ {
		raw := fmt.Sprintf("https://parallel.example.test/provider-%d", index)
		result, err := g.DB().Exec(ctx, "INSERT INTO sx_collect_api(name,api_url,status,collect_auto,collect_hours) VALUES(?,?,1,1,12)", fmt.Sprintf("parallel-%d", index), raw)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := result.LastInsertId()
		ids = append(ids, id)
		urls = append(urls, raw)
	}
	duplicate, err := g.DB().Exec(ctx, "INSERT INTO sx_collect_api(name,api_url,status,collect_auto,collect_hours) VALUES('parallel-duplicate',?,1,0,12)", urls[0]+"/")
	if err != nil {
		t.Fatal(err)
	}
	duplicateID, _ := duplicate.LastInsertId()
	fetchCollectJobPage = func(ctx context.Context, raw string, params url.Values) (macPayload, error) {
		mutex.Lock()
		calls[raw]++
		active++
		if active > maximum {
			maximum = active
		}
		mutex.Unlock()
		defer func() { mutex.Lock(); active--; mutex.Unlock() }()
		entered <- raw
		select {
		case <-ctx.Done():
			return macPayload{}, ctx.Err()
		case <-gate:
		}
		if raw == urls[5] {
			return macPayload{}, errors.New("controlled provider timeout")
		}
		return macPayload{Code: 1, Page: gconv.Int(params.Get("pg")), PageCount: 2, List: []map[string]any{{"vod_name": "parallel fixture", "vod_play_url": "https://media.example.test/episode.mp4"}}}, nil
	}
	saveCollectJobVod = func(context.Context, int64, map[string]any) (int, error) { return 2, nil }
	pauseCollectJob = func(ctx context.Context) bool { return ctx.Err() == nil }
	manual, err := startCollectJob(ctx, "manual", ids[0], 1, 0, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	manualID := gconv.Int64(manual["id"])
	select {
	case raw := <-entered:
		if raw != urls[0] {
			t.Fatal("wrong manual provider")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("manual provider did not start")
	}
	if _, err := startCollectJob(ctx, "manual", duplicateID, 1, 0, 0, true); err == nil {
		t.Fatal("duplicate URL source bypassed the persistent lease")
	}
	automatic, err := startAutoCollectJob(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	autoID := gconv.Int64(automatic["id"])
	if gconv.Int(automatic["source_count"]) != 5 {
		t.Fatalf("automatic task did not skip busy manual source: %#v", automatic)
	}
	for index := 0; index < 2; index++ {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatal("automatic providers did not run beside manual work")
		}
	}
	select {
	case <-entered:
		t.Fatal("global concurrency limit exceeded")
	case <-time.After(50 * time.Millisecond):
	}
	mutex.Lock()
	disabled := int64(0)
	for index := 1; index < 5; index++ {
		if calls[urls[index]] == 0 {
			disabled = ids[index]
			break
		}
	}
	mutex.Unlock()
	if disabled == 0 {
		t.Fatal("no queued provider available for disable check")
	}
	if err := execSQL(ctx, "UPDATE sx_collect_api SET status=0 WHERE id=?", disabled); err != nil {
		t.Fatal(err)
	}
	if err := execSQL(ctx, "UPDATE sx_collect_job SET cancel_requested=1 WHERE id=?", manualID); err != nil {
		t.Fatal(err)
	}
	collectTaskMu.Lock()
	if stop := collectTaskCancels[manualID]; stop != nil {
		stop()
	}
	collectTaskMu.Unlock()
	close(gate)
	stopped := waitCollectJob(t, ctx, manualID)
	completed := waitCollectJob(t, ctx, autoID)
	if gconv.String(stopped["status"]) != "cancelled" || gconv.Int(stopped["updated"]) != 0 {
		t.Fatalf("in-flight cancellation wrote unexpected data: %#v", stopped)
	}
	if gconv.String(completed["status"]) != "partial" || gconv.Int(completed["updated"]) != 3 || gconv.Int(completed["failed"]) != 1 {
		t.Fatalf("isolated provider failure lost successful progress: %#v", completed)
	}
	mutex.Lock()
	snapshotMaximum := maximum
	snapshotCalls := map[string]int{}
	for key, value := range calls {
		snapshotCalls[key] = value
	}
	mutex.Unlock()
	if snapshotMaximum != 3 {
		t.Fatalf("expected 3 simultaneous source requests, got %d", snapshotMaximum)
	}
	for index, id := range ids {
		if id == disabled && snapshotCalls[urls[index]] != 0 {
			t.Fatal("disabled queued source still made a request")
		}
	}
	if snapshotCalls[urls[5]] != 3 {
		t.Fatal("bounded source retry count changed")
	}
	progress, err := collectSourceProgress(ctx, autoID)
	if err != nil || len(progress) != 5 {
		t.Fatalf("per-source progress missing: %v %v", progress, err)
	}
	for _, source := range progress {
		if _, leaked := source["source_url"]; leaked {
			t.Fatal("progress leaked private provider URL")
		}
	}
	leases, err := one(ctx, "SELECT COUNT(*) n FROM sx_collect_job_source WHERE lease_key IS NOT NULL")
	if err != nil || gconv.Int(leases["n"]) != 0 {
		t.Fatal("stopped/completed source leases were not released")
	}
	restart, err := startCollectJob(ctx, "manual", ids[0], 1, 0, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	restarted := waitCollectJob(t, ctx, gconv.Int64(restart["id"]))
	if gconv.String(restarted["status"]) != "complete" || gconv.Int(restarted["updated"]) != 2 {
		t.Fatalf("cross-page collection failed after stop: %#v", restarted)
	}
}

func TestCollectParallelLegacyResumeIntegration(t *testing.T) {
	ctx := parallelIntegrationContext(t)
	if err := prepareCollectJobs(ctx); err != nil {
		t.Fatal(err)
	}
	oldFetch, oldSave, oldPause := fetchCollectJobPage, saveCollectJobVod, pauseCollectJob
	t.Cleanup(func() { fetchCollectJobPage, saveCollectJobVod, pauseCollectJob = oldFetch, oldSave, oldPause })
	gate := make(chan struct{})
	entered := make(chan int, 1)
	fetchCollectJobPage = func(ctx context.Context, _ string, params url.Values) (macPayload, error) {
		page := gconv.Int(params.Get("pg"))
		entered <- page
		select {
		case <-gate:
		case <-ctx.Done():
			return macPayload{}, ctx.Err()
		}
		return macPayload{Code: 1, Page: page, PageCount: 3, List: []map[string]any{{"vod_name": "resume fixture", "vod_play_url": "https://media.example.test/episode.mp4"}}}, nil
	}
	saveCollectJobVod = func(context.Context, int64, map[string]any) (int, error) { return 2, nil }
	pauseCollectJob = func(ctx context.Context) bool { return ctx.Err() == nil }
	result, err := g.DB().Exec(ctx, "INSERT INTO sx_collect_api(name,api_url,status,collect_auto,collect_hours) VALUES('legacy-resume','https://parallel.example.test/resume',1,0,12)")
	if err != nil {
		t.Fatal(err)
	}
	sourceID, _ := result.LastInsertId()
	now := time.Now().Unix()
	result, err = g.DB().Exec(ctx, `INSERT INTO sx_collect_job(mode,status,active_key,source_count,source_id,page,page_count,start_page,type_id,hours,one_page,added,updated,started_at,updated_at) VALUES('manual','running',1,1,?,3,5,1,2,12,0,7,9,?,?)`, sourceID, now-3600, now)
	if err != nil {
		t.Fatal(err)
	}
	jobID, _ := result.LastInsertId()
	collectJobInit.Lock()
	collectJobReady = false
	collectJobInit.Unlock()
	if err := prepareCollectJobs(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case page := <-entered:
		if page != 3 {
			t.Fatalf("restart lost current page: %d", page)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("legacy manual task did not resume")
	}
	progress, err := collectSourceProgress(ctx, jobID)
	if err != nil || len(progress) != 1 || gconv.Int(progress[0]["added"]) != 7 || gconv.Int(progress[0]["updated"]) != 9 || gconv.Int(progress[0]["hours"]) != 12 || gconv.Int(progress[0]["type_id"]) != 2 {
		t.Fatalf("legacy progress was lost: %v %v", progress, err)
	}
	close(gate)
	completed := waitCollectJob(t, ctx, jobID)
	if gconv.String(completed["status"]) != "complete" || gconv.Int(completed["added"]) != 7 || gconv.Int(completed["updated"]) != 10 {
		t.Fatalf("resume replaced parent progress: %#v", completed)
	}
}
