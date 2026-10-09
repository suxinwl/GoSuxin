package album

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCoverPreviewDimensionsCacheAndSourceUnchanged(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 2400, 3200))
	for y := 0; y < 3200; y++ {
		for x := 0; x < 2400; x++ {
			source.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), uint8(x * y), 255})
		}
	}
	var original bytes.Buffer
	png.Encode(&original, source)
	path := filepath.Join(t.TempDir(), "source.png")
	os.WriteFile(path, original.Bytes(), 0600)
	f, e := os.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	old := albumImages
	albumImages = newImageDiskCache(t.TempDir(), 512<<20, time.Hour)
	defer func() { albumImages = old }()
	preview, key, e := coverPreviewFile(context.Background(), f, "original-1")
	if e != nil {
		t.Fatal(e)
	}
	encoded, _ := io.ReadAll(preview)
	preview.Close()
	config, e := jpeg.DecodeConfig(bytes.NewReader(encoded))
	if e != nil || config.Width != 360 || config.Height != 480 {
		t.Fatal(config, e)
	}
	if len(encoded) >= original.Len() {
		t.Fatal("preview did not reduce transfer bytes")
	}
	// A cached derivative is available without reading source pixels again.
	f.Close()
	preview, key2, e := coverPreviewFile(context.Background(), f, "original-1")
	if e != nil || key2 != key {
		t.Fatal("preview cache missed", e)
	}
	preview.Close()
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, original.Bytes()) {
		t.Fatal("original modified")
	}
	f, _ = os.Open(path)
	defer f.Close()
	preview, key2, e = coverPreviewFile(context.Background(), f, "replacement-2")
	if e != nil || key2 == key {
		t.Fatal("replacement reused preview")
	}
	preview.Close()
	t.Logf("source %d bytes -> preview %d bytes", original.Len(), len(encoded))
}
