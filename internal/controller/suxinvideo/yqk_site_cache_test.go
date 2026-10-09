package suxinvideo

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func isolateYQKSiteState(t *testing.T) {
	t.Helper()
	yqkSiteState.Lock()
	loaded, refreshing, key, next, snapshot, pages := yqkSiteState.loaded, yqkSiteState.refreshing, yqkSiteState.key, yqkSiteState.nextRefresh, yqkSiteState.snapshot, yqkSiteState.pages
	yqkSiteState.loaded, yqkSiteState.refreshing, yqkSiteState.key, yqkSiteState.nextRefresh, yqkSiteState.snapshot = true, false, "fixture", time.Time{}, yqkSiteSnapshot{}
	yqkSiteState.pages = make(map[string]*yqkSitePageCache)
	yqkSiteState.Unlock()
	homeFetch, pageFetch := fetchYQKHomeSnapshot, fetchYQKChannelPage
	t.Cleanup(func() {
		fetchYQKHomeSnapshot, fetchYQKChannelPage = homeFetch, pageFetch
		yqkSiteState.Lock()
		yqkSiteState.loaded, yqkSiteState.refreshing, yqkSiteState.key, yqkSiteState.nextRefresh, yqkSiteState.snapshot, yqkSiteState.pages = loaded, refreshing, key, next, snapshot, pages
		yqkSiteState.Unlock()
	})
}

func cachedYQKSiteFixture(id string) yqkSiteRemotePage {
	return yqkSiteRemotePage{Items: []map[string]any{{"vodId": id, "vodName": "Fixture " + id}}, Page: 1}
}

func TestYQKSitePageCoalescesAndCancelledWaiterKeepsSharedFetch(t *testing.T) {
	isolateYQKSiteState(t)
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	fetchYQKChannelPage = func(ctx context.Context, channel, topic, page int, order, cursor string) (yqkSiteRemotePage, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return cachedYQKSiteFixture("101"), nil
		case <-ctx.Done():
			return yqkSiteRemotePage{}, ctx.Err()
		}
	}
	type result struct {
		data yqkSiteRemotePage
		err  error
	}
	results := make(chan result, 8)
	for range 8 {
		go func() {
			data, err := yqkSitePageForKey(context.Background(), "fixture", 2, 0, 1, "time")
			results <- result{data, err}
		}()
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("shared fetch was not launched")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := yqkSitePageForKey(cancelled, "fixture", 2, 0, 1, "time"); !errors.Is(err, context.Canceled) {
		close(release)
		t.Fatalf("cancelled waiter did not exit: %v", err)
	}
	close(release)
	for range 8 {
		select {
		case got := <-results:
			if got.err != nil || len(got.data.Items) != 1 || got.data.Items[0]["vodId"] != "101" {
				t.Fatalf("one waiter cancelled the shared fetch: %+v", got)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("waiter did not receive the completed shared page")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("one page caused %d upstream calls", calls.Load())
	}
}

func TestYQKSiteCursorPageIsInvalidatedWhenFirstPageRefreshes(t *testing.T) {
	isolateYQKSiteState(t)
	firstGeneration := 0
	var secondPageCursors []string
	fetchYQKChannelPage = func(ctx context.Context, channel, topic, page int, order, cursor string) (yqkSiteRemotePage, error) {
		if page == 1 {
			firstGeneration++
			result := cachedYQKSiteFixture("201")
			result.HasNext = true
			if firstGeneration == 1 {
				result.NextVal = "first-cursor"
			} else {
				result.NextVal = "refreshed-cursor"
			}
			return result, nil
		}
		secondPageCursors = append(secondPageCursors, cursor)
		return cachedYQKSiteFixture(cursor), nil
	}
	for _, page := range []int{1, 2} {
		if _, err := yqkSitePageForKey(context.Background(), "fixture", 0, 0, page, "time"); err != nil {
			t.Fatal(err)
		}
	}
	yqkSiteState.Lock()
	yqkSiteState.pages[yqkSitePageKey("fixture", 0, 0, 1, "time")].Until = time.Now().Add(-time.Second)
	yqkSiteState.Unlock()
	if _, err := yqkSitePageForKey(context.Background(), "fixture", 0, 0, 1, "time"); err != nil {
		t.Fatal(err)
	}
	second, err := yqkSitePageForKey(context.Background(), "fixture", 0, 0, 2, "time")
	if err != nil || len(second.Items) != 1 || second.Items[0]["vodId"] != "refreshed-cursor" {
		t.Fatalf("page two reused a cursor from the older catalogue: %+v, %v", second, err)
	}
	if len(secondPageCursors) != 2 || secondPageCursors[0] != "first-cursor" || secondPageCursors[1] != "refreshed-cursor" {
		t.Fatalf("page two was not fetched with its preceding page's cursor: %#v", secondPageCursors)
	}
}

func TestYQKSiteFailedPageRetryAndMissingCursorStayBounded(t *testing.T) {
	isolateYQKSiteState(t)
	var calls atomic.Int32
	fetchYQKChannelPage = func(context.Context, int, int, int, string, string) (yqkSiteRemotePage, error) {
		if calls.Add(1) == 1 {
			return yqkSiteRemotePage{}, errors.New("fixture outage")
		}
		return cachedYQKSiteFixture("301"), nil
	}
	if _, err := yqkSitePageForKey(context.Background(), "fixture", 0, 0, 2, "time"); err == nil || calls.Load() != 0 {
		t.Fatal("missing preceding cursor must fail before a network request")
	}
	for range 2 {
		if _, err := yqkSitePageForKey(context.Background(), "fixture", 2, 0, 1, "time"); err == nil {
			t.Fatal("upstream outage was hidden")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("brief error cache did not bound repeated requests")
	}
	yqkSiteState.Lock()
	yqkSiteState.pages[yqkSitePageKey("fixture", 2, 0, 1, "time")].Until = time.Now().Add(-time.Second)
	yqkSiteState.Unlock()
	data, err := yqkSitePageForKey(context.Background(), "fixture", 2, 0, 1, "time")
	if err != nil || len(data.Items) != 1 || calls.Load() != 2 {
		t.Fatalf("expired outage was not retried: %+v, %v", data, err)
	}
}

func TestYQKSiteHomeRefreshKeepsLastGoodDiskSnapshot(t *testing.T) {
	for _, test := range []struct {
		name string
		home yqkSiteRemoteHome
		err  error
	}{
		{name: "empty-success"},
		{name: "upstream-error", err: errors.New("fixture outage")},
	} {
		t.Run(test.name, func(t *testing.T) {
			isolateYQKSiteState(t)
			t.Chdir(t.TempDir())
			good := yqkSiteSnapshot{Key: "fixture", Updated: 100, Home: yqkSiteRemoteHome{Channels: []yqkSiteChannel{{ID: 2, Name: "Movies"}}, Recent: []map[string]any{{"vodId": "401", "vodName": "Last good film"}}}}
			if err := saveYQKSiteSnapshot(good); err != nil {
				t.Fatal(err)
			}
			yqkSiteState.Lock()
			yqkSiteState.snapshot = good
			yqkSiteState.refreshing = true
			yqkSiteState.Unlock()
			fetchYQKHomeSnapshot = func(context.Context) (yqkSiteRemoteHome, error) { return test.home, test.err }
			refreshYQKSiteHome("fixture")
			data, err := os.ReadFile(yqkSiteSnapshotPath)
			if err != nil {
				t.Fatal(err)
			}
			var disk yqkSiteSnapshot
			if json.Unmarshal(data, &disk) != nil || disk.Updated != good.Updated || len(disk.Home.Recent) != 1 || disk.Home.Recent[0]["vodId"] != "401" {
				t.Fatalf("empty/failed refresh destroyed last-good disk catalogue: %s", data)
			}
			yqkSiteState.Lock()
			memory, refreshing := yqkSiteState.snapshot, yqkSiteState.refreshing
			yqkSiteState.Unlock()
			if memory.Updated != good.Updated || refreshing {
				t.Fatal("empty/failed refresh must preserve memory and release its refresh lease")
			}
		})
	}
}

func TestYQKSiteHomeDiscardsObsoleteConfigurationRefresh(t *testing.T) {
	isolateYQKSiteState(t)
	t.Chdir(t.TempDir())
	good := yqkSiteSnapshot{Key: "fixture", Updated: 100, Home: yqkSiteRemoteHome{Recent: []map[string]any{{"vodId": "501", "vodName": "Current catalogue"}}}}
	if err := saveYQKSiteSnapshot(good); err != nil {
		t.Fatal(err)
	}
	yqkSiteState.Lock()
	yqkSiteState.snapshot = good
	yqkSiteState.Unlock()
	fetchYQKHomeSnapshot = func(context.Context) (yqkSiteRemoteHome, error) {
		return yqkSiteRemoteHome{Recent: []map[string]any{{"vodId": "502", "vodName": "Obsolete catalogue"}}}, nil
	}
	refreshYQKSiteHome("superseded-key")
	data, err := os.ReadFile(yqkSiteSnapshotPath)
	var disk yqkSiteSnapshot
	if err != nil || json.Unmarshal(data, &disk) != nil || disk.Key != "fixture" || disk.Updated != good.Updated {
		t.Fatal("an obsolete configuration overwrote the current catalogue")
	}
}
