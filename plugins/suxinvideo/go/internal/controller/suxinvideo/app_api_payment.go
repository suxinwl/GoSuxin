package suxinvideo

import (
	"crypto/rand"
	"encoding/json"
	"html/template"
	"net/url"
	"time"

	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

func appPaymentCreate(r *ghttp.Request) (row, error) {
	ctx := r.Context()
	p, err := appRequirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	channel := r.Get("pay_type").String()
	allowed := false
	for _, method := range enabledPayMethods(ctx) {
		if method.Code == channel {
			allowed = true
		}
	}
	if !allowed {
		return nil, appError(400, "支付方式未启用")
	}
	goods, err := one(ctx, "SELECT id,name,price,points,days FROM sx_goods WHERE id=? AND status=1", r.Get("goods_id").Int64())
	if err != nil {
		return nil, err
	}
	if goods == nil {
		return nil, appError(404, "套餐不存在")
	}
	amount := gconv.Float64(goods["price"])
	if amount <= 0 || amount > 100000 {
		return nil, appError(400, "套餐金额无效")
	}
	random := make([]byte, 6)
	if _, err = rand.Read(random); err != nil {
		return nil, err
	}
	for i := range random {
		random[i] = '0' + random[i]%10
	}
	orderNo := time.Now().Format("20060102150405") + string(random)
	checkout, err := appToken()
	if err != nil {
		return nil, err
	}
	kind := "points"
	if gconv.Int(goods["days"]) > 0 {
		kind = "vip"
	}
	if err = execSQL(ctx, "INSERT INTO sx_order(order_no,user_id,goods_id,type,title,amount,pay_type,status,created) VALUES(?,?,?,?,?,?,?,0,?)", orderNo, p.User["id"], goods["id"], kind, goods["name"], amount, channel, time.Now().Unix()); err != nil {
		return nil, err
	}
	checkoutURL := requestBase(r) + "/suxinvideo/app/v1/payment/checkout?token=" + url.QueryEscape(checkout)
	notify := requestBase(r) + "/suxinvideo/pay/notify?channel=" + url.QueryEscape(channel) + "&order=" + url.QueryEscape(orderNo)
	result, err := createProviderPayment(ctx, channel, orderNo, gconv.String(goods["name"]), amount, notify, checkoutURL, cmsClientIP(ctx, r), r.Header.Get("User-Agent"))
	if err != nil {
		_ = execSQL(ctx, "UPDATE sx_order SET status=2 WHERE order_no=? AND status=0", orderNo)
		return nil, appError(502, "支付下单失败，请稍后重试")
	}
	result["order_no"] = orderNo
	if gconv.String(result["type"]) == "qrcode" {
		result["qr_image"] = qrImageLink(ctx, gconv.String(result["qr"]))
	}
	payload, _ := json.Marshal(result)
	if err = execSQL(ctx, "INSERT INTO sx_app_checkout(token_hash,user_id,session_id,order_no,payload,expire) VALUES(?,?,?,?,?,?)", appHash(checkout), p.User["id"], p.SessionID, orderNo, string(payload), time.Now().Add(20*time.Minute).Unix()); err != nil {
		return nil, err
	}
	data := row{}
	for key, value := range result {
		data[key] = value
	}
	data["checkout_url"] = checkoutURL
	if gconv.String(data["url"]) == "" {
		data["url"] = checkoutURL
	}
	return data, nil
}

func appReadCheckout(r *ghttp.Request) (row, row, error) {
	token := r.Get("token").String()
	if len(token) != 48 {
		return nil, nil, appError(403, "收银台链接无效")
	}
	item, err := one(r.Context(), "SELECT c.order_no,c.payload,o.title,o.amount,o.status,o.pay_type FROM sx_app_checkout c JOIN sx_app_session s ON s.id=c.session_id JOIN sx_user u ON u.id=c.user_id JOIN sx_order o ON o.order_no=c.order_no AND o.user_id=c.user_id WHERE c.token_hash=? AND c.expire>? AND s.revoked=0 AND u.status=1", appHash(token), time.Now().Unix())
	if err != nil {
		return nil, nil, err
	}
	if item == nil {
		return nil, nil, appError(403, "收银台链接已过期，请在APP重新打开")
	}
	var data row
	if json.Unmarshal([]byte(gconv.String(item["payload"])), &data) != nil {
		return nil, nil, appError(500, "订单信息不可用")
	}
	return item, data, nil
}

func appPaymentCheckout(r *ghttp.Request) {
	item, data, err := appReadCheckout(r)
	if err != nil {
		appWrite(r, nil, err)
		return
	}
	r.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
	r.Response.Header().Set("Cache-Control", "private, no-store")
	r.Response.Header().Set("Referrer-Policy", "no-referrer")
	view := row{"Title": item["title"], "Amount": item["amount"], "Order": item["order_no"], "Paid": gconv.Int(item["status"]) == 1, "QR": data["qr_image"], "Tip": data["tip"], "Redirect": data["url"], "StatusURL": "/suxinvideo/app/v1/payment/status?token=" + url.QueryEscape(r.Get("token").String())}
	t := template.Must(template.New("checkout").Parse(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>小柒影视收银台</title><style>body{margin:0;background:#10151b;color:#edf2f9;font:16px system-ui;text-align:center}main{max-width:440px;margin:60px auto;padding:24px}img{width:240px;background:white;padding:12px;border-radius:12px}a{display:block;margin:20px;padding:14px;background:#e93679;color:white;border-radius:8px;text-decoration:none}</style></head><body><main><h1>{{.Title}}</h1><p>订单 {{.Order}}</p><h2>¥ {{.Amount}}</h2>{{if .Paid}}<p id="state">支付成功，返回APP即可刷新会员状态。</p>{{else}}<p id="state">等待支付</p>{{if .QR}}<img src="{{.QR}}" alt="支付二维码"><p>{{.Tip}}</p>{{else if .Redirect}}<a href="{{.Redirect}}" rel="noreferrer">前往支付</a>{{end}}{{end}}<a href="xiaoqi://payment">返回小柒影视APP</a></main><script>const statusURL={{.StatusURL}};let tries=0;const timer=setInterval(async()=>{if(++tries>120){clearInterval(timer);return}try{const r=await fetch(statusURL,{cache:'no-store'}),v=await r.json();if(v.code===0&&v.data.status===1){document.querySelector('#state').textContent='支付成功，返回APP即可刷新会员状态。';clearInterval(timer)}}catch(e){}},5000)</script></body></html>`))
	_ = t.Execute(r.Response.Writer, view)
}
func appPaymentStatus(r *ghttp.Request) {
	item, _, err := appReadCheckout(r)
	if err != nil {
		appWrite(r, nil, err)
		return
	}
	if gconv.Int(item["status"]) == 0 && gconv.String(item["pay_type"]) == "usdt" {
		full, e := one(r.Context(), "SELECT * FROM sx_order WHERE order_no=?", item["order_no"])
		if e == nil && full != nil {
			if txid := checkUSDT(r.Context(), full); txid != "" {
				done, e := completeOrder(r.Context(), gconv.String(item["order_no"]), txid, nil)
				if e == nil && done {
					item["status"] = 1
				}
			}
		}
	}
	appWrite(r, row{"order_no": item["order_no"], "status": gconv.Int(item["status"])}, nil)
}
