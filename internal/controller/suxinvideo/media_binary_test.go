package suxinvideo

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/internal/extend/middleware"
)

func mediaBinaryHTTPServer(t *testing.T, register func(*ghttp.RouterGroup)) string {
	t.Helper()
	server := ghttp.GetServer(fmt.Sprintf("cms-binary-%d", time.Now().UnixNano()))
	server.SetAddr("127.0.0.1:0")
	server.SetDumpRouterMap(false)
	server.Group("/", func(group *ghttp.RouterGroup) {
		// Use the actual host middleware responsible for the regression.
		group.Middleware(middleware.HandlerResponse)
		register(group)
	})
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Shutdown() })
	return fmt.Sprintf("http://127.0.0.1:%d", server.GetListenedPort())
}

func TestMediaBinaryHTTPExactBodyAndRange(t *testing.T) {
	key := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 255}
	segment := bytes.Repeat([]byte{0x47, 0x40, 0x11, 0, 0xff, 0, 3}, 20000)
	base := mediaBinaryHTTPServer(t, func(group *ghttp.RouterGroup) {
		group.GET("/raw-control", func(r *ghttp.Request) {
			r.Response.Header().Set("Content-Type", "application/octet-stream")
			_, _ = r.Response.Writer.Write(key)
			r.ExitAll()
		})
		group.GET("/key", func(r *ghttp.Request) {
			r.Response.Header().Set("Content-Type", "application/octet-stream")
			_, _ = copyMediaBinary(r.Response, http.StatusOK, bytes.NewReader(key))
			r.ExitAll()
		})
		group.GET("/range", func(r *ghttp.Request) {
			r.Response.Header().Set("Content-Type", "video/mp2t")
			r.Response.Header().Set("Content-Range", "bytes 101-70100/140000")
			_, _ = copyMediaBinary(r.Response, http.StatusPartialContent, bytes.NewReader(segment[101:70101]))
			r.ExitAll()
		})
		group.GET("/buffered-range", func(r *ghttp.Request) {
			// Same buffered response strategy used by the generic media proxy.
			r.Response.WriteHeader(http.StatusPartialContent)
			r.Response.Write(key)
		})
		group.GET("/empty", func(r *ghttp.Request) {
			_, _ = copyMediaBinary(r.Response, http.StatusOK, strings.NewReader(""))
			r.ExitAll()
		})
	})
	client := &http.Client{Timeout: 3 * time.Second}
	for _, test := range []struct {
		path   string
		status int
		body   []byte
	}{{"/key", 200, key}, {"/range", 206, segment[101:70101]}, {"/buffered-range", 206, key}} {
		response, err := client.Get(base + test.path)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil || response.StatusCode != test.status || !bytes.Equal(body, test.body) {
			t.Fatalf("%s status=%d bytes=%d want=%d; media altered or JSON/status text appended", test.path, response.StatusCode, len(body), len(test.body))
		}
		if test.path == "/range" && response.Header.Get("Content-Range") != "bytes 101-70100/140000" {
			t.Fatal("range metadata lost")
		}
	}
	response, err := client.Get(base + "/raw-control")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if len(body) <= len(key) || !bytes.Equal(body[:len(key)], key) || !bytes.Contains(body[len(key):], []byte(`"code"`)) {
		t.Fatal("control did not reproduce host JSON corruption")
	}
	response, err = client.Get(base + "/empty")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 502 || len(body) == 0 || bytes.Contains(body, []byte(`"code"`)) {
		t.Fatal("empty media must fail without a JSON success envelope")
	}
}

func TestMediaBinaryHTTPStreamsBeforeEOFAndBoundsMemory(t *testing.T) {
	chunk := bytes.Repeat([]byte{0x00, 0xff, 0x10, 0x80}, 16<<10)
	const chunks = 64
	stats := make(chan int, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	base := mediaBinaryHTTPServer(t, func(group *ghttp.RouterGroup) {
		group.GET("/movie", func(r *ghttp.Request) {
			r.Response.Header().Set("Content-Type", "video/mp4")
			writer := newMediaBinaryWriter(r.Response, http.StatusOK)
			defer writer.Close()
			maxBuffered := 0
			for i := 0; i < chunks; i++ {
				if _, err := writer.Write(chunk); err != nil {
					return
				}
				maxBuffered = max(maxBuffered, len(writer.tail), cap(writer.tail))
				if i == 0 {
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
				}
			}
			stats <- maxBuffered
			r.ExitAll()
		})
	})
	client := &http.Client{Timeout: 4 * time.Second}
	response, err := client.Get(base + "/movie")
	if err != nil {
		releaseOnce.Do(func() { close(release) })
		t.Fatal("stream was buffered until EOF", err)
	}
	defer response.Body.Close()
	first := make([]byte, 1)
	if _, err := io.ReadFull(response.Body, first); err != nil {
		releaseOnce.Do(func() { close(release) })
		t.Fatal("first media byte unavailable before EOF", err)
	}
	releaseOnce.Do(func() { close(release) })
	actual := sha256.New()
	actual.Write(first)
	n, err := io.Copy(actual, response.Body)
	expected := sha256.New()
	for i := 0; i < chunks; i++ {
		expected.Write(chunk)
	}
	if err != nil || n+1 != int64(len(chunk)*chunks) || !bytes.Equal(actual.Sum(nil), expected.Sum(nil)) {
		t.Fatal("stream truncated, reordered or appended JSON")
	}
	if retained := <-stats; retained > mediaBinaryTailSize {
		t.Fatalf("stream retained %d bytes", retained)
	}
}
