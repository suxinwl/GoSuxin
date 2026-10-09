package album

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

var previewSlots = make(chan struct{}, 2)

// Only one fixed variant is available; originals are never modified.
func encodeCoverPreview(ctx context.Context, source io.ReadSeeker) ([]byte, error) {
	select {
	case previewSlots <- struct{}{}:
		defer func() { <-previewSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if _, e := source.Seek(0, io.SeekStart); e != nil {
		return nil, e
	}
	config, _, e := image.DecodeConfig(source)
	if e != nil {
		return nil, e
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 24000000 {
		return nil, errors.New("preview source dimensions exceed limit")
	}
	source.Seek(0, io.SeekStart)
	original, _, e := image.Decode(source)
	if e != nil {
		return nil, e
	}
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	w, h := config.Width, config.Height
	if w > 480 || h > 480 {
		if w >= h {
			h = max(1, h*480/w)
			w = 480
		} else {
			w = max(1, w*480/h)
			h = 480
		}
	}
	dest := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dest, dest.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.ApproxBiLinear.Scale(dest, dest.Bounds(), original, original.Bounds(), draw.Over, nil)
	var out bytes.Buffer
	e = jpeg.Encode(&out, dest, &jpeg.Options{Quality: 80})
	return out.Bytes(), e
}

func coverPreviewFile(ctx context.Context, source *os.File, identity string) (*os.File, string, error) {
	key := fmt.Sprintf("%x", sha256.Sum256([]byte("cover-480-jpeg80-v1:"+identity)))
	c := albumImages
	path := filepath.Join(c.root, key+".cache")
	for {
		if e := ctx.Err(); e != nil {
			return nil, "", e
		}
		c.mu.Lock()
		if info, e := os.Lstat(path); e == nil && info.Mode().IsRegular() && info.Size() > 0 && info.Size() < 1<<20 && time.Since(info.ModTime()) < c.ttl {
			f, e := os.Open(path)
			if e == nil {
				now := time.Now()
				_ = os.Chtimes(path, now, now)
				c.mu.Unlock()
				return f, key, nil
			}
		}
		if flight := c.flights[key]; flight != nil {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, "", ctx.Err()
			case <-flight.done:
				if flight.err != nil {
					return nil, "", flight.err
				}
				continue
			}
		}
		flight := &imageFlight{done: make(chan struct{})}
		c.flights[key] = flight
		c.mu.Unlock()
		data, e := encodeCoverPreview(ctx, source)
		if e == nil {
			checksum := fmt.Sprintf("%x", md5.Sum(data))
			e = c.fill(ctx, path, checksum, int64(len(data)), func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil })
		}
		c.mu.Lock()
		flight.err = e
		delete(c.flights, key)
		close(flight.done)
		c.mu.Unlock()
		if e != nil {
			return nil, "", e
		}
	}
}
