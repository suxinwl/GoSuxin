package suxinvideo

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net/http"

	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
)

const mediaBinaryTailSize = 32 << 10

// mediaBinaryWriter streams all but the final small block to the raw HTTP
// writer. Close leaves that final block in GoFrame's response buffer: the host
// HandlerResponse middleware recognizes buffered content and will not append
// its JSON envelope to an AES key, HLS segment or MP4 stream.
//
// The retained bytes are actual media, never a sentinel. Memory stays bounded
// for long MP4/FFmpeg responses, and neither 200 nor 206 adds status-text bytes.
type mediaBinaryWriter struct {
	response *ghttp.Response
	status   int
	tail     []byte
	written  int64
	closed   bool
	err      error
}

func newMediaBinaryWriter(response *ghttp.Response, status int) *mediaBinaryWriter {
	response.WriteHeader(status)
	return &mediaBinaryWriter{response: response, status: status, tail: make([]byte, 0, mediaBinaryTailSize)}
}

func (w *mediaBinaryWriter) writeRaw(p []byte) error {
	if len(p) == 0 {
		return nil
	}
	w.response.Writer.WriteHeader(w.status)
	n, err := w.response.Writer.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err == nil {
		w.response.Writer.Flush()
	}
	return err
}

func (w *mediaBinaryWriter) Write(p []byte) (int, error) {
	if w.closed {
		return 0, io.ErrClosedPipe
	}
	if w.err != nil {
		return 0, w.err
	}
	n := len(p)
	if n == 0 {
		return 0, nil
	}
	excess := len(w.tail) + n - mediaBinaryTailSize
	if excess > 0 {
		fromTail := min(excess, len(w.tail))
		if w.err = w.writeRaw(w.tail[:fromTail]); w.err != nil {
			return 0, w.err
		}
		copy(w.tail, w.tail[fromTail:])
		w.tail = w.tail[:len(w.tail)-fromTail]
		if fromInput := excess - fromTail; fromInput > 0 {
			if w.err = w.writeRaw(p[:fromInput]); w.err != nil {
				return 0, w.err
			}
			p = p[fromInput:]
		}
	}
	w.tail = append(w.tail, p...)
	w.written += int64(n)
	return n, nil
}

func (w *mediaBinaryWriter) Close() error {
	if w.closed {
		return w.err
	}
	w.closed = true
	if len(w.tail) > 0 {
		w.response.Write(w.tail)
		w.tail = nil
	} else if w.written == 0 && w.response.BytesWritten() == 0 {
		// An empty/error stream must produce a visible gateway failure, never
		// a successful media response containing the host's JSON fallback.
		w.response.WriteStatus(http.StatusBadGateway, "媒体数据为空或读取失败")
		if w.err == nil {
			w.err = errors.New("媒体数据为空或读取失败")
		}
	}
	return w.err
}

func copyMediaBinary(response *ghttp.Response, status int, input io.Reader) (int64, error) {
	if status == http.StatusOK {
		var normalized bool
		input, normalized = unwrapPNGTransportStream(input)
		if normalized {
			response.Header().Set("Content-Type", "video/mp2t")
			response.Header().Del("Content-Length")
		}
	}
	writer := newMediaBinaryWriter(response, status)
	n, err := io.Copy(writer, input)
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	return n, err
}

// Some public CDNs carry a TS fragment after a short PNG header. Confirm five
// complete, aligned MPEG-TS packet headers before discarding any bytes. Actual
// images, AES keys and MP4s pass through unchanged. Partial responses preserve
// byte offsets and Content-Range, so only complete HTTP 200 bodies are unwrapped.
func unwrapPNGTransportStream(input io.Reader) (io.Reader, bool) {
	buffer := bufio.NewReaderSize(input, 2048)
	header, _ := buffer.Peek(8)
	if !bytes.Equal(header, []byte{0x89, 'P', 'N', 'G', 13, 10, 26, 10}) {
		return buffer, false
	}
	data, _ := buffer.Peek(2048)
	for offset := 8; offset <= 1024 && offset+5*188 <= len(data); offset++ {
		valid := true
		for packet := 0; packet < 5; packet++ {
			at := offset + packet*188
			if data[at] != 0x47 || data[at+1]&0x80 != 0 || data[at+3]&0x30 == 0 {
				valid = false
				break
			}
		}
		if valid {
			_, _ = buffer.Discard(offset)
			return buffer, true
		}
	}
	return buffer, false
}
