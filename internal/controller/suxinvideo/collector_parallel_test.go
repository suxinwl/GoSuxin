package suxinvideo

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCollectSourceLeaseCanonicalURI(t *testing.T) {
	first, err := collectSourceLease(" HTTPS://Example.Test:443/api/?b=2&a=1#debug ")
	if err != nil {
		t.Fatal(err)
	}
	second, err := collectSourceLease("https://example.test/api?a=1&b=2")
	if err != nil || first != second {
		t.Fatalf("duplicate provider escaped its lease: %s %s %v", first, second, err)
	}
	if len(first) != 64 || strings.Contains(first, "example") {
		t.Fatal("lease reveals provider request information")
	}
	third, _ := collectSourceLease("https://example.test/another?a=1&b=2")
	if first == third {
		t.Fatal("different provider endpoint shares its lease")
	}
	for _, native := range []string{hongguoSourceURL, fourKVMSourceURL, erciyuanSourceURL, yqkSourceURL} {
		if key, err := collectSourceLease(native); err != nil || key == "" {
			t.Fatalf("native provider lacks a lease: %s %v", native, err)
		}
	}
	if _, err := collectSourceLease("not-a-provider"); err == nil {
		t.Fatal("invalid URI received a lease")
	}
}

func TestCollectWorkersShareBoundAndCancelQueued(t *testing.T) {
	slots := newCollectWorkerSlots()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var running, maximum atomic.Int32
	entered := make(chan struct{}, 3)
	release := make(chan struct{})
	var workers sync.WaitGroup
	for index := 0; index < 18; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if !slots.acquire(ctx, func() int { return 3 }) {
				return
			}
			defer slots.release()
			count := running.Add(1)
			for old := maximum.Load(); count > old; old = maximum.Load() {
				if maximum.CompareAndSwap(old, count) {
					break
				}
			}
			entered <- struct{}{}
			<-release
			running.Add(-1)
		}()
	}
	for index := 0; index < 3; index++ {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatal("worker slots did not allow parallel providers")
		}
	}
	select {
	case <-entered:
		t.Fatal("parallel tasks exceeded the service-wide bound")
	case <-time.After(30 * time.Millisecond):
	}
	cancel()
	close(release)
	workers.Wait()
	if maximum.Load() != 3 || slots.active != 0 {
		t.Fatalf("worker accounting leaked: maximum=%d active=%d", maximum.Load(), slots.active)
	}
}

func TestCollectWorkersNoticeChangedLimitAndRetainRunning(t *testing.T) {
	slots := newCollectWorkerSlots()
	var limit atomic.Int32
	limit.Store(1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if !slots.acquire(ctx, func() int { return int(limit.Load()) }) {
		t.Fatal("first source did not start")
	}
	defer slots.release()
	acquired := make(chan bool, 1)
	go func() {
		ok := slots.acquire(ctx, func() int { return int(limit.Load()) })
		if ok {
			defer slots.release()
		}
		acquired <- ok
	}()
	limit.Store(2)
	select {
	case ok := <-acquired:
		if !ok {
			t.Fatal("new limit did not release a queued source")
		}
	case <-ctx.Done():
		t.Fatal("limit change was ignored")
	}
}

func TestCollectCompletedJobIsolatesSourceFailure(t *testing.T) {
	sources := []row{{"source_name": "first", "status": "failed", "error": "source timeout"}, {"source_name": "second", "status": "complete"}}
	status, message := collectCompletedJobStatus(sources, false, nil)
	if status != "partial" || !strings.Contains(message, "source timeout") {
		t.Fatalf("one source failure ended other providers: %s %s", status, message)
	}
	status, _ = collectCompletedJobStatus(sources, true, nil)
	if status != "cancelled" {
		t.Fatal("explicit stop was not preserved")
	}
	status, _ = collectCompletedJobStatus(sources, false, context.DeadlineExceeded)
	if status != "interrupted" {
		t.Fatal("timeout was confused with a completed job")
	}
	status, _ = collectCompletedJobStatus([]row{{"status": "failed"}}, false, nil)
	if status != "failed" {
		t.Fatal("total failure was hidden")
	}
}
