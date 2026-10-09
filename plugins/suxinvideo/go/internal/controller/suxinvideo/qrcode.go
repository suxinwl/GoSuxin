package suxinvideo

import (
	"context"
	"crypto/hmac"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	qrcode "github.com/skip2/go-qrcode"
)

type PayQRReq struct {
	g.Meta `path:"/pay/qr" method:"get" noValApi:"1"`
	Data   string `p:"data"`
	Exp    int64  `p:"exp"`
	Sig    string `p:"sig"`
}
type PayQRRes struct{}

func qrImageLink(ctx context.Context, data string) string {
	exp := time.Now().Add(20 * time.Minute).Unix()
	return "/suxinvideo/pay/qr?data=" + url.QueryEscape(data) + "&exp=" + fmt.Sprint(exp) + "&sig=" + signProxy(ctx, data, exp)
}
func (*Payment) PayQR(ctx context.Context, req *PayQRReq) (*PayQRRes, error) {
	r := g.RequestFromCtx(ctx)
	if req.Data == "" || len(req.Data) > 500 || req.Exp < time.Now().Unix() || req.Exp > time.Now().Add(25*time.Minute).Unix() || !hmac.Equal([]byte(req.Sig), []byte(signProxy(ctx, req.Data, req.Exp))) {
		r.Response.WriteStatus(http.StatusForbidden)
		return &PayQRRes{}, nil
	}
	png, err := qrcode.Encode(req.Data, qrcode.Medium, 256)
	if err != nil {
		return nil, err
	}
	r.Response.Header().Set("Content-Type", "image/png")
	r.Response.Header().Set("Cache-Control", "private, max-age=600")
	r.Response.Write(png)
	return &PayQRRes{}, nil
}
