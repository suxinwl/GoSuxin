package album

import (
	"context"
	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"os"
	"strings"
	"testing"
)

// Run only against a restored disposable copy, never the production database.
func TestCenterUpgrade(t *testing.T) {
	name := os.Getenv("ALBUM_UPGRADE_TEST_DATABASE")
	if !strings.HasPrefix(name, "ebook_test_") {
		t.Skip("requires a disposable restored ebook_test_* database")
	}
	t.Chdir(t.TempDir())
	if err := gdb.SetConfigGroup("default", gdb.ConfigGroup{{Type: "mysql", Host: "127.0.0.1", Port: "3306", User: "root", Pass: os.Getenv("ALBUM_TEST_PASSWORD"), Name: name, Prefix: "gf_", Extra: "charset=utf8mb4&parseTime=True&loc=Local"}}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	snapshot := func(table, fields string) string {
		rows, err := g.Model(table).Fields(fields).OrderAsc("id").All()
		if err != nil {
			t.Fatal(err)
		}
		return rows.Json()
	}
	albums := snapshot("album", "id,title,cover_url,page_count,source_key,status,visibility")
	pages := snapshot("album_page", "*")
	shares := snapshot("album_share", "id,album_id,share_key,password_hash,expires_at,enabled")
	for i := 0; i < 2; i++ {
		if err := EnsureSchema(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if albums != snapshot("album", "id,title,cover_url,page_count,source_key,status,visibility") || pages != snapshot("album_page", "*") || shares != snapshot("album_share", "id,album_id,share_key,password_hash,expires_at,enabled") {
		t.Fatal("migration changed existing content or links")
	}
	for _, key := range []string{"company", "equipment", "consumables"} {
		n, err := g.Model("album_category").Where("stable_key", key).Count()
		if err != nil || n != 1 {
			t.Fatalf("category %s: %d %v", key, n, err)
		}
	}
	n, err := g.Model("album").WhereNull("category_id").Count()
	if err != nil || n != 0 {
		t.Fatalf("unmapped albums: %d %v", n, err)
	}
	for _, route := range []string{"albumCenter", "albumCategories", "albumManage", "albumImages", "albumShares", "albumSite", "albumStorage"} {
		n, err := g.Model("auth_rule").Where("routename", route).Count()
		if err != nil || n != 1 {
			t.Fatalf("menu %s: %d %v", route, n, err)
		}
	}
	t.Log("Repeated migration preserved all albums, pages and share links; categories and six menus verified")
}
