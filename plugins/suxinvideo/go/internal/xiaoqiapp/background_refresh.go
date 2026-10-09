package app

import (
	"context"
	"fmt"
	"time"
)

// Catalog and ranking data are refreshed by the service. Browser requests only
// read the last completed snapshot, so opening or reloading a page cannot
// start a provider crawl or reorder the catalogue half way through a render.
const (
	catalogRefreshInterval = 6 * time.Hour
	backgroundRefreshTick  = 30 * time.Minute
)

func (a *UIApp) startBackgroundRefresh() {
	if a.backgroundStop != nil {
		return
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	a.backgroundStop, a.backgroundDone = stop, done
	go func() {
		defer close(done)
		ticker := time.NewTicker(backgroundRefreshTick)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				a.refreshServerCache()
			case <-stop:
				return
			}
		}
	}()
}

func (a *UIApp) stopBackgroundRefresh() {
	stop, done := a.backgroundStop, a.backgroundDone
	if stop == nil {
		return
	}
	close(stop)
	<-done
	a.backgroundStop, a.backgroundDone = nil, nil
}

func (a *UIApp) refreshServerCache() {
	a.refreshLibraryIfDue()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	a.downloader.refreshCachedRankings(ctx)
}

func (a *UIApp) refreshLibraryIfDue() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.libraryLoading != nil || len(a.dramas) == 0 || a.loadedAt.IsZero() || time.Since(a.loadedAt) < catalogRefreshInterval {
		return
	}
	fmt.Printf("开始定时刷新剧库缓存（上次更新：%s）\n", a.loadedAt.Format(time.RFC3339))
	a.enqueueSortMetadataLocked(selectSortMetadataBatch(a.dramas, "", nil, time.Now()), false)
	a.startLibraryLoadLocked("", libraryLoadRefresh, nil)
}
