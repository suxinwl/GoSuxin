package app

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

func (app *UIApp) handleCategories(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	works := app.workCatalogSnapshot()
	years, statuses, tags, sources := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, work := range works {
		if match := regexp.MustCompile(`(?:19|20)\d{2}`).FindString(work.OnlineDate); match != "" {
			years[match] = true
		}
		if work.ReleaseStatus != "" {
			statuses[work.ReleaseStatus] = true
		}
		for _, tag := range work.Tags {
			if tag != "" {
				tags[tag] = true
			}
		}
		for _, source := range work.Sources {
			sources[source.Source] = true
		}
	}
	writeJSON(writer, http.StatusOK, map[string]any{"tree": []map[string]any{{"id": "movie", "name": "电影"}, {"id": "tv", "name": "电视剧"}, {"id": "anime", "name": "动漫"}, {"id": "variety", "name": "综艺"}, {"id": "documentary", "name": "纪录片"}, {"id": "sports", "name": "体育"}, {"id": "short-drama", "name": "短剧", "children": []string{"都市", "古装", "仙侠", "年代", "脑洞", "悬疑", "反转", "甜宠", "女频", "男频"}}, {"id": "other", "name": "其他"}}, "facets": map[string][]string{"year": sortedKeys(years), "status": sortedKeys(statuses), "tag": sortedKeys(tags), "source": sortedKeys(sources), "quality": {"标清", "高清", "超清", "原画"}}})
}

func sortedKeys(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func (app *UIApp) handleAdminOverview(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet || !playbackRequestAllowed(writer, request, http.MethodGet) {
		return
	}
	app.mu.Lock()
	dramaCount := len(app.dramas)
	sources := cloneLibrarySources(app.librarySources)
	tasks := make(map[string]int)
	for _, task := range app.tasks {
		if task != nil {
			tasks[task.Status]++
		}
	}
	activeDownloads, loading := app.activeDownloads, app.libraryLoading != nil
	app.mu.Unlock()
	app.playbackMu.Lock()
	sessions := len(app.playbacks)
	app.playbackMu.Unlock()
	works := app.workCatalogSnapshot()
	state := workOverrideSnapshot()
	writeJSON(writer, http.StatusOK, map[string]any{"brand": "小柒影视", "rawTotal": dramaCount, "workTotal": len(works), "pendingMerge": len(state.Merges), "sourceStates": sources, "sourcePriority": getWorkSourcePriority(), "tasks": tasks, "activeDownloads": activeDownloads, "activePlaybackSessions": sessions, "libraryLoading": loading})
}

func (app *UIApp) handleAdminCategories(writer http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodGet {
		if !playbackRequestAllowed(writer, request, http.MethodGet) {
			return
		}
		dramas := appDramas(app)
		rawCounts := map[string]int{}
		for _, drama := range dramas {
			raw := firstNonEmpty(drama.CategoryName, drama.CategoryNameSnake, drama.TypeName, drama.TypeNameSnake, drama.Category, drama.ChannelName)
			if raw == "" {
				raw = "未分类"
			}
			rawCounts[raw]++
		}
		tree := []map[string]any{
			{"id": "movie", "name": "电影"}, {"id": "tv", "name": "电视剧"}, {"id": "anime", "name": "动漫"}, {"id": "variety", "name": "综艺"}, {"id": "documentary", "name": "纪录片"}, {"id": "sports", "name": "体育"},
			{"id": "short-drama", "name": "短剧", "children": []string{"都市", "古装", "仙侠", "年代", "脑洞", "悬疑", "反转", "甜宠", "女频", "男频"}}, {"id": "other", "name": "其他"},
		}
		categoryMappings.RLock()
		mappings := map[string]string{}
		for key, value := range categoryMappings.data.Rules {
			mappings[key] = value
		}
		categoryMappings.RUnlock()
		writeJSON(writer, http.StatusOK, map[string]any{"tree": tree, "rawCategories": rawCounts, "mappings": mappings})
		return
	}
	if request.Method == http.MethodPost {
		var input struct {
			Source       string `json:"source"`
			RawCategory  string `json:"rawCategory"`
			CategoryPath string `json:"categoryPath"`
		}
		if !readAccountRequest(writer, request, &input) {
			return
		}
		raw, path := strings.TrimSpace(input.RawCategory), strings.TrimSpace(input.CategoryPath)
		if raw == "" || path == "" {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "原始分类和统一分类不能为空"})
			return
		}
		key := canonicalProviderSource(input.Source) + "|" + raw
		if strings.TrimSpace(input.Source) == "" {
			key = "*|" + raw
		}
		categoryMappings.Lock()
		if categoryMappings.data.Rules == nil {
			categoryMappings.data.Rules = map[string]string{}
		}
		categoryMappings.data.Rules[key] = path
		err := saveCategoryMappingsLocked()
		categoryMappings.Unlock()
		if err != nil {
			writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		app.markWorkCatalogChanged()
		writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "key": key, "categoryPath": path})
		return
	}
	writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
}

func (app *UIApp) handleAdminWorks(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet || !playbackRequestAllowed(writer, request, http.MethodGet) {
		return
	}
	works := filterWorksForRequest(context.Background(), app.workCatalogSnapshot(), request.URL.Query().Get("source"), request.URL.Query().Get("type"), request.URL.Query().Get("category"), request.URL.Query().Get("q"))
	offset, limit := queryInt(request.URL.Query().Get("offset"), 0), queryInt(request.URL.Query().Get("limit"), 100)
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset > len(works) {
		offset = len(works)
	}
	end := offset + limit
	if end > len(works) {
		end = len(works)
	}
	writeJSON(writer, http.StatusOK, map[string]any{"data": works[offset:end], "total": len(works), "offset": offset, "limit": limit})
}

func (app *UIApp) handleAdminWorkMerge(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var input struct {
		WorkIDs   []string `json:"workIds"`
		PrimaryID string   `json:"primaryId"`
	}
	if !readAccountRequest(writer, request, &input) {
		return
	}
	if len(input.WorkIDs) < 2 {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "至少选择两部影片"})
		return
	}
	works := app.workCatalogSnapshot()
	selected := map[string]Work{}
	for _, id := range input.WorkIDs {
		if work, ok := workByID(works, id); ok {
			selected[id] = work
		}
	}
	if len(selected) < 2 {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "影片不存在或已被筛选"})
		return
	}
	target := input.PrimaryID
	if _, ok := selected[target]; !ok {
		target = input.WorkIDs[0]
	}
	var rawIDs []string
	for _, work := range selected {
		rawIDs = append(rawIDs, work.RawIDs...)
	}
	workOverridesMu.Lock()
	workOverrides.Merges[target] = uniqueStrings(rawIDs)
	delete(workOverrides.Splits, target)
	err := saveWorkOverridesLocked()
	workOverridesMu.Unlock()
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	app.markWorkCatalogChanged()
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "workId": target})
}

func (app *UIApp) handleAdminWorkSplit(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var input struct {
		WorkID   string `json:"workId"`
		SourceID string `json:"sourceId"`
	}
	if !readAccountRequest(writer, request, &input) {
		return
	}
	work, ok := workByID(app.workCatalogSnapshot(), strings.TrimSpace(input.WorkID))
	if !ok {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "影片不存在"})
		return
	}
	if input.SourceID == "" {
		if len(work.Sources) != 2 {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "请指定要拆分的片源记录"})
			return
		}
		input.SourceID = work.Sources[1].DramaID
	}
	found := false
	for _, source := range work.Sources {
		if source.DramaID == input.SourceID {
			found = true
			break
		}
	}
	if !found {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "该片源不属于影片"})
		return
	}
	workOverridesMu.Lock()
	workOverrides.Splits[input.SourceID] = true
	err := saveWorkOverridesLocked()
	workOverridesMu.Unlock()
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	app.markWorkCatalogChanged()
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "sourceId": input.SourceID})
}

func (app *UIApp) handleAdminWorkAlias(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var input struct {
		WorkID string `json:"workId"`
		Alias  string `json:"alias"`
	}
	if !readAccountRequest(writer, request, &input) {
		return
	}
	if _, ok := workByID(app.workCatalogSnapshot(), input.WorkID); !ok || strings.TrimSpace(input.Alias) == "" {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "影片或别名无效"})
		return
	}
	workOverridesMu.Lock()
	workOverrides.Aliases[input.WorkID] = appendUnique(workOverrides.Aliases[input.WorkID], strings.TrimSpace(input.Alias))
	err := saveWorkOverridesLocked()
	workOverridesMu.Unlock()
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	app.markWorkCatalogChanged()
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "aliases": workOverrideSnapshot().Aliases[input.WorkID]})
}

func appDramas(app *UIApp) []Drama {
	app.mu.Lock()
	defer app.mu.Unlock()
	return append([]Drama(nil), app.dramas...)
}

func (app *UIApp) markWorkCatalogChanged() {
	app.mu.Lock()
	app.libraryRevision++
	app.libraryDirty = true
	app.mu.Unlock()
}
func uniqueStrings(values []string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
func appendUnique(values []string, value string) []string {
	return uniqueStrings(append(values, value))
}
func queryInt(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	var n int
	if _, err := fmt.Sscanf(value, "%d", &n); err != nil {
		return fallback
	}
	return n
}
