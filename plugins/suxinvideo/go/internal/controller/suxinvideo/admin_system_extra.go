package suxinvideo

import (
	"context"
	"net/mail"
	"strings"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"github.com/suxinwl/GoSuxin/utility/gf"
	"gopkg.in/gomail.v2"
)

type MailTestReq struct {
	g.Meta `path:"/mailTest" method:"post"`
	Email  string `p:"email"`
}
type MailTestRes struct{}
type AdminPasswordReq struct {
	g.Meta      `path:"/adminPassword" method:"post"`
	OldPassword string `p:"oldpassword"`
	Password    string `p:"password"`
	RePassword  string `p:"repassword"`
}
type AdminPasswordRes struct{}

func (*Admin) MailTest(ctx context.Context, req *MailTestReq) (*MailTestRes, error) {
	r := g.RequestFromCtx(ctx)
	email := strings.TrimSpace(req.Email)
	if _, err := mail.ParseAddress(email); err != nil {
		badRequest(r, "收件邮箱无效")
		return &MailTestRes{}, nil
	}
	host := setting(ctx, "smtp_host", "")
	from := setting(ctx, "smtp_user", "")
	pass := setting(ctx, "smtp_pass", "")
	if host == "" || from == "" || pass == "" {
		badRequest(r, "邮件服务尚未配置")
		return &MailTestRes{}, nil
	}
	message := gomail.NewMessage()
	message.SetHeader("From", from)
	message.SetHeader("To", email)
	message.SetHeader("Subject", "速信影视CMS 测试邮件")
	message.SetBody("text/plain", "邮件服务配置成功。")
	dialer := gomail.NewDialer(host, gconv.Int(setting(ctx, "smtp_port", "465")), from, pass)
	dialer.SSL = setting(ctx, "smtp_secure", "ssl") == "ssl"
	if err := dialer.DialAndSend(message); err != nil {
		return nil, err
	}
	r.Response.WriteJson(gf.Success().SetData(true))
	return &MailTestRes{}, nil
}

func (*Admin) AdminPassword(ctx context.Context, req *AdminPasswordReq) (*AdminPasswordRes, error) {
	r := g.RequestFromCtx(ctx)
	if req.Password != req.RePassword || len(req.Password) < 8 {
		badRequest(r, "新密码至少8位且两次输入一致")
		return &AdminPasswordRes{}, nil
	}
	uid := gconv.Int64(ctx.Value("uid"))
	admin, err := one(ctx, "SELECT password,salt FROM gf_admin WHERE id=?", uid)
	if err != nil {
		return nil, err
	}
	if admin == nil || gf.Md5(gf.Md5(req.OldPassword)+gconv.String(admin["salt"])) != gconv.String(admin["password"]) {
		badRequest(r, "旧密码不正确")
		return &AdminPasswordRes{}, nil
	}
	if err = execSQL(ctx, "UPDATE gf_admin SET password=? WHERE id=?", gf.Md5(gf.Md5(req.Password)+gconv.String(admin["salt"])), uid); err != nil {
		return nil, err
	}
	r.Response.WriteJson(gf.Success().SetData(true))
	return &AdminPasswordRes{}, nil
}
