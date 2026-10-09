package app

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// workOverrideState is deliberately small: raw source records remain the
// source of truth, while this file stores only administrator decisions that
// cannot be inferred safely from a title.
type workOverrideState struct {
	Version int                 `json:"version"`
	Merges  map[string][]string `json:"merges,omitempty"`
	Splits  map[string]bool     `json:"splits,omitempty"`
	Aliases map[string][]string `json:"aliases,omitempty"`
}

var (
	workOverridesMu   sync.RWMutex
	workOverrides     = workOverrideState{Version: 1, Merges: map[string][]string{}, Splits: map[string]bool{}, Aliases: map[string][]string{}}
	workOverridesPath string
)

func loadWorkOverrides(dataDir string) {
	path := filepath.Join(dataDir, "work-overrides.json")
	body, err := os.ReadFile(path)
	state := workOverrideState{Version: 1, Merges: map[string][]string{}, Splits: map[string]bool{}, Aliases: map[string][]string{}}
	if err == nil {
		if json.Unmarshal(body, &state) != nil || state.Version != 1 {
			state = workOverrideState{Version: 1, Merges: map[string][]string{}, Splits: map[string]bool{}, Aliases: map[string][]string{}}
		}
	}
	if state.Merges == nil {
		state.Merges = map[string][]string{}
	}
	if state.Splits == nil {
		state.Splits = map[string]bool{}
	}
	if state.Aliases == nil {
		state.Aliases = map[string][]string{}
	}
	workOverridesMu.Lock()
	workOverrides, workOverridesPath = state, path
	workOverridesMu.Unlock()
}

func saveWorkOverridesLocked() error {
	if workOverridesPath == "" {
		return errors.New("工作聚合配置路径为空")
	}
	if err := os.MkdirAll(filepath.Dir(workOverridesPath), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(workOverridesPath), ".work-overrides-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = json.NewEncoder(tmp).Encode(workOverrides); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, workOverridesPath)
}

func workOverrideSnapshot() workOverrideState {
	workOverridesMu.RLock()
	defer workOverridesMu.RUnlock()
	copyState := workOverrideState{Version: workOverrides.Version, Merges: map[string][]string{}, Splits: map[string]bool{}, Aliases: map[string][]string{}}
	for key, values := range workOverrides.Merges {
		copyState.Merges[key] = append([]string(nil), values...)
	}
	for key, value := range workOverrides.Splits {
		copyState.Splits[key] = value
	}
	for key, values := range workOverrides.Aliases {
		copyState.Aliases[key] = append([]string(nil), values...)
	}
	return copyState
}

func applyWorkOverrides(works []Work) []Work {
	state := workOverrideSnapshot()
	if len(state.Merges) == 0 && len(state.Splits) == 0 && len(state.Aliases) == 0 {
		return works
	}
	byRaw := map[string]int{}
	byWork := map[string]int{}
	for index, work := range works {
		for _, raw := range work.RawIDs {
			byRaw[raw] = index
		}
	}
	for index, work := range works {
		byWork[work.ID] = index
	}
	removed := map[int]bool{}
	result := make([]Work, 0, len(works))
	for target, rawIDs := range state.Merges {
		indices := map[int]bool{}
		if index, ok := byWork[target]; ok {
			indices[index] = true
		}
		for _, raw := range rawIDs {
			if index, ok := byRaw[raw]; ok {
				indices[index] = true
			}
		}
		if len(indices) < 2 {
			continue
		}
		ordered := make([]int, 0, len(indices))
		for index := range indices {
			ordered = append(ordered, index)
		}
		sort.Ints(ordered)
		merged := works[ordered[0]]
		merged.ID = target
		merged.Sources = nil
		merged.RawIDs = nil
		merged.SourceCount = 0
		for _, index := range ordered {
			other := works[index]
			if merged.Title == "" {
				merged.Title = other.Title
				merged.Name = other.Name
			}
			if merged.Desc == "" {
				merged.Desc, merged.Intro = other.Desc, other.Intro
			}
			merged.Sources = append(merged.Sources, other.Sources...)
			merged.RawIDs = append(merged.RawIDs, other.RawIDs...)
			for _, category := range other.CategoryPath {
				if !containsString(merged.CategoryPath, category) {
					merged.CategoryPath = append(merged.CategoryPath, category)
				}
			}
			for _, category := range other.RawCategories {
				if !containsString(merged.RawCategories, category) {
					merged.RawCategories = append(merged.RawCategories, category)
				}
			}
			removed[index] = true
		}
		merged.SourceCount = len(merged.Sources)
		sort.SliceStable(merged.Sources, func(i, j int) bool { return merged.Sources[i].Priority < merged.Sources[j].Priority })
		if len(merged.Sources) > 0 {
			merged.PrimarySource, merged.PrimaryDramaID = merged.Sources[0].Source, merged.Sources[0].DramaID
			merged.Source, merged.SourceID = merged.Sources[0].Source, merged.Sources[0].SourceID
		}
		merged.Aliases = append([]string(nil), state.Aliases[target]...)
		result = append(result, merged)
	}
	for index, work := range works {
		if removed[index] {
			continue
		}
		result = append(result, work)
	}
	for index := range result {
		if aliases := state.Aliases[result[index].ID]; len(aliases) > 0 {
			result[index].Aliases = append([]string(nil), aliases...)
		}
	}
	// Split decisions are applied after merges so an explicitly separated raw
	// record can never be pulled back into an administrator-created group.
	if len(state.Splits) > 0 {
		out := make([]Work, 0, len(result)+len(state.Splits))
		for _, work := range result {
			kept := work
			kept.Sources, kept.RawIDs = nil, nil
			for _, source := range work.Sources {
				if !state.Splits[source.DramaID] {
					kept.Sources = append(kept.Sources, source)
					kept.RawIDs = append(kept.RawIDs, source.DramaID)
					continue
				}
				single := work
				single.ID = "work:split-" + shortHash(source.DramaID)
				single.Sources = []WorkSource{source}
				single.RawIDs = []string{source.DramaID}
				single.SourceCount = 1
				single.PrimarySource, single.PrimaryDramaID, single.Source, single.SourceID = source.Source, source.DramaID, source.Source, source.SourceID
				single.Aliases = nil
				out = append(out, single)
			}
			if len(kept.Sources) > 0 {
				kept.SourceCount = len(kept.Sources)
				kept.PrimarySource, kept.PrimaryDramaID, kept.Source, kept.SourceID = kept.Sources[0].Source, kept.Sources[0].DramaID, kept.Sources[0].Source, kept.Sources[0].SourceID
				out = append(out, kept)
			}
		}
		result = out
	}
	sort.SliceStable(result, func(i, j int) bool {
		left, right := strings.ToLower(result[i].Title), strings.ToLower(result[j].Title)
		if left != right {
			return left < right
		}
		return result[i].ID < result[j].ID
	})
	return result
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum[:6])
}
