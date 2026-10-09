package suxinvideo

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

const (
	defaultContentBlockKeywords   = "擦边\n换妻\n情色\n色情\n成人视频\n成人影片\n三级电影\n三级片"
	defaultContentBlockCategories = "伦理片\n情色\n色情\n成人影视\n三级电影"
	contentBlockMaxRules          = 200
	contentBlockMaxRuleRunes      = 120
	contentPolicyCacheTTL         = 2 * time.Second
)

type contentPolicy struct {
	enabled      bool
	keywords     []string
	categories   []string
	blockedTypes map[int64]bool
}

var contentPolicyCache struct {
	sync.Mutex
	policy *contentPolicy
	until  time.Time
}

// Rules are one literal keyword per line. Empty input intentionally disables
// that rule group; callers must not substitute defaults for a saved empty value.
func validateContentRuleText(value string) error {
	if !utf8.ValidString(value) {
		return errors.New("内容过滤规则必须使用有效 UTF-8 文本")
	}
	count := 0
	for _, line := range contentRuleLines(value) {
		if line == "" {
			continue
		}
		count++
		if count > contentBlockMaxRules {
			return fmt.Errorf("内容过滤规则最多允许 %d 条非空规则", contentBlockMaxRules)
		}
		if utf8.RuneCountInString(line) > contentBlockMaxRuleRunes {
			return fmt.Errorf("每条内容过滤规则最多允许 %d 个字符", contentBlockMaxRuleRunes)
		}
	}
	return nil
}

func contentRuleLines(value string) []string {
	lines := strings.Split(strings.ReplaceAll(value, "\r", "\n"), "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return lines
}

func contentRules(value string) []string {
	result := make([]string, 0)
	seen := map[string]bool{}
	for _, line := range contentRuleLines(value) {
		if line == "" || !utf8.ValidString(line) || utf8.RuneCountInString(line) > contentBlockMaxRuleRunes {
			continue
		}
		line = strings.ToLower(line)
		if !seen[line] {
			seen[line] = true
			result = append(result, line)
			if len(result) == contentBlockMaxRules {
				break
			}
		}
	}
	return result
}

func contentConfigValue(values map[string]string, key, fallback string) string {
	if value, exists := values[key]; exists {
		return value
	}
	return fallback
}

func newContentPolicy(values map[string]string, types []row) *contentPolicy {
	p := &contentPolicy{
		enabled:      contentConfigValue(values, "content_block_enable", "1") == "1",
		keywords:     contentRules(contentConfigValue(values, "content_block_keywords", defaultContentBlockKeywords)),
		categories:   contentRules(contentConfigValue(values, "content_block_categories", defaultContentBlockCategories)),
		blockedTypes: map[int64]bool{},
	}
	byID := make(map[int64]row, len(types))
	for _, category := range types {
		if id := gconv.Int64(category["id"]); id > 0 {
			byID[id] = category
		}
	}
	// Descendants inherit the category rule even when their own name is neutral.
	// IDs here belong only to sx_type; remote provider IDs never use this map.
	for id := range byID {
		seen := map[int64]bool{}
		for current := id; current > 0 && !seen[current]; {
			seen[current] = true
			category, exists := byID[current]
			if !exists {
				break
			}
			if contentTextMatches(gconv.String(category["name"]), p.categories) {
				p.blockedTypes[id] = true
				break
			}
			current = gconv.Int64(category["pid"])
		}
	}
	return p
}

func invalidateContentPolicyCache() {
	contentPolicyCache.Lock()
	contentPolicyCache.until = time.Time{}
	contentPolicyCache.Unlock()
}

func loadContentPolicy(ctx context.Context) *contentPolicy {
	contentPolicyCache.Lock()
	defer contentPolicyCache.Unlock()
	if contentPolicyCache.policy != nil && time.Now().Before(contentPolicyCache.until) {
		return contentPolicyCache.policy
	}
	if ctx == nil {
		ctx = context.Background()
	}
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	configs, configErr := all(readCtx, "SELECT `key`,`value` FROM sx_config WHERE `key` IN ('content_block_enable','content_block_keywords','content_block_categories')")
	types, typeErr := all(readCtx, "SELECT id,pid,name FROM sx_type")
	if configErr == nil && typeErr == nil {
		values := make(map[string]string, len(configs))
		for _, value := range configs {
			values[gconv.String(value["key"])] = gconv.String(value["value"])
		}
		contentPolicyCache.policy = newContentPolicy(values, types)
	} else if contentPolicyCache.policy == nil {
		// Preserve the last valid rules during a transient DB failure. Until the
		// first successful read, the configured default policy remains active.
		contentPolicyCache.policy = newContentPolicy(nil, nil)
	}
	contentPolicyCache.until = time.Now().Add(contentPolicyCacheTTL)
	return contentPolicyCache.policy
}

func contentTextMatches(value string, rules []string) bool {
	value = strings.ToLower(value)
	for _, rule := range rules {
		if strings.Contains(value, rule) {
			return true
		}
	}
	return false
}

func (p *contentPolicy) allowed(title, category string, localTypeID int64) bool {
	return !p.enabled || (!contentTextMatches(title, p.keywords) && !contentTextMatches(category, p.categories) && !p.blockedTypes[localTypeID])
}

func (p *contentPolicy) macAllowed(item map[string]any) bool {
	return item != nil && p.allowed(gconv.String(item["vod_name"]), gconv.String(item["type_name"])+"\n"+gconv.String(item["vod_class"]), 0)
}

func (p *contentPolicy) remoteAllowed(item map[string]any) bool {
	if item == nil {
		return false
	}
	_, kind, _ := yqkListMetadata(item)
	titles := []string{gconv.String(item["vodName"]), gconv.String(item["name"]), gconv.String(item["title"])}
	categories := []string{kind}
	for _, field := range []string{"type_name", "typeName", "categoryName", "channelName", "vodClass", "class", "tagList", "tags"} {
		categories = append(categories, gconv.String(item[field]))
	}
	return p.allowed(strings.Join(titles, "\n"), strings.Join(categories, "\n"), 0)
}

func (p *contentPolicy) vodAllowed(item row) bool {
	return item != nil && p.allowed(gconv.String(item["name"]), gconv.String(item["class"]), gconv.Int64(item["type_id"]))
}

func contentMacAllowed(ctx context.Context, item map[string]any) bool {
	return loadContentPolicy(ctx).macAllowed(item)
}

func contentRemoteAllowed(ctx context.Context, raw map[string]any) bool {
	return loadContentPolicy(ctx).remoteAllowed(raw)
}

func contentVodAllowed(ctx context.Context, item row) bool {
	return loadContentPolicy(ctx).vodAllowed(item)
}

// SQL and Go both use case-folded literal substrings. Binary collation avoids
// accent-insensitive DB matching that Go strings.Contains does not perform.
func (p *contentPolicy) sqlCondition(alias string) string {
	if !p.enabled {
		return ""
	}
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	blocked := make([]string, 0, len(p.keywords)+len(p.categories)+1)
	appendRules := func(column string, rules []string) {
		for _, rule := range rules {
			literal := "CONVERT(0x" + hex.EncodeToString([]byte(rule)) + " USING utf8mb4)"
			blocked = append(blocked, "INSTR(LOWER(COALESCE("+prefix+column+",'')) COLLATE utf8mb4_bin,"+literal+" COLLATE utf8mb4_bin)>0")
		}
	}
	appendRules("name", p.keywords)
	appendRules("class", p.categories)
	ids := make([]int64, 0, len(p.blockedTypes))
	for id := range p.blockedTypes {
		ids = append(ids, id)
	}
	if len(ids) > 0 {
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		values := make([]string, len(ids))
		for i, id := range ids {
			values[i] = strconv.FormatInt(id, 10)
		}
		blocked = append(blocked, prefix+"type_id IN ("+strings.Join(values, ",")+")")
	}
	if len(blocked) == 0 {
		return ""
	}
	return " AND NOT (" + strings.Join(blocked, " OR ") + ")"
}
