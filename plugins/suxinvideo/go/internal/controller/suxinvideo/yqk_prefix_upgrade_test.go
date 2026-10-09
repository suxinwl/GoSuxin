package suxinvideo

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

// Run actual migration SQL against connection-local tables, never application
// records. Both startup and ZIP reinstall must preserve administrator choices.
func TestYQKPrefixUpgradeIntegration(t *testing.T) {
	ctx := yqkIntegrationContext(t)
	install, err := os.ReadFile("plugins/suxinvideo/install.sql")
	if err != nil {
		t.Fatal(err)
	}
	var statements []string
	for _, line := range strings.Split(string(install), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "UPDATE sx_") && strings.Contains(line, "BINARY name") && strings.Contains(line, "一起看") {
			statements = append(statements, line)
		}
	}
	if len(statements) != 20 {
		t.Fatalf("ZIP prefix migration incomplete: %d statements", len(statements))
	}
	for _, mode := range []string{"startup", "zip"} {
		t.Run(mode, func(t *testing.T) {
			err := g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
				for _, table := range []string{"sx_player", "sx_collect_api"} {
					schema, err := tx.GetOne("SHOW CREATE TABLE " + table)
					if err != nil {
						return err
					}
					ddl := strings.Replace(schema["Create Table"].String(), "CREATE TABLE", "CREATE TEMPORARY TABLE", 1)
					if _, err := tx.Exec(ddl); err != nil {
						return err
					}
					defer tx.Exec("DROP TEMPORARY TABLE " + table)
				}
				if _, err := tx.Exec("INSERT INTO sx_player(code,name,`parse`,status) VALUES('prefix_test_sentinel','fixture','',0)"); err != nil {
					return err
				}
				// Guard that DB helpers inherit this transaction/connection before
				// invoking migration code which uses those same helpers.
				sentinel, err := one(ctx, "SELECT name FROM sx_player WHERE code='prefix_test_sentinel'")
				if err != nil || gconv.String(sentinel["name"]) != "fixture" {
					return fmt.Errorf("database helpers did not use isolated temporary tables")
				}
				codes := yqkPlaybackCodes()
				for i, code := range codes {
					legacy := strings.Replace(yqkPlayers[strings.TrimPrefix(code, "yqk_")], "小柒", "一起看", 1)
					if _, err := tx.Exec("INSERT INTO sx_player(code,name,`parse`,status) VALUES(?,?,?,?)", code, legacy, "custom-parser", i%2); err != nil {
						return err
					}
				}
				for _, name := range []string{"一起看APP聚合资源", "一起看 APP", "一起看APP", "一起看·自定义源", "一起看app"} {
					if _, err := tx.Exec("INSERT INTO sx_collect_api(name,api_url,remark,status,collect_auto,collect_hours) VALUES(?,?,'custom-remark',0,1,72)", name, yqkSourceURL); err != nil {
						return err
					}
				}
				if _, err := tx.Exec("INSERT INTO sx_collect_api(name,api_url,status) VALUES('一起看 APP','https://fixture.example/api',0)"); err != nil {
					return err
				}
				migrate := func() error {
					if mode == "startup" {
						return renameYQKDefaultNames(ctx)
					}
					for _, sql := range statements {
						if _, err := tx.Exec(sql); err != nil {
							return err
						}
					}
					return nil
				}
				for round := 0; round < 2; round++ {
					if err := migrate(); err != nil {
						return err
					}
					for i, code := range codes {
						item, err := tx.GetOne("SELECT name,`parse`,status FROM sx_player WHERE code=?", code)
						if err != nil || item["name"].String() != yqkPlayers[strings.TrimPrefix(code, "yqk_")] || item["status"].Int() != i%2 || item["parse"].String() != "custom-parser" {
							return fmt.Errorf("player default/status/parser changed incorrectly: %s", code)
						}
					}
					items, err := tx.GetAll("SELECT name,remark,status,collect_auto,collect_hours FROM sx_collect_api WHERE api_url=? ORDER BY id", yqkSourceURL)
					if err != nil || len(items) != 5 {
						return fmt.Errorf("source upgrade inserted or removed records")
					}
					for i, item := range items {
						want := []string{"小柒APP聚合资源", "小柒APP聚合资源", "小柒APP聚合资源", "一起看·自定义源", "一起看app"}[i]
						if item["name"].String() != want || item["status"].Int() != 0 || item["collect_auto"].Int() != 1 || item["collect_hours"].Int() != 72 || item["remark"].String() != "custom-remark" {
							return fmt.Errorf("source name/status/schedule was not preserved: fixture %d", i)
						}
					}
				}
				// Exact matching must preserve old-brand custom names and casing.
				if _, err := tx.Exec("UPDATE sx_player SET name=CASE code WHEN 'yqk_8' THEN '一起看·WJ 自定义' WHEN 'yqk_27' THEN '一起看·bf' ELSE '一起看 APP' END WHERE code IN ('yqk_8','yqk_27','yqk_1')"); err != nil {
					return err
				}
				if err := migrate(); err != nil {
					return err
				}
				for code, want := range map[string]string{"yqk_8": "一起看·WJ 自定义", "yqk_27": "一起看·bf", "yqk_1": "小柒APP"} {
					item, err := tx.GetOne("SELECT name FROM sx_player WHERE code=?", code)
					if err != nil || item["name"].String() != want {
						return fmt.Errorf("custom name or APP alias changed incorrectly: %s", code)
					}
				}
				outside, err := tx.GetOne("SELECT name,status FROM sx_collect_api WHERE api_url='https://fixture.example/api'")
				if err != nil || outside["name"].String() != "一起看 APP" || outside["status"].Int() != 0 {
					return fmt.Errorf("unrelated source was renamed")
				}
				before, _ := tx.GetAll("SELECT * FROM sx_player ORDER BY id")
				if err := migrate(); err != nil {
					return err
				}
				after, _ := tx.GetAll("SELECT * FROM sx_player ORDER BY id")
				if !reflect.DeepEqual(before.List(), after.List()) {
					return fmt.Errorf("repeated prefix migration was not idempotent")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
