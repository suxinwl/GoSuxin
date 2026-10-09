package suxinvideo

import "testing"

func TestAppCatalogRevisionVisibilityIntegration(t *testing.T) {
	ctx := hongguoAuditFixtureContext(t)
	read := func() string {
		t.Helper()
		value, err := appCatalogRevision(ctx)
		if err != nil || len(value) != 64 {
			t.Fatalf("catalog revision unavailable: %v", err)
		}
		return value
	}
	previous := read()
	if current := read(); current != previous {
		t.Fatal("unchanged catalog produced a different revision")
	}
	hongguoAuditFixtureInsert(t, ctx, 8801, 9001, 900, "fixture", "目录隔离影片", "2026", "中国大陆", "hongguo", "第1集$hongguo://88/1", 1)
	if err := execSQL(ctx, "UPDATE sx_vod SET total_hits=total_hits+1,updatetime=updatetime+1 WHERE id=8801"); err != nil {
		t.Fatal(err)
	}
	if read() != previous {
		t.Fatal("normal collection metadata or play count invalidated all prefetched pages")
	}
	for _, query := range []string{
		"UPDATE sx_collect_api SET status=0 WHERE id=900",
		"UPDATE sx_type SET status=0 WHERE id=9001",
		"INSERT INTO sx_config(`key`,`value`) VALUES('content_block_keywords','changed-fixture-rule') ON DUPLICATE KEY UPDATE value=VALUES(value)",
	} {
		if err := execSQL(ctx, query); err != nil {
			t.Fatal(err)
		}
		current := read()
		if current == previous {
			t.Fatal("visibility policy change did not invalidate prefetched pages")
		}
		previous = current
	}
	if err := invalidateAppFilmCatalog(ctx); err != nil {
		t.Fatal(err)
	}
	if read() == previous {
		t.Fatal("film moderation did not invalidate prefetched pages")
	}
}
