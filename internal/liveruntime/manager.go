// Package liveruntime owns only the plugin's loopback IPTV child processes.
package liveruntime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var manager = struct {
	sync.Mutex
	cancel   context.CancelFunc
	commands map[string]*exec.Cmd
	states   map[string]State
	secret   string
	stopped  bool
}{commands: map[string]*exec.Cmd{}, states: map[string]State{}}

type State struct {
	Profile  string `json:"profile"`
	PID      int    `json:"pid"`
	Healthy  bool   `json:"healthy"`
	Error    string `json:"error,omitempty"`
	Restarts int    `json:"restarts"`
}

func Origin(profile string) string {
	if profile == "public" {
		return "http://127.0.0.1:9180"
	}
	if profile == "member" {
		return "http://127.0.0.1:9181"
	}
	return ""
}
func Secret() string {
	manager.Lock()
	defer manager.Unlock()
	if manager.secret != "" {
		return manager.secret
	}
	directory := filepath.Join("data", "suxinvideo", "iptv-engine")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return ""
	}
	file := filepath.Join(directory, "internal.secret")
	data, err := os.ReadFile(file)
	if err == nil && len(strings.TrimSpace(string(data))) >= 64 {
		manager.secret = strings.TrimSpace(string(data))
		return manager.secret
	}
	token := make([]byte, 32)
	if _, err = rand.Read(token); err != nil {
		return ""
	}
	// O_EXCL makes the shared secret safe across simultaneous initializers.
	handle, err := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		data, err = os.ReadFile(file)
		if err == nil {
			manager.secret = strings.TrimSpace(string(data))
		}
		return manager.secret
	}
	if err != nil {
		return ""
	}
	_, err = handle.WriteString(hex.EncodeToString(token))
	_ = handle.Close()
	if err != nil {
		return ""
	}
	manager.secret = hex.EncodeToString(token)
	return manager.secret
}
func States() []State {
	manager.Lock()
	defer manager.Unlock()
	items := []State{}
	for _, key := range []string{"public", "member"} {
		if s, ok := manager.states[key]; ok {
			items = append(items, s)
		}
	}
	return items
}
func health(ctx context.Context, profile, secret string) bool {
	req, err := http.NewRequestWithContext(ctx, "GET", Origin(profile)+"/internal/health", nil)
	if err != nil {
		return false
	}
	req.Header.Set("X-IPTV-Secret", secret)
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	return response.StatusCode == 200
}

// Start is nonblocking; provider problems never prevent the existing Go lines.
func Start(memberConfigured func(context.Context) bool) {
	if strings.EqualFold(os.Getenv("SUXIN_IPTV_DISABLED"), "1") {
		return
	}
	manager.Lock()
	if manager.cancel != nil || manager.stopped {
		manager.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	manager.cancel = cancel
	manager.Unlock()
	secret := Secret()
	if secret == "" {
		return
	}
	go supervise(ctx, "public", secret, nil)
	go supervise(ctx, "member", secret, memberConfigured)
}
func setState(profile string, pid int, healthy bool, message string, restarts int) {
	manager.Lock()
	manager.states[profile] = State{profile, pid, healthy, message, restarts}
	manager.Unlock()
}
func supervise(ctx context.Context, profile, secret string, configured func(context.Context) bool) {
	restarts := 0
	backoff := time.Second
	for ctx.Err() == nil {
		if configured != nil && !configured(ctx) {
			setState(profile, 0, false, "未配置账号来源", restarts)
			if !pause(ctx, 5*time.Second) {
				return
			}
			continue
		}
		binary, err := nodeBinary()
		if err != nil {
			setState(profile, 0, false, "未找到 Node 24，请执行引擎部署脚本", restarts)
			if !pause(ctx, 30*time.Second) {
				return
			}
			continue
		}
		versionCtx, versionCancel := context.WithTimeout(ctx, 5*time.Second)
		versionErr := CheckNode(versionCtx)
		versionCancel()
		if versionErr != nil {
			setState(profile, 0, false, "需要 Node 24，请执行引擎部署脚本", restarts)
			if !pause(ctx, 30*time.Second) {
				return
			}
			continue
		}
		entry, err := filepath.Abs(filepath.Join("third_party", "akiralereal-iptv", "adapter", "server.mjs"))
		if err != nil {
			return
		}
		if _, err = os.Stat(entry); err != nil {
			setState(profile, 0, false, "未安装 IPTV 引擎源码", restarts)
			if !pause(ctx, 30*time.Second) {
				return
			}
			continue
		}
		dependency := filepath.Join(filepath.Dir(filepath.Dir(entry)), "node_modules", "node-fetch", "package.json")
		if _, err = os.Stat(dependency); err != nil {
			setState(profile, 0, false, "引擎依赖未安装，请执行 npm ci", restarts)
			if !pause(ctx, 30*time.Second) {
				return
			}
			continue
		}
		directory, _ := filepath.Abs(filepath.Join("data", "suxinvideo", "iptv-engine", profile))
		_ = os.MkdirAll(directory, 0700)
		command := exec.Command(binary, entry)
		command.Dir = filepath.Dir(filepath.Dir(entry))
		command.Env = append(os.Environ(), "IPTV_PROFILE="+profile, "IPTV_PORT="+strings.TrimPrefix(Origin(profile), "http://127.0.0.1:"), "IPTV_DATA_DIR="+directory, "IPTV_INTERNAL_SECRET="+secret, "IPTV_PARENT_PID="+strconv.Itoa(os.Getpid()), "IPTV_BROWSER_CONCURRENCY=2", "IPTV_SOURCE_CONCURRENCY=4")
		// Source URLs and account logs are deliberately not forwarded to host logs.
		command.Stdout = io.Discard
		command.Stderr = io.Discard
		configureCommand(command)
		err = command.Start()
		if err != nil {
			setState(profile, 0, false, "引擎启动失败", restarts)
			if !pause(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, 30*time.Second)
			continue
		}
		release, jobErr := ownProcess(command)
		if jobErr != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
			setState(profile, 0, false, "无法隔离引擎进程", restarts)
			if !pause(ctx, 30*time.Second) {
				return
			}
			continue
		}
		manager.Lock()
		manager.commands[profile] = command
		manager.Unlock()
		exited := make(chan error, 1)
		go func() { exited <- command.Wait() }()
		ticks := time.NewTicker(5 * time.Second)
		done := false
		healthySince := time.Time{}
		failures := 0
		for !done {
			select {
			case <-ctx.Done():
				_ = command.Process.Kill()
				<-exited
				done = true
			case <-exited:
				done = true
			case <-ticks.C:
				if configured != nil && !configured(ctx) {
					_ = command.Process.Kill()
					<-exited
					done = true
					continue
				}
				ok := health(ctx, profile, secret)
				if ok {
					failures = 0
					if healthySince.IsZero() {
						healthySince = time.Now()
					}
					if time.Since(healthySince) > time.Minute {
						backoff = time.Second
					}
				} else {
					failures++
					healthySince = time.Time{}
				}
				setState(profile, command.Process.Pid, ok, "", restarts)
				if failures >= 12 {
					_ = command.Process.Kill()
					<-exited
					done = true
				}
			}
		}
		ticks.Stop()
		release()
		manager.Lock()
		delete(manager.commands, profile)
		manager.Unlock()
		restarts++
		setState(profile, 0, false, "引擎重启中", restarts)
		if !pause(ctx, backoff) {
			return
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}
func pause(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// Stop is also called before uninstall; data and login profiles are preserved.
func Stop() {
	manager.Lock()
	manager.stopped = true
	if manager.cancel != nil {
		manager.cancel()
	}
	for _, cmd := range manager.commands {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
	manager.Unlock()
}
func CheckNode(ctx context.Context) error {
	binary, err := nodeBinary()
	if err != nil {
		return err
	}
	out, err := exec.CommandContext(ctx, binary, "--version").Output()
	if err != nil {
		return err
	}
	if !strings.HasPrefix(strings.TrimSpace(string(out)), "v24.") {
		return fmt.Errorf("Node 24 is required")
	}
	return nil
}

// MarshalStates contains only process health, never credentials or source URLs.
func MarshalStates() []byte { data, _ := json.Marshal(States()); return data }

func ConfigureChild(command *exec.Cmd)           { configureCommand(command) }
func OwnChild(command *exec.Cmd) (func(), error) { return ownProcess(command) }

func nodeBinary() (string, error) {
	if configured := os.Getenv("SUXIN_NODE"); configured != "" {
		return exec.LookPath(configured)
	}
	return exec.LookPath("node")
}
