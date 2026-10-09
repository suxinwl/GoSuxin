package suxinvideo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"github.com/suxinwl/GoSuxin/utility/gf"
)

var collectJobInit sync.Mutex
var collectJobReady bool
var fetchCollectJobPage = fetchCollectSource
var saveCollectJobVod = upsertMacVod
var pauseCollectJob = collectJobDelay

const collectJobTable = `CREATE TABLE IF NOT EXISTS sx_collect_job (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, mode VARCHAR(16) NOT NULL,
 status VARCHAR(16) NOT NULL DEFAULT 'running', active_key TINYINT NULL,
 source_id BIGINT NOT NULL DEFAULT 0, source_name VARCHAR(120) NOT NULL DEFAULT '',
 source_index INT NOT NULL DEFAULT 0, source_count INT NOT NULL DEFAULT 0,
 page INT NOT NULL DEFAULT 0, page_count INT NOT NULL DEFAULT 0,
 item INT NOT NULL DEFAULT 0, item_count INT NOT NULL DEFAULT 0,
 added INT NOT NULL DEFAULT 0, updated INT NOT NULL DEFAULT 0, failed INT NOT NULL DEFAULT 0, skipped INT NOT NULL DEFAULT 0,
 error TEXT, type_id INT NOT NULL DEFAULT 0, hours INT NOT NULL DEFAULT 0,
 start_page INT NOT NULL DEFAULT 1, one_page TINYINT NOT NULL DEFAULT 0,
 cancel_requested TINYINT NOT NULL DEFAULT 0, started_at BIGINT NOT NULL, updated_at BIGINT NOT NULL,
 UNIQUE KEY one_active_job (active_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`

func prepareCollectJobs(ctx context.Context) error {
	collectJobInit.Lock()
	defer collectJobInit.Unlock()
	if collectJobReady {
		return nil
	}
	if err := execSQL(ctx, collectJobTable); err != nil {
		return err
	}
	column, err := one(ctx, "SELECT COUNT(*) n FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='sx_collect_job' AND column_name='skipped'")
	if err != nil {
		return err
	}
	if gconv.Int(column["n"]) == 0 {
		if err := execSQL(ctx, "ALTER TABLE sx_collect_job ADD COLUMN skipped INT NOT NULL DEFAULT 0 AFTER failed"); err != nil {
			return err
		}
	}
	if err := execSQL(ctx, collectJobSourceTable); err != nil {
		return err
	}
	if err := execSQL(ctx, "INSERT IGNORE INTO sx_config(`key`,`value`) VALUES('collect_source_concurrency','3')"); err != nil {
		return err
	}
	resume, err := recoverCollectJobs(ctx)
	if err != nil {
		return err
	}
	collectJobReady = true
	for _, id := range resume {
		go runCollectJob(id)
	}
	return nil
}

type CollectJobStartReq struct {
	g.Meta  `path:"/collect/job/start" method:"post"`
	APIID   int64 `p:"api_id"`
	Page    int   `p:"page"`
	TypeID  int   `p:"type_id"`
	Hours   int   `p:"hours"`
	OnePage bool  `p:"one_page"`
}
type CollectJobStartRes struct{}
type CollectJobStatusReq struct {
	g.Meta `path:"/collect/job/status" method:"get"`
	ID     int64 `p:"id"`
}
type CollectJobStatusRes struct{}
type CollectJobCancelReq struct {
	g.Meta `path:"/collect/job/cancel" method:"post"`
	ID     int64 `p:"id"`
}
type CollectJobCancelRes struct{}

func (*Admin) CollectJobStart(ctx context.Context, req *CollectJobStartReq) (*CollectJobStartRes, error) {
	if req.APIID < 1 || req.Page < 0 || req.Page > 10000 || req.TypeID < 0 || req.Hours < 0 || req.Hours > 720 {
		badRequest(g.RequestFromCtx(ctx), "采集参数无效")
		return &CollectJobStartRes{}, nil
	}
	job, err := startCollectJob(ctx, "manual", req.APIID, max(1, req.Page), req.TypeID, req.Hours, req.OnePage)
	if err != nil {
		badRequest(g.RequestFromCtx(ctx), err.Error())
		return &CollectJobStartRes{}, nil
	}
	g.RequestFromCtx(ctx).Response.WriteJson(gf.Success().SetData(job))
	return &CollectJobStartRes{}, nil
}

func (*Admin) CollectJobStatus(ctx context.Context, req *CollectJobStatusReq) (*CollectJobStatusRes, error) {
	if err := prepareCollectJobs(ctx); err != nil {
		return nil, err
	}
	query := "SELECT * FROM sx_collect_job ORDER BY id DESC LIMIT 1"
	args := []any{}
	if req.ID > 0 {
		query = "SELECT * FROM sx_collect_job WHERE id=?"
		args = append(args, req.ID)
	}
	job, err := one(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	recent, err := all(ctx, "SELECT * FROM sx_collect_job ORDER BY id DESC LIMIT 20")
	if err != nil {
		return nil, err
	}
	running, err := all(ctx, "SELECT * FROM sx_collect_job WHERE status IN ('queued','running') ORDER BY id")
	if err != nil {
		return nil, err
	}
	if job == nil {
		job = row{}
	}
	if err := collectJobListDetails(ctx, []row{job}, recent, running); err != nil {
		return nil, err
	}
	if recent == nil {
		recent = []row{}
	}
	if running == nil {
		running = []row{}
	}
	job["jobs"], job["running_jobs"] = recent, running
	g.RequestFromCtx(ctx).Response.WriteJson(gf.Success().SetData(job))
	return &CollectJobStatusRes{}, nil
}

func (*Admin) CollectJobCancel(ctx context.Context, req *CollectJobCancelReq) (*CollectJobCancelRes, error) {
	if req.ID < 1 {
		badRequest(g.RequestFromCtx(ctx), "任务编号无效")
		return &CollectJobCancelRes{}, nil
	}
	err := execSQL(ctx, "UPDATE sx_collect_job SET cancel_requested=1,updated_at=? WHERE id=? AND status IN ('queued','running')", time.Now().Unix(), req.ID)
	if err != nil {
		return nil, err
	}
	collectTaskMu.Lock()
	if cancel := collectTaskCancels[req.ID]; cancel != nil {
		cancel()
	}
	collectTaskMu.Unlock()
	g.RequestFromCtx(ctx).Response.WriteJson(gf.Success().SetData(map[string]any{"id": req.ID, "cancel_requested": true}))
	return &CollectJobCancelRes{}, nil
}

func startAutoCollectJob(ctx context.Context, force bool) (row, error) {
	if !force && setting(ctx, "collect_auto_enable", "0") != "1" {
		return nil, nil
	}
	if !force {
		interval := gconv.Int64(setting(ctx, "collect_auto_interval", "60"))
		if interval < 5 {
			interval = 5
		}
		last := gconv.Int64(setting(ctx, "collect_last_auto", "0"))
		if time.Now().Unix()-last < interval*60 {
			return nil, nil
		}
	}
	mode := "scheduled"
	if force {
		mode = "auto"
	}
	return startCollectJob(ctx, mode, 0, 1, 0, 0, true)
}

var collectJobStartMu sync.Mutex

func startCollectJob(ctx context.Context, mode string, apiID int64, page, typeID, hours int, onePage bool) (row, error) {
	if err := prepareCollectJobs(ctx); err != nil {
		return nil, err
	}
	collectJobStartMu.Lock()
	defer collectJobStartMu.Unlock()
	if mode != "manual" {
		active, err := one(ctx, "SELECT id FROM sx_collect_job WHERE mode IN ('auto','scheduled') AND status IN ('queued','running') AND cancel_requested=0 LIMIT 1")
		if err != nil {
			return nil, err
		}
		if active != nil {
			if mode == "scheduled" {
				return nil, nil
			}
			return nil, fmt.Errorf("已有自动采集任务 #%v 正在执行", active["id"])
		}
	}
	var sources []row
	var err error
	if apiID > 0 {
		sources, err = all(ctx, "SELECT id,name,api_url,collect_hours FROM sx_collect_api WHERE id=? AND status=1", apiID)
	} else {
		sources, err = all(ctx, "SELECT id,name,api_url,collect_hours FROM sx_collect_api WHERE status=1 AND collect_auto=1 ORDER BY id")
	}
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, errors.New("没有已启用的采集源")
	}
	now := time.Now().Unix()
	var id int64
	err = g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		result, err := tx.Exec("INSERT INTO sx_collect_job(mode,status,active_key,source_count,start_page,type_id,hours,one_page,started_at,updated_at) VALUES(?,'queued',NULL,0,?,?,?,?,?,?)", mode, page, typeID, hours, onePage, now, now)
		if err != nil {
			return err
		}
		id, err = result.LastInsertId()
		if err != nil {
			return err
		}
		claimed := 0
		for _, source := range sources {
			key, err := collectSourceLease(gconv.String(source["api_url"]))
			if err != nil {
				if apiID > 0 {
					return err
				}
				continue
			}
			sourceHours := hours
			if mode != "manual" {
				sourceHours = gconv.Int(source["collect_hours"])
			}
			sourceHours, sourceOnePage := erciyuanCollectJobScope(mode, gconv.String(source["api_url"]), sourceHours, onePage)
			result, err := tx.Exec(`INSERT IGNORE INTO sx_collect_job_source(job_id,source_id,source_name,source_url,source_index,lease_key,status,start_page,page,type_id,hours,one_page,updated_at) VALUES(?,?,?,?,?,?,'queued',?,?,?,?,?,?)`, id, source["id"], source["name"], source["api_url"], claimed+1, key, page, page, typeID, sourceHours, sourceOnePage, now)
			if err != nil {
				return err
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if affected > 0 {
				claimed++
			} else if apiID > 0 {
				return errors.New("该采集源或相同接口已有任务正在执行，请查看进度")
			}
		}
		if claimed == 0 {
			return errors.New("所有启用的采集源均已有任务正在执行，没有空闲采集源")
		}
		_, err = tx.Exec("UPDATE sx_collect_job SET source_count=? WHERE id=?", claimed, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	job, err := one(ctx, "SELECT * FROM sx_collect_job WHERE id=?", id)
	if err != nil {
		return nil, err
	}
	job, err = collectJobDetails(ctx, job)
	if err != nil {
		return nil, err
	}
	if mode != "manual" {
		_ = execSQL(ctx, "INSERT INTO sx_config(`key`,`value`) VALUES('collect_last_auto',?) ON DUPLICATE KEY UPDATE `value`=VALUES(`value`)", fmt.Sprint(now))
	}
	go runCollectJob(id)
	return job, nil
}

func collectJobCancelled(ctx context.Context, id int64) bool {
	job, err := one(ctx, "SELECT cancel_requested FROM sx_collect_job WHERE id=?", id)
	return err != nil || job == nil || gconv.Int(job["cancel_requested"]) != 0
}

func runCollectJob(id int64) {
	initCtx, initCancel := context.WithTimeout(context.Background(), 10*time.Second)
	job, err := one(initCtx, "SELECT * FROM sx_collect_job WHERE id=? AND status IN ('queued','running')", id)
	sources, sourceErr := all(initCtx, "SELECT * FROM sx_collect_job_source WHERE job_id=? AND status IN ('queued','running') ORDER BY source_index,id", id)
	initCancel()
	if err != nil || sourceErr != nil || job == nil {
		return
	}
	mode := gconv.String(job["mode"])
	ctx, cancel := collectJobContext(context.Background(), mode, gconv.Int(job["one_page"]) != 0)
	collectTaskMu.Lock()
	if _, exists := collectTaskCancels[id]; exists {
		collectTaskMu.Unlock()
		cancel()
		return
	}
	collectTaskCancels[id] = cancel
	collectTaskMu.Unlock()
	defer func() { cancel(); collectTaskMu.Lock(); delete(collectTaskCancels, id); collectTaskMu.Unlock() }()
	if gconv.Int(job["cancel_requested"]) != 0 {
		cancel()
	}
	var workers sync.WaitGroup
	for _, source := range sources {
		workers.Add(1)
		go func(source row) { defer workers.Done(); runCollectSource(ctx, id, source) }(source)
	}
	workers.Wait()
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer finishCancel()
	_ = collectJobSummary(finishCtx, id)
	sourceProgress, _ := collectSourceProgress(finishCtx, id)
	status, firstError := collectCompletedJobStatus(sourceProgress, collectJobCancelled(finishCtx, id), ctx.Err())
	_ = execSQL(finishCtx, "UPDATE sx_collect_job SET status=?,active_key=NULL,error=?,updated_at=? WHERE id=?", status, firstError, time.Now().Unix(), id)
	if mode != "manual" {
		completed, _ := one(finishCtx, "SELECT * FROM sx_collect_job WHERE id=?", id)
		completed, _ = collectJobDetails(finishCtx, completed)
		encoded, _ := json.Marshal(completed)
		_ = execSQL(finishCtx, "INSERT INTO sx_config(`key`,`value`) VALUES('collect_last_result',?) ON DUPLICATE KEY UPDATE `value`=VALUES(`value`)", string(encoded))
	}
	completed, _ := one(finishCtx, "SELECT added FROM sx_collect_job WHERE id=?", id)
	if gconv.Int(completed["added"]) > 0 {
		if err := pushNewVodToBaidu(finishCtx, gconv.Int64(job["started_at"])); err != nil {
			g.Log().Warning(finishCtx, "CMS Baidu URL submission:", err)
		}
	}
}

func collectCompletedJobStatus(sources []row, cancelled bool, ctxErr error) (string, string) {
	status, firstError := "complete", ""
	failed, succeeded := 0, 0
	for _, source := range sources {
		if errorText := gconv.String(source["error"]); errorText != "" {
			firstError = appendCollectJobError(firstError, fmt.Sprintf("%v：%s", source["source_name"], errorText))
		}
		switch gconv.String(source["status"]) {
		case "complete":
			succeeded++
		case "partial":
			succeeded++
			failed++
		default:
			failed++
		}
	}
	if cancelled || ctxErr == context.Canceled {
		status = "cancelled"
	} else if ctxErr != nil {
		status = "interrupted"
		firstError = appendCollectJobError(firstError, "采集超过最长运行时间")
	} else if failed > 0 {
		if succeeded > 0 {
			status = "partial"
		} else {
			status = "failed"
		}
	}
	return status, firstError
}

func collectJobPageFinished(raw string, page int, data macPayload, onePage bool) bool {
	if onePage || page >= min(10000, data.PageCount) {
		return true
	}
	// Cursor feeds may return an empty filtered page while still providing a
	// valid next cursor. Only their explicit hasNext/pagecount ends the scan.
	return len(data.List) == 0 && !isYQKSource(raw) && !isErciyuanSource(raw)
}

// Full manual collection may need many hours. Each provider request already
// has a bounded timeout; explicit cancellation and the unique active task are
// still checked between items. Scheduled and single-page jobs retain a limit.
func collectJobContext(parent context.Context, mode string, onePage bool) (context.Context, context.CancelFunc) {
	if mode == "manual" && !onePage {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, 2*time.Hour)
}

// A transient upstream failure must not end an entire sequential cursor scan.
// Retries use the configured collection delay and never repeat a DB write.
func fetchCollectJobWithRetry(ctx context.Context, raw string, query url.Values) (macPayload, error) {
	return fetchCollectJobWithRetryCheck(ctx, raw, query, nil)
}

func fetchCollectJobWithRetryCheck(ctx context.Context, raw string, query url.Values, check func() error) (macPayload, error) {
	var data macPayload
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil {
			return data, ctx.Err()
		}
		if check != nil {
			if err := check(); err != nil {
				return data, err
			}
		}
		data, err = fetchCollectJobPage(ctx, raw, query)
		if err == nil {
			return data, nil
		}
		if attempt == 2 || !pauseCollectJob(ctx) {
			break
		}
	}
	return data, err
}

func collectJobDelay(ctx context.Context) bool {
	delay := map[string]time.Duration{"gentle": 2500, "slow": 1500, "normal": 800, "fast": 200}
	speed := setting(ctx, "collect_speed", "gentle")
	wait, ok := delay[speed]
	if !ok {
		wait = delay["gentle"]
	}
	if wait == 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(wait * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func appendCollectJobError(current, next string) string {
	if current != "" {
		current += "；"
	}
	value := []rune(current + next)
	if len(value) > 1000 {
		value = value[:1000]
	}
	return string(value)
}
