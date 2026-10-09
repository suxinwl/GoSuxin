package admininstall

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/suxinwl/GoSuxin/framework/encoding/gyaml"
)

func TestDatabaseConfigPreservesOtherServices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := []byte("server:\n  address: 127.0.0.1:8602\ndatabase:\n  default:\n    host: localhost\n    user: root\n    pass: old-database-password\n    name: gosuxin\n    type: mysql\n    prefix: gf_\n    deletedAt: deletetime\nredis:\n  default:\n    address: 127.0.0.1:6379\n    pass: redis-only-password\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	password := "mysql: password # with YAML punctuation"
	if err := writeDatabaseConfig(path, map[string]string{"pass": password, "name": "new_database"}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := gyaml.Decode(original)
	after, err := gyaml.Decode(content)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before["redis"], after["redis"]) || !reflect.DeepEqual(before["server"], after["server"]) {
		t.Fatal("installer changed another service's configuration")
	}
	db := after["database"].(map[string]any)["default"].(map[string]any)
	if db["pass"] != password || db["name"] != "new_database" || db["deletedAt"] != "deletetime" {
		t.Fatal("database values were not preserved correctly")
	}
}

func TestDatabaseConfigInvalidInputDoesNotOverwriteFile(t *testing.T) {
	for _, original := range []string{"database: [broken", "redis:\n  default:\n    pass: keep-me\n"} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(original), 0600); err != nil {
			t.Fatal(err)
		}
		if err := writeDatabaseConfig(path, map[string]string{"pass": "new-password"}); err == nil {
			t.Fatal("expected invalid configuration to fail")
		}
		content, err := os.ReadFile(path)
		if err != nil || string(content) != original {
			t.Fatal("invalid configuration was overwritten")
		}
	}
}
