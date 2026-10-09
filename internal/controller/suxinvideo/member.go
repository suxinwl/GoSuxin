package suxinvideo

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/gomail.v2"
)

type Member struct{}
type LoginPageReq struct {
	g.Meta `path:"/login" method:"get" noValApi:"1"`
}
type LoginPageRes struct{}
type LoginReq struct {
	g.Meta   `path:"/login" method:"post" noValApi:"1"`
	Email    string `p:"email"`
	Password string `p:"password"`
	Captcha  string `p:"captcha"`
}
type LoginRes struct{}
type RegisterPageReq struct {
	g.Meta `path:"/register" method:"get" noValApi:"1"`
}
type RegisterPageRes struct{}
type RegisterReq struct {
	g.Meta    `path:"/register" method:"post" noValApi:"1"`
	Email     string `p:"email"`
	Name      string `p:"name"`
	Password  string `p:"password"`
	EmailCode string `p:"email_code"`
	Captcha   string `p:"captcha"`
}
type RegisterRes struct{}
type SendCodeReq struct {
	g.Meta  `path:"/sendcode" method:"post" noValApi:"1"`
	Email   string `p:"email"`
	Captcha string `p:"captcha"`
	Purpose string `p:"purpose"`
}
type SendCodeRes struct{}
type CaptchaReq struct {
	g.Meta `path:"/captcha" method:"get" noValApi:"1"`
}
type CaptchaRes struct{}
type CenterReq struct {
	g.Meta `path:"/center" method:"get" noValApi:"1"`
}
type CenterRes struct{}
type LogoutReq struct {
	g.Meta `path:"/logout" method:"post" noValApi:"1"`
}
type LogoutRes struct{}
type FilmRequestReq struct {
	g.Meta `path:"/filmrequest" method:"post" noValApi:"1"`
	Title  string `p:"title"`
	Note   string `p:"note"`
}
type FilmRequestRes struct{}
type FavoriteReq struct {
	g.Meta `path:"/favorite" method:"post" noValApi:"1"`
	VodID  int64 `p:"vod_id"`
}
type FavoriteRes struct{}
type HistoryReq struct {
	g.Meta `path:"/history" method:"get" noValApi:"1"`
}
type HistoryRes struct{}
type FavoritesReq struct {
	g.Meta `path:"/favorites" method:"get" noValApi:"1"`
}
type FavoritesRes struct{}
type SignReq struct {
	g.Meta `path:"/sign" method:"post" noValApi:"1"`
}
type SignRes struct{}
type CommentReq struct {
	g.Meta  `path:"/comment" method:"post" noValApi:"1"`
	VodID   int64  `p:"vod_id"`
	Content string `p:"content"`
}
type CommentRes struct{}

func (*Member) LoginPage(ctx context.Context, _ *LoginPageReq) (*LoginPageRes, error) {
	if setting(ctx, "member_enable", "1") != "1" {
		notFound(ctx)
		return &LoginPageRes{}, nil
	}
	types, _ := all(ctx, "SELECT id,name FROM sx_type WHERE pid=0 AND status=1 ORDER BY sort LIMIT 30")
	render(ctx, "????", types, loginThemeBody, map[string]any{"RegistrationEnabled": setting(ctx, "register_enable", "1") == "1", "PasswordResetEnabled": passwordResetEnabled(ctx), "CaptchaWidget": captchaWidget(ctx)})
	return &LoginPageRes{}, nil
}
func (*Member) Login(ctx context.Context, req *LoginReq) (*LoginRes, error) {
	r := g.RequestFromCtx(ctx)
	if setting(ctx, "member_enable", "1") != "1" {
		badRequest(r, "会员系统已关闭")
		return &LoginRes{}, nil
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if _, err := mail.ParseAddress(email); err != nil {
		badRequest(r, "邮箱格式不正确")
		return &LoginRes{}, nil
	}
	if !verifyCMSCaptcha(r, req.Captcha) {
		badRequest(r, "验证码错误或已过期")
		return &LoginRes{}, nil
	}
	ip := cmsClientIP(ctx, r)
	fail, _ := one(ctx, "SELECT fails,ban_until FROM sx_login_fail WHERE type='user' AND account=? AND ip=?", email, ip)
	if fail != nil && gconv.Int64(fail["ban_until"]) > time.Now().Unix() {
		badRequest(r, "登录次数过多，请稍后再试")
		return &LoginRes{}, nil
	}
	user, err := one(ctx, "SELECT id,pwd,status FROM sx_user WHERE email=?", email)
	if err != nil {
		return nil, err
	}
	valid := false
	if user != nil {
		hash := strings.Replace(gconv.String(user["pwd"]), "$2y$", "$2a$", 1)
		valid = bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) == nil && gconv.Int(user["status"]) == 1
	}
	if !valid {
		_ = execSQL(ctx, "INSERT INTO sx_login_fail (type,account,ip,fails,ban_until,updated_at) VALUES ('user',?,?,1,0,?) ON DUPLICATE KEY UPDATE fails=fails+1,ban_until=IF(fails>=4,?,0),updated_at=?", email, ip, time.Now().Unix(), time.Now().Add(time.Hour).Unix(), time.Now().Unix())
		badRequest(r, "账号或密码错误")
		return &LoginRes{}, nil
	}
	_ = execSQL(ctx, "DELETE FROM sx_login_fail WHERE type='user' AND account=? AND ip=?", email, ip)
	if _, err = r.Session.RegenerateId(true); err != nil {
		return nil, err
	}
	if err = r.Session.Set("sx_user_id", gconv.Int64(user["id"])); err != nil {
		return nil, err
	}
	_ = execSQL(ctx, "UPDATE sx_user SET last_login_time=?,last_login_ip=? WHERE id=?", time.Now().Unix(), ip, user["id"])
	r.Response.RedirectTo("/suxinvideo/center", http.StatusSeeOther)
	return &LoginRes{}, nil
}
func (*Member) RegisterPage(ctx context.Context, _ *RegisterPageReq) (*RegisterPageRes, error) {
	if setting(ctx, "member_enable", "1") != "1" || setting(ctx, "register_enable", "1") != "1" {
		notFound(ctx)
		return &RegisterPageRes{}, nil
	}
	types, _ := all(ctx, "SELECT id,name FROM sx_type WHERE pid=0 AND status=1 ORDER BY sort LIMIT 30")
	render(ctx, "????", types, registerThemeBody, map[string]any{"RegistrationRequiresEmailCode": registrationRequiresEmailCode(ctx), "CaptchaWidget": captchaWidget(ctx)})
	return &RegisterPageRes{}, nil
}
func (*Member) Captcha(ctx context.Context, _ *CaptchaReq) (*CaptchaRes, error) {
	r := g.RequestFromCtx(ctx)
	const chars = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	b := make([]byte, 4)
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		return nil, err
	}
	for i := range b {
		b[i] = chars[int(random[i])%len(chars)]
	}
	if err := r.Session.Set("sx_captcha", strings.ToLower(string(b))); err != nil {
		return nil, err
	}
	_ = r.Session.Set("sx_captcha_time", time.Now().Unix())
	r.Response.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	r.Response.Header().Set("Cache-Control", "no-store")
	r.Response.Write(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="130" height="42"><rect width="130" height="42" fill="#f5f6f8"/><path d="M3 15L128 29M10 39L120 4" stroke="#bbc"/><text x="12" y="30" font-size="24" font-family="monospace" letter-spacing="5" fill="#253759">%s</text></svg>`, b))
	return &CaptchaRes{}, nil
}
func verifyCaptcha(r *ghttp.Request, input string) bool {
	v, _ := r.Session.Get("sx_captcha")
	t, _ := r.Session.Get("sx_captcha_time")
	_ = r.Session.Remove("sx_captcha", "sx_captcha_time")
	return v != nil && v.String() != "" && strings.EqualFold(v.String(), strings.TrimSpace(input)) && t != nil && time.Now().Unix()-t.Int64() < 300
}
func (*Member) SendCode(ctx context.Context, req *SendCodeReq) (*SendCodeRes, error) {
	r := g.RequestFromCtx(ctx)
	if setting(ctx, "member_enable", "1") != "1" || (req.Purpose != "reset" && setting(ctx, "register_enable", "1") != "1") {
		badRequest(r, "当前不可发送验证码")
		return &SendCodeRes{}, nil
	}
	smtp, ready := memberSMTP(ctx)
	if !ready {
		r.Response.WriteStatus(http.StatusServiceUnavailable, "邮件服务尚未配置，暂不可发送邮箱验证码")
		return &SendCodeRes{}, nil
	}
	if !verifyCMSCaptcha(r, req.Captcha) {
		badRequest(r, "图形验证码错误")
		return &SendCodeRes{}, nil
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if _, err := mail.ParseAddress(email); err != nil {
		badRequest(r, "邮箱格式错误")
		return &SendCodeRes{}, nil
	}
	purpose := req.Purpose
	if purpose == "" {
		purpose = "register"
	}
	if purpose != "register" && purpose != "reset" {
		badRequest(r, "验证码用途无效")
		return &SendCodeRes{}, nil
	}
	user, err := one(ctx, "SELECT id FROM sx_user WHERE email=?", email)
	if err != nil {
		return nil, err
	}
	if purpose == "reset" && user == nil {
		badRequest(r, "该邮箱未注册")
		return &SendCodeRes{}, nil
	}
	if purpose == "register" && user != nil {
		badRequest(r, "该邮箱已注册")
		return &SendCodeRes{}, nil
	}
	last, _ := one(ctx, "SELECT created FROM sx_email_code WHERE email=? ORDER BY id DESC LIMIT 1", email)
	if last != nil && time.Now().Unix()-gconv.Int64(last["created"]) < 60 {
		badRequest(r, "发送过于频繁")
		return &SendCodeRes{}, nil
	}
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		return nil, err
	}
	code := ""
	for _, n := range random {
		code += strconv.Itoa(int(n) % 10)
	}
	message := gomail.NewMessage()
	message.SetHeader("From", smtp.User)
	message.SetHeader("To", email)
	message.SetHeader("Subject", "速信影视CMS 邮箱验证码")
	message.SetBody("text/plain", "验证码："+code+"，10 分钟内有效。")
	dialer := gomail.NewDialer(smtp.Host, smtp.Port, smtp.User, smtp.Password)
	if err := dialer.DialAndSend(message); err != nil {
		r.Response.WriteStatus(http.StatusBadGateway, "邮件发送失败，请稍后重试")
		return &SendCodeRes{}, nil
	}
	if err := execSQL(ctx, "INSERT INTO sx_email_code(email,code,type,expire,used,created) VALUES(?,?,?,?,0,?)", email, code, purpose, time.Now().Add(10*time.Minute).Unix(), time.Now().Unix()); err != nil {
		return nil, err
	}
	r.Response.WriteJson(map[string]any{"ok": true, "message": "验证码已发送"})
	return &SendCodeRes{}, nil
}
func (*Member) Register(ctx context.Context, req *RegisterReq) (*RegisterRes, error) {
	r := g.RequestFromCtx(ctx)
	if setting(ctx, "member_enable", "1") != "1" || setting(ctx, "register_enable", "1") != "1" {
		badRequest(r, "注册功能已关闭")
		return &RegisterRes{}, nil
	}
	if !verifyCMSCaptcha(r, req.Captcha) {
		badRequest(r, "图形验证码错误")
		return &RegisterRes{}, nil
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if _, err := mail.ParseAddress(email); err != nil {
		badRequest(r, "邮箱格式错误")
		return &RegisterRes{}, nil
	}
	if len(req.Password) < 6 || len(req.Password) > 72 {
		badRequest(r, "密码长度须为 6 至 72 位")
		return &RegisterRes{}, nil
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > 20 {
		badRequest(r, "昵称长度不正确")
		return &RegisterRes{}, nil
	}
	if err := registerMemberAccount(ctx, email, name, req.Password, req.EmailCode, cmsClientIP(ctx, r)); err != nil {
		var known *AppError
		if errors.As(err, &known) {
			badRequest(r, known.Message)
			return &RegisterRes{}, nil
		}
		return nil, err
	}
	r.Response.RedirectTo("/suxinvideo/login", http.StatusSeeOther)
	return &RegisterRes{}, nil
}
func currentUser(ctx context.Context) (row, error) {
	if member, err := AppMember(ctx); err != nil || member != nil {
		return applyDefaultUserAvatar(ctx, member), err
	}
	if setting(ctx, "member_enable", "1") != "1" {
		return nil, nil
	}
	r := g.RequestFromCtx(ctx)
	if r == nil {
		return nil, nil
	}
	id, _ := r.Session.Get("sx_user_id")
	if id == nil || id.Int64() < 1 {
		return nil, nil
	}
	user, err := one(ctx, "SELECT id,email,name,points,vip_expire,avatar FROM sx_user WHERE id=? AND status=1", id.Int64())
	return applyDefaultUserAvatar(ctx, user), err
}
func (*Member) Center(ctx context.Context, _ *CenterReq) (*CenterRes, error) {
	user, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if user == nil {
		g.RequestFromCtx(ctx).Response.RedirectTo("/suxinvideo/login")
		return &CenterRes{}, nil
	}
	types, _ := all(ctx, "SELECT id,name FROM sx_type WHERE pid=0 AND status=1 ORDER BY sort LIMIT 30")
	render(ctx, "个人中心", types, centerThemeBody, accountPageData(user, map[string]any{"SignPoints": setting(ctx, "points_sign", "5")}))
	return &CenterRes{}, nil
}
func (*Member) Logout(ctx context.Context, _ *LogoutReq) (*LogoutRes, error) {
	r := g.RequestFromCtx(ctx)
	_ = r.Session.Remove("sx_user_id")
	r.Response.RedirectTo("/suxinvideo", http.StatusSeeOther)
	return &LogoutRes{}, nil
}
func (*Member) FilmRequest(ctx context.Context, req *FilmRequestReq) (*FilmRequestRes, error) {
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	r := g.RequestFromCtx(ctx)
	if u == nil {
		badRequest(r, "请先登录")
		return &FilmRequestRes{}, nil
	}
	title := strings.TrimSpace(req.Title)
	if title == "" || len([]rune(title)) > 100 {
		badRequest(r, "片名无效")
		return &FilmRequestRes{}, nil
	}
	err = execSQL(ctx, "INSERT INTO sx_film_request(user_id,title,note,status,created) VALUES(?,?,?,0,?)", u["id"], title, req.Note, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	r.Response.WriteJson(map[string]any{"ok": true})
	return &FilmRequestRes{}, nil
}
func (*Member) Favorite(ctx context.Context, req *FavoriteReq) (*FavoriteRes, error) {
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	r := g.RequestFromCtx(ctx)
	if u == nil {
		badRequest(r, "请先登录")
		return &FavoriteRes{}, nil
	}
	vod, err := one(ctx, "SELECT id FROM sx_vod WHERE id=? AND "+publicVodCondition(ctx, ""), req.VodID)
	if err != nil {
		return nil, err
	}
	if vod == nil {
		badRequest(r, "影片不存在")
		return &FavoriteRes{}, nil
	}
	favorite, err := one(ctx, "SELECT id FROM sx_fav WHERE user_id=? AND vod_id=?", u["id"], req.VodID)
	if err != nil {
		return nil, err
	}
	if favorite == nil {
		err = execSQL(ctx, "INSERT IGNORE INTO sx_fav(user_id,vod_id,created) VALUES(?,?,?)", u["id"], req.VodID, time.Now().Unix())
	} else {
		err = execSQL(ctx, "DELETE FROM sx_fav WHERE user_id=? AND vod_id=?", u["id"], req.VodID)
	}
	if err != nil {
		return nil, err
	}
	r.Response.WriteJson(map[string]any{"ok": true, "vod_id": req.VodID, "fav": favorite == nil})
	return &FavoriteRes{}, nil
}
func (*Member) History(ctx context.Context, _ *HistoryReq) (*HistoryRes, error) {
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if u == nil {
		g.RequestFromCtx(ctx).Response.RedirectTo("/suxinvideo/login")
		return &HistoryRes{}, nil
	}
	vods, err := all(ctx, "SELECT v.id,v.name,v.pic,v.year,v.remarks,v.score,p.episode,GREATEST(0,p.episode-1) AS episode_index,p.position,p.updated FROM sx_play_record p JOIN sx_vod v ON v.id=p.vod_id WHERE p.user_id=? AND "+publicVodCondition(ctx, "v")+" ORDER BY p.updated DESC LIMIT 60", u["id"])
	if err != nil {
		return nil, err
	}
	types, _ := all(ctx, "SELECT id,name FROM sx_type WHERE pid=0 AND status=1 ORDER BY sort LIMIT 30")
	render(ctx, "观看历史", types, historyThemeBody, accountPageData(u, map[string]any{"Records": vods}))
	return &HistoryRes{}, nil
}
func (*Member) Favorites(ctx context.Context, _ *FavoritesReq) (*FavoritesRes, error) {
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if u == nil {
		g.RequestFromCtx(ctx).Response.RedirectTo("/suxinvideo/login")
		return &FavoritesRes{}, nil
	}
	vods, err := all(ctx, "SELECT v.id,v.name,v.pic,v.year,v.remarks,v.score FROM sx_fav f JOIN sx_vod v ON v.id=f.vod_id WHERE f.user_id=? AND "+publicVodCondition(ctx, "v")+" ORDER BY f.id DESC LIMIT 60", u["id"])
	if err != nil {
		return nil, err
	}
	types, _ := all(ctx, "SELECT id,name FROM sx_type WHERE pid=0 AND status=1 ORDER BY sort LIMIT 30")
	render(ctx, "我的收藏", types, favoritesThemeBody, accountPageData(u, map[string]any{"Vods": vods}))
	return &FavoritesRes{}, nil
}
func (*Member) Sign(ctx context.Context, _ *SignReq) (*SignRes, error) {
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	r := g.RequestFromCtx(ctx)
	if u == nil {
		badRequest(r, "请先登录")
		return &SignRes{}, nil
	}
	day, _ := strconv.Atoi(time.Now().Format("20060102"))
	points := gconv.Int(setting(ctx, "points_sign", "5"))
	if points < 0 || points > 1000 {
		points = 5
	}
	already := false
	err = g.DB().Transaction(ctx, func(_ context.Context, tx gdb.TX) error {
		result, e := tx.Exec("INSERT IGNORE INTO sx_sign(user_id,day,points) VALUES(?,?,?)", u["id"], day, points)
		if e != nil {
			return e
		}
		affected, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if affected == 0 {
			already = true
			return nil
		}
		_, e = tx.Exec("UPDATE sx_user SET points=points+?,sign_day=? WHERE id=?", points, day, u["id"])
		return e
	})
	if err != nil {
		return nil, err
	}
	if already {
		badRequest(r, "今天已经签到")
		return &SignRes{}, nil
	}
	r.Response.RedirectTo("/suxinvideo/center", http.StatusSeeOther)
	return &SignRes{}, nil
}
func (*Member) Comment(ctx context.Context, req *CommentReq) (*CommentRes, error) {
	if setting(ctx, "comment_enable", "1") != "1" {
		badRequest(g.RequestFromCtx(ctx), "评论功能已关闭")
		return &CommentRes{}, nil
	}
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	r := g.RequestFromCtx(ctx)
	if u == nil {
		badRequest(r, "请先登录")
		return &CommentRes{}, nil
	}
	content := strings.TrimSpace(req.Content)
	if content == "" || len([]rune(content)) > 500 {
		badRequest(r, "评论长度须在 1 至 500 字之间")
		return &CommentRes{}, nil
	}
	vod, err := one(ctx, "SELECT id FROM sx_vod WHERE id=? AND "+publicVodCondition(ctx, ""), req.VodID)
	if err != nil {
		return nil, err
	}
	if vod == nil {
		badRequest(r, "影片不存在")
		return &CommentRes{}, nil
	}
	status := 1
	if setting(ctx, "comment_audit", "0") == "1" {
		status = 0
	}
	if err := execSQL(ctx, "INSERT INTO sx_comment(user_id,vod_id,content,status,created) VALUES(?,?,?,?,?)", u["id"], req.VodID, content, status, time.Now().Unix()); err != nil {
		return nil, err
	}
	r.Response.WriteJson(map[string]any{"ok": true, "pending": status == 0})
	return &CommentRes{}, nil
}
func badRequest(r *ghttp.Request, message string) {
	r.Response.WriteStatus(http.StatusBadRequest, html.EscapeString(message))
}
