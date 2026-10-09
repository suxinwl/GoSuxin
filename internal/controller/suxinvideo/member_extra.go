package suxinvideo

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"golang.org/x/crypto/bcrypt"
)

type ForgotPageReq struct {
	g.Meta `path:"/forgot" method:"get" noValApi:"1"`
}
type ForgotPageRes struct{}
type ResetPasswordReq struct {
	g.Meta     `path:"/forgot" method:"post" noValApi:"1"`
	Email      string `p:"email"`
	Code       string `p:"code"`
	Password   string `p:"password"`
	Repassword string `p:"repassword"`
}
type ResetPasswordRes struct{}
type AccountReq struct {
	g.Meta      `path:"/account" method:"post" noValApi:"1"`
	Act         string `p:"act"`
	Name        string `p:"name"`
	OldPassword string `p:"oldpwd"`
	NewPassword string `p:"newpwd"`
	Repassword  string `p:"repwd"`
}
type AccountRes struct{}
type OrdersReq struct {
	g.Meta `path:"/orders" method:"get" noValApi:"1"`
}
type OrdersRes struct{}
type RecordsReq struct {
	g.Meta `path:"/records" method:"get" noValApi:"1"`
}
type RecordsRes struct{}
type HistoryClearReq struct {
	g.Meta `path:"/history/clear" method:"post" noValApi:"1"`
}
type HistoryClearRes struct{}
type ProgressReq struct {
	g.Meta   `path:"/progress" method:"post" noValApi:"1"`
	VodID    int64 `p:"vod_id"`
	Episode  int   `p:"episode"`
	Position int64 `p:"position"`
}
type ProgressRes struct{}

func (*Member) ForgotPage(ctx context.Context, _ *ForgotPageReq) (*ForgotPageRes, error) {
	if setting(ctx, "member_enable", "1") != "1" {
		notFound(ctx)
		return &ForgotPageRes{}, nil
	}
	types, _ := all(ctx, "SELECT id,name FROM sx_type WHERE pid=0 AND status=1 ORDER BY sort LIMIT 30")
	render(ctx, "????", types, forgotThemeBody, map[string]any{"PasswordResetEnabled": passwordResetEnabled(ctx), "CaptchaWidget": captchaWidget(ctx)})
	return &ForgotPageRes{}, nil
}
func (*Member) ResetPassword(ctx context.Context, req *ResetPasswordReq) (*ResetPasswordRes, error) {
	r := g.RequestFromCtx(ctx)
	if setting(ctx, "member_enable", "1") != "1" {
		badRequest(r, "会员系统已关闭")
		return &ResetPasswordRes{}, nil
	}
	if !passwordResetEnabled(ctx) {
		r.Response.WriteStatus(http.StatusServiceUnavailable, "邮件服务尚未配置，暂不可通过邮箱找回密码")
		return &ResetPasswordRes{}, nil
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		badRequest(r, "邮箱格式无效")
		return &ResetPasswordRes{}, nil
	}
	if len(req.Password) < 6 || len(req.Password) > 72 || req.Password != req.Repassword {
		badRequest(r, "两次密码不一致或长度无效")
		return &ResetPasswordRes{}, nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	invalid := false
	err = g.DB().Transaction(ctx, func(_ context.Context, tx gdb.TX) error {
		code, e := tx.GetOne("SELECT id FROM sx_email_code WHERE email=? AND code=? AND type='reset' AND used=0 AND expire>? ORDER BY id DESC LIMIT 1 FOR UPDATE", email, strings.TrimSpace(req.Code), time.Now().Unix())
		if e != nil {
			return e
		}
		if code == nil {
			invalid = true
			return nil
		}
		user, e := tx.GetOne("SELECT id FROM sx_user WHERE email=? FOR UPDATE", email)
		if e != nil {
			return e
		}
		if user == nil {
			return errors.New("账号不存在")
		}
		if _, e = tx.Exec("UPDATE sx_user SET pwd=? WHERE id=?", string(hash), user["id"]); e != nil {
			return e
		}
		_, e = tx.Exec("UPDATE sx_email_code SET used=1 WHERE id=?", code["id"])
		return e
	})
	if err != nil {
		return nil, err
	}
	if invalid {
		badRequest(r, "重置码错误或已过期")
		return &ResetPasswordRes{}, nil
	}
	r.Response.RedirectTo("/suxinvideo/login", http.StatusSeeOther)
	return &ResetPasswordRes{}, nil
}
func (*Member) Account(ctx context.Context, req *AccountReq) (*AccountRes, error) {
	r := g.RequestFromCtx(ctx)
	user, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if user == nil {
		badRequest(r, "请先登录")
		return &AccountRes{}, nil
	}
	switch req.Act {
	case "nickname":
		name := strings.TrimSpace(req.Name)
		if len([]rune(name)) < 2 || len([]rune(name)) > 20 || strings.ContainsAny(name, `<>"'\/`) {
			badRequest(r, "昵称长度或内容无效")
			return &AccountRes{}, nil
		}
		if err := execSQL(ctx, "UPDATE sx_user SET name=? WHERE id=?", name, user["id"]); err != nil {
			return nil, err
		}
	case "password":
		if len(req.NewPassword) < 6 || len(req.NewPassword) > 72 || req.NewPassword != req.Repassword {
			badRequest(r, "新密码长度无效或两次输入不一致")
			return &AccountRes{}, nil
		}
		record, err := one(ctx, "SELECT pwd FROM sx_user WHERE id=?", user["id"])
		if err != nil {
			return nil, err
		}
		old := strings.Replace(gconv.String(record["pwd"]), "$2y$", "$2a$", 1)
		if bcrypt.CompareHashAndPassword([]byte(old), []byte(req.OldPassword)) != nil {
			badRequest(r, "当前密码错误")
			return &AccountRes{}, nil
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		if err := execSQL(ctx, "UPDATE sx_user SET pwd=? WHERE id=?", string(hash), user["id"]); err != nil {
			return nil, err
		}
	default:
		badRequest(r, "操作无效")
		return &AccountRes{}, nil
	}
	r.Response.RedirectTo("/suxinvideo/center", http.StatusSeeOther)
	return &AccountRes{}, nil
}
func (*Member) Orders(ctx context.Context, _ *OrdersReq) (*OrdersRes, error) {
	r := g.RequestFromCtx(ctx)
	user, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if user == nil {
		r.Response.RedirectTo("/suxinvideo/login")
		return &OrdersRes{}, nil
	}
	orders, err := all(ctx, "SELECT order_no,title,amount,pay_type,status,created FROM sx_order WHERE user_id=? ORDER BY id DESC LIMIT 50", user["id"])
	if err != nil {
		return nil, err
	}
	types, _ := all(ctx, "SELECT id,name FROM sx_type WHERE pid=0 AND status=1 ORDER BY sort LIMIT 30")
	render(ctx, "充值订单", types, ordersThemeBody, accountPageData(user, map[string]any{"Orders": orders}))
	return &OrdersRes{}, nil
}
func (*Member) Records(ctx context.Context, _ *RecordsReq) (*RecordsRes, error) {
	r := g.RequestFromCtx(ctx)
	user, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if user == nil {
		badRequest(r, "请先登录")
		return &RecordsRes{}, nil
	}
	rows, err := all(ctx, "SELECT p.vod_id AS id,p.episode AS ep,p.updated AS time,v.name,v.pic,v.remarks FROM sx_play_record p JOIN sx_vod v ON v.id=p.vod_id WHERE p.user_id=? AND "+publicVodCondition(ctx, "v")+" ORDER BY p.updated DESC LIMIT 50", user["id"])
	if err != nil {
		return nil, err
	}
	r.Response.WriteJson(map[string]any{"list": rows, "logged": 1})
	return &RecordsRes{}, nil
}
func (*Member) HistoryClear(ctx context.Context, _ *HistoryClearReq) (*HistoryClearRes, error) {
	r := g.RequestFromCtx(ctx)
	user, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if user == nil {
		badRequest(r, "请先登录")
		return &HistoryClearRes{}, nil
	}
	if err := execSQL(ctx, "DELETE FROM sx_play_record WHERE user_id=?", user["id"]); err != nil {
		return nil, err
	}
	r.Response.RedirectTo("/suxinvideo/history", http.StatusSeeOther)
	return &HistoryClearRes{}, nil
}
func (*Member) Progress(ctx context.Context, req *ProgressReq) (*ProgressRes, error) {
	r := g.RequestFromCtx(ctx)
	user, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if user == nil {
		badRequest(r, "请先登录")
		return &ProgressRes{}, nil
	}
	if req.VodID < 1 || req.Episode < 1 || req.Position < 0 || req.Position > 86400 {
		badRequest(r, "播放进度无效")
		return &ProgressRes{}, nil
	}
	vod, err := one(ctx, "SELECT id,vip,points FROM sx_vod WHERE id=? AND "+publicVodCondition(ctx, ""), req.VodID)
	if err != nil {
		return nil, err
	}
	if vod == nil {
		badRequest(r, "影片不存在")
		return &ProgressRes{}, nil
	}
	if gconv.Int(vod["vip"]) == 1 && gconv.Int64(user["vip_expire"]) <= time.Now().Unix() {
		badRequest(r, "需要会员权限")
		return &ProgressRes{}, nil
	}
	if gconv.Int(vod["vip"]) != 1 && gconv.Int(vod["points"]) > 0 {
		owned, err := one(ctx, "SELECT id FROM sx_user_vod WHERE user_id=? AND vod_id=?", user["id"], req.VodID)
		if err != nil {
			return nil, err
		}
		if owned == nil {
			badRequest(r, "需要购买影片")
			return &ProgressRes{}, nil
		}
	}
	if err := execSQL(ctx, "INSERT INTO sx_play_record(user_id,vod_id,episode,position,updated) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE episode=VALUES(episode),position=VALUES(position),updated=VALUES(updated)", user["id"], req.VodID, req.Episode, req.Position, time.Now().Unix()); err != nil {
		return nil, err
	}
	r.Response.WriteJson(map[string]any{"ok": true, "episode": req.Episode, "position": req.Position, "vod_id": strconv.FormatInt(req.VodID, 10)})
	return &ProgressRes{}, nil
}
