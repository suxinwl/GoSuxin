package suxinvideo

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	_ "github.com/suxinwl/GoSuxin/framework/contrib/drivers/mysql"
	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

func TestContentPolicyDefaultsAndExplicitEmptyRules(t *testing.T) {
	p := newContentPolicy(nil, nil)
	if len(p.keywords) != 8 || len(p.categories) != 5 {
		t.Fatal("default title/category policy differs from the settings defaults")
	}
	for _, title := range []string{"换妻影片", "情色剧情", "成人影片合集", "三级片"} {
		if p.vodAllowed(row{"name": title}) {
			t.Fatalf("blocked title was allowed: %s", title)
		}
	}
	for _, item := range []row{{"name": "Avatar", "class": "科幻"}, {"name": "家庭伦理故事", "class": "家庭伦理"}, {"name": "正常剧情", "class": "爱情片"}} {
		if !p.vodAllowed(item) {
			t.Fatalf("ordinary title/category was accidentally blocked: %+v", item)
		}
	}
	empty := newContentPolicy(map[string]string{"content_block_keywords": "\n\r\n ", "content_block_categories": ""}, nil)
	if len(empty.keywords) != 0 || len(empty.categories) != 0 || !empty.vodAllowed(row{"name": "换妻影片", "class": "伦理片"}) || empty.sqlCondition("v") != "" {
		t.Fatal("saved blank rule groups were replaced with defaults")
	}
	disabled := newContentPolicy(map[string]string{"content_block_enable": "0"}, nil)
	if !disabled.macAllowed(map[string]any{"vod_name": "换妻", "type_name": "伦理片"}) || disabled.sqlCondition("") != "" {
		t.Fatal("disabled filtering remained active")
	}
	if p.vodAllowed(nil) || p.macAllowed(nil) || p.remoteAllowed(nil) {
		t.Fatal("missing content was treated as a valid public film")
	}
}

func TestContentPolicyLocalAncestorsAndCycles(t *testing.T) {
	types := []row{
		{"id": 90, "pid": 0, "name": "伦理片"},
		{"id": 91, "pid": 90, "name": "其它"},
		{"id": 92, "pid": 93, "name": "普通分类"},
		{"id": 93, "pid": 92, "name": "循环分类"},
		{"id": 94, "pid": 95, "name": "中性分类"},
		{"id": 95, "pid": 94, "name": "情色"},
		{"id": 96, "pid": 999, "name": "动漫"},
	}
	p := newContentPolicy(nil, types)
	for _, id := range []int64{90, 91, 94, 95} {
		if p.vodAllowed(row{"name": "中性标题", "type_id": id}) {
			t.Fatalf("blocked ancestor/category was not inherited by %d", id)
		}
	}
	for _, id := range []int64{92, 93, 96, 999} {
		if !p.vodAllowed(row{"name": "中性标题", "type_id": id}) {
			t.Fatalf("cycle or missing ancestor hid an unrelated category: %d", id)
		}
	}
	sql := p.sqlCondition("v")
	if !strings.Contains(sql, "v.type_id IN (90,91,94,95)") {
		t.Fatalf("SQL category inheritance differs from Go: %s", sql)
	}
	// A disabled policy must not hide a descendant through the ancestor index.
	off := newContentPolicy(map[string]string{"content_block_enable": "0"}, types)
	if !off.vodAllowed(row{"name": "普通名称", "type_id": 91}) || off.sqlCondition("v") != "" {
		t.Fatal("disabled category inheritance remained active")
	}
}

func TestContentPolicyRemoteIDsNeverUseLocalCategories(t *testing.T) {
	p := newContentPolicy(nil, []row{{"id": 8, "pid": 0, "name": "伦理片"}})
	remote := map[string]any{"vodName": "仙逆", "flags": "2023 / 动漫 / 中国", "type_id": 8, "channelId": 8}
	if !p.remoteAllowed(remote) || !p.macAllowed(map[string]any{"vod_name": "仙逆", "type_name": "动漫", "type_id": 8}) {
		t.Fatal("provider category ID was mistaken for a blocked sx_type ID")
	}
	for _, remote := range []map[string]any{
		{"vodName": "普通名称", "flags": "2023 / 伦理片 / 中国"},
		{"vodName": "普通名称", "tagList": []string{"剧情", "情色"}},
		{"vodName": "成人视频", "flags": "2023 / 电影 / 中国"},
		{"name": "普通名称", "class": "成人影视"},
	} {
		if p.remoteAllowed(remote) {
			t.Fatalf("remote title/category bypassed the policy: %+v", remote)
		}
	}
	if p.macAllowed(map[string]any{"vod_name": "普通名称", "vod_class": "剧情,情色"}) {
		t.Fatal("collection tag metadata bypassed the category rules")
	}
}

func TestContentPolicySQLUsesCaseFoldedLiteralSubstrings(t *testing.T) {
	value := " Secret \n100%\nunder_score\n' OR 1=1 --\né"
	p := newContentPolicy(map[string]string{"content_block_keywords": value, "content_block_categories": ""}, nil)
	for _, title := range []string{"The SECRET film", "rating 100%", "under_score", "' OR 1=1 --", "é"} {
		if p.vodAllowed(row{"name": title}) {
			t.Fatalf("literal rule was not applied: %s", title)
		}
	}
	for _, title := range []string{"rating 100", "underXscore", "e", "Avatar"} {
		if !p.vodAllowed(row{"name": title}) {
			t.Fatalf("wildcard or accent folding changed a literal rule: %s", title)
		}
	}
	sql := p.sqlCondition("v")
	if strings.Contains(sql, "LIKE") || strings.Contains(sql, "' OR 1=1 --") || !strings.Contains(sql, "LOWER(COALESCE(v.name,'')) COLLATE utf8mb4_bin") {
		t.Fatalf("SQL changed literal/case matching or interpolated user text: %s", sql)
	}
	for _, rule := range p.keywords {
		literal := "CONVERT(0x" + hex.EncodeToString([]byte(rule)) + " USING utf8mb4)"
		if !strings.Contains(sql, literal) {
			t.Fatalf("SQL did not encode rule %q as a literal", rule)
		}
	}
	if !strings.Contains(p.sqlCondition(""), "LOWER(COALESCE(name,''))") {
		t.Fatal("unqualified SQL used an invalid column prefix")
	}
}

func TestContentRuleValidationBoundsAndBlankLines(t *testing.T) {
	for _, value := range []string{"", " \n\r\n\t", strings.Repeat("规", 120), strings.Repeat("重复\r\n", 200)} {
		if err := validateContentRuleText(value); err != nil {
			t.Fatalf("valid rule text was rejected: %v", err)
		}
	}
	for _, value := range []string{strings.Repeat("规", 121), strings.Repeat("重复\n", 201), string([]byte{0xff})} {
		if err := validateContentRuleText(value); err == nil {
			t.Fatal("unbounded or invalid rule text was accepted")
		}
	}
	p := newContentPolicy(map[string]string{"content_block_keywords": " SECRET \r\nsecret\n\n甲", "content_block_categories": ""}, nil)
	if len(p.keywords) != 2 || p.keywords[0] != "secret" || p.keywords[1] != "甲" {
		t.Fatalf("rule whitespace/case duplicates were not normalized: %v", p.keywords)
	}
}

// This acceptance gate performs SELECTs only against the existing development
// database. It is separate from the integration fixtures that insert data.
func TestContentPolicyDatabaseConsistency(t *testing.T) {
	if os.Getenv("SUXIN_CONTENT_ACCEPTANCE") != "1" {
		t.Skip("set SUXIN_CONTENT_ACCEPTANCE=1 for the read-only content policy acceptance check")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(filepath.Clean("../../..")); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	configs, err := all(ctx, "SELECT `key`,`value` FROM sx_config WHERE `key` IN ('content_block_enable','content_block_keywords','content_block_categories')")
	if err != nil {
		t.Fatal(err)
	}
	types, err := all(ctx, "SELECT id,pid,name FROM sx_type")
	if err != nil {
		t.Fatal(err)
	}
	values := make(map[string]string, len(configs))
	for _, item := range configs {
		values[gconv.String(item["key"])] = gconv.String(item["value"])
	}
	invalidateContentPolicyCache()
	policy := loadContentPolicy(ctx)
	if !reflect.DeepEqual(policy, newContentPolicy(values, types)) {
		t.Fatal("cached content policy differs from the current database rules/categories")
	}
	var total, allowed, hidden int
	var blockedSamples []row
	err = g.DB().TransactionWithOptions(ctx, gdb.TxOptions{Propagation: gdb.PropagationRequiresNew, Isolation: sql.LevelRepeatableRead, ReadOnly: true}, func(_ context.Context, tx gdb.TX) error {
		films, err := tx.GetAll("SELECT id,name,class,type_id,api_id,api_vid FROM sx_vod ORDER BY id")
		if err != nil {
			return err
		}
		visible, err := tx.GetAll("SELECT id FROM sx_vod WHERE 1=1" + policy.sqlCondition("") + " ORDER BY id")
		if err != nil {
			return err
		}
		sources, err := tx.GetAll("SELECT id FROM sx_collect_api WHERE api_url=?", yqkSourceURL)
		if err != nil {
			return err
		}
		visibleIDs := make(map[int64]bool, len(visible))
		for _, item := range visible {
			visibleIDs[item["id"].Int64()] = true
		}
		yqkSources := make(map[int64]bool, len(sources))
		for _, item := range sources {
			yqkSources[item["id"].Int64()] = true
		}
		for _, item := range films {
			film := gconv.Map(item)
			id := gconv.Int64(film["id"])
			goAllowed := policy.vodAllowed(film)
			if goAllowed != visibleIDs[id] {
				return fmt.Errorf("content policy Go/SQL mismatch for film ID %d: Go=%v SQL=%v", id, goAllowed, visibleIDs[id])
			}
			total++
			if goAllowed {
				allowed++
			} else {
				hidden++
				if len(blockedSamples) < 2 && yqkSources[gconv.Int64(film["api_id"])] && yqkPositiveID(gconv.String(film["api_vid"])) {
					blockedSamples = append(blockedSamples, row{"id": id, "api_vid": gconv.String(film["api_vid"])})
				}
			}
		}
		if allowed != len(visibleIDs) {
			return fmt.Errorf("content policy ID set count differs: Go=%d SQL=%d", allowed, len(visibleIDs))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("checked %d films: allowed=%d hidden=%d; Go and SQL ID sets are identical", total, allowed, hidden)
	for _, sample := range blockedSamples {
		sample["url"] = yqkSiteVodLink(ctx, gconv.String(sample["api_vid"]))
		delete(sample, "api_vid")
	}
	data, err := json.MarshalIndent(blockedSamples, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join("data", "tmp"), 0750); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join("data", "tmp", "content-blocked-links.json")
	if err = os.WriteFile(artifact, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("saved %d blocked-entry acceptance links to %s (URLs omitted)", len(blockedSamples), artifact)
}
