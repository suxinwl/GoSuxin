package app

import (
	"context"
	"net/http"
	"strings"
	"time"
)

func (app *UIApp) handleLibrarySearch(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	keyword := strings.TrimSpace(request.URL.Query().Get("q"))
	if keyword == "" {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "搜索关键词不能为空"})
		return
	}
	if len(keyword) > 80 {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "搜索词过长"})
		return
	}

	sourceParam := strings.TrimSpace(request.URL.Query().Get("source"))
	var searchSources []string
	if sourceParam == "all" || sourceParam == "*" {
		for _, choice := range accountSourceChoices {
			if sourceAllowed(request.Context(), choice.ID) {
				searchSources = append(searchSources, choice.ID)
			}
		}
	} else if sourceParam != "" {
		canon := canonicalProviderSource(sourceParam)
		if canon == "" || !sourceAllowed(request.Context(), canon) {
			writeSourceDenied(writer)
			return
		}
		searchSources = []string{canon}
	} else {
		if !sourceAllowed(request.Context(), sourceHongguo) {
			writeSourceDenied(writer)
			return
		}
		searchSources = []string{sourceHongguo}
	}

	ctx, cancel := context.WithTimeout(request.Context(), 45*time.Second)
	defer cancel()

	type searchResultItem struct {
		source string
		dramas []Drama
		err    error
	}

	ch := make(chan searchResultItem, len(searchSources))
	for _, src := range searchSources {
		src := src
		go func() {
			if src == sourceHongguo {
				res, err := app.downloader.searchHongguoDramas(ctx, keyword)
				if err != nil {
					ch <- searchResultItem{source: src, err: err}
				} else {
					ch <- searchResultItem{source: src, dramas: res.Dramas}
				}
			} else if src == sourceHuangdou {
				var decoded any
				err := newHuangdouAPIClient(app.downloader).call(ctx, "/drama/list", map[string]any{"keywords": keyword, "page": "1", "page_size": "50"}, &decoded)
				if err != nil {
					ch <- searchResultItem{source: src, err: err}
				} else {
					list := huangdouList(decoded)
					var res []Drama
					for _, item := range list {
						res = append(res, huangdouDramaFromMap(item))
					}
					ch <- searchResultItem{source: src, dramas: res}
				}
			} else if isMacCMSSource(src) {
				dramas, err := app.downloader.searchMacCMSDramas(ctx, src, keyword)
				ch <- searchResultItem{source: src, dramas: dramas, err: err}
			} else if src == source4KVM {
				dramas, err := app.downloader.search4KVMDramas(ctx, keyword)
				ch <- searchResultItem{source: src, dramas: dramas, err: err}
			} else {
				ch <- searchResultItem{source: src}
			}
		}()
	}

	var foundDramas []Drama
	for range searchSources {
		item := <-ch
		if len(item.dramas) > 0 {
			foundDramas = append(foundDramas, item.dramas...)
		}
	}

	dramas := onlySupportedDramas(foundDramas)
	if len(dramas) > 0 {
		positions := make(map[string]int, len(dramas))
		for index, drama := range dramas {
			positions[drama.ID] = index
		}
		app.mu.Lock()
		app.normalizeDramaCovers(dramas)
		newDramas := app.newSortMetadataDramasLocked(dramas)
		app.dramas = mergeSourceDramas(app.dramas, dramas, nil, "")
		app.enqueueSortMetadataLocked(newDramas, false)
		app.loadedAt = time.Now()
		if app.librarySources == nil {
			app.librarySources = map[string]librarySourceState{}
		}
		for _, dr := range dramas {
			src := dramaProvider(dr)
			state := app.librarySources[src]
			state.Count++
			state.UpdatedAt = app.loadedAt
			if state.Status == "" {
				state.Status = "ready"
			}
			app.librarySources[src] = state
		}
		for index, drama := range app.dramas {
			if pos, found := positions[drama.ID]; found {
				dramas[pos] = app.dramas[index]
			}
		}
		app.libraryDirty = true
		app.libraryRevision++
		app.mu.Unlock()
		app.persistLibrary()
	}
	app.mu.Lock()
	saved := app.librarySaved && !app.libraryDirty
	app.mu.Unlock()
	writeJSON(writer, http.StatusOK, map[string]any{"query": keyword, "data": dramas, "saved": saved})
}
