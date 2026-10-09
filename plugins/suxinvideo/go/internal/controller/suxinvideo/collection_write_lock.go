package suxinvideo

import (
	"context"
	"sort"
	"strconv"
	"sync"
)

// These locks cover the lookup and first insert, before there is a database
// row to SELECT FOR UPDATE. Different films can still be collected in parallel.
// Entries disappear when their last owner/waiter leaves; a full crawl does not
// retain a lock for every film ever seen. Existing-row writes still use their
// database transaction so discovery and administrative changes remain safe.
type collectionWriteMutex struct {
	mu      sync.Mutex
	entries map[string]*collectionWriteEntry
}

type collectionWriteEntry struct {
	ready chan struct{}
	refs  int
}

var collectionWrites collectionWriteMutex

func (locks *collectionWriteMutex) acquire(ctx context.Context, keys ...string) (func(), error) {
	ordered := append([]string(nil), keys...)
	sort.Strings(ordered)
	releases := make([]func(), 0, len(ordered))
	releaseAll := func() {
		for index := len(releases) - 1; index >= 0; index-- {
			releases[index]()
		}
	}
	for index, key := range ordered {
		if index > 0 && key == ordered[index-1] {
			continue
		}
		release, err := locks.acquireOne(ctx, key)
		if err != nil {
			releaseAll()
			return nil, err
		}
		releases = append(releases, release)
	}
	var once sync.Once
	return func() { once.Do(releaseAll) }, nil
}

func (locks *collectionWriteMutex) acquireOne(ctx context.Context, key string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	locks.mu.Lock()
	if locks.entries == nil {
		locks.entries = make(map[string]*collectionWriteEntry)
	}
	entry := locks.entries[key]
	if entry == nil {
		entry = &collectionWriteEntry{ready: make(chan struct{}, 1)}
		entry.ready <- struct{}{}
		locks.entries[key] = entry
	}
	entry.refs++
	locks.mu.Unlock()
	dropReference := func() {
		locks.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(locks.entries, key)
		}
		locks.mu.Unlock()
	}
	select {
	case <-ctx.Done():
		dropReference()
		return nil, ctx.Err()
	case <-entry.ready:
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			entry.ready <- struct{}{}
			dropReference()
		})
	}
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

func collectionVodWriteKeys(apiID int64, remoteID, name string) []string {
	return []string{
		"vod:api:" + strconv.FormatInt(apiID, 10) + ":" + remoteID,
		"vod:title:" + normalizeVodName(discoveryNormalizeTitle(name)),
	}
}
