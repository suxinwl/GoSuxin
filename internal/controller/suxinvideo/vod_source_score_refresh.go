package suxinvideo

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

// Explicit bounded batches: a normal page view never scans the remote catalogue.
type VodScoreRefreshOptions struct {
	Apply          bool
	IDs            []int64
	AfterID        int64
	Limit          int
	OutputDir      string
	NativeZeroOnly bool
	Concurrency    int
	IntervalMS     int
}
type VodScoreRefreshEntry struct {
	VodID     int64   `json:"vod_id"`
	Title     string  `json:"title"`
	Collector string  `json:"collector"`
	RemoteID  string  `json:"remote_id"`
	Score     float64 `json:"score"`
	Field     string  `json:"field"`
	Available bool    `json:"available"`
	Applied   bool    `json:"applied"`
	Error     string  `json:"error,omitempty"`
}
type VodScoreRefreshReport struct {
	Scanned int                    `json:"scanned"`
	LastID  int64                  `json:"last_id"`
	Entries []VodScoreRefreshEntry `json:"entries"`
}

func scoreRefreshBindings(vod row, collectors []row) map[int64]string {
	ids := map[string]string{}
	for _, src := range playlist(vod) {
		for _, ep := range src.Episodes {
			if sid, _, valid := hongguoDiscoveryMarker(ep.URL); valid {
				if old := ids["hongguo"]; old == "" || old == sid {
					ids["hongguo"] = sid
				} else {
					ids["hongguo"] = "conflict"
				}
			}
			if sid, _, _, err := yqkMarkerParts(ep.URL); err == nil {
				if old := ids["yqk"]; old == "" || old == sid {
					ids["yqk"] = sid
				} else {
					ids["yqk"] = "conflict"
				}
			}
		}
	}
	bindings := map[int64]string{}
	for _, c := range collectors {
		if gconv.Int(c["status"]) != 1 {
			continue
		}
		id := gconv.Int64(c["id"])
		kind := ""
		if isYQKSource(gconv.String(c["api_url"])) {
			kind = "yqk"
		}
		if gconv.String(c["api_url"]) == hongguoSourceURL {
			kind = "hongguo"
		}
		if kind == "" {
			continue
		}
		remote := ids[kind]
		if gconv.Int64(vod["api_id"]) == id {
			remote = gconv.String(vod["api_vid"])
		}
		if onlyDigits(remote) && len(remote) <= 32 {
			bindings[id] = remote
		}
	}
	return bindings
}

func RunVodScoreRefresh(ctx context.Context, opts VodScoreRefreshOptions) (*VodScoreRefreshReport, error) {
	if opts.Limit == 0 {
		opts.Limit = 50
	}
	if opts.Limit < 1 || opts.Limit > 200 || len(opts.IDs) > 200 {
		return nil, errors.New("评分刷新每批最多200部影片")
	}
	if opts.Concurrency == 0 {
		opts.Concurrency = 2
	}
	if opts.IntervalMS == 0 {
		opts.IntervalMS = 300
	}
	if opts.Concurrency < 1 || opts.Concurrency > 4 || opts.IntervalMS < 100 || opts.IntervalMS > 10000 {
		return nil, errors.New("评分刷新并发为1至4，同来源间隔至少100毫秒")
	}
	output, err := hongguoAuditOutputDirectory(opts.OutputDir)
	if err != nil {
		return nil, err
	}
	collectors, err := all(ctx, "SELECT id,name,api_url,status FROM sx_collect_api ORDER BY id")
	if err != nil {
		return nil, err
	}
	query := "SELECT id,api_id,api_vid,name,year,area,type_id,play_from,play_url FROM sx_vod WHERE id>?"
	args := []any{opts.AfterID}
	if len(opts.IDs) > 0 {
		query = "SELECT id,api_id,api_vid,name,year,area,type_id,play_from,play_url FROM sx_vod WHERE id IN ("
		args = nil
		for i, id := range opts.IDs {
			if id <= 0 {
				return nil, errors.New("影片ID无效")
			}
			if i > 0 {
				query += ","
			}
			query += "?"
			args = append(args, id)
		}
		query += ")"
	}
	if opts.NativeZeroOnly {
		nativeIDs := []string{}
		for _, collector := range collectors {
			if gconv.Int(collector["status"]) == 1 && (isYQKSource(gconv.String(collector["api_url"])) || gconv.String(collector["api_url"]) == hongguoSourceURL) {
				nativeIDs = append(nativeIDs, gconv.String(collector["id"]))
			}
		}
		query += " AND score=0 AND (play_url LIKE '%yqk://%' OR play_url LIKE '%hongguo://%'"
		if len(nativeIDs) > 0 {
			query += " OR api_id IN (" + strings.Join(nativeIDs, ",") + ")"
		}
		query += ")"
	}
	query += " ORDER BY id LIMIT ?"
	args = append(args, opts.Limit)
	films, err := all(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	if opts.Apply {
		if err := backupSourceScores(ctx, films, filepath.Join(output, "score-before.private.json")); err != nil {
			return nil, err
		}
	}
	report := &VodScoreRefreshReport{Scanned: len(films)}
	cm := map[int64]row{}
	for _, c := range collectors {
		cm[gconv.Int64(c["id"])] = c
	}
	// Bounded parallelism across films, with independent start spacing for
	// each native provider. No media downloads or catalogue scans occur.
	gate := &sourceScoreRefreshGate{next: map[int64]time.Time{}, interval: time.Duration(opts.IntervalMS) * time.Millisecond}
	entries := make([][]VodScoreRefreshEntry, len(films))
	errorsByFilm := make([]error, len(films))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for worker := 0; worker < opts.Concurrency; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				entries[index], errorsByFilm[index] = refreshFilmSourceScores(ctx, opts, films[index], collectors, cm, gate)
			}
		}()
	}
	for index := range films {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	for index, film := range films {
		vodID := gconv.Int64(film["id"])
		report.LastID = vodID
		report.Entries = append(report.Entries, entries[index]...)
		if errorsByFilm[index] != nil {
			return report, errorsByFilm[index]
		}
	}
	if err := hongguoAuditWriteJSON(filepath.Join(output, "score-refresh.safe.json"), report, false); err != nil {
		return report, fmt.Errorf("评分刷新结果写入失败: %w", err)
	}
	return report, nil
}

type sourceScoreRefreshGate struct {
	mu       sync.Mutex
	next     map[int64]time.Time
	interval time.Duration
}

func (gate *sourceScoreRefreshGate) wait(ctx context.Context, id int64) error {
	gate.mu.Lock()
	start := gate.next[id]
	if start.Before(time.Now()) {
		start = time.Now()
	}
	gate.next[id] = start.Add(gate.interval)
	gate.mu.Unlock()
	if delay := time.Until(start); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	return ctx.Err()
}

func refreshFilmSourceScores(ctx context.Context, opts VodScoreRefreshOptions, film row, collectors []row, cm map[int64]row, gate *sourceScoreRefreshGate) ([]VodScoreRefreshEntry, error) {
	vodID := gconv.Int64(film["id"])
	entries := []VodScoreRefreshEntry{}
	bindings := scoreRefreshBindings(film, collectors)
	ids := []int64{}
	for id := range bindings {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, apiID := range ids {
		collector := cm[apiID]
		remoteID := bindings[apiID]
		entry := VodScoreRefreshEntry{VodID: vodID, Title: gconv.String(film["name"]), Collector: sourceScoreName(collector), RemoteID: remoteID}
		if err := gate.wait(ctx, int64(sourceScorePriority(collector))); err != nil {
			return entries, err
		}
		sub, cancel := context.WithTimeout(ctx, 15*time.Second)
		payload, e := fetchCollectSource(sub, gconv.String(collector["api_url"]), url.Values{"ac": {"detail"}, "ids": {remoteID}})
		cancel()
		if ctx.Err() != nil {
			return entries, ctx.Err()
		}
		if e != nil || len(payload.List) != 1 {
			entry.Error = "上游评分详情获取失败"
			entries = append(entries, entry)
			continue
		}
		item := payload.List[0]
		if gconv.String(item["vod_id"]) != remoteID || discoveryNormalizeTitle(gconv.String(item["vod_name"])) != discoveryNormalizeTitle(gconv.String(film["name"])) {
			entry.Error = "远端影片身份不同，保留原评分"
			entries = append(entries, entry)
			continue
		}
		target := discoveryTarget{Name: gconv.String(film["name"]), Year: gconv.String(film["year"]), Area: gconv.String(film["area"]), SourceAPIID: apiID, SourceAPIVID: remoteID}
		if !discoveryCompatible(target, item, apiID, true) {
			entry.Error = "远端年份或地区冲突，保留原评分"
			entries = append(entries, entry)
			continue
		}
		entry.Score, entry.Field = collectedSourceScore(item)
		entry.Available = entry.Score > 0
		if opts.Apply {
			fresh, e := one(ctx, "SELECT id,api_id,api_vid,name,year,area,type_id,play_from,play_url FROM sx_vod WHERE id=?", vodID)
			if e != nil {
				return entries, e
			}
			if fresh == nil || discoveryFilmIdentity(fresh) != discoveryFilmIdentity(film) {
				entry.Error = "影片身份已变化，跳过"
			} else if e = saveCollectedVodScore(ctx, vodID, apiID, item); e != nil {
				return entries, e
			} else {
				entry.Applied = true
			}
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func backupSourceScores(ctx context.Context, films []row, destination string) error {
	ids := []any{}
	for _, film := range films {
		ids = append(ids, gconv.Int64(film["id"]))
	}
	if len(ids) == 0 {
		return hongguoAuditWriteJSON(destination, map[string]any{"films": []row{}, "source_scores": []row{}}, true)
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	aliases, err := all(ctx, "SELECT vod_id,canonical_id FROM sx_vod_alias WHERE vod_id IN ("+ph+")", ids...)
	if err != nil {
		return err
	}
	targets := map[int64]bool{}
	for _, film := range films {
		targets[gconv.Int64(film["id"])] = true
	}
	for _, alias := range aliases {
		targets[gconv.Int64(alias["canonical_id"])] = true
	}
	args := []any{}
	for id := range targets {
		args = append(args, id)
	}
	ph = strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")
	members, err := all(ctx, "SELECT vod_id FROM sx_vod_alias WHERE canonical_id IN ("+ph+")", args...)
	if err != nil {
		return err
	}
	for _, member := range members {
		targets[gconv.Int64(member["vod_id"])] = true
	}
	args = nil
	for id := range targets {
		args = append(args, id)
	}
	ph = strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")
	scores, err := all(ctx, "SELECT id,name,score FROM sx_vod WHERE id IN ("+ph+") ORDER BY id", args...)
	if err != nil {
		return err
	}
	sources, err := all(ctx, "SELECT vod_id,api_id,api_vid,score,score_field,updated FROM sx_vod_source_score WHERE vod_id IN ("+ph+") ORDER BY vod_id,api_id", args...)
	if err != nil {
		return err
	}
	return hongguoAuditWriteJSON(destination, map[string]any{"films": scores, "source_scores": sources, "captured_at": time.Now().Unix()}, true)
}
