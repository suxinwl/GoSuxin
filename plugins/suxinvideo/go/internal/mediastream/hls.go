// Package mediastream turns already authorized encrypted media into seekable VOD
// HLS. It has no knowledge of CMS users or provider accounts.
package mediastream

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const segmentSeconds = 2
const batchSize = 12
const cacheLimit = 96 << 20

type Config struct {
	URL, Referer, Binary, Directory string
	Headers                         map[string]string
	Key                             []byte
	Duration                        time.Duration
	Background                      bool
}

type batch struct {
	done chan struct{}
	err  error
}
type Session struct {
	cfg      Config
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	jobs     map[int]*batch
	segments map[int][]byte
	bytes    int
	closed   bool
	changed  chan struct{}
	wg       sync.WaitGroup
}

func New(cfg Config) (*Session, error) {
	if cfg.Binary == "" || !strings.HasPrefix(cfg.URL, "https://") && !strings.HasPrefix(cfg.URL, "http://") || len(cfg.Key) != 16 || cfg.Duration <= 0 || cfg.Duration > 24*time.Hour {
		return nil, errors.New("媒体转码参数无效")
	}
	for name, value := range cfg.Headers {
		if strings.ContainsAny(name+value, "\r\n") {
			return nil, errors.New("媒体请求头无效")
		}
	}
	if strings.ContainsAny(cfg.Referer, "\r\n") {
		return nil, errors.New("媒体来源无效")
	}
	dir, err := os.MkdirTemp(cfg.Directory, "suxin-native-hls-")
	if err != nil {
		return nil, err
	}
	cfg.Directory = dir
	ctx, cancel := context.WithCancel(context.Background())
	return &Session{cfg: cfg, ctx: ctx, cancel: cancel, jobs: map[int]*batch{}, segments: map[int][]byte{}, changed: make(chan struct{})}, nil
}

func (s *Session) Count() int { return int(math.Ceil(s.cfg.Duration.Seconds() / segmentSeconds)) }

func (s *Session) Manifest(link func(int) string) []byte {
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:2\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-INDEPENDENT-SEGMENTS\n")
	for i := 0; i < s.Count(); i++ {
		duration := math.Min(segmentSeconds, s.cfg.Duration.Seconds()-float64(i*segmentSeconds))
		fmt.Fprintf(&b, "#EXTINF:%.6f,\n%s\n", duration, link(i))
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return []byte(b.String())
}

func (s *Session) Segment(ctx context.Context, index int) ([]byte, error) {
	if index < 0 || index >= s.Count() {
		return nil, errors.New("分片不存在")
	}
	start := index / batchSize * batchSize
	for {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return nil, errors.New("媒体会话已关闭")
		}
		if body := s.segments[index]; body != nil {
			s.mu.Unlock()
			return body, nil
		}
		job := s.jobs[start]
		if job == nil {
			job = &batch{done: make(chan struct{})}
			s.jobs[start] = job
			s.wg.Add(1)
			go s.render(start, job)
		}
		changed := s.changed
		select {
		case <-job.done:
			err := job.err
			s.mu.Unlock()
			if err != nil {
				return nil, err
			}
			return nil, errors.New("分片缓存已更新，请重试")
		default:
		}
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.ctx.Done():
			return nil, s.ctx.Err()
		case <-changed:
		case <-job.done:
		}
	}
}

func (s *Session) render(start int, job *batch) {
	defer s.wg.Done()
	err := acquire(s.ctx, s.cfg.Background)
	if err == nil {
		defer release()
		err = s.renderBatch(start)
	}
	s.mu.Lock()
	job.err = err
	close(job.done)
	if err != nil {
		delete(s.jobs, start)
	}
	s.mu.Unlock()
}

func (s *Session) renderBatch(start int) error {
	dir, err := os.MkdirTemp(s.cfg.Directory, "batch-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	end := min(start+batchSize, s.Count())
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-rw_timeout", "20000000", "-protocol_whitelist", "http,https,tcp,tls,crypto,httpproxy", "-decryption_key", hex.EncodeToString(s.cfg.Key)}
	var headers strings.Builder
	if s.cfg.Referer != "" {
		fmt.Fprintf(&headers, "Referer: %s\r\n", s.cfg.Referer)
	}
	for name, value := range s.cfg.Headers {
		fmt.Fprintf(&headers, "%s: %s\r\n", name, value)
	}
	if headers.Len() > 0 {
		args = append(args, "-headers", headers.String())
	}
	offset := strconv.Itoa(start * segmentSeconds)
	args = append(args, "-ss", offset, "-i", s.cfg.URL, "-map", "0:v:0", "-map", "0:a:0?", "-sn", "-dn", "-vf", "fps=30,scale=trunc(iw/2)*2:trunc(ih/2)*2,setsar=1", "-c:v", "libx264", "-preset", "veryfast", "-profile:v", "baseline", "-pix_fmt", "yuv420p", "-crf", "23", "-threads", "2", "-g", "60", "-keyint_min", "60", "-sc_threshold", "0", "-c:a", "aac", "-b:a", "128k", "-ar", "48000", "-ac", "2", "-t", strconv.Itoa((end-start)*segmentSeconds), "-output_ts_offset", offset, "-f", "hls", "-hls_time", "2", "-hls_list_size", "0", "-hls_segment_type", "mpegts", "-hls_segment_options", "mpegts_flags=+initial_discontinuity", "-hls_flags", "independent_segments+temp_file", "-start_number", strconv.Itoa(start), "-hls_segment_filename", filepath.Join(dir, "%06d.ts"), filepath.Join(dir, "index.m3u8"))
	ctx, cancel := context.WithTimeout(s.ctx, 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, s.cfg.Binary, args...)
	configureCommand(command)
	// Arguments and upstream errors may contain decryption keys and signed URLs.
	// Return a bounded generic error; never attach Cmd or provider stderr.
	if err = command.Start(); err != nil {
		return errors.New("无法启动媒体分段转码")
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	next := start
	collect := func() error {
		for next < end {
			path := filepath.Join(dir, fmt.Sprintf("%06d.ts", next))
			info, readErr := os.Stat(path)
			if os.IsNotExist(readErr) {
				return nil
			}
			if readErr != nil {
				return readErr
			}
			if info.Size() < 1 || info.Size() > 16<<20 {
				return errors.New("媒体分片大小异常")
			}
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			s.store(next, start, end, body)
			next++
		}
		return nil
	}
	for {
		select {
		case runErr := <-done:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if runErr != nil {
				return errors.New("媒体分段转码失败，请重试或更换线路")
			}
			if err = collect(); err != nil {
				return err
			}
			if next < end {
				return errors.New("媒体分片未完整生成")
			}
			return nil
		case <-ticker.C:
			if err = collect(); err != nil {
				cancel()
				<-done
				return err
			}
		case <-ctx.Done():
			<-done
			return ctx.Err()
		}
	}
}

func (s *Session) store(index, start, end int, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.bytes -= len(s.segments[index])
	s.segments[index] = body
	s.bytes += len(body)
	for s.bytes > cacheLimit {
		victim, distance := -1, -1
		for k := range s.segments {
			if k >= start && k < end {
				continue
			}
			delta := abs(k - index)
			if delta > distance {
				victim, distance = k, delta
			}
		}
		if victim < 0 {
			break
		}
		s.bytes -= len(s.segments[victim])
		delete(s.segments, victim)
		delete(s.jobs, victim/batchSize*batchSize)
	}
	close(s.changed)
	s.changed = make(chan struct{})
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
func (s *Session) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
	_ = os.RemoveAll(s.cfg.Directory)
}

type waiter struct {
	ctx        context.Context
	ready      chan struct{}
	background bool
	granted    bool
}

var slots = struct {
	sync.Mutex
	active int
	queue  []*waiter
}{}

// Acquire shares the two-process budget with compatibility MP4 preparation.
// The returned release function must be called once after FFmpeg exits.
func Acquire(ctx context.Context, background bool) (func(), error) {
	if err := acquire(ctx, background); err != nil {
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(release) }, nil
}

func acquire(ctx context.Context, background bool) error {
	slots.Lock()
	if ctx.Err() != nil {
		slots.Unlock()
		return ctx.Err()
	}
	if slots.active < 2 {
		slots.active++
		slots.Unlock()
		return nil
	}
	w := &waiter{ctx: ctx, ready: make(chan struct{}), background: background}
	index := len(slots.queue)
	if !background {
		for i, v := range slots.queue {
			if v.background {
				index = i
				break
			}
		}
	}
	slots.queue = append(slots.queue, nil)
	copy(slots.queue[index+1:], slots.queue[index:])
	slots.queue[index] = w
	slots.Unlock()
	select {
	case <-w.ready:
		return nil
	case <-ctx.Done():
		slots.Lock()
		found := false
		for i, v := range slots.queue {
			if v == w {
				slots.queue = append(slots.queue[:i], slots.queue[i+1:]...)
				found = true
				break
			}
		}
		granted := w.granted
		slots.Unlock()
		if !found && granted {
			release()
		}
		return ctx.Err()
	}
}
func release() {
	slots.Lock()
	defer slots.Unlock()
	for len(slots.queue) > 0 {
		w := slots.queue[0]
		slots.queue = slots.queue[1:]
		if w.ctx.Err() != nil {
			continue
		}
		w.granted = true
		close(w.ready)
		return
	}
	slots.active--
}
