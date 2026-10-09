package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type categoryMappingState struct {
	Version int               `json:"version"`
	Rules   map[string]string `json:"rules,omitempty"`
}

var categoryMappings = struct {
	sync.RWMutex
	path string
	data categoryMappingState
}{data: categoryMappingState{Version: 1, Rules: map[string]string{}}}

func loadCategoryMappings(dataDir string) {
	path := filepath.Join(dataDir, "category-mappings.json")
	state := categoryMappingState{Version: 1, Rules: map[string]string{}}
	if body, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(body, &state)
	}
	if state.Rules == nil {
		state.Rules = map[string]string{}
	}
	categoryMappings.Lock()
	categoryMappings.path, categoryMappings.data = path, state
	categoryMappings.Unlock()
}

func categoryMapping(source, raw string) string {
	categoryMappings.RLock()
	defer categoryMappings.RUnlock()
	if value := categoryMappings.data.Rules[canonicalProviderSource(source)+"|"+strings.TrimSpace(raw)]; value != "" {
		return value
	}
	return categoryMappings.data.Rules["*|"+strings.TrimSpace(raw)]
}

func saveCategoryMappingsLocked() error {
	if categoryMappings.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(categoryMappings.path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(categoryMappings.path), ".category-mappings-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = json.NewEncoder(tmp).Encode(categoryMappings.data); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, categoryMappings.path)
}
