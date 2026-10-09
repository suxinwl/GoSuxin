package suxinvideo

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

func vodAliasOwnerSource(src source) source {
	if src.BaseCode != "" {
		src.Code = src.BaseCode
	}
	return src
}

func vodAliasMembers(ctx context.Context, id int64) ([]row, error) {
	return all(ctx, "SELECT v.*,a.canonical_id AS __alias_canonical FROM sx_vod_alias a JOIN sx_vod v ON v.id=a.vod_id WHERE a.canonical_id=(SELECT canonical_id FROM sx_vod_alias WHERE vod_id=?) ORDER BY (v.id=?) DESC,v.id", id, id)
}

func vodAliasMemberAccessible(member row, user row, unlocked map[int64]bool, now int64) bool {
	if gconv.Int(member["status"]) != 1 {
		return false
	}
	if gconv.Int(member["vip"]) == 1 && (user == nil || gconv.Int64(user["vip_expire"]) <= now) {
		return false
	}
	return gconv.Int(member["points"]) <= 0 || user != nil && unlocked[gconv.Int64(member["id"])]
}

func vodAliasVersionLabel(member row) string {
	name := strings.TrimSpace(gconv.String(member["name"]))
	year := strings.TrimSpace(gconv.String(member["year"]))
	if discoveryYearPattern.MatchString(year) {
		name += " · " + year
	}
	return name
}

var vodAliasSeasonPattern = regexp.MustCompile(`第([0-9零〇一二两三四五六七八九十百]+)季`)

func vodAliasSeasonNumber(raw string) int {
	if number, err := strconv.Atoi(raw); err == nil && number > 0 {
		return number
	}
	digits := map[rune]int{'零': 0, '〇': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	total, pending := 0, 0
	for _, ch := range raw {
		if ch == '十' || ch == '百' {
			if pending == 0 {
				pending = 1
			}
			unit := 10
			if ch == '百' {
				unit = 100
			}
			total += pending * unit
			pending = 0
		} else if digit, ok := digits[ch]; ok {
			pending = digit
		} else {
			return 0
		}
	}
	return total + pending
}

// Approved aliases share resources, while format and explicit season still
// define interchangeable episodes. Missing seasons do not imply season one.
// Missing or conflicting short-drama years do not split one approved version.
func vodAliasVersionKey(member row) string {
	id, canonical := gconv.Int64(member["id"]), gconv.Int64(member["__alias_canonical"])
	if canonical <= 0 {
		return fmt.Sprintf("vod:%d", id)
	}
	kind := gconv.String(member["__alias_kind"])
	if kind == "" {
		return fmt.Sprintf("alias:%d:unknown:%d", canonical, id)
	}
	season := "unspecified"
	if match := vodAliasSeasonPattern.FindStringSubmatch(gconv.String(member["name"])); len(match) == 2 {
		if number := vodAliasSeasonNumber(match[1]); number > 0 {
			season = strconv.Itoa(number)
		}
	}
	key := fmt.Sprintf("alias:%d:%s:season:%s", canonical, kind, season)
	if kind != "short" {
		year := strings.TrimSpace(gconv.String(member["year"]))
		if !discoveryYearPattern.MatchString(year) {
			year = "unspecified"
		}
		key += ":year:" + year
	}
	return key
}

func vodAliasMemberKind(member row, categories map[int64]row, confirmedShortSeries map[string]bool) string {
	typeID := gconv.Int64(member["type_id"])
	if kind := ordinaryCollectionCategoryProof(typeID, categories); kind != "" {
		return kind
	}
	// An approved member's matching native series can establish the format for
	// an orphan genre such as 重生民国, including mixed-provider rows. Another
	// season's native ID cannot supply that evidence, and explicit non-short
	// category/class metadata prevents inferring a different format.
	if kind := discoveryKind(gconv.String(member["class"])); kind != "" && kind != "short" {
		return ""
	}
	seen := map[int64]bool{}
	for depth := 0; typeID > 0 && depth < 32 && !seen[typeID]; depth++ {
		seen[typeID] = true
		category := categories[typeID]
		if category == nil {
			break
		}
		if kind := discoveryKind(gconv.String(category["name"])); kind != "" && kind != "short" {
			return ""
		}
		typeID = gconv.Int64(category["pid"])
	}
	if series, valid := hongguoStoredSeries(member); valid && series != "" && confirmedShortSeries[series] {
		return "short"
	}
	return ""
}

func vodAliasListingCondition(ctx context.Context, alias string) string {
	if alias == "" {
		alias = "sx_vod"
	}
	// A disabled or policy-blocked canonical record must not hide its visible
	// aliases. The lowest currently public member represents the group in lists.
	return "NOT EXISTS (SELECT 1 FROM sx_vod_alias va_current JOIN sx_vod_alias va_other ON va_other.canonical_id=va_current.canonical_id JOIN sx_vod va_film ON va_film.id=va_other.vod_id WHERE va_current.vod_id=" + alias + ".id AND va_other.vod_id<" + alias + ".id AND " + publicVodCondition(ctx, "va_film") + ")"
}

func publicVodListingCondition(ctx context.Context, alias string) string {
	return publicVodCondition(ctx, alias) + " AND " + vodAliasListingCondition(ctx, alias)
}

// Every source remains in its original owner's database row. Composite codes
// identify versions in the UI; native resolution uses OwnerVodID and BaseCode.
// The requesting member is first, preserving old plain-line bookmarks.
func composeVodAliasSources(currentID int64, members []row, byMember map[int64][]source) []source {
	result := []source{}
	seen := map[string]bool{}
	for _, member := range members {
		id := gconv.Int64(member["id"])
		for _, src := range byMember[id] {
			key := discoveryHash([]any{vodAliasVersionKey(member), src.Code, src.Parse, src.Episodes})
			if seen[key] {
				continue
			}
			seen[key] = true
			src.OwnerVodID, src.BaseCode = id, src.Code
			src.VersionLabel, src.VersionKey = vodAliasVersionLabel(member), vodAliasVersionKey(member)
			if id != currentID {
				src.Code = fmt.Sprintf("alias_%d_%s", id, src.BaseCode)
				src.Name += " · " + src.VersionLabel
			}
			result = append(result, src)
		}
	}
	return result
}

func vodAliasAccessibleSources(ctx context.Context, vod row, original []source, players map[string]row, collectors []row) ([]source, error) {
	id := gconv.Int64(vod["id"])
	members, err := vodAliasMembers(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(members) < 2 {
		visible := dedupePageSources(availableSources(vod, original, players, collectors))
		// Single-record films still need an explicit stable version identity for
		// native downloads, TV playback and cross-client history. Empty legacy
		// request versions remain valid and resolve to this record's version.
		for index := range visible {
			visible[index].OwnerVodID = id
			visible[index].BaseCode = visible[index].Code
			visible[index].VersionKey = vodAliasVersionKey(vod)
			visible[index].VersionLabel = vodAliasVersionLabel(vod)
		}
		return filterUnhealthySources(ctx, id, visible), nil
	}
	categories, err := all(ctx, "SELECT id,name,pid FROM sx_type ORDER BY id")
	if err != nil {
		return nil, err
	}
	byType := make(map[int64]row, len(categories))
	for _, category := range categories {
		byType[gconv.Int64(category["id"])] = category
	}
	confirmedShortSeries := map[string]bool{}
	for _, member := range members {
		if ordinaryCollectionCategoryProof(gconv.Int64(member["type_id"]), byType) == "short" {
			if series, valid := hongguoStoredSeries(member); valid && series != "" {
				confirmedShortSeries[series] = true
			}
		}
	}
	for _, member := range members {
		member["__alias_kind"] = vodAliasMemberKind(member, byType, confirmedShortSeries)
	}
	var user row
	// Background health/discovery checks have no HTTP session. They must see
	// public aliases only, rather than querying a nil request or exposing VIP.
	if g.RequestFromCtx(ctx) != nil {
		user, err = currentUser(ctx)
		if err != nil {
			return nil, err
		}
	}
	unlocked := make(map[int64]bool)
	if user != nil {
		owned, err := all(ctx, "SELECT vod_id FROM sx_user_vod WHERE user_id=?", user["id"])
		if err != nil {
			return nil, err
		}
		for _, record := range owned {
			unlocked[gconv.Int64(record["vod_id"])] = true
		}
	}
	byMember := make(map[int64][]source)
	for _, member := range members {
		memberID := gconv.Int64(member["id"])
		if memberID != id && (!vodAliasMemberAccessible(member, user, unlocked, time.Now().Unix()) || !contentVodAllowed(ctx, member)) {
			continue
		}
		input := playlist(member)
		if memberID == id {
			input = original
		}
		visible := dedupePageSources(availableSources(member, input, players, collectors))
		byMember[memberID] = filterUnhealthySources(ctx, memberID, visible)
	}
	return composeVodAliasSources(id, members, byMember), nil
}

// Explicit groups only bypass a former identity conflict for a source already
// pinned to this member. They do not allow a new unknown remake or native ID to
// replace another season's raw line in the same row.
func vodAliasCollectionBound(ctx context.Context, vod row, apiID int64, item map[string]any) (bool, error) {
	group, err := one(ctx, "SELECT canonical_id FROM sx_vod_alias WHERE vod_id=?", vod["id"])
	if err != nil || group == nil {
		return false, err
	}
	remoteID := gconv.String(item["vod_id"])
	bound := gconv.Int64(vod["api_id"]) == apiID && gconv.String(vod["api_vid"]) == remoteID
	if !bound && gconv.String(item["vod_play_from"]) == "hongguo" {
		series, valid := hongguoStoredSeries(vod)
		bound = valid && series == remoteID
	}
	if !bound {
		return false, nil
	}
	members, err := vodAliasMembers(ctx, gconv.Int64(vod["id"]))
	if err != nil {
		return false, err
	}
	for _, member := range members {
		if discoveryNormalizeTitle(gconv.String(member["name"])) == discoveryNormalizeTitle(gconv.String(item["vod_name"])) {
			return true, nil
		}
	}
	return false, nil
}
