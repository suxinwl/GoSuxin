package suxinvideo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

func collectionLockWaitForReferences(t *testing.T, locks *collectionWriteMutex, key string, refs int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		locks.mu.Lock()
		entry := locks.entries[key]
		found := entry != nil && entry.refs >= refs
		locks.mu.Unlock()
		if found {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("lock %q did not receive %d references", key, refs)
}

func TestCollectionWriteLockSerializesIdentityWithoutBlockingOtherFilms(t *testing.T) {
	var locks collectionWriteMutex
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	first := collectionVodWriteKeys(25, "first", "Ａ 仙逆\u3000剧场版")
	second := collectionVodWriteKeys(41, "second", "a仙逆剧场版")
	if first[1] != second[1] {
		t.Fatal("width/Unicode spacing aliases must share the creation lock")
	}
	unlock, err := locks.acquire(ctx, first...)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	acquired := make(chan func(), 1)
	result := make(chan error, 1)
	go func() {
		release, err := locks.acquire(ctx, second...)
		if err == nil {
			acquired <- release
		}
		result <- err
	}()
	collectionLockWaitForReferences(t, &locks, first[1], 2)
	select {
	case release := <-acquired:
		release()
		t.Fatal("two providers entered the same new-film lookup/insert simultaneously")
	default:
	}
	other, err := locks.acquire(ctx, collectionVodWriteKeys(99, "other", "另一部影片")...)
	if err != nil {
		t.Fatal("same-film waiter blocked an unrelated film:", err)
	}
	other()
	unlock()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	(<-acquired)()
	locks.mu.Lock()
	defer locks.mu.Unlock()
	if len(locks.entries) != 0 {
		t.Fatal("completed collection retained identity locks")
	}
}

func TestCollectionWriteLockSerializesRenamedRemoteID(t *testing.T) {
	var locks collectionWriteMutex
	ctx := context.Background()
	first := collectionVodWriteKeys(25, "35604", "仙逆")
	unlock, err := locks.acquire(ctx, first...)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	secondCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		release, err := locks.acquire(secondCtx, collectionVodWriteKeys(25, "35604", "新标题")...)
		if err == nil {
			release()
		}
		done <- err
	}()
	collectionLockWaitForReferences(t, &locks, first[0], 2)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("renamed remote identity bypassed the source/id lock: %v", err)
	}
	unlock()
	locks.mu.Lock()
	defer locks.mu.Unlock()
	if len(locks.entries) != 0 {
		t.Fatal("cancelled same-id waiter leaked a lock reference")
	}
}

func TestCollectionWriteLockCancellationReleasesEarlierKeys(t *testing.T) {
	var locks collectionWriteMutex
	held, err := locks.acquire(context.Background(), "b")
	if err != nil {
		t.Fatal(err)
	}
	defer held()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		release, err := locks.acquire(ctx, "b", "a", "a")
		if err == nil {
			release()
		}
		done <- err
	}()
	collectionLockWaitForReferences(t, &locks, "b", 2)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled collector did not leave the queue: %v", err)
	}
	freeCtx, freeCancel := context.WithTimeout(context.Background(), time.Second)
	defer freeCancel()
	free, err := locks.acquire(freeCtx, "a")
	if err != nil {
		t.Fatal("cancelled waiter kept its previously acquired key:", err)
	}
	free()
	held()
	locks.mu.Lock()
	defer locks.mu.Unlock()
	if len(locks.entries) != 0 {
		t.Fatal("cancelled collection retained entries")
	}
}

func TestCollectionConcurrentCoverWritesRemainComplete(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "covers", "shared-cover")
	makeCover := func(pixel color.RGBA) []byte {
		cover := image.NewRGBA(image.Rect(0, 0, 64, 64))
		for y := 0; y < 64; y++ {
			for x := 0; x < 64; x++ {
				cover.SetRGBA(x, y, pixel)
			}
		}
		var data bytes.Buffer
		if err := png.Encode(&data, cover); err != nil {
			t.Fatal(err)
		}
		return data.Bytes()
	}
	covers := [][]byte{makeCover(color.RGBA{R: 255, A: 255}), makeCover(color.RGBA{B: 255, A: 255})}
	if !saveImageCache(cache, covers[0]) {
		t.Fatal("could not initialize cover cache")
	}
	var writers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		writers.Add(1)
		go func(index int) {
			defer writers.Done()
			for pass := 0; pass < 10; pass++ {
				// Windows may decline to replace an open cached file. Keeping
				// the previous complete image is a valid cache outcome.
				_ = saveImageCache(cache, covers[(index+pass)%2])
			}
		}(worker)
	}
	done := make(chan struct{})
	go func() { writers.Wait(); close(done) }()
	defer func() { <-done }()
	for {
		data, err := os.ReadFile(cache)
		if err != nil {
			if runtime.GOOS != "windows" || !errors.Is(err, syscall.Errno(32)) {
				t.Fatal(err)
			}
			// An atomic rename can briefly overlap Windows share checks.
			// No bytes were returned, so the caller can retry its cache read.
			continue
		}
		if !bytes.Equal(data, covers[0]) && !bytes.Equal(data, covers[1]) {
			t.Fatal("parallel reader saw a partially written or mixed cover")
		}
		select {
		case <-done:
			return
		default:
		}
	}
}

func TestOrdinaryCollectionLockedMergePreservesMetadataAndRejectsStaleIdentity(t *testing.T) {
	original := collectionMatchingFixtureRow(900, "2026", "中国大陆", "anime_movie")
	existing := fourKVMFixtureRowCopy(original)
	existing["__collection_identity"] = discoveryFilmIdentity(original)
	tx := &fourKVMCollectionFixtureTX{film: fourKVMFixtureRowCopy(original)}
	update := collectedVodUpdate{existing: existing, apiID: 99, from: "another", play: "正片$https://example.test/another.m3u8",
		remarks: "must keep stored remarks", preserveMetadata: true, needsPicture: func(row) bool { return false }}
	if err := update.apply(tx); err != nil {
		t.Fatal(err)
	}
	if len(playlist(tx.film)) != 2 {
		t.Fatal("parallel source merge lost original playback")
	}
	for _, key := range []string{"id", "name", "year", "area", "type_id", "api_id", "api_vid", "vip", "points", "pic", "remarks", "updatetime"} {
		if !reflect.DeepEqual(tx.film[key], original[key]) {
			t.Fatalf("foreign source overwrote %s", key)
		}
	}
	for _, field := range []string{"name", "year", "area", "type_id", "api_vid"} {
		changed := fourKVMFixtureRowCopy(original)
		changed[field] = "edited"
		if field == "type_id" {
			changed[field] = 42
		}
		race := &fourKVMCollectionFixtureTX{film: changed}
		if err := update.apply(race); err == nil || race.writes != 0 {
			t.Fatalf("a concurrent %s edit bypassed the identity check", field)
		}
	}
}

func TestOrdinaryCollectionMatchesRejectsExplicitConflictsAndRetainsMissingFields(t *testing.T) {
	vod := row{"name": "相同片名", "year": "2026", "area": "中国大陆", "api_id": 5, "api_vid": "123"}
	item := map[string]any{"vod_name": " 相同\u3000片名 ", "vod_year": "2026", "vod_area": "大陆", "type_name": "电影", "vod_id": "123"}
	if !ordinaryCollectionMatches(vod, "movie", 6, item) {
		t.Fatal("same title/year/normalized region/category must merge across providers")
	}
	for _, tc := range []struct{ field, value string }{{"vod_name", "相同片名第二季"}, {"vod_year", "2025"}, {"vod_area", "日本"}, {"type_name", "国产剧"}} {
		changed := make(map[string]any, len(item))
		for key, value := range item {
			changed[key] = value
		}
		changed[tc.field] = tc.value
		for _, apiID := range []int64{5, 6} {
			if ordinaryCollectionMatches(vod, "movie", apiID, changed) {
				t.Fatalf("conflicting %s merged (including existing remote ID): api=%d", tc.field, apiID)
			}
		}
	}
	for _, field := range []string{"vod_year", "vod_area", "type_name"} {
		missing := make(map[string]any, len(item))
		for key, value := range item {
			missing[key] = value
		}
		delete(missing, field)
		if !ordinaryCollectionMatches(vod, "movie", 5, missing) {
			t.Fatalf("an absent %s rejected a previously known HTTP source update", field)
		}
	}
}

func TestCollectionConcurrentFirstInsertMergesProviders(t *testing.T) {
	if os.Getenv("SUXIN_INTEGRATION") != "1" {
		t.Skip("set SUXIN_INTEGRATION=1 for isolated concurrent first inserts")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(filepath.Clean("../../..")); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if setting(ctx, "collect_dedup_title", "1") != "1" {
		t.Skip("site explicitly disabled title deduplication")
	}
	name := fmt.Sprintf("collection-parallel-fixture-%d", time.Now().UnixNano())
	typeName := fmt.Sprintf("电影并发%d", time.Now().UnixNano())
	typeResult, err := g.DB().Exec(ctx, "INSERT INTO sx_type(pid,name,sort,status) VALUES(0,?,50,1)", typeName)
	if err != nil {
		t.Fatal(err)
	}
	typeID, _ := typeResult.LastInsertId()
	defer execSQL(context.Background(), "DELETE FROM sx_type WHERE id=?", typeID)
	defer execSQL(context.Background(), "DELETE FROM sx_vod WHERE name=?", name)
	items := []map[string]any{
		{"vod_id": "first", "vod_name": name, "type_name": typeName, "vod_year": "2026", "vod_area": "中国大陆", "vod_play_from": "firstfixture", "vod_play_url": "第1集$https://example.test/first.m3u8"},
		{"vod_id": "second", "vod_name": name, "type_name": typeName, "vod_year": "2026", "vod_area": "大陆", "vod_play_from": "secondfixture", "vod_play_url": "第1集$https://example.test/second.m3u8"},
	}
	start := make(chan struct{})
	results := make(chan struct {
		state int
		err   error
	}, 2)
	for index := range items {
		go func(index int) {
			<-start
			state, err := upsertMacVod(ctx, int64(1000001+index), items[index])
			results <- struct {
				state int
				err   error
			}{state, err}
		}(index)
	}
	close(start)
	states := make(map[int]int)
	for range items {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		states[result.state]++
	}
	if states[1] != 1 || states[2] != 1 {
		t.Fatalf("two providers did not share their first insertion: %v", states)
	}
	films, err := all(ctx, "SELECT * FROM sx_vod WHERE name=?", name)
	if err != nil || len(films) != 1 {
		t.Fatalf("concurrent sources created duplicate films: count=%d error=%v", len(films), err)
	}
	if len(playlist(films[0])) != 2 || gconv.Int64(films[0]["type_id"]) != typeID {
		t.Fatal("first insert/second merge lost a source or category")
	}
}
