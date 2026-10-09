package suxinvideo

import (
	"bufio"
	"context"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"github.com/suxinwl/GoSuxin/internal/mediastream"
)

const appCastJobSchema = `CREATE TABLE IF NOT EXISTS sx_app_cast_job (
 id CHAR(48) PRIMARY KEY,user_id INT UNSIGNED NOT NULL,device_id VARCHAR(120) NOT NULL,
 vod_id INT UNSIGNED NOT NULL,status VARCHAR(20) NOT NULL,progress INT NOT NULL DEFAULT 0,
 position_ms BIGINT NOT NULL DEFAULT 0,duration_ms BIGINT NOT NULL DEFAULT 0,size BIGINT NOT NULL DEFAULT 0,
 error VARCHAR(200) NOT NULL DEFAULT '',created BIGINT NOT NULL,expires BIGINT NOT NULL,
 KEY owner(user_id,device_id,created),KEY expires(expires)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

var castPreparation = struct {
	sync.Mutex
	cancels map[string]context.CancelFunc
}{cancels: map[string]context.CancelFunc{}}
var castCacheBudget = struct {
	sync.Mutex
	reserved int64
}{}

func reserveCastCache(bytes int64) (func(), error) {
	castCacheBudget.Lock()
	defer castCacheBudget.Unlock()
	var used int64
	entries, _ := os.ReadDir(castMediaDirectory())
	for _, entry := range entries {
		if info, e := entry.Info(); e == nil {
			used += info.Size()
		}
	}
	if used+castCacheBudget.reserved+bytes > 5<<30 {
		return nil, appError(507, "投屏媒体缓存空间不足，请稍后重试")
	}
	castCacheBudget.reserved += bytes
	var once sync.Once
	return func() {
		once.Do(func() { castCacheBudget.Lock(); castCacheBudget.reserved -= bytes; castCacheBudget.Unlock() })
	}, nil
}

func castMediaDirectory() string   { return filepath.Join("data", "tmp", "app-cast-mp4") }
func castMP4Path(id string) string { return filepath.Join(castMediaDirectory(), id+".mp4") }

type AppCastPrepareReq struct {
	g.Meta `path:"/app/v1/cast/prepare" method:"post" noValApi:"1"`
	AppPlaybackRequest
}
type AppCastPrepareRes struct{}

func (*AppClients) CastPrepare(ctx context.Context, req *AppCastPrepareReq) (*AppCastPrepareRes, error) {
	r := g.RequestFromCtx(ctx)
	p, err := clientPrincipal(r)
	if err != nil {
		clientReply(r, nil, err)
		return &AppCastPrepareRes{}, nil
	}
	film, err := appVisibleFilm(r.Context(), req.VodID)
	if err == nil {
		_, err = AppAuthorizeFilm(r.Context(), film)
	}
	if err != nil {
		clientReply(r, nil, err)
		return &AppCastPrepareRes{}, nil
	}
	uid := gconv.Int64(p.User["id"])
	if !allowClientOperation("prepare:"+gconv.String(uid)+":"+p.DeviceID, 6, 2) {
		clientReply(r, nil, appError(429, "媒体准备请求过于频繁"))
		return &AppCastPrepareRes{}, nil
	}
	pending, err := one(ctx, "SELECT COUNT(*) n FROM sx_app_cast_job WHERE user_id=? AND status IN ('queued','resolving','running')", uid)
	if err != nil {
		return nil, err
	}
	if gconv.Int(pending["n"]) >= 2 {
		clientReply(r, nil, appError(429, "已有两项投屏媒体准备任务，请等待或取消"))
		return &AppCastPrepareRes{}, nil
	}
	castPreparation.Lock()
	queued := len(castPreparation.cancels)
	castPreparation.Unlock()
	if queued >= 24 {
		clientReply(r, nil, appError(503, "投屏准备队列已满，请稍后重试"))
		return &AppCastPrepareRes{}, nil
	}
	id, err := appToken()
	if err != nil {
		return nil, err
	}
	expires := time.Now().Add(2 * time.Hour).Unix()
	if err = execSQL(ctx, "INSERT INTO sx_app_cast_job(id,user_id,device_id,vod_id,status,created,expires) VALUES(?,?,?,?,'queued',?,?)", id, uid, p.DeviceID, req.VodID, time.Now().Unix(), expires); err != nil {
		return nil, err
	}
	jobCtx, cancel := context.WithTimeout(AppWithPrincipal(context.Background(), p), 30*time.Minute)
	castPreparation.Lock()
	castPreparation.cancels[id] = cancel
	castPreparation.Unlock()
	input := req.AppPlaybackRequest
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	host := r.Host
	go prepareCastMP4(jobCtx, cancel, id, token, host, input)
	clientReply(r, row{"job_id": id, "status": "queued", "progress": 0, "expires_at": expires}, nil)
	return &AppCastPrepareRes{}, nil
}
func prepareCastMP4(ctx context.Context, cancel context.CancelFunc, id, token, host string, input AppPlaybackRequest) {
	defer cancel()
	defer func() { castPreparation.Lock(); delete(castPreparation.cancels, id); castPreparation.Unlock() }()
	fail := func(message string) {
		status := "failed"
		if ctx.Err() != nil {
			status = "cancelled"
			message = "任务已取消或超时"
		}
		_ = execSQL(context.Background(), "UPDATE sx_app_cast_job SET status=?,error=? WHERE id=? AND status<>'cancelled'", status, cutRunes(message, 180), id)
		_ = os.Remove(castMP4Path(id))
		_ = os.Remove(castMP4Path(id) + ".part")
	}
	if err := os.MkdirAll(castMediaDirectory(), 0700); err != nil {
		fail("无法创建媒体临时目录")
		return
	}
	expired, _ := all(ctx, "SELECT id FROM sx_app_cast_job WHERE expires<?", time.Now().Unix())
	for _, item := range expired {
		_ = os.Remove(castMP4Path(gconv.String(item["id"])))
		_ = os.Remove(castMP4Path(gconv.String(item["id"])) + ".part")
		_ = os.Remove(castMP4Path(gconv.String(item["id"])) + ".cenc")
		_ = execSQL(ctx, "DELETE FROM sx_app_cast_job WHERE id=?", item["id"])
	}
	var used int64
	entries, _ := os.ReadDir(castMediaDirectory())
	for _, entry := range entries {
		if info, e := entry.Info(); e == nil {
			used += info.Size()
		}
	}
	if used > 4<<30 {
		fail("投屏媒体缓存空间不足，请稍后重试")
		return
	}
	gateway, err := newJellyfinGateway()
	if err != nil {
		fail("无法建立安全媒体连接")
		return
	}
	_ = execSQL(ctx, "UPDATE sx_app_cast_job SET status='resolving' WHERE id=?", id)
	request, _ := http.NewRequestWithContext(ctx, "POST", "https://"+host+"/", nil)
	request.Host = host
	var descriptor AppPlaybackDescriptor
	input.PositionMS = 0 // Prepare the complete film; the receiver seeks itself.
	if err = gateway.backendJSON(request, token, "/suxinvideo/app/v1/playback/resolve", input, &descriptor); err != nil {
		fail(err.Error())
		return
	}
	binary, err := ffmpegBinary()
	if err != nil {
		fail("服务器未安装 FFmpeg")
		return
	}
	source := descriptor.URL
	parsed, err := url.Parse(source)
	if err != nil {
		fail("媒体地址无效")
		return
	}
	if parsed.Host == "" {
		parsed.Scheme = "https"
		parsed.Host = "127.0.0.1:8600"
	}
	if gateway.localMediaHost(parsed.Host, host) {
		parsed.Host = "127.0.0.1:8600"
	}
	source = parsed.String()
	reservationBytes := int64(1 << 30)
	if parsed.Path == "/suxinvideo/native/hls" || parsed.Path == "/suxinvideo/native/stream" {
		reservationBytes = 2 << 30
	}
	releaseCache, err := reserveCastCache(reservationBytes)
	if err != nil {
		fail(err.Error())
		return
	}
	defer releaseCache()
	inputArgs := castFFmpegInputArgs(source, gateway.backend)
	if parsed.Path == "/suxinvideo/native/hls" || parsed.Path == "/suxinvideo/native/stream" {
		session, err := getNativeSession(ctx, parsed.Query().Get("token"))
		if err != nil || len(session.Media.Key) != 16 {
			fail("加密媒体会话已过期，请重新准备")
			return
		}
		// Download through Go's verified TLS and SSRF-aware transport, then
		// decrypt once locally. Two MP4 encoders must not each occupy a slot
		// while waiting for another encoder behind the HLS proxy.
		encrypted := castMP4Path(id) + ".cenc"
		defer os.Remove(encrypted)
		if err = downloadCastEncrypted(ctx, id, session, encrypted); err != nil {
			fail(err.Error())
			return
		}
		inputArgs = []string{"-decryption_key", hex.EncodeToString(session.Media.Key), "-i", encrypted}
		if descriptor.DurationMS <= 0 {
			descriptor.DurationMS = session.Media.Duration.Milliseconds()
		}
	}
	release, err := mediastream.Acquire(ctx, true)
	if err != nil {
		fail(err.Error())
		return
	}
	defer release()
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y"}
	args = append(args, inputArgs...)
	args = append(args, "-map", "0:v:0", "-map", "0:a:0?", "-c:v", "libx264", "-preset", "veryfast", "-crf", "23", "-pix_fmt", "yuv420p", "-c:a", "aac", "-b:a", "128k", "-movflags", "+faststart", "-fs", "1073741824", "-progress", "pipe:1", "-nostats", "-f", "mp4", castMP4Path(id)+".part")
	command := exec.CommandContext(ctx, binary, args...)
	castProcessHidden(command)
	stdout, err := command.StdoutPipe()
	if err != nil {
		fail("无法启动媒体转换")
		return
	}
	command.Stderr = io.Discard
	if err = command.Start(); err != nil {
		fail("无法启动媒体转换")
		return
	}
	_ = execSQL(ctx, "UPDATE sx_app_cast_job SET status='running',duration_ms=? WHERE id=?", descriptor.DurationMS, id)
	scanner := bufio.NewScanner(stdout)
	lastUpdate := time.Time{}
	lastPosition := int64(0)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "out_time_us=") {
			continue
		}
		microseconds, _ := strconv.ParseInt(strings.TrimPrefix(line, "out_time_us="), 10, 64)
		lastPosition = microseconds / 1000
		if time.Since(lastUpdate) < time.Second {
			continue
		}
		lastUpdate = time.Now()
		progress := 0
		if descriptor.DurationMS > 0 {
			progress = int(min(int64(99), lastPosition*100/descriptor.DurationMS))
		}
		_ = execSQL(ctx, "UPDATE sx_app_cast_job SET progress=?,position_ms=? WHERE id=?", progress, lastPosition, id)
	}
	if err = command.Wait(); err != nil {
		fail("媒体转换失败，请选择其他线路")
		return
	}
	if descriptor.DurationMS > 0 && lastPosition+3000 < descriptor.DurationMS {
		fail("媒体转换未完成或超过缓存大小限制")
		return
	}
	if ctx.Err() != nil {
		fail("任务已取消")
		return
	}
	if err = os.Rename(castMP4Path(id)+".part", castMP4Path(id)); err != nil {
		fail("无法保存投屏媒体")
		return
	}
	info, err := os.Stat(castMP4Path(id))
	if err != nil || info.Size() < 1024 || info.Size() >= (1<<30)-(1<<20) {
		fail("媒体转换结果为空")
		return
	}
	if err = execSQL(ctx, "UPDATE sx_app_cast_job SET status='completed',progress=100,position_ms=?,size=?,error='' WHERE id=? AND status='running'", lastPosition, info.Size(), id); err != nil {
		fail("无法保存转换状态")
	}
}

// Keep media traffic on loopback while verifying the site's certificate name.
// Public certificates contain the DNS name rather than the loopback IP.
func castFFmpegInputArgs(source, backend string) []string {
	args := []string{"-tls_verify", "1", "-ca_file", "manifest/cert/fullchain.pem"}
	media, mediaErr := url.Parse(source)
	site, siteErr := url.Parse(backend)
	if mediaErr == nil && siteErr == nil && media.Scheme == "https" && media.Host == "127.0.0.1:8600" && site.Hostname() != "" {
		args = append(args, "-verifyhost", site.Hostname())
	}
	return append(args, "-i", source)
}

func downloadCastEncrypted(ctx context.Context, id string, session nativeSession, target string) error {
	if err := safeCollectorURL(ctx, session.Media.URL); err != nil {
		return appError(502, "加密媒体地址无效")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, session.Media.URL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", mediaUserAgent)
	for name, value := range session.Headers {
		request.Header.Set(name, value)
	}
	applyMediaReferer(request, session.Media.Referer)
	response, err := playbackHTTPClient.Do(request)
	if err != nil {
		return appError(502, "加密媒体下载失败")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return appError(502, "加密媒体暂时不可用")
	}
	if response.ContentLength > 1<<30 {
		return appError(413, "投屏媒体超过 1GB 缓存限制")
	}
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	data := make([]byte, 128<<10)
	var total int64
	for {
		n, readErr := response.Body.Read(data)
		if n > 0 {
			total += int64(n)
			if total > 1<<30 {
				return appError(413, "投屏媒体超过 1GB 缓存限制")
			}
			if _, err = file.Write(data[:n]); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return appError(502, "加密媒体下载中断")
		}
	}
	if total < 1024 {
		return appError(502, "加密媒体返回空内容")
	}
	return nil
}

type AppCastPrepareStatusReq struct {
	g.Meta `path:"/app/v1/cast/prepare/:id" method:"get" noValApi:"1"`
	ID     string `p:"id"`
}
type AppCastPrepareStatusRes struct{}

func (*AppClients) CastPrepareStatus(ctx context.Context, req *AppCastPrepareStatusReq) (*AppCastPrepareStatusRes, error) {
	r := g.RequestFromCtx(ctx)
	p, err := clientPrincipal(r)
	if err != nil {
		clientReply(r, nil, err)
		return &AppCastPrepareStatusRes{}, nil
	}
	item, err := one(ctx, "SELECT * FROM sx_app_cast_job WHERE id=? AND user_id=? AND device_id=? AND expires>?", req.ID, p.User["id"], p.DeviceID, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	if item == nil {
		clientReply(r, nil, appError(404, "投屏媒体任务不存在或已过期"))
		return &AppCastPrepareStatusRes{}, nil
	}
	data := row{"job_id": item["id"], "status": item["status"], "progress": gconv.Int(item["progress"]), "position_ms": gconv.Int64(item["position_ms"]), "duration_ms": gconv.Int64(item["duration_ms"]), "size": gconv.Int64(item["size"]), "error": item["error"], "expires_at": gconv.Int64(item["expires"])}
	if gconv.String(item["status"]) == "completed" {
		grant, err := AppCreateMediaGrant(r.Context(), gconv.Int64(item["vod_id"]), time.Unix(gconv.Int64(item["expires"]), 0))
		if err != nil {
			return nil, err
		}
		data["url"] = clientOrigin(r) + "/suxinvideo/app/v1/cast/file/" + req.ID + "?app_grant=" + url.QueryEscape(grant)
	}
	clientReply(r, data, nil)
	return &AppCastPrepareStatusRes{}, nil
}

type AppCastPrepareCancelReq struct {
	g.Meta `path:"/app/v1/cast/prepare/:id/cancel" method:"post" noValApi:"1"`
	ID     string `p:"id"`
}
type AppCastPrepareCancelRes struct{}

func (*AppClients) CastPrepareCancel(ctx context.Context, req *AppCastPrepareCancelReq) (*AppCastPrepareCancelRes, error) {
	r := g.RequestFromCtx(ctx)
	p, err := clientPrincipal(r)
	if err != nil {
		clientReply(r, nil, err)
		return &AppCastPrepareCancelRes{}, nil
	}
	item, err := one(ctx, "SELECT id FROM sx_app_cast_job WHERE id=? AND user_id=? AND device_id=?", req.ID, p.User["id"], p.DeviceID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		clientReply(r, nil, appError(404, "任务不存在"))
		return &AppCastPrepareCancelRes{}, nil
	}
	castPreparation.Lock()
	if cancel := castPreparation.cancels[req.ID]; cancel != nil {
		cancel()
	}
	castPreparation.Unlock()
	if err = execSQL(ctx, "UPDATE sx_app_cast_job SET status='cancelled',expires=? WHERE id=?", time.Now().Unix(), req.ID); err != nil {
		return nil, err
	}
	_ = os.Remove(castMP4Path(req.ID))
	clientReply(r, true, nil)
	return &AppCastPrepareCancelRes{}, nil
}

type AppCastFileReq struct {
	g.Meta `path:"/app/v1/cast/file/:id" method:"get,head" noValApi:"1"`
	ID     string `p:"id"`
}
type AppCastFileRes struct{}

func (*AppClients) CastFile(ctx context.Context, req *AppCastFileReq) (*AppCastFileRes, error) {
	r := g.RequestFromCtx(ctx)
	if err := AppAuthenticateRequest(r); err != nil {
		clientReply(r, nil, err)
		return &AppCastFileRes{}, nil
	}
	p, ok := AppPrincipalFromContext(r.Context())
	if !ok {
		clientReply(r, nil, appError(401, "请先登录"))
		return &AppCastFileRes{}, nil
	}
	item, err := one(ctx, "SELECT vod_id FROM sx_app_cast_job WHERE id=? AND user_id=? AND device_id=? AND status='completed' AND expires>?", req.ID, p.User["id"], p.DeviceID, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	if item == nil {
		clientReply(r, nil, appError(404, "投屏媒体不可用"))
		return &AppCastFileRes{}, nil
	}
	film, err := appVisibleFilm(r.Context(), gconv.Int64(item["vod_id"]))
	if err == nil {
		_, err = AppAuthorizeFilm(r.Context(), film)
	}
	if err != nil {
		clientReply(r, nil, err)
		return &AppCastFileRes{}, nil
	}
	file, err := os.Open(castMP4Path(req.ID))
	if err != nil {
		clientReply(r, nil, appError(404, "投屏媒体已清理"))
		return &AppCastFileRes{}, nil
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	r.Response.Header().Set("Content-Type", "video/mp4")
	r.Response.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(r.Response.Writer, r.Request, req.ID+".mp4", info.ModTime(), file)
	r.ExitAll()
	return &AppCastFileRes{}, nil
}

func prepareCastJobRecovery(ctx context.Context) error {
	jobs, err := all(ctx, "SELECT id FROM sx_app_cast_job WHERE status IN ('queued','resolving','running')")
	if err != nil {
		return err
	}
	for _, job := range jobs {
		_ = os.Remove(castMP4Path(gconv.String(job["id"])) + ".part")
		_ = os.Remove(castMP4Path(gconv.String(job["id"])) + ".cenc")
	}
	return execSQL(ctx, "UPDATE sx_app_cast_job SET status='failed',error='服务器重启，请重新准备' WHERE status IN ('queued','resolving','running')")
}
