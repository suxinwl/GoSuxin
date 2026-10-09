package album

import (
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
)

func cacheFixture(data []byte) (string, string) {
	sum := fmt.Sprintf("%x", md5.Sum(data))
	return sum + sum, sum
}
func TestImageCacheConcurrentPersistentAndEviction(t *testing.T) {
	dir := t.TempDir()
	c := newImageDiskCache(dir, 12, time.Hour)
	data := []byte("image-one")
	key, sum := cacheFixture(data)
	var downloads atomic.Int32
	fetch := func(context.Context) (io.ReadCloser, error) {
		downloads.Add(1)
		time.Sleep(20 * time.Millisecond)
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f, e := c.open(context.Background(), key, sum, int64(len(data)), fetch)
			if e != nil {
				t.Error(e)
				return
			}
			defer f.Close()
			got, _ := io.ReadAll(f)
			if !bytes.Equal(got, data) {
				t.Error("bad content")
			}
		}()
	}
	wg.Wait()
	if downloads.Load() != 1 {
		t.Fatal("duplicate CDN reads", downloads.Load())
	}
	c = newImageDiskCache(dir, 12, time.Hour) // cache survives plugin process recreation
	f, e := c.open(context.Background(), key, sum, int64(len(data)), fetch)
	if e != nil {
		t.Fatal(e)
	}
	f.Close()
	if downloads.Load() != 1 {
		t.Fatal("persistent hit missed")
	}
	other := []byte("second")
	key2, sum2 := cacheFixture(other)
	f, e = c.open(context.Background(), key2, sum2, int64(len(other)), func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(other)), nil })
	if e != nil {
		t.Fatal(e)
	}
	f.Close()
	if _, e = os.Stat(filepath.Join(dir, key+".cache")); !os.IsNotExist(e) {
		t.Fatal("LRU did not evict")
	}
	old := time.Now().Add(-2 * time.Hour)
	os.Chtimes(filepath.Join(dir, key2+".cache"), old, old)
	calls := 0
	f, e = c.open(context.Background(), key2, sum2, int64(len(other)), func(context.Context) (io.ReadCloser, error) {
		calls++
		return io.NopCloser(bytes.NewReader(other)), nil
	})
	if e != nil {
		t.Fatal(e)
	}
	f.Close()
	if calls != 1 {
		t.Fatal("expired hit")
	}
}
func TestImageCacheRejectsBadDownloadsAndCancelledWaiter(t *testing.T) {
	c := newImageDiskCache(t.TempDir(), 1024, time.Hour)
	data := []byte("original")
	key, sum := cacheFixture(data)
	for _, bad := range [][]byte{[]byte("modified"), []byte("short"), []byte("too-long-data")} {
		if f, e := c.open(context.Background(), key, sum, int64(len(data)), func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(bad)), nil }); e == nil {
			f.Close()
			t.Fatal("accepted corrupted/partial body")
		}
		entries, _ := os.ReadDir(c.root)
		if len(entries) != 0 {
			t.Fatal("failed download leaked cache or temporary file")
		}
	}
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		f, e := c.open(context.Background(), key, sum, int64(len(data)), func(context.Context) (io.ReadCloser, error) {
			close(entered)
			<-release
			return io.NopCloser(bytes.NewReader(data)), nil
		})
		if f != nil {
			f.Close()
		}
		done <- e
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, e := c.open(ctx, key, sum, int64(len(data)), nil); e != context.DeadlineExceeded {
		t.Fatal("waiting request ignored cancellation", e)
	}
	close(release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if _, e := c.open(context.Background(), "../escape", sum, 8, nil); e == nil {
		t.Fatal("unsafe key accepted")
	}
}
func TestImageHTTPConditionalRangeHeadAndRevocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "picture.png")
	os.WriteFile(path, []byte("0123456789"), 0600)
	var allowed atomic.Bool
	allowed.Store(true)
	s := g.Server(fmt.Sprintf("image-cache-%d", time.Now().UnixNano()))
	s.SetAddr("127.0.0.1:0")
	s.SetDumpRouterMap(false)
	s.BindHandler("/image", func(r *ghttp.Request) {
		r.Response.Header().Set("Cache-Control", "private, no-store")
		if !allowed.Load() {
			r.Response.WriteStatus(403)
			return
		}
		serveLocalImage(r, path)
	})
	if e := s.Start(); e != nil {
		t.Fatal(e)
	}
	defer s.Shutdown()
	address := fmt.Sprintf("http://127.0.0.1:%d/image", s.GetListenedPort())
	get := func(method, etag, byteRange string) (int, http.Header, []byte) {
		t.Helper()
		req, _ := http.NewRequest(method, address, nil)
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		if byteRange != "" {
			req.Header.Set("Range", byteRange)
		}
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, res.Header, b
	}
	status, h, b := get("GET", "", "")
	tag := h.Get("ETag")
	if status != 200 || string(b) != "0123456789" || tag == "" || h.Get("Cache-Control") != imageCacheControl {
		t.Fatal(status, h, string(b))
	}
	status, _, b = get("GET", tag, "")
	if status != 304 || len(b) != 0 {
		t.Fatal("conditional request retransmitted image", status)
	}
	status, _, _ = get("GET", "W/"+tag, "")
	if status != 304 {
		t.Fatal("weak validator failed")
	}
	status, _, b = get("HEAD", "", "")
	if status != 200 || len(b) != 0 {
		t.Fatal("HEAD failed")
	}
	status, _, b = get("GET", "", "bytes=2-5")
	if status != 206 || string(b) != "2345" {
		t.Fatal("range failed", status, string(b))
	}
	os.WriteFile(path, []byte("replacement-new"), 0600)
	status, h, b = get("GET", tag, "")
	if status != 200 || h.Get("ETag") == tag || string(b) != "replacement-new" {
		t.Fatal("replacement served stale content")
	}
	allowed.Store(false)
	status, h, _ = get("GET", h.Get("ETag"), "")
	if status != 403 || h.Get("Cache-Control") != "private, no-store" {
		t.Fatal("conditional cache bypassed authorization")
	}
}
func TestImageGrantStableWithinWindow(t *testing.T) {
	now := time.Now().Truncate(30 * time.Minute).Add(time.Minute)
	a := newFileGrantAt(1, 2, 0, 1, now)
	b := newFileGrantAt(1, 2, 0, 1, now.Add(time.Second))
	if a != b {
		t.Fatal("grant URL changed on each refresh")
	}
	if a == newFileGrantAt(1, 2, 0, 2, now) || a == newFileGrantAt(1, 3, 0, 1, now) {
		t.Fatal("grant crossed authorization scope")
	}
	if a == newFileGrantAt(1, 2, 0, 1, now.Add(30*time.Minute)) {
		t.Fatal("grant not renewed")
	}
	if !strings.Contains(a, ".") {
		t.Fatal("missing signature")
	}
}
