package suxinvideo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/go-sql-driver/mysql"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

const vodSourceScoreTable = `CREATE TABLE IF NOT EXISTS sx_vod_source_score (
 vod_id INT UNSIGNED NOT NULL, api_id INT UNSIGNED NOT NULL,
 api_vid VARCHAR(32) NOT NULL DEFAULT '', score DECIMAL(3,1) NOT NULL DEFAULT 0.0,
 score_field VARCHAR(40) NOT NULL DEFAULT '', updated INT UNSIGNED NOT NULL DEFAULT 0,
 PRIMARY KEY (vod_id,api_id), KEY api_id (api_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`

func prepareVodSourceScores(ctx context.Context) error { return execSQL(ctx, vodSourceScoreTable) }

// A zero, missing, out-of-range or malformed upstream value means unrated.
// Never turn popularity, review counts or a guessed five-star scale into score.
func collectedSourceScore(item map[string]any) (float64, string) {
	for _, field := range []string{"vod_douban_score", "vod_score", "score"} {
		raw, exists := item[field]
		if !exists {
			continue
		}
		text := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(gconv.String(raw)), "分"))
		value, err := strconv.ParseFloat(text, 64)
		if err == nil && !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0 && value <= 10 {
			return math.Round(value*10) / 10, field
		}
	}
	return 0, "unrated"
}

func sourceScorePriority(collector row) int {
	raw := gconv.String(collector["api_url"])
	if isYQKSource(raw) {
		return 0
	}
	if raw == hongguoSourceURL {
		return 1
	}
	return 2
}

func sourceScoreName(collector row) string {
	if isYQKSource(gconv.String(collector["api_url"])) {
		return "小柒"
	}
	if gconv.String(collector["api_url"]) == hongguoSourceURL {
		return "红果"
	}
	name := strings.TrimSpace(gconv.String(collector["name"]))
	if name == "" {
		return "采集源"
	}
	return name
}

func chooseVodSourceScore(rows []row) row {
	valid := make([]row, 0, len(rows))
	for _, r := range rows {
		if gconv.Int(r["status"]) == 1 && gconv.Float64(r["score"]) > 0 && gconv.Float64(r["score"]) <= 10 {
			valid = append(valid, r)
		}
	}
	sort.SliceStable(valid, func(i, j int) bool {
		a, b := sourceScorePriority(valid[i]), sourceScorePriority(valid[j])
		if a != b {
			return a < b
		}
		if gconv.Int64(valid[i]["updated"]) != gconv.Int64(valid[j]["updated"]) {
			return gconv.Int64(valid[i]["updated"]) > gconv.Int64(valid[j]["updated"])
		}
		return gconv.Int64(valid[i]["api_id"]) < gconv.Int64(valid[j]["api_id"])
	})
	if len(valid) == 0 {
		return nil
	}
	return valid[0]
}

// Save only after collection has independently validated the film identity.
// Ratings may change while poster, title, VIP, points and every old line stay.
func saveCollectedVodScore(ctx context.Context, vodID, apiID int64, item map[string]any) error {
	return saveVodSourceScoreWithIdentity(ctx, vodID, apiID, item, "")
}

func saveVodSourceScoreWithIdentity(ctx context.Context, vodID, apiID int64, item map[string]any, expectedIdentity string) error {
	if vodID <= 0 || apiID <= 0 {
		return nil
	}
	// Some auxiliary feeds omit ratings entirely. Absence of the field is
	// not a new zero rating and must not erase the film's previously saved score.
	hasScore := false
	for _, field := range []string{"vod_douban_score", "vod_score", "score"} {
		if _, exists := item[field]; exists {
			hasScore = true
			break
		}
	}
	if !hasScore {
		return nil
	}
	value, field := collectedSourceScore(item)
	return sourceScoreTransaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		collector, err := tx.GetOne("SELECT id,name,api_url,status FROM sx_collect_api WHERE id=?", apiID)
		if err != nil {
			return err
		}
		if collector == nil || collector["status"].Int() != 1 {
			return nil
		}
		target, members, err := sourceScoreAliasMembers(tx, vodID)
		if err != nil {
			return err
		}
		// Always lock only this film group, in primary-key order. An UPDATE
		// with an OR/subquery can scan and lock unrelated films under load.
		for _, id := range members {
			locked, err := tx.GetOne("SELECT id FROM sx_vod FORCE INDEX(PRIMARY) WHERE id=? FOR UPDATE", id)
			if err != nil {
				return err
			}
			if locked == nil {
				return errDiscoveryChanged
			}
		}
		freshTarget, freshMembers, err := sourceScoreAliasMembers(tx, vodID)
		if err != nil {
			return err
		}
		if target != freshTarget || !sourceScoreSameMembers(members, freshMembers) {
			return errSourceScoreGroupChanged
		}
		film, err := tx.GetOne("SELECT id,name,year,area,type_id,api_id,api_vid FROM sx_vod WHERE id=?", vodID)
		if err != nil {
			return err
		}
		if film == nil {
			return nil
		}
		if expectedIdentity != "" && discoveryFilmIdentity(gconv.Map(film)) != expectedIdentity {
			return errDiscoveryChanged
		}
		if discoveryNormalizeTitle(film["name"].String()) != discoveryNormalizeTitle(gconv.String(item["vod_name"])) {
			return nil
		}
		_, err = tx.Exec(`INSERT INTO sx_vod_source_score(vod_id,api_id,api_vid,score,score_field,updated) VALUES(?,?,?,?,?,?) ON DUPLICATE KEY UPDATE api_vid=VALUES(api_vid),score=VALUES(score),score_field=VALUES(score_field),updated=VALUES(updated)`, target, apiID, cutRunes(gconv.String(item["vod_id"]), 32), value, field, time.Now().Unix())
		if err != nil {
			return err
		}
		arguments := []any{}
		for _, id := range members {
			arguments = append(arguments, id)
		}
		rows, err := tx.GetAll(`SELECT s.score,s.api_id,s.updated,c.name,c.api_url,c.status FROM sx_vod_source_score s JOIN sx_collect_api c ON c.id=s.api_id WHERE s.vod_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(arguments)), ",")+")", arguments...)
		if err != nil {
			return err
		}
		chosen := chooseVodSourceScore(gconv.Maps(rows))
		effective := 0.0
		if chosen != nil {
			effective = gconv.Float64(chosen["score"])
		}
		for _, id := range members {
			if _, err = tx.Exec("UPDATE sx_vod SET score=? WHERE id=?", effective, id); err != nil {
				return err
			}
		}
		return nil
	})
}

var errSourceScoreGroupChanged = errors.New("source score alias group changed")

func sourceScoreAliasMembers(tx gdb.TX, vodID int64) (int64, []int64, error) {
	canonical, err := tx.GetValue("SELECT canonical_id FROM sx_vod_alias WHERE vod_id=?", vodID)
	if err != nil {
		return 0, nil, err
	}
	target := vodID
	if canonical.Int64() > 0 {
		target = canonical.Int64()
	}
	aliases, err := tx.GetAll("SELECT vod_id FROM sx_vod_alias WHERE canonical_id=? ORDER BY vod_id", target)
	if err != nil {
		return 0, nil, err
	}
	set := map[int64]bool{vodID: true, target: true}
	for _, a := range aliases {
		set[a["vod_id"].Int64()] = true
	}
	members := []int64{}
	for id := range set {
		members = append(members, id)
	}
	sort.Slice(members, func(i, j int) bool { return members[i] < members[j] })
	return target, members, nil
}

func sourceScoreSameMembers(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sourceScoreTransaction(ctx context.Context, write func(context.Context, gdb.TX) error) error {
	options := gdb.DefaultTxOptions()
	options.Isolation = sql.LevelReadCommitted
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = g.DB().TransactionWithOptions(ctx, options, write)
		if err == nil {
			return nil
		}
		var databaseError *mysql.MySQLError
		retry := errors.Is(err, errSourceScoreGroupChanged) || errors.As(err, &databaseError) && (databaseError.Number == 1213 || databaseError.Number == 1205)
		if !retry || attempt == 2 {
			return err
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return err
}

func scoreLabel(value any) string {
	v := gconv.Map(value)
	if display := gconv.String(v["score_display"]); display != "" {
		return display
	}
	score := gconv.Float64(v["score"])
	if score <= 0 || score > 10 || math.IsNaN(score) || math.IsInf(score, 0) {
		return "暂无评分"
	}
	return fmt.Sprintf("%.1f", score)
}

func scoreSource(value any) string {
	v := gconv.Map(value)
	if scoreLabel(v) == "暂无评分" {
		return ""
	}
	if source := gconv.String(v["score_source"]); source != "" {
		return source
	}
	// Legacy rows have a numeric value but no per-source provenance. Do not
	// claim that a manually edited score came from the original collector.
	return "已有评分"
}

func scoreAvailable(value any) bool { return scoreLabel(value) != "暂无评分" }

func hydrateVodSourceScores(ctx context.Context, films []row) error {
	ids := []int64{}
	seen := map[int64]bool{}
	for _, v := range films {
		if id := gconv.Int64(v["id"]); id > 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	aliases, err := all(ctx, "SELECT vod_id,canonical_id FROM sx_vod_alias WHERE vod_id IN ("+placeholders+")", args...)
	if err != nil {
		return err
	}
	canonical := map[int64]int64{}
	for _, a := range aliases {
		canonical[gconv.Int64(a["vod_id"])] = gconv.Int64(a["canonical_id"])
	}
	targets := []any{}
	done := map[int64]bool{}
	for _, id := range ids {
		target := id
		if canonical[id] > 0 {
			target = canonical[id]
		}
		if !done[target] {
			done[target] = true
			targets = append(targets, target)
		}
	}
	ratings, err := all(ctx, `SELECT COALESCE(a.canonical_id,s.vod_id) AS target_id,s.score,s.api_id,s.updated,c.name,c.api_url,c.status FROM sx_vod_source_score s LEFT JOIN sx_collect_api c ON c.id=s.api_id LEFT JOIN sx_vod_alias a ON a.vod_id=s.vod_id WHERE COALESCE(a.canonical_id,s.vod_id) IN (`+strings.TrimSuffix(strings.Repeat("?,", len(targets)), ",")+")", targets...)
	if err != nil {
		return err
	}
	groups := map[int64][]row{}
	for _, r := range ratings {
		target := gconv.Int64(r["target_id"])
		groups[target] = append(groups[target], r)
	}
	for _, v := range films {
		id := gconv.Int64(v["id"])
		target := id
		if canonical[id] > 0 {
			target = canonical[id]
		}
		if rows := groups[target]; len(rows) > 0 {
			chosen := chooseVodSourceScore(rows)
			v["score"], v["score_display"], v["score_source"] = 0.0, "暂无评分", ""
			if chosen != nil {
				v["score"] = gconv.Float64(chosen["score"])
				v["score_display"] = fmt.Sprintf("%.1f", gconv.Float64(chosen["score"]))
				v["score_source"] = sourceScoreName(chosen)
			}
		}
	}
	return nil
}

func prepareVodScores(ctx context.Context, data map[string]any) {
	films := []row{}
	var visit func(any)
	visit = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			if _, has := v["score"]; has && gconv.Int64(v["id"]) > 0 {
				films = append(films, v)
			}
			for _, child := range v {
				visit(child)
			}
		case []row:
			for _, child := range v {
				visit(child)
			}
		case []any:
			for _, child := range v {
				visit(child)
			}
		}
	}
	visit(data)
	if err := hydrateVodSourceScores(ctx, films); err != nil {
		g.Log().Warning(ctx, "CMS source scores unavailable")
	}
}
