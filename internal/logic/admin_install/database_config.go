package admininstall

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/suxinwl/GoSuxin/framework/encoding/gyaml"
)

// Update only database.default; Redis has an independent password.
func writeDatabaseConfig(path string, values map[string]string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	config, err := gyaml.Decode(content)
	if err != nil {
		return err
	}
	database, ok := config["database"].(map[string]any)
	if !ok {
		return fmt.Errorf("missing database configuration")
	}
	defaults, ok := database["default"].(map[string]any)
	if !ok {
		return fmt.Errorf("missing database.default configuration")
	}
	for _, key := range []string{"host", "port", "user", "pass", "name", "prefix"} {
		if value, exists := values[key]; exists {
			defaults[key] = value
		}
	}
	updated, err := gyaml.Encode(config)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".db-config-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	defer temporary.Close()
	if err = temporary.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if _, err = temporary.Write(updated); err != nil {
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), path)
}
