package suxinvideo

import (
	"context"
	"errors"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

// LiveSeedResult contains identifiers and counters only, never source secrets.
type LiveSeedResult struct {
	Channels  int     `json:"channels"`
	Streams   int     `json:"streams"`
	Added     int     `json:"added"`
	Updated   int     `json:"updated"`
	Failed    int     `json:"failed"`
	JobID     int64   `json:"job_id"`
	StreamIDs []int64 `json:"stream_ids"`
}

// SeedLiveChannels is a local maintenance entry point for an already vetted
// playlist. It shares admission checks, upserts and probes with admin imports.
// Existing channel groups and all manually edited properties are preserved.
func SeedLiveChannels(ctx context.Context, content, group string) (LiveSeedResult, error) {
	var result LiveSeedResult
	if err := ensureLiveSchema(ctx); err != nil {
		return result, errors.New("直播初始化失败")
	}
	items, preview, err := liveParseImport(content, group)
	if err != nil {
		return result, err
	}
	for i := range items {
		existing, _, err := liveFindImportChannel(ctx, items[i])
		if err != nil {
			return result, errors.New("读取既有频道失败")
		}
		if existing != nil {
			items[i].GroupID = gconv.Int64(existing["group_id"])
			group, err := one(ctx, "SELECT name FROM sx_live_group WHERE id=?", existing["group_id"])
			if err != nil {
				return result, errors.New("读取既有分组失败")
			}
			if group != nil && gconv.String(group["name"]) != "" {
				items[i].Group = gconv.String(group["name"])
			}
		}
	}
	now := time.Now().Unix()
	job, err := g.DB().Exec(ctx, "INSERT INTO sx_live_job(status,kind,message,created,updated) VALUES('running','local_seed','正在导入本机已验证的 AVC 直播备用',?,?)", now, now)
	if err != nil {
		return result, errors.New("创建本机直播导入记录失败")
	}
	result.JobID, _ = job.LastInsertId()
	p := &liveProgress{ID: result.JobID}
	err = liveImportItems(ctx, p, items, 0)
	status, message := "completed", "本机已验证的直播备用导入完成"
	if p.Failed > 0 {
		status, message = "partial", "已导入；检测失败的备用保留供重试"
	}
	if err != nil {
		status, message = "failed", "本机直播导入中断；已导入数据保留"
	}
	_ = execSQL(context.Background(), "UPDATE sx_live_job SET status=?,total=?,processed=?,added=?,updated_count=?,failed=?,message=?,updated=? WHERE id=?", status, p.Total, p.Processed, p.Added, p.Updated, p.Failed, message, time.Now().Unix(), result.JobID)
	result.Channels, result.Streams, result.Added, result.Updated, result.Failed = preview.Channels, preview.Streams, p.Added, p.Updated, p.Failed
	seen := map[int64]bool{}
	for _, item := range items {
		channel, _, lookupErr := liveFindImportChannel(ctx, item)
		if lookupErr != nil || channel == nil {
			continue
		}
		stream, lookupErr := one(ctx, "SELECT id FROM sx_live_stream WHERE channel_id=? AND url_hash=?", channel["id"], liveIdentity(item.URL))
		if lookupErr != nil || stream == nil {
			continue
		}
		id := gconv.Int64(stream["id"])
		if !seen[id] {
			seen[id] = true
			result.StreamIDs = append(result.StreamIDs, id)
			// Vetted AVC backups precede older HEVC browser-incompatible streams.
			// Explicit administrator priorities retain their meaning.
			_ = execSQL(ctx, "UPDATE sx_live_stream SET priority=100 WHERE id=? AND manual_edited=0 AND priority<100 AND health='healthy'", id)
		}
	}
	if err != nil {
		return result, errors.New(message)
	}
	return result, nil
}
