package suxinvideo

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

type VodAliasOptions struct {
	Apply                               bool
	ReportFile, VariantsFile, OutputDir string
}
type VodAliasGroup struct {
	CanonicalID int64    `json:"canonical_id"`
	IDs         []int64  `json:"ids"`
	Names       []string `json:"names"`
}
type VodAliasReport struct {
	Applied        bool            `json:"applied"`
	ApprovedPairs  int             `json:"approved_pairs"`
	ExcludedPairs  int             `json:"excluded_pairs"`
	ChangedMembers int             `json:"changed_members"`
	Groups         []VodAliasGroup `json:"groups"`
}
type vodAliasEdge struct {
	A, B         int64
	AName, BName string
}
type vodAliasAuditInput struct {
	Entries []struct {
		ID      int64  `json:"id"`
		Name    string `json:"name"`
		Details []struct {
			ID     int64  `json:"id"`
			Status string `json:"status"`
		} `json:"candidate_details"`
	} `json:"entries"`
}
type vodAliasVariantInput struct {
	A      int64  `json:"hongguo_id"`
	B      int64  `json:"candidate_id"`
	AName  string `json:"hongguo_title"`
	BName  string `json:"candidate_title"`
	Status string `json:"status"`
}

func vodAliasApprovedEdges(reportData, variantsData []byte) ([]vodAliasEdge, []vodAliasEdge, error) {
	var audit vodAliasAuditInput
	var variants []vodAliasVariantInput
	if json.Unmarshal(reportData, &audit) != nil || json.Unmarshal(variantsData, &variants) != nil {
		return nil, nil, errors.New("显式别名候选报告格式错误")
	}
	approved, excluded := []vodAliasEdge{}, []vodAliasEdge{}
	seen := map[string]bool{}
	add := func(edge vodAliasEdge) error {
		if edge.A <= 0 || edge.B <= 0 || edge.A == edge.B || strings.TrimSpace(edge.AName) == "" || strings.TrimSpace(edge.BName) == "" {
			return errors.New("显式别名候选缺少原记录身份")
		}
		key := fmt.Sprintf("%d:%d", min(edge.A, edge.B), max(edge.A, edge.B))
		if !seen[key] {
			approved = append(approved, edge)
			seen[key] = true
		}
		return nil
	}
	for _, entry := range audit.Entries {
		for _, candidate := range entry.Details {
			if strings.HasPrefix(candidate.Status, "待核实") {
				if err := add(vodAliasEdge{entry.ID, candidate.ID, entry.Name, entry.Name}); err != nil {
					return nil, nil, err
				}
			}
		}
	}
	for _, variant := range variants {
		edge := vodAliasEdge{variant.A, variant.B, variant.AName, variant.BName}
		if strings.HasPrefix(variant.Status, "待核实") {
			if err := add(edge); err != nil {
				return nil, nil, err
			}
		} else if strings.HasPrefix(variant.Status, "已确认不同季") {
			excluded = append(excluded, edge)
		}
	}
	if len(approved) == 0 {
		return nil, nil, errors.New("报告没有用户指定的待核实别名候选")
	}
	return approved, excluded, nil
}

func buildVodAliasGroups(edges, excluded []vodAliasEdge, films []row, aliases []row) ([]VodAliasGroup, error) {
	rows := map[int64]row{}
	parent := map[int64]int64{}
	for _, film := range films {
		rows[gconv.Int64(film["id"])] = film
	}
	var find func(int64) int64
	find = func(id int64) int64 {
		if parent[id] == 0 {
			parent[id] = id
		}
		if parent[id] != id {
			parent[id] = find(parent[id])
		}
		return parent[id]
	}
	join := func(a, b int64) {
		ra, rb := find(a), find(b)
		if ra != rb {
			if ra < rb {
				parent[rb] = ra
			} else {
				parent[ra] = rb
			}
		}
	}
	for _, alias := range aliases {
		join(gconv.Int64(alias["vod_id"]), gconv.Int64(alias["canonical_id"]))
	}
	wanted := map[int64]bool{}
	for _, edge := range edges {
		for _, entry := range []struct {
			id   int64
			name string
		}{{edge.A, edge.AName}, {edge.B, edge.BName}} {
			film := rows[entry.id]
			if film == nil || discoveryNormalizeTitle(gconv.String(film["name"])) != discoveryNormalizeTitle(entry.name) {
				return nil, errors.New("候选原记录已删除或完整标题已变更，请重新生成报告")
			}
			wanted[entry.id] = true
		}
		join(edge.A, edge.B)
	}
	roots := map[int64]bool{}
	for id := range wanted {
		roots[find(id)] = true
	}
	for _, edge := range excluded {
		if parent[edge.A] != 0 && parent[edge.B] != 0 && find(edge.A) == find(edge.B) && roots[find(edge.A)] {
			return nil, errors.New("候选闭包会包含已确认不同季的排除项，已停止别名统一")
		}
	}
	groups := map[int64][]int64{}
	for id := range parent {
		root := find(id)
		if roots[root] {
			if rows[id] == nil {
				return nil, errors.New("已有别名组包含已删除的原记录")
			}
			groups[root] = append(groups[root], id)
		}
	}
	result := []VodAliasGroup{}
	for _, ids := range groups {
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		group := VodAliasGroup{CanonicalID: ids[0], IDs: ids}
		for _, id := range ids {
			group.Names = append(group.Names, gconv.String(rows[id]["name"]))
		}
		result = append(result, group)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CanonicalID < result[j].CanonicalID })
	return result, nil
}

// A dedicated connection holds only the maintenance advisory lock; it holds
// no business row or empty-table gap locks. Releasing on this same connection
// is essential because pooled connections retain MySQL named locks.
func acquireVodAliasMaintenance(ctx context.Context) (func(), error) {
	db, err := g.DB().Master()
	if err != nil {
		return nil, err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	var lockName string
	if err = conn.QueryRowContext(ctx, "SELECT CONCAT('sx:vod-alias:',SUBSTRING(SHA2(DATABASE(),256),1,32))").Scan(&lockName); err != nil {
		_ = conn.Close()
		return nil, err
	}
	var acquired sql.NullInt64
	if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,10)", lockName).Scan(&acquired); err != nil || !acquired.Valid || acquired.Int64 != 1 {
		_ = conn.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("另一个别名维护正在执行，请稍后继续")
	}
	return func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var released sql.NullInt64
		if err := conn.QueryRowContext(cleanup, "SELECT RELEASE_LOCK(?)", lockName).Scan(&released); err != nil || !released.Valid || released.Int64 != 1 {
			// Never return a still-locked connection to the shared pool.
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		_ = conn.Close()
	}, nil
}

func vodAliasRetryTransaction(ctx context.Context, run func(int) error) error {
	for attempt := 1; attempt <= 3; attempt++ {
		err := run(attempt)
		if err == nil {
			return nil
		}
		var mysqlError *mysql.MySQLError
		if attempt == 3 || !errors.As(err, &mysqlError) || mysqlError.Number != 1213 && mysqlError.Number != 1205 {
			return err
		}
		timer := time.NewTimer(time.Duration(attempt) * 150 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

// RunVodAliasMaintenance persists only explicit grouping. It never rewrites a
// film, concatenates seasons, deletes records or moves member history.
func RunVodAliasMaintenance(ctx context.Context, options VodAliasOptions) (*VodAliasReport, error) {
	if options.ReportFile == "" {
		options.ReportFile = filepath.Join("data", "tmp", "hongguo-audit-final", "report.safe.json")
	}
	if options.VariantsFile == "" {
		options.VariantsFile = filepath.Join("data", "tmp", "hongguo-audit-final", "title-variants.safe.json")
	}
	if options.OutputDir == "" {
		options.OutputDir = filepath.Join("data", "tmp", "vod-alias", time.Now().Format("20060102-150405.000000000"))
	}
	output, err := hongguoAuditOutputDirectory(options.OutputDir)
	if err != nil {
		return nil, errors.New("别名维护输出目录必须位于 data/tmp 子目录")
	}
	reportData, err := hongguoSeasonReadPrivateFile(options.ReportFile)
	if err != nil {
		return nil, err
	}
	variantData, err := hongguoSeasonReadPrivateFile(options.VariantsFile)
	if err != nil {
		return nil, err
	}
	edges, excluded, err := vodAliasApprovedEdges(reportData, variantData)
	if err != nil {
		return nil, err
	}
	films, err := all(ctx, "SELECT * FROM sx_vod ORDER BY id")
	if err != nil {
		return nil, errors.New("无法读取别名候选原记录")
	}
	aliases, err := all(ctx, "SELECT * FROM sx_vod_alias ORDER BY vod_id")
	if err != nil {
		return nil, errors.New("无法读取持久别名组，请先升级安装表结构")
	}
	groups, err := buildVodAliasGroups(edges, excluded, films, aliases)
	if err != nil {
		return nil, err
	}
	report := &VodAliasReport{ApprovedPairs: len(edges), ExcludedPairs: len(excluded), Groups: groups}
	if err := hongguoAuditWriteJSON(filepath.Join(output, "plan.private.json"), map[string]any{"approved_pairs": edges, "excluded_pairs": excluded, "groups": groups}, true); err != nil {
		return report, errors.New("私有别名计划未保存，未开始入库")
	}
	if options.Apply {
		release, lockErr := acquireVodAliasMaintenance(ctx)
		if lockErr != nil {
			return report, fmt.Errorf("无法取得别名维护锁: %w", lockErr)
		}
		defer release()
		err = vodAliasRetryTransaction(ctx, func(attempt int) error {
			report.ChangedMembers = 0
			transactionOptions := gdb.DefaultTxOptions()
			transactionOptions.Isolation = sql.LevelReadCommitted
			transactionErr := g.DB().TransactionWithOptions(ctx, transactionOptions, func(_ context.Context, tx gdb.TX) error {
				// All CMS writers use film PRIMARY locks in ascending order before
				// alias/provenance writes. Never gap-lock an empty aliases table.
				aliasSnapshot, e := tx.GetAll("SELECT * FROM sx_vod_alias ORDER BY vod_id")
				if e != nil {
					return fmt.Errorf("无法读取持久别名记录: %w", e)
				}
				currentAliases := gconv.Maps(aliasSnapshot)
				currentGroups, e := buildVodAliasGroups(edges, excluded, films, currentAliases)
				if e != nil {
					return e
				}
				ids := []int64{}
				for _, group := range currentGroups {
					ids = append(ids, group.IDs...)
				}
				sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
				args := make([]any, len(ids))
				for i, id := range ids {
					args[i] = id
				}
				lockedFilms, e := tx.GetAll("SELECT * FROM sx_vod WHERE id IN ("+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+") ORDER BY id FOR UPDATE", args...)
				if e != nil {
					return fmt.Errorf("无法锁定候选原影片记录: %w", e)
				}
				currentFilms := gconv.Maps(lockedFilms)
				selected := make(map[int64]bool, len(ids))
				for _, id := range ids {
					selected[id] = true
				}
				existingArgs := []any{}
				for _, alias := range currentAliases {
					if id := gconv.Int64(alias["vod_id"]); selected[id] {
						existingArgs = append(existingArgs, id)
					}
				}
				if len(existingArgs) > 0 {
					if _, e = tx.GetAll("SELECT vod_id FROM sx_vod_alias WHERE vod_id IN ("+strings.TrimSuffix(strings.Repeat("?,", len(existingArgs)), ",")+") ORDER BY vod_id FOR UPDATE", existingArgs...); e != nil {
						return fmt.Errorf("无法锁定已有别名成员: %w", e)
					}
				}
				currentGroups, e = buildVodAliasGroups(edges, excluded, currentFilms, currentAliases)
				if e != nil {
					return e
				}
				if e = hongguoAuditWriteJSON(filepath.Join(output, fmt.Sprintf("before-attempt-%02d.private.json", attempt)), map[string]any{"films": currentFilms, "aliases": currentAliases, "groups": currentGroups, "attempt": attempt}, true); e != nil {
					return errors.New("原记录与别名私有备份未保存，未提交统一")
				}
				old := map[int64]row{}
				for _, alias := range currentAliases {
					old[gconv.Int64(alias["vod_id"])] = alias
				}
				now := time.Now().Unix()
				for _, group := range currentGroups {
					for _, id := range group.IDs {
						if previous := old[id]; previous != nil && gconv.Int64(previous["canonical_id"]) == group.CanonicalID {
							continue
						}
						if _, e = tx.Exec("INSERT INTO sx_vod_alias(vod_id,canonical_id,reason,created,updatetime) VALUES(?,?,?, ?,?) ON DUPLICATE KEY UPDATE canonical_id=VALUES(canonical_id),reason=VALUES(reason),updatetime=VALUES(updatetime)", id, group.CanonicalID, "用户指定同名或近似标题统一；保留原记录及独立版本", now, now); e != nil {
							return fmt.Errorf("持久别名写入失败，数据库统一已回滚: %w", e)
						}
						report.ChangedMembers++
					}
				}
				report.Groups = currentGroups
				return nil
			})
			if transactionErr != nil {
				_ = hongguoAuditWriteJSON(filepath.Join(output, fmt.Sprintf("error-attempt-%02d.private.json", attempt)), map[string]any{"attempt": attempt, "error": transactionErr.Error()}, true)
			}
			return transactionErr
		})
		if err != nil {
			report.ChangedMembers = 0
			return report, err
		}
		report.Applied = true
	}
	if err := hongguoAuditWriteJSON(filepath.Join(output, "report.safe.json"), report, false); err != nil {
		return report, errors.New("别名维护报告未保存，请检查私有计划和备份后继续")
	}
	return report, nil
}
