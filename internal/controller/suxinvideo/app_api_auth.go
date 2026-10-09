package suxinvideo

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// App auth is independent of the administrative JWT and browser session.
const appSessionTable = `CREATE TABLE IF NOT EXISTS sx_app_session (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,user_id INT UNSIGNED NOT NULL,
 device_id VARCHAR(120) NOT NULL,device_name VARCHAR(120) NOT NULL DEFAULT '',
 access_hash CHAR(64) NOT NULL,refresh_hash CHAR(64) NOT NULL,
 access_expire BIGINT NOT NULL,refresh_expire BIGINT NOT NULL,revoked TINYINT NOT NULL DEFAULT 0,
 created BIGINT NOT NULL,updated BIGINT NOT NULL,UNIQUE KEY access_hash(access_hash),
 UNIQUE KEY refresh_hash(refresh_hash),KEY user_device(user_id,device_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

const appCaptchaTable = `CREATE TABLE IF NOT EXISTS sx_app_captcha (
 challenge CHAR(48) NOT NULL PRIMARY KEY,answer_hash CHAR(64) NOT NULL,expire BIGINT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

const appLicenseTable = `CREATE TABLE IF NOT EXISTS sx_app_license (
 id CHAR(48) NOT NULL PRIMARY KEY,user_id INT UNSIGNED NOT NULL,session_id BIGINT UNSIGNED NOT NULL,
 device_id VARCHAR(120) NOT NULL,vod_id INT UNSIGNED NOT NULL,line VARCHAR(100) NOT NULL,
 episode_key VARCHAR(200) NOT NULL,version_key VARCHAR(100) NOT NULL,quality VARCHAR(120) NOT NULL DEFAULT '',revision CHAR(64) NOT NULL,
 expire BIGINT NOT NULL,revoked TINYINT NOT NULL DEFAULT 0,created BIGINT NOT NULL,
 KEY device(device_id,user_id),KEY vod(vod_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

const appCheckoutTable = `CREATE TABLE IF NOT EXISTS sx_app_checkout (
 token_hash CHAR(64) NOT NULL PRIMARY KEY,user_id INT UNSIGNED NOT NULL,session_id BIGINT UNSIGNED NOT NULL,
 order_no VARCHAR(30) NOT NULL,payload TEXT NOT NULL,expire BIGINT NOT NULL,
 KEY order_no(order_no)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

var appSchemaMutex sync.Mutex
var appSchemaReady bool

func EnsureAppSchema(ctx context.Context) error {
	appSchemaMutex.Lock()
	defer appSchemaMutex.Unlock()
	if appSchemaReady {
		return nil
	}
	for _, query := range []string{appSessionTable, appCaptchaTable, appLicenseTable, appCheckoutTable} {
		if err := execSQL(ctx, query); err != nil {
			return err
		}
	}
	for column, definition := range map[string]string{
		"source_code": "VARCHAR(100) NOT NULL DEFAULT ''", "episode_key": "VARCHAR(200) NOT NULL DEFAULT ''",
		"version_key": "VARCHAR(100) NOT NULL DEFAULT ''", "duration_ms": "BIGINT NOT NULL DEFAULT 0",
	} {
		item, err := one(ctx, "SELECT COUNT(*) n FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='sx_play_record' AND column_name=?", column)
		if err != nil {
			return err
		}
		if gconv.Int(item["n"]) == 0 {
			if err = execSQL(ctx, "ALTER TABLE sx_play_record ADD COLUMN "+column+" "+definition); err != nil {
				return err
			}
		}
	}
	quality, err := one(ctx, "SELECT COUNT(*) n FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='sx_app_license' AND column_name='quality'")
	if err != nil {
		return err
	}
	if gconv.Int(quality["n"]) == 0 {
		if err = execSQL(ctx, "ALTER TABLE sx_app_license ADD COLUMN quality VARCHAR(120) NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	appSchemaReady = true
	return nil
}

type appPrincipalKey struct{}
type AppPrincipal struct {
	User       row
	SessionID  int64
	DeviceID   string
	GrantVodID int64
}

type AppTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
	User         row    `json:"user"`
	DeviceID     string `json:"device_id"`
}

type AppError struct {
	Status  int
	Code    int
	Message string
}

func (e *AppError) Error() string { return e.Message }
func appError(status int, message string) error {
	return &AppError{Status: status, Code: status, Message: message}
}

func appToken() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
func appHash(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}
func AppWithPrincipal(ctx context.Context, p AppPrincipal) context.Context {
	return context.WithValue(ctx, appPrincipalKey{}, p)
}
func AppPrincipalFromContext(ctx context.Context) (AppPrincipal, bool) {
	p, ok := ctx.Value(appPrincipalKey{}).(AppPrincipal)
	return p, ok
}
func AppMemberFromContext(ctx context.Context) (row, bool) {
	p, ok := AppPrincipalFromContext(ctx)
	return p.User, ok
}

// AppMember validates the credential on every request. Media grants also remain
// bound to a live device session, so logout immediately revokes streaming.
func AppMember(ctx context.Context) (row, error) {
	if p, ok := AppPrincipalFromContext(ctx); ok {
		return p.User, nil
	}
	r := g.RequestFromCtx(ctx)
	if r == nil {
		return nil, nil
	}
	p, err := appRequestPrincipal(ctx, r)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, nil
	}
	return p.User, nil
}

func AppAuthenticateRequest(r *ghttp.Request) error {
	p, err := appRequestPrincipal(r.Context(), r)
	if err != nil {
		return err
	}
	if p != nil {
		r.SetCtx(AppWithPrincipal(r.Context(), *p))
	}
	return nil
}

func appRequestPrincipal(ctx context.Context, r *ghttp.Request) (*AppPrincipal, error) {
	if p, ok := AppPrincipalFromContext(ctx); ok {
		return &p, nil
	}
	if setting(ctx, "member_enable", "1") != "1" {
		return nil, nil
	}
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header != "" {
		if len(header) < 8 || !strings.EqualFold(header[:7], "Bearer ") {
			return nil, appError(401, "登录凭据无效")
		}
		return appPrincipalByToken(ctx, strings.TrimSpace(header[7:]))
	}
	if grant := r.Get("app_grant").String(); grant != "" {
		return appPrincipalByGrant(ctx, grant)
	}
	return nil, nil
}

func appPrincipalByToken(ctx context.Context, token string) (*AppPrincipal, error) {
	if len(token) != 48 {
		return nil, appError(401, "登录凭据无效")
	}
	item, err := one(ctx, "SELECT s.id session_id,s.device_id,u.id,u.email,u.name,u.points,u.vip_expire,u.avatar FROM sx_app_session s JOIN sx_user u ON u.id=s.user_id WHERE s.access_hash=? AND s.revoked=0 AND s.access_expire>? AND u.status=1", appHash(token), time.Now().Unix())
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, appError(401, "登录已过期，请重新登录")
	}
	return appPrincipalRow(ctx, item, 0), nil
}
func AppUserForAccessToken(ctx context.Context, token string) (AppPrincipal, error) {
	p, err := appPrincipalByToken(ctx, token)
	if err != nil {
		return AppPrincipal{}, err
	}
	return *p, nil
}
func appPrincipalRow(ctx context.Context, item row, vodID int64) *AppPrincipal {
	p := &AppPrincipal{SessionID: gconv.Int64(item["session_id"]), DeviceID: gconv.String(item["device_id"]), GrantVodID: vodID, User: row{}}
	for _, k := range []string{"id", "email", "name", "points", "vip_expire", "avatar"} {
		p.User[k] = item[k]
	}
	applyDefaultUserAvatar(ctx, p.User)
	return p
}

type appMediaGrant struct {
	SessionID int64 `json:"s"`
	VodID     int64 `json:"v"`
	Expires   int64 `json:"e"`
}

func AppCreateMediaGrant(ctx context.Context, vodID int64, expires time.Time) (string, error) {
	p, ok := AppPrincipalFromContext(ctx)
	if !ok {
		return "", appError(401, "请先登录")
	}
	payload, _ := json.Marshal(appMediaGrant{p.SessionID, vodID, expires.Unix()})
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return encoded + "." + signProxy(ctx, "app-media:"+encoded, expires.Unix()), nil
}
func appPrincipalByGrant(ctx context.Context, value string) (*AppPrincipal, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 || len(value) > 1024 {
		return nil, appError(401, "媒体授权无效")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, appError(401, "媒体授权无效")
	}
	var grant appMediaGrant
	if json.Unmarshal(payload, &grant) != nil || grant.VodID < 1 || grant.SessionID < 1 || grant.Expires <= time.Now().Unix() || grant.Expires > time.Now().Add(25*time.Hour).Unix() {
		return nil, appError(401, "媒体授权已过期")
	}
	if !hmac.Equal([]byte(parts[1]), []byte(signProxy(ctx, "app-media:"+parts[0], grant.Expires))) {
		return nil, appError(401, "媒体授权无效")
	}
	item, err := one(ctx, "SELECT s.id session_id,s.device_id,u.id,u.email,u.name,u.points,u.vip_expire,u.avatar FROM sx_app_session s JOIN sx_user u ON u.id=s.user_id WHERE s.id=? AND s.revoked=0 AND s.refresh_expire>? AND u.status=1", grant.SessionID, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, appError(401, "设备授权已撤销")
	}
	return appPrincipalRow(ctx, item, grant.VodID), nil
}

// A media grant cannot be reused to unlock a different film.
func AppAuthorizeGrantVod(ctx context.Context, vodID int64) error {
	if p, ok := AppPrincipalFromContext(ctx); ok && p.GrantVodID > 0 && p.GrantVodID != vodID {
		return appError(403, "媒体授权与影片不匹配")
	}
	return nil
}

func AppIssueDeviceSession(ctx context.Context, userID int64, deviceID, deviceName string) (AppTokens, error) {
	if err := EnsureAppSchema(ctx); err != nil {
		return AppTokens{}, err
	}
	deviceID = strings.TrimSpace(deviceID)
	deviceName = strings.TrimSpace(deviceName)
	if deviceID == "" || len(deviceID) > 120 || len(deviceName) > 120 {
		return AppTokens{}, appError(400, "设备信息无效")
	}
	user, err := one(ctx, "SELECT id,email,name,points,vip_expire,avatar FROM sx_user WHERE id=? AND status=1", userID)
	if err != nil {
		return AppTokens{}, err
	}
	if user == nil {
		return AppTokens{}, appError(401, "会员账号不可用")
	}
	access, err := appToken()
	if err != nil {
		return AppTokens{}, err
	}
	refresh, err := appToken()
	if err != nil {
		return AppTokens{}, err
	}
	now := time.Now()
	expires := now.Add(time.Hour).Unix()
	err = g.DB().Transaction(ctx, func(_ context.Context, tx gdb.TX) error {
		if _, e := tx.Exec("UPDATE sx_app_session SET revoked=1,updated=? WHERE user_id=? AND device_id=?", now.Unix(), userID, deviceID); e != nil {
			return e
		}
		_, e := tx.Exec("INSERT INTO sx_app_session(user_id,device_id,device_name,access_hash,refresh_hash,access_expire,refresh_expire,created,updated) VALUES(?,?,?,?,?,?,?,?,?)", userID, deviceID, deviceName, appHash(access), appHash(refresh), expires, now.Add(30*24*time.Hour).Unix(), now.Unix(), now.Unix())
		return e
	})
	return AppTokens{access, refresh, expires, applyDefaultUserAvatar(ctx, user), deviceID}, err
}

func AppAuthenticatePassword(ctx context.Context, email, password, deviceID, name, ip string) (AppTokens, error) {
	if setting(ctx, "member_enable", "1") != "1" {
		return AppTokens{}, appError(403, "会员系统已关闭")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if _, err := mail.ParseAddress(email); err != nil {
		return AppTokens{}, appError(400, "邮箱格式不正确")
	}
	ban, err := one(ctx, "SELECT fails,ban_until FROM sx_login_fail WHERE type='user' AND account=? AND ip=?", email, ip)
	if err != nil {
		return AppTokens{}, err
	}
	if ban != nil && gconv.Int64(ban["ban_until"]) > time.Now().Unix() {
		return AppTokens{}, appError(429, "登录次数过多，请稍后再试")
	}
	user, err := one(ctx, "SELECT id,pwd,status FROM sx_user WHERE email=?", email)
	if err != nil {
		return AppTokens{}, err
	}
	valid := user != nil && gconv.Int(user["status"]) == 1 && bcrypt.CompareHashAndPassword([]byte(strings.Replace(gconv.String(user["pwd"]), "$2y$", "$2a$", 1)), []byte(password)) == nil
	if !valid {
		_ = execSQL(ctx, "INSERT INTO sx_login_fail(type,account,ip,fails,ban_until,updated_at) VALUES('user',?,?,1,0,?) ON DUPLICATE KEY UPDATE fails=fails+1,ban_until=IF(fails>=4,?,0),updated_at=?", email, ip, time.Now().Unix(), time.Now().Add(time.Hour).Unix(), time.Now().Unix())
		return AppTokens{}, appError(401, "账号或密码错误")
	}
	_ = execSQL(ctx, "DELETE FROM sx_login_fail WHERE type='user' AND account=? AND ip=?", email, ip)
	_ = execSQL(ctx, "UPDATE sx_user SET last_login_time=?,last_login_ip=? WHERE id=?", time.Now().Unix(), ip, user["id"])
	return AppIssueDeviceSession(ctx, gconv.Int64(user["id"]), deviceID, name)
}

func appRefresh(ctx context.Context, refresh, deviceID string) (AppTokens, error) {
	if len(refresh) != 48 {
		return AppTokens{}, appError(401, "刷新凭据无效")
	}
	access, e := appToken()
	if e != nil {
		return AppTokens{}, e
	}
	next, e := appToken()
	if e != nil {
		return AppTokens{}, e
	}
	var result AppTokens
	err := g.DB().Transaction(ctx, func(_ context.Context, tx gdb.TX) error {
		record, err := tx.GetOne("SELECT id,user_id,device_id FROM sx_app_session WHERE refresh_hash=? AND device_id=? AND revoked=0 AND refresh_expire>? FOR UPDATE", appHash(refresh), deviceID, time.Now().Unix())
		if err != nil {
			return err
		}
		if record == nil {
			return appError(401, "设备登录已失效")
		}
		user, err := tx.GetOne("SELECT id,email,name,points,vip_expire,avatar FROM sx_user WHERE id=? AND status=1", record["user_id"])
		if err != nil {
			return err
		}
		if user == nil {
			return appError(401, "账号不可用")
		}
		exp := time.Now().Add(time.Hour).Unix()
		_, err = tx.Exec("UPDATE sx_app_session SET access_hash=?,refresh_hash=?,access_expire=?,updated=? WHERE id=?", appHash(access), appHash(next), exp, time.Now().Unix(), record["id"])
		result = AppTokens{access, next, exp, applyDefaultUserAvatar(ctx, gconv.Map(user)), deviceID}
		return err
	})
	return result, err
}

func appCaptcha(ctx context.Context) (row, error) {
	challenge, err := appToken()
	if err != nil {
		return nil, err
	}
	const chars = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	value := make([]byte, 4)
	random := make([]byte, 4)
	if _, err = rand.Read(random); err != nil {
		return nil, err
	}
	for i := range value {
		value[i] = chars[int(random[i])%len(chars)]
	}
	expires := time.Now().Add(5 * time.Minute).Unix()
	if err = execSQL(ctx, "INSERT INTO sx_app_captcha(challenge,answer_hash,expire) VALUES(?,?,?)", challenge, appHash(challenge+":"+strings.ToLower(string(value))), expires); err != nil {
		return nil, err
	}
	_ = execSQL(ctx, "DELETE FROM sx_app_captcha WHERE expire<?", time.Now().Unix())
	// Render a portable PNG: Android image decoders need no SVG plugin.
	small := image.NewRGBA(image.Rect(0, 0, 70, 24))
	draw.Draw(small, small.Bounds(), &image.Uniform{color.RGBA{232, 237, 243, 255}}, image.Point{}, draw.Src)
	d := font.Drawer{Dst: small, Src: image.NewUniform(color.RGBA{30, 48, 70, 255}), Face: basicfont.Face7x13, Dot: fixed.P(8, 17)}
	d.DrawString(string(value))
	large := image.NewRGBA(image.Rect(0, 0, 210, 72))
	for y := 0; y < 72; y++ {
		for x := 0; x < 210; x++ {
			large.Set(x, y, small.At(x/3, y/3))
		}
	}
	var output bytes.Buffer
	if err = png.Encode(&output, large); err != nil {
		return nil, err
	}
	return row{"challenge_id": challenge, "image_mime": "image/png", "image_base64": base64.StdEncoding.EncodeToString(output.Bytes()), "expires_at": expires}, nil
}

func appConsumeCaptcha(ctx context.Context, challenge, answer string) error {
	if len(challenge) != 48 {
		return appError(400, "图形验证码无效")
	}
	valid := false
	err := g.DB().Transaction(ctx, func(_ context.Context, tx gdb.TX) error {
		item, err := tx.GetOne("SELECT answer_hash,expire FROM sx_app_captcha WHERE challenge=? FOR UPDATE", challenge)
		if err != nil {
			return err
		}
		if item == nil {
			return nil
		}
		valid = gconv.Int64(item["expire"]) > time.Now().Unix() && hmac.Equal([]byte(gconv.String(item["answer_hash"])), []byte(appHash(challenge+":"+strings.ToLower(strings.TrimSpace(answer)))))
		_, err = tx.Exec("DELETE FROM sx_app_captcha WHERE challenge=?", challenge)
		return err
	})
	if err != nil {
		return err
	}
	if !valid {
		return appError(400, "图形验证码错误或已过期")
	}
	return nil
}

func appWrite(r *ghttp.Request, data any, err error) {
	r.Response.Header().Set("Cache-Control", "private, no-store")
	if err == nil {
		r.Response.WriteJson(row{"code": 0, "message": "ok", "data": data})
		return
	}
	status, message := http.StatusInternalServerError, "服务暂不可用，请稍后重试"
	var known *AppError
	if errors.As(err, &known) {
		status, message = known.Status, known.Message
	} else {
		g.Log().Errorf(r.Context(), "APP API failure: %T", err)
	}
	r.Response.WriteHeader(status)
	r.Response.WriteJson(row{"code": status, "message": message, "data": nil})
}
