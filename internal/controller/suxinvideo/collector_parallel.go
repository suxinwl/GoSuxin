package suxinvideo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

const collectJobSourceTable = `CREATE TABLE IF NOT EXISTS sx_collect_job_source (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 job_id BIGINT UNSIGNED NOT NULL, source_id BIGINT NOT NULL,
 source_name VARCHAR(120) NOT NULL DEFAULT '', source_url TEXT NOT NULL,
 source_index INT NOT NULL DEFAULT 1, lease_key CHAR(64) NULL,
 status VARCHAR(16) NOT NULL DEFAULT 'queued',
 start_page INT NOT NULL DEFAULT 1, page INT NOT NULL DEFAULT 1, page_count INT NOT NULL DEFAULT 0,
 item INT NOT NULL DEFAULT 0, item_count INT NOT NULL DEFAULT 0,
 added INT NOT NULL DEFAULT 0, updated INT NOT NULL DEFAULT 0, failed INT NOT NULL DEFAULT 0, skipped INT NOT NULL DEFAULT 0,
 error TEXT, type_id INT NOT NULL DEFAULT 0, hours INT NOT NULL DEFAULT 0,
 one_page TINYINT NOT NULL DEFAULT 0, started_at BIGINT NOT NULL DEFAULT 0, updated_at BIGINT NOT NULL,
 UNIQUE KEY source_lease (lease_key), KEY job_sources (job_id,status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`

// Slots are shared across every parent task. Leases in MySQL additionally keep
// queued tasks and duplicate source records from fetching the same provider.
type collectWorkerSlots struct {
	mu      sync.Mutex
	active  int
	changed chan struct{}
}

func newCollectWorkerSlots() *collectWorkerSlots {
	return &collectWorkerSlots{changed: make(chan struct{})}
}

func (slots *collectWorkerSlots) acquire(ctx context.Context, limit func() int) bool {
	for {
		slots.mu.Lock()
		if ctx.Err() != nil {
			slots.mu.Unlock()
			return false
		}
		if slots.active < max(1, min(6, limit())) {
			slots.active++
			slots.mu.Unlock()
			return true
		}
		changed := slots.changed
		slots.mu.Unlock()
		select {
		case <-ctx.Done():
			return false
		case <-changed:
		case <-time.After(time.Second): // Notice a changed concurrency setting.
		}
	}
}

func (slots *collectWorkerSlots) release() {
	slots.mu.Lock()
	slots.active--
	close(slots.changed)
	slots.changed = make(chan struct{})
	slots.mu.Unlock()
}

var collectionSlots = newCollectWorkerSlots()
var collectTaskMu sync.Mutex
var collectTaskCancels = map[int64]context.CancelFunc{}

func collectSourceLease(raw string) (string, error) {
	// The existing 4kvm marker starts with a digit and is intentionally not an
	// RFC URL scheme; native provider descriptors need their own canonical form.
	for _, native := range []string{hongguoSourceURL, fourKVMSourceURL, erciyuanSourceURL, yqkSourceURL} {
		if strings.EqualFold(strings.TrimSpace(raw), native) {
			key := sha256.Sum256([]byte(native))
			return hex.EncodeToString(key[:]), nil
		}
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("采集源地址无效")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	if (parsed.Scheme == "https" && parsed.Port() == "443") || (parsed.Scheme == "http" && parsed.Port() == "80") {
		parsed.Host = parsed.Hostname()
		if strings.Contains(parsed.Host, ":") {
			parsed.Host = "[" + parsed.Host + "]"
		}
	}
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return "", fmt.Errorf("采集源地址参数无效")
	}
	parsed.RawQuery = query.Encode()
	key := sha256.Sum256([]byte(parsed.String()))
	return hex.EncodeToString(key[:]), nil
}

func collectSourceProgress(ctx context.Context, jobID int64) ([]row, error) {
	progress, err := all(ctx, "SELECT id,job_id,source_id,source_name,source_url,source_index,status,start_page,page,page_count,item,item_count,added,updated,failed,skipped,error,type_id,hours,one_page,started_at,updated_at FROM sx_collect_job_source WHERE job_id=? ORDER BY source_index,id", jobID)
	if err != nil {
		return nil, err
	}
	for _, source := range progress {
		source["dynamic_total"] = isYQKSource(gconv.String(source["source_url"]))
		delete(source, "source_url")
	}
	return progress, nil
}

func collectJobDetails(ctx context.Context, job row) (row, error) {
	if job == nil {
		return nil, nil
	}
	progress, err := collectSourceProgress(ctx, gconv.Int64(job["id"]))
	if err != nil {
		return nil, err
	}
	if progress == nil {
		progress = []row{}
	}
	job["source_progress"] = progress
	return job, nil
}

func collectJobListDetails(ctx context.Context, groups ...[]row) error {
	ids := map[int64]bool{}
	args := []any{}
	for _, jobs := range groups {
		for _, job := range jobs {
			id := gconv.Int64(job["id"])
			if id > 0 && !ids[id] {
				ids[id] = true
				args = append(args, id)
			}
		}
	}
	if len(args) == 0 {
		return nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(args)), ",")
	progress, err := all(ctx, "SELECT id,job_id,source_id,source_name,source_url,source_index,status,start_page,page,page_count,item,item_count,added,updated,failed,skipped,error,type_id,hours,one_page,started_at,updated_at FROM sx_collect_job_source WHERE job_id IN ("+placeholders+") ORDER BY job_id,source_index,id", args...)
	if err != nil {
		return err
	}
	byJob := map[int64][]row{}
	for _, source := range progress {
		source["dynamic_total"] = isYQKSource(gconv.String(source["source_url"]))
		delete(source, "source_url")
		id := gconv.Int64(source["job_id"])
		byJob[id] = append(byJob[id], source)
	}
	for _, jobs := range groups {
		for _, job := range jobs {
			sources := byJob[gconv.Int64(job["id"])]
			if sources == nil {
				sources = []row{}
			}
			job["source_progress"] = sources
		}
	}
	return nil
}

func collectJobSummary(ctx context.Context, jobID int64, currentSource ...int64) error {
	current := int64(0)
	if len(currentSource) > 0 {
		current = currentSource[0]
	}
	return execSQL(ctx, `UPDATE sx_collect_job j JOIN (
 SELECT job_id,COUNT(*) source_count,SUM(added) added,SUM(updated) updated,SUM(failed) failed,SUM(skipped) skipped
 FROM sx_collect_job_source WHERE job_id=? GROUP BY job_id
 ) s ON s.job_id=j.id LEFT JOIN sx_collect_job_source c ON c.id=? SET j.source_count=s.source_count,j.added=s.added,j.updated=s.updated,j.failed=s.failed,j.skipped=s.skipped,
 j.source_id=COALESCE(c.source_id,j.source_id),j.source_name=COALESCE(c.source_name,j.source_name),j.source_index=COALESCE(c.source_index,j.source_index),j.page=COALESCE(c.page,j.page),j.page_count=COALESCE(c.page_count,j.page_count),j.item=COALESCE(c.item,j.item),j.item_count=COALESCE(c.item_count,j.item_count),j.updated_at=?`, jobID, current, time.Now().Unix())
}

func collectSourceStillEnabled(ctx context.Context, jobID int64, source row) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	state, err := one(ctx, `SELECT j.cancel_requested,a.status,a.api_url FROM sx_collect_job j
 LEFT JOIN sx_collect_api a ON a.id=? WHERE j.id=?`, source["source_id"], jobID)
	if err != nil {
		return err
	}
	if state == nil || gconv.Int(state["cancel_requested"]) != 0 {
		return context.Canceled
	}
	if gconv.Int(state["status"]) != 1 {
		return fmt.Errorf("采集源已禁用，已停止该源")
	}
	current, err := collectSourceLease(gconv.String(state["api_url"]))
	previous, _ := collectSourceLease(gconv.String(source["source_url"]))
	if err != nil || current != previous {
		return fmt.Errorf("采集源地址已修改，已停止旧地址任务")
	}
	return nil
}

func collectSourceFinish(id int64, status, message string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := execSQL(ctx, "UPDATE sx_collect_job_source SET status=?,lease_key=NULL,error=?,updated_at=? WHERE id=?", status, message, time.Now().Unix(), id); err != nil {
		g.Log().Error(ctx, "CMS collection source completion:", err)
	}
}

func collectSourceRecord(ctx context.Context, sourceID int64, page, pageCount, item, itemCount int, deltaField, message string) error {
	extra := ""
	if deltaField != "" {
		extra = "," + deltaField + "=" + deltaField + "+1"
	}
	return execSQL(ctx, "UPDATE sx_collect_job_source SET page=?,page_count=?,item=?,item_count=?,updated_at=?,error=?"+extra+" WHERE id=?", page, pageCount, item, itemCount, time.Now().Unix(), message, sourceID)
}

func runCollectSource(ctx context.Context, jobID int64, source row) {
	id := gconv.Int64(source["id"])
	status, message := "complete", gconv.String(source["error"])
	defer func() {
		if recovered := recover(); recovered != nil {
			status, message = "failed", "采集内部错误，已停止该源"
		}
		collectSourceFinish(id, status, message)
	}()
	if !collectionSlots.acquire(ctx, func() int { return gconv.Int(setting(ctx, "collect_source_concurrency", "3")) }) {
		status, message = "cancelled", "任务已停止"
		return
	}
	defer collectionSlots.release()
	if err := collectSourceStillEnabled(ctx, jobID, source); err != nil {
		status, message = "cancelled", err.Error()
		return
	}
	if err := execSQL(ctx, "UPDATE sx_collect_job_source SET status='running',started_at=IF(started_at=0,?,started_at),updated_at=? WHERE id=? AND status='queued'", time.Now().Unix(), time.Now().Unix(), id); err != nil {
		status, message = "failed", "无法保存采集任务状态"
		return
	}
	raw := gconv.String(source["source_url"])
	checkSource := func() error { return collectSourceStillEnabled(ctx, jobID, source) }
	page, typeID, hours := max(1, gconv.Int(source["page"])), gconv.Int(source["type_id"]), gconv.Int(source["hours"])
	onePage := gconv.Int(source["one_page"]) != 0
	_ = execSQL(ctx, "UPDATE sx_collect_job SET status='running',source_id=?,source_name=?,source_index=?,hours=?,updated_at=? WHERE id=?", source["source_id"], source["source_name"], source["source_index"], hours, time.Now().Unix(), jobID)
	for page <= 10000 {
		if err := collectSourceStillEnabled(ctx, jobID, source); err != nil {
			status, message = "cancelled", err.Error()
			return
		}
		query := url.Values{"ac": {"videolist"}, "pg": {fmt.Sprint(page)}}
		if typeID > 0 {
			query.Set("t", fmt.Sprint(typeID))
		}
		if hours > 0 {
			query.Set("h", fmt.Sprint(hours))
		}
		data, err := fetchCollectJobWithRetryCheck(ctx, raw, query, checkSource)
		if err != nil {
			status = "failed"
			message = appendCollectJobError(message, fmt.Sprintf("第 %d 页：%v", page, err))
			if ctx.Err() != nil || checkSource() != nil {
				status = "cancelled"
			}
			_ = collectSourceRecord(ctx, id, page, 0, 0, 0, "failed", message)
			return
		}
		pageCount := max(page, min(10000, data.PageCount))
		if err := collectSourceRecord(ctx, id, page, pageCount, 0, len(data.List), "", message); err != nil {
			status, message = "failed", "无法保存采集进度"
			return
		}
		_ = execSQL(ctx, "UPDATE sx_collect_job SET page=?,page_count=?,item=0,item_count=?,updated_at=? WHERE id=?", page, pageCount, len(data.List), time.Now().Unix(), jobID)
		for itemIndex, item := range data.List {
			if err := collectSourceStillEnabled(ctx, jobID, source); err != nil {
				status, message = "cancelled", err.Error()
				return
			}
			delta := ""
			if !contentMacAllowed(ctx, item) {
				delta = "skipped"
			} else {
				var detailError error
				if strings.TrimSpace(gconv.String(item["vod_play_url"])) == "" && gconv.String(item["vod_id"]) != "" {
					if !pauseCollectJob(ctx) {
						status, message = "cancelled", "任务已停止"
						return
					}
					if err := collectSourceStillEnabled(ctx, jobID, source); err != nil {
						status, message = "cancelled", err.Error()
						return
					}
					detail, detailErr := fetchCollectJobWithRetryCheck(ctx, raw, url.Values{"ac": {"detail"}, "ids": {gconv.String(item["vod_id"])}}, checkSource)
					if detailErr == nil && len(detail.List) > 0 {
						item = collectedDetailItem(item, detail.List[0])
					}
					detailError = detailErr
				}
				if err := checkSource(); err != nil {
					status, message = "cancelled", err.Error()
					return
				}
				if !contentMacAllowed(ctx, item) {
					delta = "skipped"
				} else {
					state, itemErr := 0, error(nil)
					if missing := requiredCollectPlaylistError(raw, item); missing != nil {
						itemErr = missing
						if detailError != nil {
							itemErr = fmt.Errorf("影片详情获取失败：%w", detailError)
						}
					} else {
						state, itemErr = saveCollectJobVod(ctx, gconv.Int64(source["source_id"]), item)
					}
					switch {
					case itemErr != nil:
						delta = "failed"
						message = appendCollectJobError(message, fmt.Sprintf("第 %d 页 %v：%v", page, item["vod_name"], itemErr))
					case state == collectVodBlocked:
						delta = "skipped"
					case state == 1:
						delta = "added"
					case state == 2:
						delta = "updated"
					}
				}
			}
			if err := collectSourceRecord(ctx, id, page, pageCount, itemIndex+1, len(data.List), delta, message); err != nil {
				status, message = "failed", "无法保存采集进度"
				return
			}
			if err := collectJobSummary(ctx, jobID, id); err != nil {
				status, message = "failed", "无法保存采集统计"
				return
			}
		}
		if collectJobPageFinished(raw, page, data, onePage) {
			if !onePage && page == 10000 && data.PageCount > page {
				status, message = "interrupted", appendCollectJobError(message, "达到页数上限，尚未完成全量更新")
			}
			if status == "complete" && message != "" {
				status = "partial"
			}
			return
		}
		page++
		// Persist the next page before waiting, so restart replays at most the
		// current page while keeping every previously stored film and count.
		if err := collectSourceRecord(ctx, id, page, pageCount, 0, 0, "", message); err != nil {
			status, message = "failed", "无法保存采集进度"
			return
		}
		if !pauseCollectJob(ctx) {
			status, message = "cancelled", "任务已停止"
			return
		}
	}
}
