package suxinvideo

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	xq "github.com/suxinwl/GoSuxin/internal/xiaoqiapp"
)

// HongguoSeasonRestoreOptions restores independently verified seasons removed
// from old mixed-drama lines. It is a local maintenance operation, never a route.
type HongguoSeasonRestoreOptions struct {
	Apply             bool
	OutputDir         string
	RemoteResultsFile string
	RepairFile        string
}

type HongguoSeasonRestoreEntry struct {
	Series      string  `json:"series"`
	Title       string  `json:"title"`
	ProofVodIDs []int64 `json:"proof_vod_ids"`
	Episodes    int     `json:"episodes"`
	Status      string  `json:"status"`
	Reason      string  `json:"reason,omitempty"`
	LocalID     int64   `json:"local_id,omitempty"`
}

type HongguoSeasonRestoreReport struct {
	CreatedAt string                      `json:"created_at"`
	Applied   bool                        `json:"applied"`
	Eligible  int                         `json:"eligible"`
	Planned   int                         `json:"planned"`
	Restored  int                         `json:"restored"`
	Skipped   int                         `json:"skipped"`
	Failed    int                         `json:"failed"`
	Entries   []HongguoSeasonRestoreEntry `json:"entries"`
}

type hongguoSeasonRepairProof struct {
	Series   string          `json:"series_id"`
	Title    string          `json:"title"`
	Episodes int             `json:"episodes"`
	Error    json.RawMessage `json:"error"`
}

type hongguoSeasonRepairEvidence struct {
	ID          int64                      `json:"id"`
	APIID       int64                      `json:"expected_api_id"`
	APIVID      string                     `json:"expected_api_vid"`
	RemoteTitle string                     `json:"remote_title"`
	Verified    bool                       `json:"remote_identity_matches"`
	Proofs      []hongguoSeasonRepairProof `json:"stored_series_proofs"`
}

type hongguoSeasonRemoteEvidence struct {
	Request struct {
		Series string `json:"series_id"`
	} `json:"request"`
	Drama    xq.Drama        `json:"drama"`
	Chapters []xq.Chapter    `json:"chapters"`
	Error    json.RawMessage `json:"error"`
}

func hongguoSeasonHasEvidenceError(raw json.RawMessage) bool {
	switch string(bytes.TrimSpace(raw)) {
	case "", "false", "null", `""`:
		return false
	default:
		return true
	}
}

type hongguoSeasonRestorePlan struct {
	Entry    HongguoSeasonRestoreEntry `json:"entry"`
	APIID    int64                     `json:"api_id"`
	Drama    xq.Drama                  `json:"drama"`
	Chapters []xq.Chapter              `json:"chapters"`
	Item     map[string]any            `json:"item"`
}

func hongguoSeasonReadPrivateFile(path string) ([]byte, error) {
	base, err := filepath.Abs(filepath.Join("data", "tmp"))
	if err != nil {
		return nil, errors.New("无法定位本地私有数据目录")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, errors.New("未找到本地验证文件")
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return nil, errors.New("无法定位本地验证文件")
	}
	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return nil, errors.New("未找到本地私有数据目录")
	}
	rel, err := filepath.Rel(realBase, resolved)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return nil, errors.New("验证文件必须位于项目 data/tmp 子目录")
	}
	f, err := os.Open(resolved)
	if err != nil {
		return nil, errors.New("无法读取本地验证文件")
	}
	defer f.Close()
	const limit = 16 << 20
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || len(data) > limit || !utf8.Valid(data) {
		return nil, errors.New("验证文件无效、过大或编码不是 UTF-8")
	}
	return bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), nil
}

func parseHongguoSeasonRemoteEvidence(data []byte) (map[string]hongguoSeasonRemoteEvidence, error) {
	results := make(map[string]hongguoSeasonRemoteEvidence)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var result hongguoSeasonRemoteEvidence
		if json.Unmarshal(line, &result) != nil {
			return nil, errors.New("本地红果详情验证记录格式错误")
		}
		if hongguoSeasonHasEvidenceError(result.Error) {
			continue
		}
		id := result.Request.Series
		if !onlyDigits(id) || strings.Trim(id, "0") == "" || result.Drama.Source != "hongguo" || result.Drama.SourceID != id {
			return nil, errors.New("本地红果详情验证记录缺少一致的原生身份")
		}
		if previous, found := results[id]; found && discoveryHash([]any{previous.Drama, previous.Chapters}) != discoveryHash([]any{result.Drama, result.Chapters}) {
			return nil, errors.New("同一红果剧集存在相互冲突的本地详情验证记录")
		}
		results[id] = result
	}
	if scanner.Err() != nil {
		return nil, errors.New("本地红果详情验证记录读取失败")
	}
	return results, nil
}

func buildHongguoSeasonRestorePlans(repairs []hongguoSeasonRepairEvidence, remotes map[string]hongguoSeasonRemoteEvidence) ([]hongguoSeasonRestorePlan, error) {
	plans := make(map[string]hongguoSeasonRestorePlan)
	// Only the five reviewed mixed-series films are recovery donors. This
	// prevents an arbitrary remote catalog file from becoming an import batch.
	allowed := map[int64]bool{1311: true, 1361: true, 1496: true, 1541: true, 6487: true}
	for _, repair := range repairs {
		if !allowed[repair.ID] || !repair.Verified || repair.APIID <= 0 || !onlyDigits(repair.APIVID) || strings.Trim(repair.APIVID, "0") == "" || strings.TrimSpace(repair.RemoteTitle) == "" {
			return nil, errors.New("季集恢复证明不是已审核的五条原生串片修复记录")
		}
		for _, proof := range repair.Proofs {
			if proof.Series == repair.APIVID || discoveryNormalizeTitle(proof.Title) == discoveryNormalizeTitle(repair.RemoteTitle) {
				// 同名吞天剑碑的另一原生 ID stays pending; never infer a remake.
				continue
			}
			if hongguoSeasonHasEvidenceError(proof.Error) || proof.Episodes <= 0 || !onlyDigits(proof.Series) || strings.Trim(proof.Series, "0") == "" || strings.TrimSpace(proof.Title) == "" || strings.ContainsRune(proof.Title, utf8.RuneError) {
				return nil, errors.New("独立季集的原生身份或详情证明尚未核实")
			}
			remote, found := remotes[proof.Series]
			if !found || remote.Drama.SourceID != proof.Series || discoveryNormalizeTitle(remote.Drama.DisplayTitle()) != discoveryNormalizeTitle(proof.Title) || strings.ContainsRune(remote.Drama.DisplayTitle(), utf8.RuneError) || len(remote.Chapters) != proof.Episodes {
				return nil, errors.New("独立季集的标题或完整分集与验证证明不一致")
			}
			item := cmsProviderVodItem("hongguo", remote.Drama)
			var episodes []string
			labels := make(map[string]string)
			for _, chapter := range remote.Chapters {
				video := strings.TrimPrefix(chapter.VideoURL, "hongguo-cenc://")
				if chapter.Source != "hongguo" || !strings.HasPrefix(chapter.VideoURL, "hongguo-cenc://") || !onlyDigits(video) || strings.Trim(video, "0") == "" || strings.TrimSpace(chapter.Title) == "" || strings.ContainsAny(chapter.Title, "$#\r\n") {
					return nil, errors.New("独立季集包含未经核实的原生分集")
				}
				key, _ := playbackEpisodeIdentity(chapter.Title)
				if _, exists := labels[key]; exists {
					return nil, errors.New("独立季集存在重复的分集标签，暂不恢复")
				}
				labels[key] = video
				episodes = append(episodes, chapter.Title+"$hongguo://"+proof.Series+"/"+video)
			}
			item["vod_play_from"], item["vod_play_url"] = "hongguo", strings.Join(episodes, "#")
			_, _, err := validateHongguoCollectedPlaylist(row{"api_url": hongguoSourceURL, "status": 1}, proof.Series, "hongguo", gconv.String(item["vod_play_url"]))
			if err != nil {
				return nil, errors.New("独立季集的原生播放列表验证失败")
			}
			plan := hongguoSeasonRestorePlan{Entry: HongguoSeasonRestoreEntry{Series: proof.Series, Title: remote.Drama.DisplayTitle(), Episodes: len(remote.Chapters), Status: "待恢复", ProofVodIDs: []int64{repair.ID}},
				APIID: repair.APIID, Drama: remote.Drama, Chapters: remote.Chapters, Item: item}
			if previous, exists := plans[proof.Series]; exists {
				if previous.APIID != plan.APIID || discoveryHash(previous.Item) != discoveryHash(plan.Item) {
					return nil, errors.New("同一独立季集的来源或验证证明存在冲突")
				}
				plan.Entry.ProofVodIDs = append(previous.Entry.ProofVodIDs, repair.ID)
			}
			plans[proof.Series] = plan
		}
	}
	// Nine independently titled seasons were reviewed. Do not broaden recovery
	// to an arbitrary number of extra IDs or silently run an incomplete batch.
	if len(plans) != 9 {
		return nil, errors.New("恢复范围必须是已核实的九个不同标题的独立季集")
	}
	ordered := make([]hongguoSeasonRestorePlan, 0, len(plans))
	for _, plan := range plans {
		ordered = append(ordered, plan)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Entry.Series < ordered[j].Entry.Series })
	return ordered, nil
}

func hongguoSeasonExistingReason(films []row, apiID int64, series, title string) (string, int64) {
	title = discoveryNormalizeTitle(title)
	for _, film := range films {
		if gconv.Int64(film["api_id"]) == apiID && gconv.String(film["api_vid"]) == series {
			return "原生系列已经存在", gconv.Int64(film["id"])
		}
		for _, src := range playlist(film) {
			if src.Code != "hongguo" {
				continue
			}
			for _, ep := range src.Episodes {
				stored, _, valid := hongguoDiscoveryMarker(ep.URL)
				if valid && stored == series {
					return "原生系列仍存在于本地播放列表", gconv.Int64(film["id"])
				}
			}
		}
		if title == discoveryNormalizeTitle(gconv.String(film["name"])) {
			return "同名影片已经存在，等待原有记录核实", gconv.Int64(film["id"])
		}
	}
	return "", 0
}

func RunHongguoSeasonRestore(ctx context.Context, options HongguoSeasonRestoreOptions) (*HongguoSeasonRestoreReport, error) {
	if options.RemoteResultsFile == "" {
		options.RemoteResultsFile = filepath.Join("data", "tmp", "hongguo-full-audit", "remote-results.private.jsonl")
	}
	if options.RepairFile == "" {
		options.RepairFile = filepath.Join("data", "tmp", "hongguo-full-audit", "repair-lines.private.json")
	}
	if options.OutputDir == "" {
		options.OutputDir = filepath.Join("data", "tmp", "hongguo-season-restore", time.Now().Format("20060102-150405.000000000"))
	}
	output, err := hongguoAuditOutputDirectory(options.OutputDir)
	if err != nil {
		return nil, errors.New("恢复计划输出目录必须位于项目 data/tmp 子目录")
	}
	remoteData, err := hongguoSeasonReadPrivateFile(options.RemoteResultsFile)
	if err != nil {
		return nil, err
	}
	remotes, err := parseHongguoSeasonRemoteEvidence(remoteData)
	if err != nil {
		return nil, err
	}
	repairData, err := hongguoSeasonReadPrivateFile(options.RepairFile)
	if err != nil {
		return nil, err
	}
	var repairs []hongguoSeasonRepairEvidence
	if json.Unmarshal(repairData, &repairs) != nil {
		return nil, errors.New("本地原生串片修复证明格式错误")
	}
	plans, err := buildHongguoSeasonRestorePlans(repairs, remotes)
	if err != nil {
		return nil, err
	}
	report := &HongguoSeasonRestoreReport{CreatedAt: time.Now().UTC().Format(time.RFC3339), Eligible: len(plans), Applied: options.Apply}
	collectors, err := all(ctx, "SELECT id,api_url,status FROM sx_collect_api ORDER BY id")
	if err != nil {
		return report, errors.New("无法读取本地红果采集源")
	}
	sources := make(map[int64]row)
	for _, collector := range collectors {
		sources[gconv.Int64(collector["id"])] = collector
	}
	films, err := all(ctx, "SELECT id,api_id,api_vid,name,play_from,play_url FROM sx_vod ORDER BY id")
	if err != nil {
		return report, errors.New("无法读取本地影片身份")
	}
	for index := range plans {
		plan := &plans[index]
		if !hongguoCollectionSourceEnabled(sources[plan.APIID]) {
			return report, errors.New("恢复证明中的原生红果采集源不存在或已停用")
		}
		if reason, id := hongguoSeasonExistingReason(films, plan.APIID, plan.Entry.Series, plan.Entry.Title); reason != "" {
			plan.Entry.Status, plan.Entry.Reason, plan.Entry.LocalID = "跳过", reason, id
			report.Skipped++
		} else {
			report.Planned++
		}
	}
	// The full independently reviewed evidence is durable before any write.
	// Files include raw provider covers/markers and must remain private.
	proof := struct {
		CreatedAt         string                        `json:"created_at"`
		RemoteResultsHash string                        `json:"remote_results_sha256"`
		RepairHash        string                        `json:"repair_evidence_sha256"`
		Repairs           []hongguoSeasonRepairEvidence `json:"repairs"`
		Plans             []hongguoSeasonRestorePlan    `json:"plans"`
	}{report.CreatedAt, fmt.Sprintf("%x", sha256.Sum256(remoteData)), fmt.Sprintf("%x", sha256.Sum256(repairData)), repairs, plans}
	if err := hongguoAuditWriteJSON(filepath.Join(output, "plan.private.json"), proof, true); err != nil {
		return report, errors.New("私有恢复计划未保存，未开始入库")
	}
	for index := range plans {
		plan := &plans[index]
		entry := plan.Entry
		if options.Apply && entry.Status != "跳过" {
			// Recheck immediately before normal collection; its title/API locks
			// provide the final concurrent first-insert deduplication guarantee.
			collector, sourceErr := one(ctx, "SELECT api_url,status FROM sx_collect_api WHERE id=?", plan.APIID)
			current, currentErr := all(ctx, "SELECT id,api_id,api_vid,name,play_from,play_url FROM sx_vod ORDER BY id")
			if sourceErr != nil || currentErr != nil || !hongguoCollectionSourceEnabled(collector) {
				entry.Status, entry.Reason = "失败", "入库前原生源或本地身份核验失败"
				report.Failed++
			} else if reason, id := hongguoSeasonExistingReason(current, plan.APIID, entry.Series, entry.Title); reason != "" {
				entry.Status, entry.Reason, entry.LocalID = "跳过", reason, id
				report.Skipped++
			} else if state, importErr := upsertMacVod(ctx, plan.APIID, plan.Item); importErr != nil {
				entry.Status, entry.Reason = "失败", "正常采集入库失败，详情已保存在本地私有诊断"
				report.Failed++
				_ = os.WriteFile(filepath.Join(output, fmt.Sprintf("series-%s-error.private.log", entry.Series)), []byte(importErr.Error()), 0600)
			} else if state == collectVodBlocked {
				entry.Status, entry.Reason = "跳过", "命中站点现有内容过滤规则"
				report.Skipped++
			} else {
				stored, findErr := one(ctx, "SELECT id FROM sx_vod WHERE api_id=? AND api_vid=? ORDER BY id LIMIT 1", plan.APIID, entry.Series)
				if findErr == nil && stored != nil {
					entry.LocalID = gconv.Int64(stored["id"])
				}
				if state == 1 {
					entry.Status = "已恢复"
					report.Restored++
				} else if state == 2 {
					entry.Status, entry.Reason = "跳过", "并发采集已创建同片，由正常采集规则合并"
					report.Skipped++
				} else {
					entry.Status, entry.Reason = "失败", "正常采集未返回新增或更新结果"
					report.Failed++
				}
			}
		}
		report.Entries = append(report.Entries, entry)
		// A safe per-entry journal survives cancellation or a later failure.
		if err := hongguoAuditWriteJSON(filepath.Join(output, "report.safe.json"), report, false); err != nil {
			return report, errors.New("恢复进度报告未保存，请查看已有私有计划后再继续")
		}
	}
	return report, nil
}
