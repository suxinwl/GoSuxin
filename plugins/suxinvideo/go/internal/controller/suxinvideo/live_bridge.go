package suxinvideo

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/suxinwl/GoSuxin/internal/liveruntime"
	"github.com/suxinwl/GoSuxin/internal/mediastream"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type liveBridge struct {
	directory string
	command   *exec.Cmd
	done      chan struct{}
	touched   time.Time
	release   func()
	failed    bool
}

var liveBridges = struct {
	sync.Mutex
	items map[string]*liveBridge
	once  sync.Once
}{items: map[string]*liveBridge{}}
var liveBridgeName = regexp.MustCompile(`^(?:index\.m3u8|part[0-9]+\.ts)$`)

func liveBridgeAcquire(ctx context.Context, stream LiveStream, viewer LiveViewer) (string, error) {
	if !liveEngineMediaURL(stream, stream.URL) {
		return "", appError(502, "实时桥仅接收已授权的引擎媒体")
	}
	scope := stream.AccessLevel + ":" + stream.ModuleKey + ":" + strconv.FormatInt(stream.SourceRevision, 10)
	key := liveIdentity(fmt.Sprintf("%s:%d:%s:%s", stream.ProviderKey, stream.ID, stream.Quality, scope))[:32]
	liveBridges.once.Do(func() { go liveBridgeJanitor() })
	liveBridges.Lock()
	bridge := liveBridges.items[key]
	if bridge != nil && bridge.failed {
		delete(liveBridges.items, key)
		bridge.release()
		os.RemoveAll(bridge.directory)
		bridge = nil
	}
	if bridge == nil {
		if len(liveBridges.items) >= 2 {
			liveBridges.Unlock()
			return "", appError(503, "实时直播桥繁忙，请稍后重试或选择 HLS 线路")
		}
		binary, binaryErr := ffmpegBinary()
		if binaryErr != nil || binary == "" {
			liveBridges.Unlock()
			return "", appError(503, "尚未安装直播桥所需 FFmpeg")
		}
		parent := filepath.Join("data", "tmp", "live-bridges")
		if err := os.MkdirAll(parent, 0700); err != nil {
			liveBridges.Unlock()
			return "", err
		}
		directory, err := os.MkdirTemp(parent, "bridge-")
		if err != nil {
			liveBridges.Unlock()
			return "", err
		}
		codecArgs := []string{"-c", "copy"}
		budgetRelease := func() {}
		if liveBridgeRequiresEncoding(ctx, binary, stream) {
			budgetCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			budgetRelease, err = mediastream.Acquire(budgetCtx, false)
			cancel()
			if err != nil {
				os.RemoveAll(directory)
				liveBridges.Unlock()
				return "", appError(503, "直播转码繁忙，请选择备用线路")
			}
			codecArgs = []string{"-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency", "-c:a", "aac", "-b:a", "128k", "-g", "50"}
		}
		args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-rw_timeout", "15000000", "-headers", "X-IPTV-Secret: " + liveEngineSecret() + "\r\n", "-i", stream.URL, "-map", "0:v:0?", "-map", "0:a:0?"}
		args = append(args, codecArgs...)
		args = append(args, "-f", "hls", "-hls_time", "2", "-hls_list_size", "6", "-hls_delete_threshold", "4", "-hls_flags", "delete_segments+omit_endlist+temp_file", "-hls_segment_filename", filepath.Join(directory, "part%09d.ts"), filepath.Join(directory, "index.m3u8"))
		command := exec.Command(binary, args...)
		command.Stdout = io.Discard
		command.Stderr = io.Discard
		liveruntime.ConfigureChild(command)
		if err = command.Start(); err != nil {
			budgetRelease()
			os.RemoveAll(directory)
			liveBridges.Unlock()
			return "", appError(502, "直播桥启动失败")
		}
		release, err := liveruntime.OwnChild(command)
		if err != nil {
			budgetRelease()
			_ = command.Process.Kill()
			_ = command.Wait()
			os.RemoveAll(directory)
			liveBridges.Unlock()
			return "", err
		}
		bridge = &liveBridge{directory: directory, command: command, done: make(chan struct{}), touched: time.Now(), release: release}
		liveBridges.items[key] = bridge
		go func() {
			_ = command.Wait()
			budgetRelease()
			liveBridges.Lock()
			bridge.failed = true
			close(bridge.done)
			liveBridges.Unlock()
		}()
	}
	bridge.touched = time.Now()
	liveBridges.Unlock()
	timer := time.NewTicker(150 * time.Millisecond)
	defer timer.Stop()
	deadline := time.NewTimer(22 * time.Second)
	defer deadline.Stop()
	for {
		body, err := os.ReadFile(filepath.Join(bridge.directory, "index.m3u8"))
		if err == nil && strings.Contains(string(body), "#EXTINF:") {
			return "livebridge://" + key + "/index.m3u8", nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-bridge.done:
			return "", appError(502, "直播桥取流失败，请选择备用线路")
		case <-deadline.C:
			return "", appError(504, "直播桥准备超时，请重试或换线")
		case <-timer.C:
		}
	}
}

func liveBridgeRequiresEncoding(ctx context.Context, ffmpeg string, stream LiveStream) bool {
	name := "ffprobe"
	if strings.HasSuffix(strings.ToLower(ffmpeg), ".exe") {
		name += ".exe"
	}
	binary := filepath.Join(filepath.Dir(ffmpeg), name)
	if _, err := os.Stat(binary); err != nil {
		var e error
		binary, e = exec.LookPath(name)
		if e != nil {
			return false
		}
	}
	probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	command := exec.CommandContext(probeCtx, binary, "-v", "error", "-rw_timeout", "5000000", "-probesize", "1048576", "-analyzeduration", "2000000", "-headers", "X-IPTV-Secret: "+liveEngineSecret()+"\r\n", "-show_entries", "stream=codec_type,codec_name", "-of", "json", stream.URL)
	liveruntime.ConfigureChild(command)
	data, err := command.Output()
	if err != nil {
		return false
	}
	var info struct {
		Streams []struct {
			Type  string `json:"codec_type"`
			Codec string `json:"codec_name"`
		} `json:"streams"`
	}
	if json.Unmarshal(data, &info) != nil {
		return false
	}
	for _, s := range info.Streams {
		if s.Type == "video" && s.Codec != "h264" || s.Type == "audio" && s.Codec != "aac" {
			return true
		}
	}
	return false
}
func liveBridgeRead(raw string) ([]byte, string, error) {
	identity := strings.TrimPrefix(raw, "livebridge://")
	key, name, ok := strings.Cut(identity, "/")
	if !ok || !liveBridgeName.MatchString(name) {
		return nil, "", fmt.Errorf("直播桥资源无效")
	}
	liveBridges.Lock()
	bridge := liveBridges.items[key]
	if bridge != nil {
		bridge.touched = time.Now()
	}
	liveBridges.Unlock()
	if bridge == nil {
		return nil, "", appError(410, "直播桥已释放，请重新连接")
	}
	body, err := os.ReadFile(filepath.Join(bridge.directory, name))
	if err != nil {
		return nil, "", appError(502, "直播分片已更新，请返回直播位置")
	}
	if len(body) > 64<<20 {
		return nil, "", appError(502, "直播分片过大")
	}
	return body, name, nil
}
func liveBridgeJanitor() {
	timer := time.NewTicker(10 * time.Second)
	defer timer.Stop()
	for range timer.C {
		liveBridges.Lock()
		for key, item := range liveBridges.items {
			if time.Since(item.touched) > 60*time.Second {
				delete(liveBridges.items, key)
				_ = item.command.Process.Kill()
				item.release()
				os.RemoveAll(item.directory)
			}
		}
		liveBridges.Unlock()
	}
}
