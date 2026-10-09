package suxinvideo

import (
	"context"
	"fmt"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

// This runs once at service startup, before any new task can claim a lease.
// Only active manual work resumes; automatic work is picked up by its normal
// schedule. Completed rows, explicit stops and administrator source choices
// remain untouched.
func recoverCollectJobs(ctx context.Context) ([]int64, error) {
	jobs, err := all(ctx, "SELECT * FROM sx_collect_job WHERE status IN ('queued','running') ORDER BY id")
	if err != nil {
		return nil, err
	}
	var resume []int64
	now := time.Now().Unix()
	for _, job := range jobs {
		id := gconv.Int64(job["id"])
		children, err := all(ctx, "SELECT * FROM sx_collect_job_source WHERE job_id=? ORDER BY source_index,id", id)
		if err != nil {
			return nil, err
		}
		manual := gconv.String(job["mode"]) == "manual" && gconv.Int(job["cancel_requested"]) == 0
		if len(children) == 0 && manual {
			// Convert the original global singleton task without changing its id,
			// filters, current page or accumulated counters.
			params, resumeErr := manualCollectResumeParameters(job, now)
			if resumeErr != nil {
				g.Log().Warning(ctx, "CMS collection resume skipped:", id, resumeErr)
			}
			if params != nil && resumeErr == nil {
				source, err := one(ctx, "SELECT id,name,api_url FROM sx_collect_api WHERE id=? AND status=1", params.SourceID)
				if err != nil {
					return nil, err
				}
				if source != nil {
					key, err := collectSourceLease(gconv.String(source["api_url"]))
					if err == nil {
						result, err := g.DB().Exec(ctx, `INSERT IGNORE INTO sx_collect_job_source(job_id,source_id,source_name,source_url,source_index,lease_key,status,start_page,page,page_count,item,item_count,added,updated,failed,skipped,error,type_id,hours,one_page,started_at,updated_at) VALUES(?,?,?,?,1,?,'queued',?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, source["id"], source["name"], source["api_url"], key, max(1, gconv.Int(job["start_page"])), params.Page, job["page_count"], job["item"], job["item_count"], job["added"], job["updated"], job["failed"], job["skipped"], job["error"], params.TypeID, params.Hours, params.OnePage, job["started_at"], now)
						if err != nil {
							return nil, err
						}
						affected, err := result.RowsAffected()
						if err != nil {
							return nil, err
						}
						if affected > 0 {
							children, err = all(ctx, "SELECT * FROM sx_collect_job_source WHERE job_id=?", id)
							if err != nil {
								return nil, err
							}
						}
					}
				}
			}
		}
		updated := gconv.Int64(job["updated_at"])
		fresh := gconv.String(job["status"]) == "queued" || (updated > 0 && updated <= now && now-updated <= 10*60)
		if manual && fresh && len(children) > 0 {
			pending := 0
			for _, source := range children {
				state := gconv.String(source["status"])
				if state != "running" && state != "queued" {
					continue
				}
				if err := collectSourceStillEnabled(ctx, id, source); err != nil {
					if err := execSQL(ctx, "UPDATE sx_collect_job_source SET status='cancelled',lease_key=NULL,error=?,updated_at=? WHERE id=?", err.Error(), now, source["id"]); err != nil {
						return nil, err
					}
					continue
				}
				if err := execSQL(ctx, "UPDATE sx_collect_job_source SET status='queued',updated_at=? WHERE id=?", now, source["id"]); err != nil {
					return nil, err
				}
				pending++
			}
			if pending > 0 {
				if err := execSQL(ctx, "UPDATE sx_collect_job SET status='queued',active_key=NULL,updated_at=? WHERE id=?", now, id); err != nil {
					return nil, err
				}
				resume = append(resume, id)
				g.Log().Info(ctx, "CMS collection resumes existing task:", id)
				continue
			}
		}
		status, message := "interrupted", "服务重启，自动任务等待下次计划执行"
		if gconv.Int(job["cancel_requested"]) != 0 {
			status, message = "cancelled", "任务已停止"
		} else if manual {
			message = "服务重启，任务进度未满足安全恢复条件"
		}
		if err := execSQL(ctx, "UPDATE sx_collect_job_source SET status=?,lease_key=NULL,error=?,updated_at=? WHERE job_id=? AND status IN ('queued','running')", status, message, now, id); err != nil {
			return nil, err
		}
		if len(children) > 0 {
			_ = collectJobSummary(ctx, id)
		}
		if err := execSQL(ctx, "UPDATE sx_collect_job SET status=?,active_key=NULL,error=?,updated_at=? WHERE id=?", status, message, now, id); err != nil {
			return nil, fmt.Errorf("采集任务恢复状态保存失败：%w", err)
		}
	}
	return resume, nil
}
