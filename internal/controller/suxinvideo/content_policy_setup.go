package suxinvideo

import "context"

// Existing policies, including intentionally empty rule lists, survive upgrades.
func prepareContentPolicy(ctx context.Context) error {
	return execSQL(ctx, "INSERT IGNORE INTO sx_config (`key`,`value`) VALUES('content_block_enable','1'),('content_block_keywords',?),('content_block_categories',?)", defaultContentBlockKeywords, defaultContentBlockCategories)
}

func contentCategoryAllowed(ctx context.Context, category row) bool {
	return contentVodAllowed(ctx, row{"class": category["name"], "type_id": category["id"]})
}

func visibleCategories(ctx context.Context, categories []row) []row {
	visible := make([]row, 0, len(categories))
	for _, category := range categories {
		if contentCategoryAllowed(ctx, category) {
			visible = append(visible, category)
		}
	}
	return visible
}
