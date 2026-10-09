package suxinvideo

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"
)

type AppClients struct{}

var clientOperationLimits = struct {
	sync.Mutex
	entries map[string]struct {
		limiter *rate.Limiter
		updated time.Time
	}
}{entries: make(map[string]struct {
	limiter *rate.Limiter
	updated time.Time
})}

func allowClientOperation(key string, perMinute, burst int) bool {
	clientOperationLimits.Lock()
	defer clientOperationLimits.Unlock()
	now := time.Now()
	if len(clientOperationLimits.entries) > 4096 {
		for key, entry := range clientOperationLimits.entries {
			if now.Sub(entry.updated) > 10*time.Minute {
				delete(clientOperationLimits.entries, key)
			}
		}
		if len(clientOperationLimits.entries) > 8192 {
			return false
		}
	}
	entry, exists := clientOperationLimits.entries[key]
	if !exists {
		entry.limiter = rate.NewLimiter(rate.Limit(float64(perMinute)/60), burst)
	}
	entry.updated = now
	clientOperationLimits.entries[key] = entry
	return entry.limiter.Allow()
}

const appPairingSchema = `CREATE TABLE IF NOT EXISTS sx_app_pairing (
 id CHAR(48) PRIMARY KEY,code CHAR(6) NOT NULL,poll_hash CHAR(64) NOT NULL,
 device_id VARCHAR(120) NOT NULL,name VARCHAR(120) NOT NULL,user_id INT UNSIGNED NOT NULL DEFAULT 0,
 status VARCHAR(20) NOT NULL DEFAULT 'pending',expires BIGINT NOT NULL,created BIGINT NOT NULL,
 UNIQUE KEY pair_code(code),KEY expires(expires)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`
const appPairedDeviceSchema = `CREATE TABLE IF NOT EXISTS sx_app_paired_device (
 user_id INT UNSIGNED NOT NULL,device_id VARCHAR(120) NOT NULL,name VARCHAR(120) NOT NULL,
 last_seen BIGINT NOT NULL,revoked TINYINT NOT NULL DEFAULT 0,PRIMARY KEY(user_id,device_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`
const appCastSchema = `CREATE TABLE IF NOT EXISTS sx_app_cast_session (
 id CHAR(48) PRIMARY KEY,user_id INT UNSIGNED NOT NULL,mobile_device VARCHAR(120) NOT NULL,
 tv_device VARCHAR(120) NOT NULL,expires BIGINT NOT NULL,created BIGINT NOT NULL,
 KEY tv(user_id,tv_device,expires)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

func PrepareClientIntegrations(ctx context.Context) error {
	for _, query := range []string{appPairingSchema, appPairedDeviceSchema, appCastSchema, appReleaseSchema, appCastJobSchema} {
		if err := execSQL(ctx, query); err != nil {
			return err
		}
	}
	if err := prepareCastJobRecovery(ctx); err != nil {
		return err
	}
	return upgradeClientPermissions(ctx)
}

func clientReply(r *ghttp.Request, data any, err error) {
	if err != nil {
		status := 500
		message := "服务器暂时不可用"
		if e, ok := err.(*AppError); ok {
			status, message = e.Status, e.Message
		}
		r.Response.WriteHeader(status)
		r.Response.WriteJson(map[string]any{"code": status, "message": message, "data": nil})
		return
	}
	r.Response.WriteJson(map[string]any{"code": 0, "message": "", "data": data})
}
func clientPrincipal(r *ghttp.Request) (AppPrincipal, error) {
	if err := AppAuthenticateRequest(r); err != nil {
		return AppPrincipal{}, err
	}
	p, ok := AppPrincipalFromContext(r.Context())
	if !ok || p.GrantVodID != 0 {
		return AppPrincipal{}, appError(401, "请先登录")
	}
	return p, nil
}
func clientOrigin(r *ghttp.Request) string {
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	return scheme + "://" + r.Host
}

type AppPairCreateReq struct {
	g.Meta   `path:"/app/v1/devices/pair/create" method:"post" noValApi:"1"`
	DeviceID string `json:"device_id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
}
type AppPairCreateRes struct{}

func (*AppClients) PairCreate(ctx context.Context, req *AppPairCreateReq) (*AppPairCreateRes, error) {
	r := g.RequestFromCtx(ctx)
	if len(req.DeviceID) < 8 || len(req.DeviceID) > 120 || req.Platform != "tv" {
		clientReply(r, nil, appError(400, "设备信息无效"))
		return &AppPairCreateRes{}, nil
	}
	if !allowClientOperation("create:"+cmsClientIP(ctx, r), 4, 2) {
		clientReply(r, nil, appError(429, "申请过于频繁"))
		return &AppPairCreateRes{}, nil
	}
	id, err := appToken()
	if err != nil {
		return nil, err
	}
	poll, err := appToken()
	if err != nil {
		return nil, err
	}
	expire := time.Now().Add(5 * time.Minute).Unix()
	// The code and polling credential have different roles. Only the TV holding
	// poll_token can receive the approved member session.
	var code string
	for n := 0; n < 8; n++ {
		b := make([]byte, 4)
		if _, err = rand.Read(b); err != nil {
			return nil, err
		}
		value := (uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])) % 1000000
		code = fmt.Sprintf("%06d", value)
		_ = execSQL(ctx, "DELETE FROM sx_app_pairing WHERE expires<?", time.Now().Unix())
		err = execSQL(ctx, "INSERT INTO sx_app_pairing(id,code,poll_hash,device_id,name,expires,created) VALUES(?,?,?,?,?,?,?)", id, code, appHash(poll), req.DeviceID, cutRunes(req.Name, 120), expire, time.Now().Unix())
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	clientReply(r, row{"pair_id": id, "code": code, "poll_token": poll, "expires_at": expire, "qr_url": clientOrigin(r) + "/suxinvideo/app-download?pair=" + code}, nil)
	return &AppPairCreateRes{}, nil
}

type AppPairStatusReq struct {
	g.Meta    `path:"/app/v1/devices/pair/status" method:"get" noValApi:"1"`
	PairID    string `p:"pair_id"`
	PollToken string `p:"poll_token"`
}
type AppPairStatusRes struct{}

var deliveredPairTokens = struct {
	sync.Mutex
	items map[string]struct {
		tokens AppTokens
		expire int64
	}
}{items: map[string]struct {
	tokens AppTokens
	expire int64
}{}}

func (*AppClients) PairStatus(ctx context.Context, req *AppPairStatusReq) (*AppPairStatusRes, error) {
	r := g.RequestFromCtx(ctx)
	if len(req.PairID) != 48 || len(req.PollToken) != 48 {
		clientReply(r, nil, appError(400, "配对请求无效"))
		return &AppPairStatusRes{}, nil
	}
	if !allowClientOperation("poll:"+req.PairID, 60, 5) {
		clientReply(r, nil, appError(429, "轮询过于频繁"))
		return &AppPairStatusRes{}, nil
	}
	item, err := one(ctx, "SELECT * FROM sx_app_pairing WHERE id=? AND poll_hash=?", req.PairID, appHash(req.PollToken))
	if err != nil {
		return nil, err
	}
	if item == nil || gconv.Int64(item["expires"]) <= time.Now().Unix() {
		clientReply(r, row{"status": "expired"}, nil)
		return &AppPairStatusRes{}, nil
	}
	status := gconv.String(item["status"])
	if status == "pending" {
		clientReply(r, row{"status": status}, nil)
		return &AppPairStatusRes{}, nil
	}
	deliveredPairTokens.Lock()
	defer deliveredPairTokens.Unlock()
	for key, value := range deliveredPairTokens.items {
		if value.expire < time.Now().Unix() {
			delete(deliveredPairTokens.items, key)
		}
	}
	if value, ok := deliveredPairTokens.items[req.PairID]; ok {
		clientReply(r, row{"status": "approved", "tokens": value.tokens}, nil)
		return &AppPairStatusRes{}, nil
	}
	// Atomically claim issuance. Restarting after a consumed response requires a
	// fresh pairing rather than minting unbounded sessions for a leaked poll URL.
	result, err := g.DB().Exec(ctx, "UPDATE sx_app_pairing SET status='consumed' WHERE id=? AND status='approved'", req.PairID)
	if err != nil {
		return nil, err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		clientReply(r, row{"status": "expired"}, nil)
		return &AppPairStatusRes{}, nil
	}
	tokens, err := AppIssueDeviceSession(ctx, gconv.Int64(item["user_id"]), gconv.String(item["device_id"]), gconv.String(item["name"]))
	if err != nil {
		return nil, err
	}
	deliveredPairTokens.items[req.PairID] = struct {
		tokens AppTokens
		expire int64
	}{tokens, gconv.Int64(item["expires"])}
	clientReply(r, row{"status": "approved", "tokens": tokens}, nil)
	return &AppPairStatusRes{}, nil
}

type AppPairApproveReq struct {
	g.Meta `path:"/app/v1/devices/pair/approve" method:"post" noValApi:"1"`
	Code   string `json:"code"`
}
type AppPairApproveRes struct{}

func (*AppClients) PairApprove(ctx context.Context, req *AppPairApproveReq) (*AppPairApproveRes, error) {
	r := g.RequestFromCtx(ctx)
	p, err := clientPrincipal(r)
	if err != nil {
		clientReply(r, nil, err)
		return &AppPairApproveRes{}, nil
	}
	if len(req.Code) != 6 {
		clientReply(r, nil, appError(400, "请输入六位配对码"))
		return &AppPairApproveRes{}, nil
	}
	if !allowClientOperation("approve:"+gconv.String(p.User["id"]), 10, 3) {
		clientReply(r, nil, appError(429, "配对尝试过于频繁"))
		return &AppPairApproveRes{}, nil
	}
	item, err := one(ctx, "SELECT * FROM sx_app_pairing WHERE code=? AND status='pending' AND expires>?", req.Code, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	if item == nil {
		clientReply(r, nil, appError(404, "配对码无效或已过期"))
		return &AppPairApproveRes{}, nil
	}
	uid := gconv.Int64(p.User["id"])
	result, err := g.DB().Exec(ctx, "UPDATE sx_app_pairing SET user_id=?,status='approved' WHERE id=? AND status='pending'", uid, item["id"])
	if err != nil {
		return nil, err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		clientReply(r, nil, appError(409, "配对码已被使用"))
		return &AppPairApproveRes{}, nil
	}
	if err = execSQL(ctx, "INSERT INTO sx_app_paired_device(user_id,device_id,name,last_seen,revoked) VALUES(?,?,?,?,0) ON DUPLICATE KEY UPDATE name=VALUES(name),last_seen=VALUES(last_seen),revoked=0", uid, item["device_id"], item["name"], time.Now().Unix()); err != nil {
		return nil, err
	}
	clientReply(r, row{"pair_id": item["id"], "device_id": item["device_id"], "name": item["name"]}, nil)
	return &AppPairApproveRes{}, nil
}

type AppDevicesReq struct {
	g.Meta `path:"/app/v1/devices" method:"get" noValApi:"1"`
}
type AppDevicesRes struct{}

func (*AppClients) Devices(ctx context.Context, _ *AppDevicesReq) (*AppDevicesRes, error) {
	r := g.RequestFromCtx(ctx)
	p, err := clientPrincipal(r)
	if err != nil {
		clientReply(r, nil, err)
		return &AppDevicesRes{}, nil
	}
	items, err := all(ctx, "SELECT device_id,name,last_seen,'tv' platform FROM sx_app_paired_device WHERE user_id=? AND revoked=0 ORDER BY last_seen DESC", p.User["id"])
	clientReply(r, row{"devices": items}, err)
	return &AppDevicesRes{}, nil
}

type AppPairRevokeReq struct {
	g.Meta   `path:"/app/v1/devices/pair/revoke" method:"post" noValApi:"1"`
	DeviceID string `json:"device_id"`
}
type AppPairRevokeRes struct{}

func (*AppClients) PairRevoke(ctx context.Context, req *AppPairRevokeReq) (*AppPairRevokeRes, error) {
	r := g.RequestFromCtx(ctx)
	p, err := clientPrincipal(r)
	if err != nil {
		clientReply(r, nil, err)
		return &AppPairRevokeRes{}, nil
	}
	uid := gconv.Int64(p.User["id"])
	for _, query := range []string{"UPDATE sx_app_paired_device SET revoked=1 WHERE user_id=? AND device_id=?", "UPDATE sx_app_session SET revoked=1 WHERE user_id=? AND device_id=?", "UPDATE sx_app_pairing SET status='revoked',expires=0 WHERE user_id=? AND device_id=?", "DELETE FROM sx_app_cast_session WHERE user_id=? AND tv_device=?"} {
		if err = execSQL(ctx, query, uid, req.DeviceID); err != nil {
			return nil, err
		}
	}
	castHub.disconnectDevice(uid, req.DeviceID)
	clientReply(r, true, nil)
	return &AppPairRevokeRes{}, nil
}

type AppCastCreateReq struct {
	g.Meta   `path:"/app/v1/devices/cast/create" method:"post" noValApi:"1"`
	DeviceID string `json:"device_id"`
}
type AppCastCreateRes struct{}

func (*AppClients) CastCreate(ctx context.Context, req *AppCastCreateReq) (*AppCastCreateRes, error) {
	r := g.RequestFromCtx(ctx)
	p, err := clientPrincipal(r)
	if err != nil {
		clientReply(r, nil, err)
		return &AppCastCreateRes{}, nil
	}
	device, err := one(ctx, "SELECT device_id FROM sx_app_paired_device WHERE user_id=? AND device_id=? AND revoked=0", p.User["id"], req.DeviceID)
	if err != nil {
		return nil, err
	}
	if device == nil || req.DeviceID == p.DeviceID {
		clientReply(r, nil, appError(404, "目标电视未配对"))
		return &AppCastCreateRes{}, nil
	}
	existing, err := one(ctx, "SELECT id,mobile_device,expires FROM sx_app_cast_session WHERE user_id=? AND tv_device=? AND expires>? ORDER BY created DESC LIMIT 1", p.User["id"], req.DeviceID, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	if existing != nil && gconv.String(existing["mobile_device"]) == p.DeviceID {
		expires := time.Now().Add(2 * time.Hour).Unix()
		if err = execSQL(ctx, "UPDATE sx_app_cast_session SET expires=? WHERE id=?", expires, existing["id"]); err != nil {
			return nil, err
		}
		clientReply(r, castDescription(r, gconv.String(existing["id"]), expires), nil)
		return &AppCastCreateRes{}, nil
	}
	if existing != nil {
		castHub.disconnectSession(gconv.String(existing["id"]))
	}
	id, err := appToken()
	if err != nil {
		return nil, err
	}
	expire := time.Now().Add(2 * time.Hour).Unix()
	_ = execSQL(ctx, "DELETE FROM sx_app_cast_session WHERE user_id=? AND tv_device=?", p.User["id"], req.DeviceID)
	if err = execSQL(ctx, "INSERT INTO sx_app_cast_session(id,user_id,mobile_device,tv_device,expires,created) VALUES(?,?,?,?,?,?)", id, p.User["id"], p.DeviceID, req.DeviceID, expire, time.Now().Unix()); err != nil {
		return nil, err
	}
	clientReply(r, castDescription(r, id, expire), nil)
	return &AppCastCreateRes{}, nil
}

type AppCastCurrentReq struct {
	g.Meta `path:"/app/v1/devices/cast/current" method:"get" noValApi:"1"`
}
type AppCastCurrentRes struct{}

func (*AppClients) CastCurrent(ctx context.Context, _ *AppCastCurrentReq) (*AppCastCurrentRes, error) {
	r := g.RequestFromCtx(ctx)
	p, err := clientPrincipal(r)
	if err != nil {
		clientReply(r, nil, err)
		return &AppCastCurrentRes{}, nil
	}
	item, err := one(ctx, "SELECT id,expires FROM sx_app_cast_session WHERE user_id=? AND tv_device=? AND expires>? ORDER BY created DESC LIMIT 1", p.User["id"], p.DeviceID, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	if item == nil {
		clientReply(r, row{"active": false}, nil)
	} else {
		value := castDescription(r, gconv.String(item["id"]), gconv.Int64(item["expires"]))
		value["active"] = true
		clientReply(r, value, nil)
	}
	return &AppCastCurrentRes{}, nil
}
func castDescription(r *ghttp.Request, id string, expires int64) row {
	origin := clientOrigin(r)
	origin = strings.Replace(strings.Replace(origin, "https://", "wss://", 1), "http://", "ws://", 1)
	return row{"session_id": id, "expires_at": expires, "ws_url": origin + "/suxinvideo/app/v1/devices/cast/ws?session_id=" + url.QueryEscape(id)}
}

type AppCastWSReq struct {
	g.Meta      `path:"/app/v1/devices/cast/ws" method:"get" noValApi:"1"`
	SessionID   string `p:"session_id"`
	AccessToken string `p:"access_token"`
}
type AppCastWSRes struct{}
type castPeer struct {
	conn   *websocket.Conn
	send   chan []byte
	uid    int64
	device string
	tv     bool
}
type appCastHub struct {
	sync.Mutex
	rooms    map[string]map[*castPeer]bool
	commands map[string][][]byte
}

var castHub = appCastHub{rooms: map[string]map[*castPeer]bool{}, commands: map[string][][]byte{}}

func (h *appCastHub) disconnectDevice(uid int64, device string) {
	h.Lock()
	defer h.Unlock()
	for _, room := range h.rooms {
		for peer := range room {
			if peer.uid == uid && peer.device == device {
				_ = peer.conn.Close()
			}
		}
	}
}
func (h *appCastHub) disconnectSession(id string) {
	h.Lock()
	defer h.Unlock()
	for peer := range h.rooms[id] {
		_ = peer.conn.Close()
	}
	delete(h.commands, id)
}
func validCastCommand(action string) bool {
	switch action {
	case "play", "resume", "pause", "seek", "next", "quality", "stop":
		return true
	}
	return false
}
func (*AppClients) CastWS(ctx context.Context, req *AppCastWSReq) (*AppCastWSRes, error) {
	r := g.RequestFromCtx(ctx)
	if r.Header.Get("Authorization") == "" && req.AccessToken != "" {
		r.Header.Set("Authorization", "Bearer "+req.AccessToken)
	}
	p, err := clientPrincipal(r)
	if err != nil {
		clientReply(r, nil, err)
		return &AppCastWSRes{}, nil
	}
	session, err := one(ctx, "SELECT * FROM sx_app_cast_session WHERE id=? AND user_id=? AND expires>?", req.SessionID, p.User["id"], time.Now().Unix())
	if err != nil {
		return nil, err
	}
	if session == nil || (p.DeviceID != gconv.String(session["mobile_device"]) && p.DeviceID != gconv.String(session["tv_device"])) {
		clientReply(r, nil, appError(403, "无权连接此投屏会话"))
		return &AppCastWSRes{}, nil
	}
	upgrade := websocket.Upgrader{CheckOrigin: func(req *http.Request) bool {
		origin := req.Header.Get("Origin")
		if origin == "" {
			return true
		}
		u, e := url.Parse(origin)
		return e == nil && strings.EqualFold(u.Host, req.Host)
	}}
	conn, err := upgrade.Upgrade(r.Response.Writer, r.Request, nil)
	if err != nil {
		return &AppCastWSRes{}, nil
	}
	defer conn.Close()
	peer := &castPeer{conn: conn, send: make(chan []byte, 32), uid: gconv.Int64(p.User["id"]), device: p.DeviceID, tv: p.DeviceID == gconv.String(session["tv_device"])}
	castHub.Lock()
	if castHub.rooms[req.SessionID] == nil {
		castHub.rooms[req.SessionID] = map[*castPeer]bool{}
	}
	for old := range castHub.rooms[req.SessionID] {
		if old.device == peer.device {
			_ = old.conn.Close()
		}
	}
	castHub.rooms[req.SessionID][peer] = true
	if peer.tv {
		for _, command := range castHub.commands[req.SessionID] {
			peer.send <- command
		}
		delete(castHub.commands, req.SessionID)
	}
	castHub.Unlock()
	defer func() {
		castHub.Lock()
		delete(castHub.rooms[req.SessionID], peer)
		if len(castHub.rooms[req.SessionID]) == 0 {
			delete(castHub.rooms, req.SessionID)
			delete(castHub.commands, req.SessionID)
		}
		castHub.Unlock()
	}()
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case message := <-peer.send:
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
					return
				}
			case <-ticker.C:
				// A revoked user/device or expired session ends an established websocket.
				checkCtx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
				live, e := one(checkCtx, "SELECT s.id FROM sx_app_session s JOIN sx_user u ON u.id=s.user_id JOIN sx_app_cast_session c ON c.user_id=s.user_id WHERE s.id=? AND s.revoked=0 AND s.access_expire>? AND u.status=1 AND c.id=? AND c.expires>?", p.SessionID, time.Now().Unix(), req.SessionID, time.Now().Unix())
				cancel()
				if e != nil || live == nil {
					conn.Close()
					return
				}
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
					return
				}
			case <-done:
				return
			}
		}
	}()
	conn.SetReadLimit(16 << 10)
	_ = conn.SetReadDeadline(time.Now().Add(70 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(70 * time.Second)) })
	for {
		_, data, e := conn.ReadMessage()
		if e != nil {
			break
		}
		var message struct {
			Type    string          `json:"type"`
			ID      string          `json:"id"`
			Action  string          `json:"action"`
			Payload json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(data, &message) != nil {
			continue
		}
		if peer.tv {
			if message.Type != "status" && message.Type != "ack" {
				continue
			}
			_ = execSQL(context.Background(), "UPDATE sx_app_paired_device SET last_seen=? WHERE user_id=? AND device_id=?", time.Now().Unix(), p.User["id"], p.DeviceID)
		} else {
			if message.Type != "command" || !validCastCommand(message.Action) {
				continue
			}
		}
		castHub.Lock()
		delivered := false
		for other := range castHub.rooms[req.SessionID] {
			if other.tv != peer.tv {
				select {
				case other.send <- data:
					delivered = true
				default:
					_ = other.conn.Close()
				}
			}
		}
		if !peer.tv && !delivered {
			queue := castHub.commands[req.SessionID]
			if len(queue) >= 16 {
				queue = queue[1:]
			}
			castHub.commands[req.SessionID] = append(queue, append([]byte(nil), data...))
		}
		castHub.Unlock()
	}
	r.ExitAll()
	return &AppCastWSRes{}, nil
}

// No credential is included in the QR image: it only identifies a short-lived
// approval code. Authentication and approval are separate POST requests.
func appPairCode(value string) bool {
	if len(value) != 6 {
		return false
	}
	_, err := strconv.Atoi(value)
	return err == nil && !strings.ContainsAny(value, "+-")
}
