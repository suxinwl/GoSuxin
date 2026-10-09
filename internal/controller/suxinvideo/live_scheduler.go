package suxinvideo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

var liveJobs = struct {
	sync.Mutex
	active int64
}{}

type liveProgress struct {
	ID                                       int64
	Total, Processed, Added, Updated, Failed int
	Message                                  string
}

func (p *liveProgress) persist(ctx context.Context) error {
	return execSQL(ctx, "UPDATE sx_live_job SET total=?,processed=?,added=?,updated_count=?,failed=?,message=?,updated=? WHERE id=?", p.Total, p.Processed, p.Added, p.Updated, p.Failed, cutRunes(p.Message, 500), time.Now().Unix(), p.ID)
}
func startLiveScheduler(ctx context.Context) {
	go func() {
		_, _ = liveStartJob(ctx, "refresh", func(ctx context.Context, p *liveProgress) error { return liveRefreshSubscriptions(ctx, p, 0, false) })
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if started, err := liveScheduleProviderSync(ctx); started || err != nil {
				continue
			}
			rows, err := all(ctx, "SELECT id FROM sx_live_subscription WHERE enabled=1 AND last_refresh<? LIMIT 1", time.Now().Add(-6*time.Hour).Unix())
			if err != nil {
				continue
			}
			if len(rows) == 0 {
				rows, err = liveStaleStreams(ctx, true)
				if err != nil || len(rows) == 0 {
					continue
				}
			}
			_, _ = liveStartJob(ctx, "refresh", func(ctx context.Context, p *liveProgress) error { return liveRefreshSubscriptions(ctx, p, 0, false) })
		}
	}()
}

func liveStartJob(ctx context.Context, kind string, work func(context.Context, *liveProgress) error) (int64, error) {
	liveJobs.Lock()
	defer liveJobs.Unlock()
	if liveJobs.active != 0 {
		return liveJobs.active, errors.New("已有直播导入或刷新任务正在执行，请先查看进度")
	}
	result, err := g.DB().Exec(ctx, "INSERT INTO sx_live_job(status,kind,created,updated) VALUES('pending',?,?,?)", kind, time.Now().Unix(), time.Now().Unix())
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	liveJobs.active = id
	go func() {
		defer func() { liveJobs.Lock(); liveJobs.active = 0; liveJobs.Unlock() }()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		_ = execSQL(ctx, "UPDATE sx_live_job SET status='running',message='正在读取频道列表并检测直播源',updated=? WHERE id=?", time.Now().Unix(), id)
		progress := &liveProgress{ID: id}
		err := work(ctx, progress)
		status := "completed"
		message := "直播频道导入和检测已完成"
		if kind == "provider_catalog_sync" {
			message = "直播来源目录与节目单同步已完成"
		}
		if err != nil {
			status = "failed"
			message = err.Error()
		}
		if progress.Failed > 0 && status == "completed" {
			status = "partial"
			message = "已完成；部分直播源暂不可用，数据已保留"
			if kind == "provider_catalog_sync" {
				message = "部分目录记录未能同步；已有数据保留"
			}
		}
		if ctx.Err() != nil {
			status = "interrupted"
			message = "任务达到时间上限；已导入频道保留，可再次刷新"
		}
		_ = execSQL(context.Background(), "UPDATE sx_live_job SET status=?,total=?,processed=?,added=?,updated_count=?,failed=?,message=?,updated=? WHERE id=?", status, progress.Total, progress.Processed, progress.Added, progress.Updated, progress.Failed, cutRunes(message, 500), time.Now().Unix(), id)
	}()
	return id, nil
}

func liveUpsertImport(ctx context.Context, item liveImportItem, subscriptionID int64) (int64, bool, error) {
	now := time.Now().Unix()
	identity := liveChannelIdentity(item)
	channel, preserveCatalog, err := liveFindImportChannel(ctx, item)
	if err != nil {
		return 0, false, err
	}
	group := row{}
	tvgID := item.TVGID
	if tvgID == "" && channel != nil {
		tvgID = gconv.String(channel["tvg_id"])
	}
	layout, systemLayout := liveSystemChannelLayout(tvgID)
	manualChannel := channel != nil && gconv.Int(channel["manual_edited"]) != 0
	systemLayout = systemLayout && !manualChannel
	if manualChannel || (preserveCatalog && !systemLayout) {
		group["id"] = channel["group_id"]
	} else if systemLayout {
		group, err = liveEnsureSystemGroup(ctx, layout)
		if err != nil {
			return 0, false, err
		}
	} else if item.GroupID > 0 {
		group, err = one(ctx, "SELECT id FROM sx_live_group WHERE id=?", item.GroupID)
		if err != nil || group == nil {
			return 0, false, errors.New("已有直播分组不可用")
		}
	} else {
		group, err = liveFindImportGroup(ctx, item.Group)
		if err != nil {
			return 0, false, err
		}
		if group == nil {
			groupKey := liveGroupIdentity(item.Group)
			if err := execSQL(ctx, "INSERT INTO sx_live_group(name,identity_key,created,updated) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE updated=VALUES(updated)", liveGroupDisplayName(item.Group), groupKey, now, now); err != nil {
				return 0, false, err
			}
			group, err = one(ctx, "SELECT id FROM sx_live_group WHERE identity_key=?", groupKey)
			if err != nil {
				return 0, false, err
			}
		}
	}
	if channel == nil {
		order := 0
		if systemLayout {
			order = layout.Sort
		}
		if err = execSQL(ctx, "INSERT INTO sx_live_channel(tvg_id,identity_key,name,logo,group_id,sort,created,updated) VALUES(?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE updated=VALUES(updated)", item.TVGID, identity, item.Name, item.Logo, group["id"], order, now, now); err != nil {
			return 0, false, err
		}
		channel, err = one(ctx, "SELECT id FROM sx_live_channel WHERE identity_key=?", identity)
		if err != nil {
			return 0, false, err
		}
	} else if !preserveCatalog {
		if err = execSQL(ctx, "UPDATE sx_live_channel SET tvg_id=IF(manual_edited=0 AND tvg_id='',?,tvg_id),name=IF(manual_edited=0,?,name),logo=IF(manual_edited=0 AND ?<>'',?,logo),group_id=IF(manual_edited=0,?,group_id),updated=? WHERE id=?", item.TVGID, item.Name, item.Logo, item.Logo, group["id"], now, channel["id"]); err != nil {
			return 0, false, err
		}
	}
	if systemLayout {
		if err = execSQL(ctx, "UPDATE sx_live_channel SET group_id=?,sort=? WHERE id=? AND manual_edited=0", group["id"], layout.Sort, channel["id"]); err != nil {
			return 0, false, err
		}
	}
	urlHash := liveIdentity(item.URL)
	existing, err := one(ctx, "SELECT id FROM sx_live_stream WHERE channel_id=? AND url_hash=?", channel["id"], urlHash)
	if err != nil {
		return 0, false, err
	}
	headers, _ := json.Marshal(item.Headers)
	if err = execSQL(ctx, `INSERT INTO sx_live_stream(channel_id,subscription_id,name,url,url_hash,headers_json,quality,created,updated)
 VALUES(?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE name=IF(manual_edited=0,VALUES(name),name),headers_json=IF(manual_edited=0,VALUES(headers_json),headers_json),
 quality=IF(manual_edited=0 AND VALUES(quality)<>'',VALUES(quality),quality),updated=VALUES(updated)`, channel["id"], subscriptionID, item.Name, item.URL, urlHash, string(headers), item.Quality, now, now); err != nil {
		return 0, false, err
	}
	r, err := one(ctx, "SELECT id FROM sx_live_stream WHERE channel_id=? AND url_hash=?", channel["id"], urlHash)
	if err != nil {
		return 0, false, err
	}
	return gconv.Int64(r["id"]), existing == nil, nil
}

func liveImportNameKey(name string) string {
	return strings.ToLower(strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, name))
}

// Missing TVG identifiers may use administrator aliases, but ambiguity never
// chooses an arbitrary channel. Matching a name/alias preserves its catalogue
// identity, name, group and disabled state rather than adopting source labels.
func liveFindImportChannel(ctx context.Context, item liveImportItem) (row, bool, error) {
	if item.TVGID != "" {
		exact, err := one(ctx, "SELECT id,identity_key,tvg_id,name,group_id,manual_edited FROM sx_live_channel WHERE identity_key=?", liveChannelIdentity(item))
		if err != nil || exact != nil {
			return exact, false, err
		}
	}
	query := "SELECT id,identity_key,tvg_id,name,group_id,aliases_json,manual_edited FROM sx_live_channel"
	if item.TVGID != "" {
		query += " WHERE tvg_id=''"
	}
	candidates, err := all(ctx, query)
	if err != nil {
		return nil, false, err
	}
	key := liveImportNameKey(item.Name)
	var match row
	for _, candidate := range candidates {
		names := []string{gconv.String(candidate["name"])}
		if item.TVGID == "" {
			var aliases []string
			_ = json.Unmarshal([]byte(gconv.String(candidate["aliases_json"])), &aliases)
			names = append(names, aliases...)
		}
		matched := false
		for _, name := range names {
			if key != "" && liveImportNameKey(name) == key {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if match != nil {
			return nil, false, errors.New("频道名称或别名匹配到多个频道，未合并该线路；请填写明确的 tvg-id")
		}
		match = candidate
	}
	return match, match != nil && item.TVGID == "", nil
}

func liveImportItems(ctx context.Context, p *liveProgress, items []liveImportItem, subscriptionID int64) error {
	p.Total += len(items)
	_ = p.persist(ctx)
	ids := make([]int64, 0, len(items))
	seen := map[int64]bool{}
	sort.SliceStable(items, func(i, j int) bool {
		return strings.EqualFold(items[i].TVGID, "Jade.hk") && !strings.EqualFold(items[j].TVGID, "Jade.hk")
	})
	for _, item := range items {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		id, created, err := liveUpsertImport(ctx, item, subscriptionID)
		if err != nil {
			p.Failed++
			p.Processed++
			continue
		}
		if created {
			p.Added++
		} else {
			p.Updated++
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return liveProbeIDs(ctx, p, ids)
}
func liveProbeIDs(ctx context.Context, p *liveProgress, ids []int64) error {
	jobs := make(chan int64)
	var wg sync.WaitGroup
	var progressLock sync.Mutex
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				stream, err := liveStream(ctx, id)
				if err == nil && stream.Enabled {
					checkCtx, cancel := context.WithTimeout(ctx, 24*time.Second)
					resolved, e := liveMaterializeStream(checkCtx, stream, 0, 0)
					var probe LiveProbe
					if e == nil {
						probe, e = liveProbeStream(checkCtx, resolved)
					}
					cancel()
					err = e
					message := "直播源当前无法确认可播放，已保留供后续重试"
					if err == nil {
						message = ""
						if probe.Quality != "" {
							_ = execSQL(ctx, "UPDATE sx_live_stream SET quality=IF(manual_edited=0,?,quality) WHERE id=?", probe.Quality, id)
						}
					}
					_ = liveSetHealth(ctx, id, err == nil, message)
				}
				progressLock.Lock()
				p.Processed++
				if err != nil {
					p.Failed++
				}
				p.Message = fmt.Sprintf("正在检测直播源：%d / %d；新增 %d，更新 %d，暂不可用 %d", p.Processed, p.Total, p.Added, p.Updated, p.Failed)
				_ = p.persist(ctx)
				progressLock.Unlock()
			}
		}()
	}
	for _, id := range ids {
		select {
		case jobs <- id:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	return nil
}

func liveRefreshSubscriptions(ctx context.Context, p *liveProgress, subscriptionID int64, first bool) error {
	query := "SELECT * FROM sx_live_subscription WHERE enabled=1"
	args := []any{}
	if subscriptionID > 0 {
		query += " AND id=?"
		args = append(args, subscriptionID)
	} else if !first {
		query += " AND last_refresh<?"
		args = append(args, time.Now().Add(-6*time.Hour).Unix())
	}
	subs, err := all(ctx, query+" ORDER BY CASE WHEN url LIKE '%/hk.m3u' THEN 0 WHEN url LIKE '%/mo.m3u' THEN 1 WHEN url LIKE '%/tw.m3u' THEN 2 ELSE 3 END,id", args...)
	if err != nil {
		return errors.New("读取直播订阅失败")
	}
	for _, sub := range subs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		p.Message = "正在刷新：" + gconv.String(sub["name"])
		_ = p.persist(ctx)
		content, fetchErr := liveFetchSubscription(ctx, gconv.String(sub["url"]))
		var items []liveImportItem
		if fetchErr == nil {
			items, _, fetchErr = liveParseImport(content, gconv.String(sub["name"]))
		}
		if fetchErr == nil {
			fetchErr = liveImportItems(ctx, p, items, gconv.Int64(sub["id"]))
		}
		message := ""
		if fetchErr != nil {
			p.Failed++
			message = "订阅刷新失败；现有频道和源已保留"
		}
		_ = execSQL(ctx, "UPDATE sx_live_subscription SET last_refresh=?,last_error=?,updated=? WHERE id=?", time.Now().Unix(), message, time.Now().Unix(), sub["id"])
	}
	// Successful refreshes may omit a previously imported URL. Retain that line,
	// but periodically check its health together with manual sources. Disabling
	// a subscription pauses its health sweep without deleting its saved streams.
	stale, err := liveStaleStreams(ctx, false)
	if err != nil {
		return err
	}
	ids := make([]int64, 0, len(stale))
	for _, r := range stale {
		ids = append(ids, gconv.Int64(r["id"]))
	}
	p.Total += len(ids)
	return liveProbeIDs(ctx, p, ids)
}

func liveStaleStreams(ctx context.Context, onlyFirst bool) ([]row, error) {
	query := `SELECT s.id FROM sx_live_stream s WHERE s.enabled=1 AND s.last_checked<?
 AND (s.source_kind<>'provider' OR EXISTS(SELECT 1 FROM sx_live_module lm WHERE lm.provider_key=s.provider_key AND lm.module_key=s.module_key AND lm.enabled=1))
 AND (s.subscription_id=0 OR EXISTS(SELECT 1 FROM sx_live_subscription sub WHERE sub.id=s.subscription_id AND sub.enabled=1)) ORDER BY s.id`
	if onlyFirst {
		query += " LIMIT 1"
	}
	return all(ctx, query, time.Now().Add(-6*time.Hour).Unix())
}
