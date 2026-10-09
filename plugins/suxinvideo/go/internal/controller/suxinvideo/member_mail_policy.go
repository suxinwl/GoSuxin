package suxinvideo

import (
	"context"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"golang.org/x/crypto/bcrypt"
)

type memberSMTPConfig struct {
	Host, User, Password string
	Port                 int
}

// Readiness depends only on saved configuration. A delivery failure must never
// silently turn off email verification for registrations or password resets.
func parseMemberSMTP(host, user, password, port string) (memberSMTPConfig, bool) {
	config := memberSMTPConfig{Host: strings.TrimSpace(host), User: strings.TrimSpace(user), Password: password, Port: 465}
	if config.Host == "" || config.User == "" || strings.TrimSpace(password) == "" {
		return config, false
	}
	if port = strings.TrimSpace(port); port != "" {
		parsed, err := strconv.Atoi(port)
		if err != nil || parsed < 1 || parsed > 65535 {
			return config, false
		}
		config.Port = parsed
	}
	return config, true
}

func memberSMTP(ctx context.Context) (memberSMTPConfig, bool) {
	return parseMemberSMTP(setting(ctx, "smtp_host", ""), setting(ctx, "smtp_user", ""), setting(ctx, "smtp_pass", ""), setting(ctx, "smtp_port", "465"))
}

func registrationRequiresEmailCode(ctx context.Context) bool {
	_, ready := memberSMTP(ctx)
	return ready
}

func passwordResetEnabled(ctx context.Context) bool {
	return setting(ctx, "member_enable", "1") == "1" && registrationRequiresEmailCode(ctx)
}

// Both HTML and native clients use the same server-owned policy and atomic
// verification-code consumption. Clients cannot select email verification.
func registerMemberAccount(ctx context.Context, email, name, password, emailCode, ip string) error {
	if setting(ctx, "member_enable", "1") != "1" || setting(ctx, "register_enable", "1") != "1" {
		return appError(403, "注册功能已关闭")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	name = strings.TrimSpace(name)
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		return appError(400, "邮箱格式错误")
	}
	if len(password) < 6 || len(password) > 72 || len([]rune(name)) < 1 || len([]rune(name)) > 20 {
		return appError(400, "昵称或密码长度不正确")
	}
	requiresEmailCode := registrationRequiresEmailCode(ctx)
	if requiresEmailCode && strings.TrimSpace(emailCode) == "" {
		return appError(400, "请输入邮箱验证码")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	bonus := gconv.Int(setting(ctx, "points_register", "0"))
	if bonus < 0 || bonus > 100000 {
		bonus = 0
	}
	return g.DB().Transaction(ctx, func(_ context.Context, tx gdb.TX) error {
		var code gdb.Record
		if requiresEmailCode {
			var e error
			code, e = tx.GetOne("SELECT id FROM sx_email_code WHERE email=? AND code=? AND type='register' AND used=0 AND expire>? ORDER BY id DESC LIMIT 1 FOR UPDATE", email, strings.TrimSpace(emailCode), time.Now().Unix())
			if e != nil {
				return e
			}
			if code == nil {
				return appError(400, "邮箱验证码错误或已过期")
			}
		}
		existing, e := tx.GetOne("SELECT id FROM sx_user WHERE email=?", email)
		if e != nil {
			return e
		}
		if existing != nil {
			return appError(409, "邮箱已注册")
		}
		verified := 0
		if requiresEmailCode {
			verified = 1
		}
		if _, e = tx.Exec("INSERT INTO sx_user(email,name,pwd,points,status,reg_ip,reg_time,email_verified) VALUES(?,?,?,?,1,?,?,?)", email, name, string(hash), bonus, ip, time.Now().Unix(), verified); e != nil {
			return e
		}
		if requiresEmailCode {
			_, e = tx.Exec("UPDATE sx_email_code SET used=1 WHERE id=?", code["id"])
		}
		return e
	})
}
