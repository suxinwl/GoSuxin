package suxinvideo

import (
	"context"
	"strings"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

// This state belongs to one incoming item. Loading the local category tree is
// lazy and shared by every candidate and the final revalidation in its upsert.
type ordinaryCollectionMatcher struct {
	apiID          int64
	item           map[string]any
	loadCategories func(context.Context) ([]row, error)
	categoryKind   func(context.Context, int64) (string, error)
	loaded         bool
	loadErr        error
	categories     map[int64]row
	matchingItem   map[string]any
}

func newOrdinaryCollectionMatcher(apiID int64, item map[string]any) *ordinaryCollectionMatcher {
	return &ordinaryCollectionMatcher{apiID: apiID, item: item, categoryKind: discoveryCategoryKind,
		loadCategories: func(ctx context.Context) ([]row, error) {
			return all(ctx, "SELECT id,name,pid FROM sx_type ORDER BY id")
		}}
}

func ordinaryCollectionLocalKind(typeID int64, categories map[int64]row) string {
	seen := map[int64]bool{}
	for depth := 0; typeID > 0 && depth < 8 && !seen[typeID]; depth++ {
		seen[typeID] = true
		category := categories[typeID]
		if category == nil {
			return ""
		}
		if kind := discoveryKind(gconv.String(category["name"])); kind != "" {
			return kind
		}
		typeID = gconv.Int64(category["pid"])
	}
	return ""
}

// The provider's unknown genre may be interpreted only from a complete local
// ancestry. Missing parents, cycles, mixed format ancestors or an unresolved
// root do not establish the short-drama format.
func ordinaryCollectionCategoryProof(typeID int64, categories map[int64]row) string {
	seen, result := map[int64]bool{}, ""
	for depth := 0; typeID > 0 && depth < 32; depth++ {
		if seen[typeID] {
			return ""
		}
		seen[typeID] = true
		category := categories[typeID]
		if category == nil {
			return ""
		}
		if kind := discoveryKind(gconv.String(category["name"])); kind != "" {
			if result != "" && result != kind {
				return ""
			}
			result = kind
		}
		typeID = gconv.Int64(category["pid"])
		if typeID == 0 {
			return result
		}
	}
	return ""
}

func ordinaryCollectionMatchItemWithCategories(item map[string]any, categories map[int64]row) map[string]any {
	typeName := strings.TrimSpace(gconv.String(item["type_name"]))
	if typeName == "" || discoveryKind(typeName) != "" {
		return item
	}
	found := false
	for id, category := range categories {
		if strings.TrimSpace(gconv.String(category["name"])) != typeName {
			continue
		}
		found = true
		if ordinaryCollectionCategoryProof(id, categories) != "short" {
			return item
		}
	}
	if !found {
		return item
	}
	copy := make(map[string]any, len(item))
	for key, value := range item {
		copy[key] = value
	}
	copy["type_name"] = "短剧"
	return copy
}

func (matcher *ordinaryCollectionMatcher) compatible(ctx context.Context, vod row) (bool, error) {
	var kind string
	if matcher.loaded {
		if matcher.loadErr != nil {
			return false, matcher.loadErr
		}
		kind = ordinaryCollectionLocalKind(gconv.Int64(vod["type_id"]), matcher.categories)
	} else {
		var err error
		kind, err = matcher.categoryKind(ctx, gconv.Int64(vod["type_id"]))
		if err != nil {
			return false, err
		}
	}
	item := matcher.item
	series, nativeValid := hongguoStoredSeries(vod)
	if kind == "short" && nativeValid && series != "" && discoveryKind(gconv.String(item["type_name"])) == "" {
		if !matcher.loaded {
			matcher.loaded = true
			categories, err := matcher.loadCategories(ctx)
			matcher.loadErr = err
			if err != nil {
				return false, err
			}
			matcher.categories = make(map[int64]row, len(categories))
			for _, category := range categories {
				matcher.categories[gconv.Int64(category["id"])] = category
			}
			matcher.matchingItem = ordinaryCollectionMatchItemWithCategories(item, matcher.categories)
			kind = ordinaryCollectionLocalKind(gconv.Int64(vod["type_id"]), matcher.categories)
		}
		if kind == "short" {
			item = matcher.matchingItem
		}
	}
	return ordinaryCollectionMatches(vod, kind, matcher.apiID, item), nil
}
