package suxinvideo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Fresh plugin installations and existing hosts must expose identical alias
// columns so a reviewed alias group survives reinstall without conversion.
func TestVodAliasInstallSchemaMatchesStartup(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate source tree")
	}
	var install []byte
	for dir := filepath.Dir(file); ; dir = filepath.Dir(dir) {
		path := filepath.Join(dir, "plugins", "suxinvideo", "install.sql")
		if filepath.Base(dir) == "suxinvideo" {
			path = filepath.Join(dir, "install.sql")
		}
		if data, err := os.ReadFile(path); err == nil {
			install = data
			break
		}
		if filepath.Dir(dir) == dir {
			t.Fatal("cannot locate plugin install.sql")
		}
	}
	start := strings.Index(string(install), "CREATE TABLE IF NOT EXISTS `sx_vod_alias`")
	if start < 0 {
		t.Fatal("ZIP installer is missing verified aliases table")
	}
	statement := string(install[start:])
	end := strings.Index(statement, ";")
	if end < 0 {
		t.Fatal("unterminated aliases schema")
	}
	normalize := func(sql string) string {
		return strings.ToLower(strings.Join(strings.Fields(strings.ReplaceAll(sql, "`", "")), " "))
	}
	if normalize(statement[:end]) != normalize(vodAliasTable) {
		t.Fatal("ZIP and startup aliases schemas disagree")
	}
	if strings.Contains(strings.ToUpper(vodAliasTable), "FOREIGN KEY") || strings.Contains(strings.ToUpper(vodAliasTable), "AUTO_INCREMENT") {
		t.Fatal("alias IDs must reference existing films without cascade or new IDs")
	}
}
