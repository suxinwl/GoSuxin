package suxinvideo

import (
	"context"
	"errors"
	"net/url"
	"testing"
)

func TestFullManualCollectionHasNoTotalDeadline(t *testing.T) {
	for _, test := range []struct {
		mode    string
		once    bool
		limited bool
	}{{"manual", false, false}, {"manual", true, true}, {"scheduled", true, true}, {"auto", true, true}} {
		parent, stop := context.WithCancel(context.Background())
		ctx, cancel := collectJobContext(parent, test.mode, test.once)
		_, limited := ctx.Deadline()
		if limited != test.limited {
			t.Fatalf("%s once=%v deadline=%v", test.mode, test.once, limited)
		}
		stop()
		if ctx.Err() != context.Canceled {
			t.Fatal("parent cancellation lost")
		}
		cancel()
	}
}

func TestCursorCollectionDoesNotFinishOnEmptyIntermediatePage(t *testing.T) {
	data := macPayload{Code: 1, Page: 148, PageCount: 149}
	if collectJobPageFinished(yqkSourceURL, 148, data, false) {
		t.Fatal("valid next cursor was discarded")
	}
	if !collectJobPageFinished("https://example.test/api", 148, data, false) {
		t.Fatal("ordinary empty feed stopping rule changed")
	}
	if !collectJobPageFinished(yqkSourceURL, 148, data, true) {
		t.Fatal("one-page mode did not end")
	}
	data.PageCount = 148
	if !collectJobPageFinished(yqkSourceURL, 148, data, false) {
		t.Fatal("exhausted cursor did not end")
	}
	data.Page, data.PageCount = 10000, 10001
	if !collectJobPageFinished(yqkSourceURL, 10000, data, false) {
		t.Fatal("page cap ignored")
	}
}

func TestCollectionFetchRetryPreservesPageAndStops(t *testing.T) {
	oldFetch, oldPause := fetchCollectJobPage, pauseCollectJob
	defer func() { fetchCollectJobPage, pauseCollectJob = oldFetch, oldPause }()
	pauseCollectJob = func(ctx context.Context) bool { return ctx.Err() == nil }
	attempts := 0
	query := url.Values{"ac": {"videolist"}, "pg": {"148"}, "t": {"65"}}
	fetchCollectJobPage = func(_ context.Context, raw string, params url.Values) (macPayload, error) {
		attempts++
		if raw != yqkSourceURL || params.Encode() != query.Encode() {
			t.Fatal("retry changed source/page/category")
		}
		if attempts < 3 {
			return macPayload{}, errors.New("temporary upstream timeout")
		}
		return macPayload{Code: 1, Page: 148, PageCount: 149}, nil
	}
	data, err := fetchCollectJobWithRetry(context.Background(), yqkSourceURL, query)
	if err != nil || attempts != 3 || data.Page != 148 {
		t.Fatalf("retry failed: attempts=%d data=%+v err=%v", attempts, data, err)
	}
	attempts = 0
	fetchCollectJobPage = func(context.Context, string, url.Values) (macPayload, error) {
		attempts++
		return macPayload{}, errors.New("persistent failure")
	}
	if _, err := fetchCollectJobWithRetry(context.Background(), yqkSourceURL, query); err == nil || attempts != 3 {
		t.Fatal("persistent failure was hidden or unbounded")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	attempts = 0
	if _, err := fetchCollectJobWithRetry(ctx, yqkSourceURL, query); err != context.Canceled || attempts != 0 {
		t.Fatal("cancelled request retried")
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	pauseCollectJob = func(context.Context) bool { cancel(); return false }
	if _, err := fetchCollectJobWithRetry(ctx, yqkSourceURL, query); err == nil || attempts != 1 {
		t.Fatal("stop during backoff did not stop retries")
	}
}
