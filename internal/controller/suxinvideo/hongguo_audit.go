package suxinvideo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

// RunHongguoAudit is a local maintenance entry point, not a public HTTP route.
// Its default is read-only. Private backups must stay outside distributable data.
type HongguoAuditOptions struct {
	Apply      bool
	OutputDir  string
	RepairFile string
}

type HongguoAuditEntry struct {
	ID               int64                   `json:"id"`
	Name             string                  `json:"name"`
	Kind             string                  `json:"kind"`
	Sources          []string                `json:"sources"`
	Series           string                  `json:"series,omitempty"`
	Problems         []string                `json:"problems,omitempty"`
	Candidates       []int64                 `json:"candidates,omitempty"`
	CandidateDetails []HongguoAuditCandidate `json:"candidate_details,omitempty"`
}

type HongguoAuditCandidate struct {
	ID     int64  `json:"id"`
	Kind   string `json:"kind"`
	Year   string `json:"year,omitempty"`
	Area   string `json:"area,omitempty"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

func hongguoAuditPendingReason(target discoveryTarget, item map[string]any, ambiguous bool) string {
	if ambiguous {
		return "同名候选有多个未核实或冲突版本"
	}
	if discoveryKind(target.Kind) != "short" {
		return "独立主分类不是短剧"
	}
	a, b := strings.TrimSpace(target.Year), strings.TrimSpace(gconv.String(item["vod_year"]))
	if discoveryYearPattern.MatchString(a) && discoveryYearPattern.MatchString(b) && a != b {
		return "独立年份冲突"
	}
	a, b = discoveryRegion(target.Area), discoveryRegion(gconv.String(item["vod_area"]))
	if a != "" && b != "" && a != b {
		return "独立地区冲突"
	}
	if len([]rune(discoveryNormalizeTitle(target.Name))) < 8 {
		return "片名简短且缺少完整独立身份依据"
	}
	return "缺少同片身份依据或已有红果剧集绑定不一致"
}

type HongguoAuditAction struct {
	ID       int64    `json:"id"`
	DonorIDs []int64  `json:"donor_ids,omitempty"`
	Reasons  []string `json:"reasons"`
}

type HongguoAuditReport struct {
	ScannedAt        string               `json:"scanned_at"`
	Scope            string               `json:"scope"`
	Scanned          int                  `json:"scanned"`
	MaxID            int64                `json:"max_id"`
	HongguoRecords   int                  `json:"hongguo_records"`
	MixedRecords     int                  `json:"mixed_records"`
	CrossSourcePairs int                  `json:"cross_source_pairs"`
	ConfirmedPairs   int                  `json:"confirmed_pairs"`
	PendingPairs     int                  `json:"pending_pairs"`
	CategoryRepairs  int                  `json:"category_repairs"`
	NativeRepairs    int                  `json:"native_repairs"`
	ResourceCopies   int                  `json:"resource_copies"`
	ChangedRecords   int                  `json:"changed_records"`
	Applied          bool                 `json:"applied"`
	Entries          []HongguoAuditEntry  `json:"entries"`
	Actions          []HongguoAuditAction `json:"actions"`
}

type hongguoAuditRepair struct {
	ID          int64  `json:"id"`
	Name        string `json:"expected_name"`
	APIID       int64  `json:"expected_api_id"`
	APIVID      string `json:"expected_api_vid"`
	RemoteTitle string `json:"remote_title"`
	Verified    bool   `json:"remote_identity_matches"`
	From        string `json:"play_from"`
	Play        string `json:"play_url"`
	OldHash     string `json:"original_playlist_sha256"`
}

func hongguoAuditPlaylistHash(vod row) string {
	digest := sha256.Sum256([]byte(gconv.String(vod["play_from"]) + "\x00" + gconv.String(vod["play_url"])))
	return hex.EncodeToString(digest[:])
}

func hongguoAuditRowHash(vod row) string {
	fields := []string{"id", "api_id", "api_vid", "name", "class", "year", "area", "type_id", "status", "play_from", "play_url"}
	values := make([]string, len(fields))
	for i, field := range fields {
		values[i] = gconv.String(vod[field])
	}
	return discoveryHash(values)
}

func hongguoAuditOutputDirectory(raw string) (string, error) {
	base, err := filepath.Abs(filepath.Join("data", "tmp"))
	if err != nil {
		return "", err
	}
	if raw == "" {
		raw = filepath.Join(base, "hongguo-audit", time.Now().Format("20060102-150405"))
	}
	output, err := filepath.Abs(raw)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(base, output)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errors.New("审计和私有备份目录必须位于项目 data/tmp 的子目录")
	}
	if err = os.MkdirAll(output, 0700); err != nil {
		return "", err
	}
	// Refuse directory links that could publish private playlists in another tree.
	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", err
	}
	realOutput, err := filepath.EvalSymlinks(output)
	if err != nil {
		return "", err
	}
	rel, err = filepath.Rel(realBase, realOutput)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errors.New("审计目录链接不能指向 data/tmp 之外")
	}
	return output, nil
}

func hongguoAuditWriteJSON(path string, value any, exclusive bool) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if exclusive {
		flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	}
	f, err := os.OpenFile(path, flags, 0600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(value); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func hongguoAuditKind(vod row, types map[int64]row) string {
	seen := map[int64]bool{}
	for id, depth := gconv.Int64(vod["type_id"]), 0; id > 0 && depth < 8 && !seen[id]; depth++ {
		seen[id] = true
		category := types[id]
		if category == nil {
			break
		}
		if kind := discoveryKind(gconv.String(category["name"])); kind != "" {
			return kind
		}
		id = gconv.Int64(category["pid"])
	}
	return discoveryKind(gconv.String(vod["class"]))
}

func hongguoAuditGroupAmbiguous(group []row, donorID, apiID int64, series string, item map[string]any, types map[int64]row) bool {
	var identities []discoveryTarget
	for _, candidate := range group {
		if other, valid := hongguoStoredSeries(candidate); valid && other != "" && other != series {
			return true
		}
		if gconv.Int64(candidate["id"]) == donorID {
			continue
		}
		target := hongguoCollectionTargetWithKind(candidate, hongguoAuditKind(candidate, types), apiID, series)
		if !hongguoCollectionMatches(candidate, target, apiID, item) {
			continue
		}
		for _, previous := range identities {
			if !hongguoCollectionAliasesCompatible(previous, target, apiID, series) {
				return true
			}
		}
		identities = append(identities, target)
	}
	return false
}

func RunHongguoAudit(ctx context.Context, options HongguoAuditOptions) (*HongguoAuditReport, error) {
	output, err := hongguoAuditOutputDirectory(options.OutputDir)
	if err != nil {
		return nil, err
	}
	var vods, collectors, categories []row
	// One repeatable-read snapshot, without the admin duplicate endpoint's limits.
	err = g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		result, e := tx.GetAll("SELECT id,api_id,api_vid,name,class,year,area,type_id,status,play_from,play_url FROM sx_vod ORDER BY id")
		if e != nil {
			return e
		}
		vods = gconv.Maps(result)
		result, e = tx.GetAll("SELECT id,api_url,status FROM sx_collect_api ORDER BY id")
		if e != nil {
			return e
		}
		collectors = gconv.Maps(result)
		result, e = tx.GetAll("SELECT id,name,pid,status FROM sx_type ORDER BY id")
		if e != nil {
			return e
		}
		categories = gconv.Maps(result)
		return nil
	})
	if err != nil {
		return nil, err
	}
	var collector row
	owners, types := map[int64]bool{}, map[int64]row{}
	var shortType int64
	for _, item := range collectors {
		if gconv.String(item["api_url"]) == hongguoSourceURL {
			owners[gconv.Int64(item["id"])] = true
			if collector == nil && gconv.Int(item["status"]) == 1 {
				collector = item
			}
		}
	}
	if collector == nil {
		return nil, errors.New("没有已启用的红果采集源")
	}
	for _, item := range categories {
		id := gconv.Int64(item["id"])
		types[id] = item
		if shortType == 0 && gconv.String(item["name"]) == "短剧" && gconv.Int64(item["pid"]) == 0 && gconv.Int(item["status"]) == 1 {
			shortType = id
		}
	}
	if shortType == 0 {
		return nil, errors.New("短剧主分类不存在或已停用")
	}
	repairs := map[int64]hongguoAuditRepair{}
	if options.RepairFile != "" {
		data, e := os.ReadFile(options.RepairFile)
		if e != nil {
			return nil, e
		}
		var items []hongguoAuditRepair
		if e = json.Unmarshal(data, &items); e != nil {
			return nil, e
		}
		for _, item := range items {
			repairs[item.ID] = item
		}
	}
	report := &HongguoAuditReport{ScannedAt: time.Now().Format(time.RFC3339), Scope: "本地已采集 sx_vod 全量（含停用记录）；不代表红果上游全目录", Scanned: len(vods)}
	originals, planned := map[int64]row{}, map[int64]row{}
	groups := map[string][]row{}
	actions := map[int64]*HongguoAuditAction{}
	addAction := func(id int64, donor int64, reason string) {
		if actions[id] == nil {
			actions[id] = &HongguoAuditAction{ID: id}
		}
		a := actions[id]
		a.Reasons = append(a.Reasons, reason)
		if donor > 0 {
			a.DonorIDs = append(a.DonorIDs, donor)
		}
	}
	for _, vod := range vods {
		id := gconv.Int64(vod["id"])
		report.MaxID = max(report.MaxID, id)
		originals[id] = vod
		copy := row{}
		for k, v := range vod {
			copy[k] = v
		}
		planned[id] = copy
		groups[discoveryNormalizeTitle(gconv.String(vod["name"]))] = append(groups[discoveryNormalizeTitle(gconv.String(vod["name"]))], copy)
	}
	for _, original := range vods {
		id := gconv.Int64(original["id"])
		vod := planned[id]
		series, valid := hongguoStoredSeries(vod)
		owned := owners[gconv.Int64(vod["api_id"])]
		hasNative := false
		for _, src := range playlist(vod) {
			if src.Code == "hongguo" {
				hasNative = true
			}
		}
		if !owned && !hasNative {
			continue
		}
		report.HongguoRecords++
		entry := HongguoAuditEntry{ID: id, Name: gconv.String(vod["name"]), Kind: hongguoAuditKind(vod, types), Series: series}
		for _, src := range playlist(vod) {
			if src.Code != "" {
				entry.Sources = append(entry.Sources, src.Code)
			}
		}
		if len(entry.Sources) > 1 {
			report.MixedRecords++
		}
		if !valid {
			entry.Problems = append(entry.Problems, "红果线路含无效分集或不同剧集/季ID")
		}
		if owned && !hasNative {
			entry.Problems = append(entry.Problems, "没有红果可播放分集")
		}
		if owned && valid && series != "" && series != gconv.String(vod["api_vid"]) {
			entry.Problems = append(entry.Problems, "红果线路与原始剧集ID不同")
		}
		if repair, exists := repairs[id]; exists && owned && (!valid || series != "" && series != gconv.String(vod["api_vid"])) {
			if !repair.Verified || repair.Name != gconv.String(vod["name"]) || repair.APIID != gconv.Int64(vod["api_id"]) || repair.APIVID != gconv.String(vod["api_vid"]) || discoveryNormalizeTitle(repair.RemoteTitle) != discoveryNormalizeTitle(repair.Name) || repair.OldHash != hongguoAuditPlaylistHash(original) {
				entry.Problems = append(entry.Problems, "远程修复证据或原播放列表已变化，跳过")
			} else {
				from, play, e := validateHongguoCollectedPlaylist(collector, repair.APIVID, repair.From, repair.Play)
				if e != nil {
					return nil, errors.New("修复文件包含无效红果线路")
				}
				incoming := playlist(row{"play_from": from, "play_url": play})
				vod["play_from"], vod["play_url"] = replaceHongguoCollectedLine(vod, incoming[0])
				addAction(id, 0, "按已核实的原始ID重建红果本季线路")
				report.NativeRepairs++
			}
		}
		// A native original's genre labels are not television/movie categories.
		// Foreign originals with a Hongguo line keep their independent category.
		if owned && onlyDigits(gconv.String(vod["api_vid"])) && entry.Kind != "short" {
			vod["type_id"] = shortType
			addAction(id, 0, "纠正原始红果影片的旧标签分类为短剧，保留class标签")
			report.CategoryRepairs++
		}
		report.Entries = append(report.Entries, entry)
	}
	// All native repairs/category plans are established before matching groups.
	for index := range report.Entries {
		entry := &report.Entries[index]
		donor := planned[entry.ID]
		series, valid := hongguoStoredSeries(donor)
		if !valid || series == "" {
			continue
		}
		if owners[gconv.Int64(donor["api_id"])] && series != gconv.String(donor["api_vid"]) {
			continue
		}
		item := map[string]any{"vod_id": series, "vod_name": donor["name"], "type_name": "短剧", "vod_year": donor["year"], "vod_area": donor["area"]}
		group := groups[discoveryNormalizeTitle(gconv.String(donor["name"]))]
		ambiguous := hongguoAuditGroupAmbiguous(group, entry.ID, gconv.Int64(collector["id"]), series, item, types)
		if ambiguous {
			entry.Problems = append(entry.Problems, "同名候选有多个未核实或冲突版本，保留待核实")
		}
		var native source
		for _, src := range playlist(donor) {
			if src.Code == "hongguo" {
				native = src
				break
			}
		}
		for _, target := range group {
			id := gconv.Int64(target["id"])
			if id == entry.ID {
				continue
			}
			// Existing native records are already synchronized by remote identity.
			if owners[gconv.Int64(target["api_id"])] {
				continue
			}
			entry.Candidates = append(entry.Candidates, id)
			report.CrossSourcePairs++
			kind := hongguoAuditKind(target, types)
			identity := hongguoCollectionTargetWithKind(target, kind, gconv.Int64(collector["id"]), series)
			candidate := HongguoAuditCandidate{ID: id, Kind: kind, Year: gconv.String(target["year"]), Area: gconv.String(target["area"]), Status: "身份匹配"}
			if ambiguous || !hongguoCollectionMatches(target, identity, gconv.Int64(collector["id"]), item) {
				candidate.Status, candidate.Reason = "待核实", hongguoAuditPendingReason(identity, item, ambiguous)
				entry.CandidateDetails = append(entry.CandidateDetails, candidate)
				report.PendingPairs++
				continue
			}
			entry.CandidateDetails = append(entry.CandidateDetails, candidate)
			report.ConfirmedPairs++
			merged, added, updated := mergeDiscoveryPlaylists(playlist(target), []source{native})
			if added+updated == 0 {
				continue
			}
			from, play := serializeDiscoverySources(merged)
			if len(from) > maxPlaybackSourceNames || len(play) > 1<<20 {
				return nil, errors.New("审计合并播放列表超过存储限制")
			}
			target["play_from"], target["play_url"] = from, play
			addAction(id, entry.ID, "已核实同名短剧，补齐红果线路并保留已有来源")
			report.ResourceCopies++
		}
	}
	ids := make([]int64, 0, len(actions))
	for id := range actions {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		report.Actions = append(report.Actions, *actions[id])
	}
	report.ChangedRecords = len(ids)
	if err = hongguoAuditWriteJSON(filepath.Join(output, "report.safe.json"), report, false); err != nil {
		return nil, err
	}
	if !options.Apply || len(ids) == 0 {
		return report, nil
	}
	// Include donor evidence in the durable backup, not only modified targets.
	footprint := map[int64]bool{}
	for _, action := range report.Actions {
		footprint[action.ID] = true
		for _, id := range action.DonorIDs {
			footprint[id] = true
		}
		// All same-title candidates are evidence for the ambiguity gate, including
		// rows that were deliberately left unchanged. Revalidate those too.
		if len(action.DonorIDs) > 0 {
			name := discoveryNormalizeTitle(gconv.String(originals[action.ID]["name"]))
			for _, candidate := range groups[name] {
				footprint[gconv.Int64(candidate["id"])] = true
			}
		}
	}
	lockedIDs := make([]int64, 0, len(footprint))
	backup := []row{}
	for id := range footprint {
		lockedIDs = append(lockedIDs, id)
	}
	sort.Slice(lockedIDs, func(i, j int) bool { return lockedIDs[i] < lockedIDs[j] })
	for _, id := range lockedIDs {
		backup = append(backup, originals[id])
	}
	if err = hongguoAuditWriteJSON(filepath.Join(output, "before.private.json"), backup, true); err != nil {
		return nil, err
	}
	err = g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		// Verify the entire footprint before writing. Concurrent collection may
		// append resources; it must never be replaced by an older audit snapshot.
		for _, id := range lockedIDs {
			fresh, e := tx.GetOne("SELECT id,api_id,api_vid,name,class,year,area,type_id,status,play_from,play_url FROM sx_vod WHERE id=? FOR UPDATE", id)
			if e != nil {
				return e
			}
			if fresh == nil || hongguoAuditRowHash(gconv.Map(fresh)) != hongguoAuditRowHash(originals[id]) {
				return fmt.Errorf("影片 %d 在审计后发生变化，请重新扫描", id)
			}
		}
		fresh, e := tx.GetOne("SELECT id,api_url,status FROM sx_collect_api WHERE id=? FOR UPDATE", collector["id"])
		if e != nil {
			return e
		}
		if !hongguoCollectionSourceEnabled(gconv.Map(fresh)) {
			return errors.New("红果采集源已停用，取消修复")
		}
		freshTypes, e := tx.GetAll("SELECT id,name,pid,status FROM sx_type ORDER BY id FOR UPDATE")
		if e != nil {
			return e
		}
		if discoveryHash(gconv.Maps(freshTypes)) != discoveryHash(categories) {
			return errors.New("分类在审计后发生变化，请重新扫描")
		}
		for _, id := range ids {
			old, next := originals[id], planned[id]
			typeChanged := gconv.Int64(old["type_id"]) != gconv.Int64(next["type_id"])
			playChanged := hongguoAuditPlaylistHash(old) != hongguoAuditPlaylistHash(next)
			var e error
			switch {
			case typeChanged && playChanged:
				_, e = tx.Exec("UPDATE sx_vod SET type_id=?,play_from=?,play_url=? WHERE id=?", next["type_id"], next["play_from"], next["play_url"], id)
			case typeChanged:
				_, e = tx.Exec("UPDATE sx_vod SET type_id=? WHERE id=?", next["type_id"], id)
			case playChanged:
				_, e = tx.Exec("UPDATE sx_vod SET play_from=?,play_url=? WHERE id=?", next["play_from"], next["play_url"], id)
			}
			if e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	report.Applied = true
	if err = hongguoAuditWriteJSON(filepath.Join(output, "report.safe.json"), report, false); err != nil {
		return report, err
	}
	return report, nil
}
