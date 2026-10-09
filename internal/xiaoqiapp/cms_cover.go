package app

import (
	"bytes"
	"context"
	"errors"
	"image/jpeg"
	"os/exec"
	"strings"
	"time"
)

const MaxCMSCoverBytes = maxCoverBytes

// CMSIsHEICCover identifies the HEIC covers supplied by the Hongguo feed.
func CMSIsHEICCover(data []byte) bool { return isHEICImage(data) }

type cmsCoverWriter struct{ bytes.Buffer }

func (w *cmsCoverWriter) Write(data []byte) (int, error) {
	if len(data) > (4<<20)-w.Len() {
		return 0, errors.New("converted cover is too large")
	}
	return w.Buffer.Write(data)
}

// CMSConvertHEICCover reuses the source library's HEIC parser and the CMS's
// bundled FFmpeg. The result is a browser-compatible JPEG with bounded size.
func CMSConvertHEICCover(ctx context.Context, binary string, data []byte) ([]byte, error) {
	image, err := extractHEICImage(data)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	filters := append(image.filters, "scale=w='min(800,iw)':h='min(800,ih)':force_original_aspect_ratio=decrease")
	command := exec.CommandContext(ctx, binary, "-hide_banner", "-loglevel", "error", "-nostdin",
		"-max_alloc", "67108864", "-protocol_whitelist", "pipe", "-threads", "1", "-f", "hevc", "-i", "pipe:0",
		"-frames:v", "1", "-an", "-sn", "-vf", strings.Join(filters, ","), "-filter_threads", "1",
		"-threads", "1", "-c:v", "mjpeg", "-pix_fmt", "yuvj420p", "-q:v", "3", "-f", "image2pipe", "pipe:1")
	command.Stdin = bytes.NewReader(image.data)
	var output cmsCoverWriter
	command.Stdout = &output
	if err := command.Run(); err != nil {
		return nil, errors.New("HEIC cover conversion failed")
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(output.Bytes()))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 800 || config.Height > 800 {
		return nil, errors.New("invalid converted cover")
	}
	return output.Bytes(), nil
}
