package suxinvideo

import (
	"context"
	"net/http"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

type payMethod struct{ Code, Name string }

func enabledPayMethods(ctx context.Context) []payMethod {
	methods := []payMethod{}
	if setting(ctx, "alipay_appid", "") != "" && setting(ctx, "alipay_private_key", "") != "" {
		methods = append(methods, payMethod{"alipay", "支付宝（官方）"}, payMethod{"alipayf2f", "支付宝当面付"})
	}
	if setting(ctx, "wxpay_appid", "") != "" && setting(ctx, "wxpay_mchid", "") != "" && setting(ctx, "wxpay_key", "") != "" {
		methods = append(methods, payMethod{"wxpay", "微信支付（官方）"})
	}
	for _, method := range []payMethod{{"codepay", "码支付"}, {"epay", "易支付"}} {
		if setting(ctx, method.Code+"_gateway", "") != "" && setting(ctx, method.Code+"_pid", "") != "" && setting(ctx, method.Code+"_key", "") != "" {
			methods = append(methods, method)
		}
	}
	if setting(ctx, "usdt_address", "") != "" && gconv.Float64(setting(ctx, "usdt_rate", "0")) > 0 {
		methods = append(methods, payMethod{"usdt", "USDT-TRC20"})
	}
	return methods
}

var payIndexThemeBody = accountPageBody("pay", `<h2>选择充值套餐</h2>{{if .Goods}}<div class="paycards">{{range .Goods}}<button type="button" class="paycard" aria-pressed="false" data-gid="{{.id}}"><span>{{if .days}}👑{{else}}💎{{end}} {{.name}}</span><span class="pr">¥{{.price}}</span><span class="ds">{{if .days}}VIP {{.days}} 天{{end}}{{if .points}} · {{.points}} 积分{{end}}</span></button>{{end}}</div>{{else}}<p class="sx-account-help">暂无充值套餐，请联系站长配置。</p>{{end}}<h3>选择支付方式</h3><div class="paymethods">{{range .Methods}}<button type="button" class="pm" aria-pressed="false" data-method="{{.Code}}">{{.Name}}</button>{{else}}<p class="sx-account-help">当前没有已开通的支付方式</p>{{end}}</div><button type="button" class="btn-main" id="createOrder">立即支付</button><p class="sx-account-result" id="pay-message" role="status"></p><script>(function(){let goods=0,method='';document.querySelectorAll('.paycard').forEach(el=>el.onclick=()=>{document.querySelectorAll('.paycard').forEach(x=>{x.classList.remove('on');x.setAttribute('aria-pressed','false')});el.classList.add('on');el.setAttribute('aria-pressed','true');goods=Number(el.dataset.gid)});document.querySelectorAll('.paymethods .pm').forEach(el=>el.onclick=()=>{document.querySelectorAll('.paymethods .pm').forEach(x=>{x.classList.remove('on');x.setAttribute('aria-pressed','false')});el.classList.add('on');el.setAttribute('aria-pressed','true');method=el.dataset.method});const button=document.getElementById('createOrder'),message=document.getElementById('pay-message');button.onclick=async()=>{if(!goods||!method){message.textContent='请选择充值套餐和支付方式';return}button.disabled=true;message.textContent='正在创建订单…';const data=new FormData();data.set('goods_id',String(goods));data.set('pay_type',method);try{const response=await fetch('/suxinvideo/pay/create',{method:'POST',body:data,credentials:'same-origin'}),raw=await response.text();let result;try{result=JSON.parse(raw)}catch(e){throw Error(raw.slice(0,160))}if(!response.ok||result.code&&result.code!==0)throw Error(result.message||result.msg||'创建订单失败');const pay=result.data;if(pay.type==='redirect'&&pay.url){location.href=pay.url;return}location.href='/suxinvideo/pay/cashier?ono='+encodeURIComponent(pay.order_no)}catch(e){message.textContent=e.message;button.disabled=false}}})();</script>`)

var payCashierThemeBody = accountPageBody("pay", `<div class="sx-cashier"><h2>订单收银台</h2><p>{{.Order.title}} · 应付 <b>¥{{.Order.amount}}</b></p>{{if .Checkout.qr_image}}<div class="qrbox"><p>付款完成后本页将自动跳转</p><img src="{{.Checkout.qr_image}}" alt="支付二维码" width="220" height="220"><p class="addr">{{.Checkout.qr}}</p></div>{{else}}<div class="qrbox"><p>付款信息已失效，请返回订单列表查看状态。</p></div>{{end}}<p id="pay-state" role="status">等待支付中…</p><a class="btn-line" href="/suxinvideo/orders">查看订单列表</a></div><script>(function(){const order={{.OrderJSON}},state=document.getElementById('pay-state');const timer=setInterval(async()=>{try{const response=await fetch('/suxinvideo/pay/check?ono='+encodeURIComponent(order),{credentials:'same-origin'}),result=await response.json();if(result.data&&Number(result.data.status)===1){clearInterval(timer);state.textContent='支付成功，正在跳转…';location.href='/suxinvideo/pay/result?ono='+encodeURIComponent(order)}}catch(e){}},4000);window.addEventListener('pagehide',()=>clearInterval(timer))})();</script>`)

var payResultThemeBody = accountPageBody("orders", `<div class="sx-pay-result"><h2>支付结果</h2><p>订单：{{.Order.order_no}}</p><p>{{.Order.title}} · ¥{{.Order.amount}}</p><p>{{if eq (str .Order.status) "1"}}支付成功{{else if eq (str .Order.status) "2"}}订单已取消{{else}}等待支付{{end}}</p><div class="sx-account-result-actions">{{if eq (str .Order.status) "0"}}<a class="btn-main" href="/suxinvideo/pay/cashier?ono={{.Order.order_no}}">返回收银台</a>{{end}}<a class="btn-line" href="/suxinvideo/center">用户中心</a></div></div>`)

type PayCashierReq struct {
	g.Meta  `path:"/pay/cashier" method:"get" noValApi:"1"`
	OrderNo string `p:"ono"`
}
type PayCashierRes struct{}

func (*Payment) PayCashier(ctx context.Context, req *PayCashierReq) (*PayCashierRes, error) {
	r := g.RequestFromCtx(ctx)
	user, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if user == nil {
		r.Response.RedirectTo("/suxinvideo/login", http.StatusSeeOther)
		return &PayCashierRes{}, nil
	}
	order, err := one(ctx, "SELECT order_no,title,amount,status FROM sx_order WHERE order_no=? AND user_id=?", req.OrderNo, user["id"])
	if err != nil {
		return nil, err
	}
	if order == nil {
		notFound(ctx)
		return &PayCashierRes{}, nil
	}
	checkout := map[string]any{}
	if saved, e := r.Session.Get("sx_checkout_" + req.OrderNo); e == nil && saved != nil {
		checkout = saved.Map()
	}
	types, _ := all(ctx, "SELECT id,name FROM sx_type WHERE pid=0 AND status=1 ORDER BY sort LIMIT 30")
	render(ctx, "收银台", types, payCashierThemeBody, accountPageData(user, map[string]any{"Order": order, "Checkout": checkout, "OrderJSON": req.OrderNo}))
	return &PayCashierRes{}, nil
}
