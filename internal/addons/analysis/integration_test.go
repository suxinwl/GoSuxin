package analysis

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/suxinwl/GoSuxin/framework/contrib/drivers/mysql"
	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// Opt-in only: a dedicated disposable database, never the host business database.
func TestMySQLStatisticsScopesAndDateFilters(t *testing.T) {
	name := os.Getenv("SUXIN_ADDON_TEST_DATABASE")
	if !strings.HasPrefix(name, "addons_test_") {
		t.Skip("set a disposable addons_test_* database")
	}
	t.Chdir(t.TempDir())
	err := gdb.SetConfigGroup("default", gdb.ConfigGroup{{Type: "mysql", Host: "127.0.0.1", Port: "3306", User: "root", Pass: os.Getenv("SUXIN_ADDON_TEST_PASSWORD"), Name: name, Prefix: "gf_", Extra: "charset=utf8mb4&parseTime=True"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, sql := range []string{
		`CREATE TABLE gf_auth_role (id BIGINT PRIMARY KEY,rules VARCHAR(10))`,
		`CREATE TABLE gf_auth_role_access (uid BIGINT,role_id BIGINT)`,
		`INSERT INTO gf_auth_role VALUES (1,'*')`, `INSERT INTO gf_auth_role_access VALUES (1,1)`,
		`CREATE TABLE gf_operation_log (uid BIGINT,url VARCHAR(200),ip VARCHAR(50),createtime DATETIME)`,
		`INSERT INTO gf_operation_log VALUES (2,'/admin/album/list','10.0.0.2','2026-10-08 12:00:00'),(2,'/admin/album/save','10.0.0.2','2026-10-07 12:00:00'),(1,'/admin/suxinvideo/list','10.0.0.1','2026-10-06 08:00:00'),(2,'/admin/album/list','10.0.0.3','2026-09-01 12:00:00')`,
		`INSERT INTO gf_operation_log VALUES (2,'/admin/album/list','10.0.0.4','2026-10-08 18:00:00')`,
		`CREATE TABLE gf_login_log (uid BIGINT,status INT,browser VARCHAR(30),os VARCHAR(30),address VARCHAR(100),createtime DATETIME)`,
		`INSERT INTO gf_login_log VALUES (2,0,'Chrome','Windows','深圳','2026-10-08 12:00:00'),(2,1,'Safari','iOS','上海','2026-10-08 12:00:00'),(1,0,'Firefox','Linux','北京','2026-10-06 08:00:00')`,
		`CREATE TABLE gf_album (id BIGINT PRIMARY KEY,owner_id BIGINT)`, `INSERT INTO gf_album VALUES (1,1),(2,2)`,
		`CREATE TABLE gf_album_visit (album_id BIGINT,createtime DATETIME)`,
		`INSERT INTO gf_album_visit VALUES (2,'2026-10-08 12:00:00'),(2,'2026-10-07 12:00:00'),(1,'2026-10-06 08:00:00'),(2,'2026-09-01 12:00:00')`,
		`CREATE TABLE gf_privatecode_content (owner_id BIGINT)`, `INSERT INTO gf_privatecode_content VALUES (1),(2)`,
		`CREATE TABLE sx_vod (id BIGINT)`, `INSERT INTO sx_vod VALUES (1),(2)`,
		`CREATE TABLE sx_user (id BIGINT)`, `INSERT INTO sx_user VALUES (1)`,
		`CREATE TABLE sx_order (id BIGINT)`,
	} {
		if _, err = g.DB().Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 10, 8, 16, 0, 0, 0, time.FixedZone("Shanghai", 8*3600))
	own, err := Snapshot(context.WithValue(ctx, "uid", int64(2)), 7, now)
	if err != nil {
		t.Fatal(err)
	}
	summary := own["summary"].(g.Map)
	for key, want := range map[string]int64{"operations": 2, "uniqueIPs": 1, "logins": 1, "albumVisits": 2, "albums": 1, "privatePackages": 1, "films": 0, "members": 0} {
		if summary[key] != want {
			t.Fatalf("own %s: got %v want %d", key, summary[key], want)
		}
	}
	if own["canManageAll"] != false {
		t.Fatal("ordinary user acquired global scope")
	}
	if own["timeslots"].([]Point)[10].Value != 2 {
		t.Fatal("UTC records were not grouped into local 20:00 bucket")
	}
	browser := own["browser"].([]Point)
	if len(browser) != 1 || browser[0].Name != "Chrome" || browser[0].Value != 1 {
		t.Fatalf("failed or foreign login leaked: %#v", browser)
	}
	trend := own["trend"].([]Day)
	if len(trend) != 7 || trend[6].Operations != 1 || trend[5].Operations != 1 || trend[0].Operations != 0 {
		t.Fatalf("date filter failed: %#v", trend)
	}
	root, err := Snapshot(context.WithValue(ctx, "uid", int64(1)), 7, now)
	if err != nil {
		t.Fatal(err)
	}
	summary = root["summary"].(g.Map)
	for key, want := range map[string]int64{"operations": 3, "uniqueIPs": 2, "logins": 2, "albumVisits": 3, "albums": 2, "privatePackages": 2, "films": 2, "members": 1} {
		if summary[key] != want {
			t.Fatalf("global %s: got %v want %d", key, summary[key], want)
		}
	}
	if root["canManageAll"] != true {
		t.Fatal("super role lost global scope")
	}
}
