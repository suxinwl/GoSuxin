package album

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
)

const imageCacheControl = "private, no-cache, must-revalidate"
const maxCachedImageBytes int64 = 64 << 20

type imageFlight struct {
	done chan struct{}
	err  error
}
type imageDiskCache struct {
	root    string
	limit   int64
	ttl     time.Duration
	mu      sync.Mutex
	flights map[string]*imageFlight
	slots   chan struct{}
}

func newImageDiskCache(root string, limit int64, ttl time.Duration) *imageDiskCache {
	return &imageDiskCache{root: root, limit: limit, ttl: ttl, flights: make(map[string]*imageFlight), slots: make(chan struct{}, 4)}
}

var albumImages = newImageDiskCache("storage/cache/album-images", 512<<20, 7*24*time.Hour)

// Identity comes only from the authorized database record, never a user supplied URL.
func imageCacheKey(asset gdb.Record) string {
	identity := fmt.Sprintf("%s:%s:%s:%s:%s:%s", asset["id"], asset["provider"], asset["profile_id"], asset["file_id"], asset["checksum"], asset["size_bytes"])
	return fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))
}
func (c *imageDiskCache) open(ctx context.Context, key, checksum string, size int64, fetch func(context.Context) (io.ReadCloser, error)) (*os.File, error) {
	digest, err := hex.DecodeString(checksum)
	if len(key) != 64 || strings.ContainsAny(key, "/\\") || err != nil || len(digest) != md5.Size || size <= 0 || size > maxCachedImageBytes || size > c.limit {
		return nil, errors.New("图片缓存元数据无效或超过大小限制")
	}
	if _, err = hex.DecodeString(key); err != nil {
		return nil, err
	}
	path := filepath.Join(c.root, key+".cache")
	for {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		c.mu.Lock()
		if info, e := os.Lstat(path); e == nil && info.Mode().IsRegular() && info.Size() == size && time.Since(info.ModTime()) < c.ttl {
			f, e := os.Open(path)
			if e == nil {
				now := time.Now()
				_ = os.Chtimes(path, now, now)
				c.mu.Unlock()
				return f, nil
			}
		}
		if flight := c.flights[key]; flight != nil {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-flight.done:
				if flight.err != nil {
					return nil, flight.err
				}
				continue
			}
		}
		flight := &imageFlight{done: make(chan struct{})}
		c.flights[key] = flight
		c.mu.Unlock()
		err = c.fill(ctx, path, checksum, size, fetch)
		c.mu.Lock()
		flight.err = err
		delete(c.flights, key)
		close(flight.done)
		c.mu.Unlock()
		if err != nil {
			return nil, err
		}
	}
}
func (c *imageDiskCache) fill(ctx context.Context, path, checksum string, size int64, fetch func(context.Context) (io.ReadCloser, error)) error {
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := os.MkdirAll(c.root, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(c.root, "download-*.tmp")
	if err != nil {
		return err
	}
	defer func() { f.Close(); _ = os.Remove(f.Name()) }()
	body, err := fetch(ctx)
	if err != nil {
		return err
	}
	defer body.Close()
	h := md5.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(body, size+1))
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if n != size || !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), checksum) {
		return errors.New("云盘图片大小或摘要校验失败")
	}
	if err = f.Close(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// Remove only files in this private cache, with names owned by this implementation.
	if err = c.prune(path, size); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func (c *imageDiskCache) prune(target string, incoming int64) error {
	entries, err := os.ReadDir(c.root)
	if err != nil {
		return err
	}
	type entry struct {
		path string
		size int64
		time time.Time
	}
	var files []entry
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		name := e.Name()
		p := filepath.Join(c.root, name)
		if strings.HasPrefix(name, "download-") && strings.HasSuffix(name, ".tmp") && time.Since(info.ModTime()) > 10*time.Minute {
			_ = os.Remove(p)
			continue
		}
		if len(name) != 70 || !strings.HasSuffix(name, ".cache") {
			continue
		}
		if _, err := hex.DecodeString(strings.TrimSuffix(name, ".cache")); err != nil {
			continue
		}
		if p == target || time.Since(info.ModTime()) >= c.ttl {
			if err = os.Remove(p); err == nil {
				continue
			}
		}
		total += info.Size()
		files = append(files, entry{p, info.Size(), info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].time.Before(files[j].time) })
	count := len(files)
	for _, f := range files {
		if total+incoming <= c.limit && count < 4096 {
			break
		}
		if os.Remove(f.path) == nil {
			total -= f.size
			count--
		}
	}
	if total+incoming > c.limit || count >= 4096 {
		return errors.New("图片缓存空间不足")
	}
	return nil
}
func cachedCloudImage(ctx context.Context, asset gdb.Record) (*os.File, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	return albumImages.open(ctx, imageCacheKey(asset), asset["checksum"].String(), asset["size_bytes"].Int64(), func(ctx context.Context) (io.ReadCloser, error) {
		address, err := cloudURL(ctx, asset, time.Now().Add(60*time.Second))
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return nil, err
		}
		res, err := panCDNClient(panHTTP).Do(req)
		if err != nil {
			return nil, errors.New("下载云盘图片失败")
		}
		if res.StatusCode != http.StatusOK {
			res.Body.Close()
			return nil, fmt.Errorf("下载云盘图片失败（HTTP %d）", res.StatusCode)
		}
		return res.Body, nil
	})
}

// The caller must authorize and resolve the CURRENT reference before every call,
// including conditional requests. Never send a 304 before checking permissions.
func serveImageContent(r *ghttp.Request, f *os.File, etag string) {
	if r.Get("preview").String() == "cover" {
		preview, key, err := coverPreviewFile(r.Context(), f, etag)
		if err == nil {
			defer preview.Close()
			f = preview
			etag = key
		} else {
			// Oversized/unsupported legacy images remain readable at original quality.
			f.Seek(0, io.SeekStart)
		}
	}
	r.Response.Header().Set("Cache-Control", imageCacheControl)
	r.Response.Header().Set("ETag", `"`+etag+`"`)
	r.Response.Header().Set("X-Content-Type-Options", "nosniff")
	// No Last-Modified: two replacements in one second must not validate as equal.
	r.Response.ServeContent("image", time.Time{}, f)
}
func serveLocalImage(r *ghttp.Request, path string) {
	f, err := os.Open(path)
	if err != nil {
		r.Response.WriteStatus(http.StatusNotFound)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		f.Close()
		r.Response.WriteStatus(http.StatusNotFound)
		return
	}
	etag := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d", path, info.Size(), info.ModTime().UnixNano()))))
	serveImageContent(r, f, etag)
}
