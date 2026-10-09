package suxinvideo

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/suxinwl/GoSuxin/framework/contrib/drivers/mysql"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

func waitCollectJob(t *testing.T, ctx context.Context, id int64) row {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		job, err := one(ctx, "SELECT * FROM sx_collect_job WHERE id=?", id)
		if err != nil {
			t.Fatal(err)
		}
		if job != nil && gconv.String(job["status"]) != "running" && gconv.String(job["status"]) != "queued" {
			return job
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("collection job %d did not finish", id)
	return nil
}

func TestCollectJobLifecycle(t *testing.T) {
	if os.Getenv("SUXIN_INTEGRATION") != "1" {
		t.Skip("set SUXIN_INTEGRATION=1 to use the development database")
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
	active, err := one(ctx, "SELECT id FROM sx_collect_job WHERE status IN ('queued','running') LIMIT 1")
	if err != nil {
		t.Fatal(err)
	}
	if active != nil {
		t.Skip("another collection is running")
	}
	result, err := g.DB().Exec(ctx, "INSERT INTO sx_collect_api(name,api_url,status,collect_auto,collect_hours) VALUES(?,?,1,0,12)", fmt.Sprintf("test-%d", time.Now().UnixNano()), "https://example.test/api")
	if err != nil {
		t.Fatal(err)
	}
	sourceID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	defer execSQL(ctx, "DELETE FROM sx_collect_api WHERE id=?", sourceID)
	var jobs []int64
	defer func() {
		for _, id := range jobs {
			_ = execSQL(ctx, "DELETE FROM sx_collect_job_source WHERE job_id=?", id)
			_ = execSQL(ctx, "DELETE FROM sx_collect_job WHERE id=?", id)
		}
	}()
	oldFetch, oldSave, oldPause := fetchCollectJobPage, saveCollectJobVod, pauseCollectJob
	defer func() { fetchCollectJobPage, saveCollectJobVod, pauseCollectJob = oldFetch, oldSave, oldPause }()
	fetchCollectJobPage = func(_ context.Context, _ string, params url.Values) (macPayload, error) {
		page := gconv.Int(params.Get("pg"))
		return macPayload{Code: 1, Page: page, PageCount: 2, List: []map[string]any{{"vod_name": fmt.Sprintf("episode-%d", page), "vod_play_url": "https://example.test/video.m3u8"}}}, nil
	}
	saveCollectJobVod = func(context.Context, int64, map[string]any) (int, error) { return 2, nil }
	gate := make(chan struct{})
	pauseCollectJob = func(ctx context.Context) bool {
		select {
		case <-gate:
			return true
		case <-ctx.Done():
			return false
		}
	}
	first, err := startCollectJob(ctx, "manual", sourceID, 1, 0, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	firstID := gconv.Int64(first["id"])
	jobs = append(jobs, firstID)
	if _, err = startCollectJob(ctx, "manual", sourceID, 1, 0, 0, true); err == nil {
		t.Fatal("concurrent collection was accepted")
	}
	close(gate)
	completed := waitCollectJob(t, ctx, firstID)
	if gconv.String(completed["status"]) != "complete" || gconv.Int(completed["page"]) != 2 || gconv.Int(completed["updated"]) != 2 {
		t.Fatalf("unexpected cross-page progress: %#v", completed)
	}
	once, err := startCollectJob(ctx, "manual", sourceID, 1, 0, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	onceID := gconv.Int64(once["id"])
	jobs = append(jobs, onceID)
	single := waitCollectJob(t, ctx, onceID)
	if gconv.String(single["status"]) != "complete" || gconv.Int(single["page"]) != 1 || gconv.Int(single["updated"]) != 1 {
		t.Fatalf("unexpected one-page progress: %#v", single)
	}
	stopGate := make(chan struct{})
	pauseCollectJob = func(ctx context.Context) bool {
		select {
		case <-stopGate:
			return true
		case <-ctx.Done():
			return false
		}
	}
	stopped, err := startCollectJob(ctx, "manual", sourceID, 1, 0, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	stopID := gconv.Int64(stopped["id"])
	jobs = append(jobs, stopID)
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		progress, e := one(ctx, "SELECT item FROM sx_collect_job WHERE id=?", stopID)
		if e != nil {
			t.Fatal(e)
		}
		if gconv.Int(progress["item"]) == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err = execSQL(ctx, "UPDATE sx_collect_job SET cancel_requested=1 WHERE id=?", stopID); err != nil {
		t.Fatal(err)
	}
	close(stopGate)
	cancelled := waitCollectJob(t, ctx, stopID)
	if gconv.String(cancelled["status"]) != "cancelled" || gconv.Int(cancelled["updated"]) != 1 {
		t.Fatalf("cancel did not preserve first page: %#v", cancelled)
	}
	fetchCollectJobPage = func(context.Context, string, url.Values) (macPayload, error) {
		return macPayload{}, errors.New("controlled source timeout")
	}
	failed, err := startCollectJob(ctx, "manual", sourceID, 1, 0, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	failedID := gconv.Int64(failed["id"])
	jobs = append(jobs, failedID)
	failure := waitCollectJob(t, ctx, failedID)
	if gconv.String(failure["status"]) != "failed" || gconv.Int(failure["failed"]) != 1 || !strings.Contains(gconv.String(failure["error"]), "controlled source timeout") {
		t.Fatalf("source error is not visible: %#v", failure)
	}
}
