package suxinvideo

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"github.com/suxinwl/GoSuxin/utility/gf"
)

type Payment struct{}
type PayIndexReq struct {
	g.Meta `path:"/pay" method:"get" noValApi:"1"`
}
type PayIndexRes struct{}
type PayCreateReq struct {
	g.Meta  `path:"/pay/create" method:"post" noValApi:"1"`
	GoodsID int64  `p:"goods_id"`
	PayType string `p:"pay_type"`
}
type PayCreateRes struct{}
type PayCheckReq struct {
	g.Meta  `path:"/pay/check" method:"get" noValApi:"1"`
	OrderNo string `p:"ono"`
}
type PayCheckRes struct{}
type PayResultReq struct {
	g.Meta  `path:"/pay/result" method:"get" noValApi:"1"`
	OrderNo string `p:"ono"`
}
type PayResultRes struct{}
type PayNotifyReq struct {
	g.Meta  `path:"/pay/notify" method:"post" noValApi:"1"`
	Channel string `p:"channel"`
	OrderNo string `p:"order"`
}
type PayNotifyRes struct{}
type BuyVodReq struct {
	g.Meta `path:"/pay/buyvod" method:"post" noValApi:"1"`
	VodID  int64 `p:"id"`
}
type BuyVodRes struct{}

func (*Payment) PayIndex(ctx context.Context, _ *PayIndexReq) (*PayIndexRes, error) {
	user, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	r := g.RequestFromCtx(ctx)
	if user == nil {
		r.Response.RedirectTo("/suxinvideo/login")
		return &PayIndexRes{}, nil
	}
	goods, err := all(ctx, "SELECT id,name,price,points,days FROM sx_goods WHERE status=1 ORDER BY sort,price")
	if err != nil {
		return nil, err
	}
	types, _ := all(ctx, "SELECT id,name FROM sx_type WHERE pid=0 AND status=1 ORDER BY sort LIMIT 30")
	render(ctx, "充值中心", types, payIndexThemeBody, accountPageData(user, map[string]any{"Goods": goods, "Methods": enabledPayMethods(ctx)}))
	return &PayIndexRes{}, nil
}
func (*Payment) PayCreate(ctx context.Context, req *PayCreateReq) (*PayCreateRes, error) {
	r := g.RequestFromCtx(ctx)
	user, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if user == nil {
		badRequest(r, "请先登录")
		return &PayCreateRes{}, nil
	}
	if !map[string]bool{"codepay": true, "epay": true, "usdt": true, "wxpay": true, "alipay": true, "alipayf2f": true}[req.PayType] {
		badRequest(r, "支付方式无效")
		return &PayCreateRes{}, nil
	}
	goods, err := one(ctx, "SELECT * FROM sx_goods WHERE id=? AND status=1", req.GoodsID)
	if err != nil {
		return nil, err
	}
	if goods == nil {
		badRequest(r, "套餐不存在")
		return &PayCreateRes{}, nil
	}
	money := gconv.Float64(goods["price"])
	if money <= 0 || money > 100000 {
		badRequest(r, "套餐金额无效")
		return &PayCreateRes{}, nil
	}
	suffix := make([]byte, 6)
	if _, err = rand.Read(suffix); err != nil {
		return nil, err
	}
	for i := range suffix {
		suffix[i] = '0' + suffix[i]%10
	}
	orderNo := time.Now().Format("20060102150405") + string(suffix)
	kind := "points"
	if gconv.Int(goods["days"]) > 0 {
		kind = "vip"
	}
	err = execSQL(ctx, "INSERT INTO sx_order(order_no,user_id,goods_id,type,title,amount,pay_type,status,created) VALUES(?,?,?,?,?,?,?,0,?)", orderNo, user["id"], req.GoodsID, kind, goods["name"], money, req.PayType, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	notify := requestBase(r) + "/suxinvideo/pay/notify?channel=" + req.PayType + "&order=" + orderNo
	back := requestBase(r) + "/suxinvideo/pay/result?ono=" + orderNo
	result, err := createProviderPayment(ctx, req.PayType, orderNo, gconv.String(goods["name"]), money, notify, back, cmsClientIP(ctx, r), r.Header.Get("User-Agent"))
	if err != nil {
		_ = execSQL(ctx, "UPDATE sx_order SET status=2 WHERE order_no=? AND status=0", orderNo)
		badRequest(r, err.Error())
		return &PayCreateRes{}, nil
	}
	result["order_no"] = orderNo
	if gconv.String(result["type"]) == "qrcode" {
		result["qr_image"] = qrImageLink(ctx, gconv.String(result["qr"]))
		_ = r.Session.Set("sx_checkout_"+orderNo, result)
	}
	r.Response.WriteJson(gf.Success().SetData(result))
	return &PayCreateRes{}, nil
}
func (*Payment) PayCheck(ctx context.Context, req *PayCheckReq) (*PayCheckRes, error) {
	r := g.RequestFromCtx(ctx)
	user, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if user == nil {
		badRequest(r, "请先登录")
		return &PayCheckRes{}, nil
	}
	order, err := one(ctx, "SELECT * FROM sx_order WHERE order_no=? AND user_id=?", req.OrderNo, user["id"])
	if err != nil {
		return nil, err
	}
	if order == nil {
		badRequest(r, "订单不存在")
		return &PayCheckRes{}, nil
	}
	if gconv.Int(order["status"]) == 0 && gconv.String(order["pay_type"]) == "usdt" {
		if txid := checkUSDT(ctx, order); txid != "" {
			done, finishErr := completeOrder(ctx, req.OrderNo, txid, nil)
			if finishErr != nil {
				return nil, finishErr
			}
			if done {
				order["status"] = 1
			}
		}
	}
	r.Response.WriteJson(gf.Success().SetData(map[string]any{"status": order["status"]}))
	return &PayCheckRes{}, nil
}
func (*Payment) PayResult(ctx context.Context, req *PayResultReq) (*PayResultRes, error) {
	user, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	r := g.RequestFromCtx(ctx)
	if user == nil {
		r.Response.RedirectTo("/suxinvideo/login")
		return &PayResultRes{}, nil
	}
	order, err := one(ctx, "SELECT order_no,title,amount,status FROM sx_order WHERE order_no=? AND user_id=?", req.OrderNo, user["id"])
	if err != nil {
		return nil, err
	}
	if order == nil {
		notFound(ctx)
		return &PayResultRes{}, nil
	}
	types, _ := all(ctx, "SELECT id,name FROM sx_type WHERE pid=0 AND status=1 ORDER BY sort LIMIT 30")
	render(ctx, "支付结果", types, payResultThemeBody, accountPageData(user, map[string]any{"Order": order}))
	return &PayResultRes{}, nil
}
func (*Payment) PayNotify(ctx context.Context, req *PayNotifyReq) (*PayNotifyRes, error) {
	r := g.RequestFromCtx(ctx)
	number, trade, money, ok := verifyProviderNotify(ctx, req.Channel, r)
	if !ok || number != req.OrderNo {
		r.Response.WriteStatus(http.StatusForbidden, "fail")
		return &PayNotifyRes{}, nil
	}
	order, err := one(ctx, "SELECT pay_type FROM sx_order WHERE order_no=?", number)
	if err != nil {
		return nil, err
	}
	if order == nil || gconv.String(order["pay_type"]) != req.Channel {
		r.Response.WriteStatus(http.StatusForbidden, "fail")
		return &PayNotifyRes{}, nil
	}
	done, err := completeOrder(ctx, number, trade, &money)
	if err != nil {
		return nil, err
	}
	if !done {
		r.Response.WriteStatus(http.StatusConflict, "fail")
		return &PayNotifyRes{}, nil
	}
	if req.Channel == "wxpay" {
		r.Response.Header().Set("Content-Type", "text/xml")
		r.Response.Write("<xml><return_code>SUCCESS</return_code><return_msg>OK</return_msg></xml>")
	} else {
		r.Response.Write("success")
	}
	return &PayNotifyRes{}, nil
}
func completeOrder(ctx context.Context, orderNo, tradeNo string, money *float64) (bool, error) {
	if tradeNo == "" {
		return false, errors.New("交易号为空")
	}
	changed := false
	err := g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		record, err := tx.GetOne("SELECT * FROM sx_order WHERE order_no=? FOR UPDATE", orderNo)
		if err != nil {
			return err
		}
		if record == nil {
			return errors.New("订单不存在")
		}
		order := gconv.Map(record)
		if gconv.Int(order["status"]) == 1 {
			if gconv.String(order["trade_no"]) == tradeNo {
				changed = true
				return nil
			}
			return errors.New("交易号不匹配")
		}
		if gconv.Int(order["status"]) != 0 {
			return errors.New("订单状态无效")
		}
		if money != nil && math.Abs(*money-gconv.Float64(order["amount"])) > 0.01 {
			return errors.New("支付金额不匹配")
		}
		if len(tradeNo) > 60 {
			tradeNo = tradeNo[:60]
		}
		if _, err = tx.Exec("UPDATE sx_order SET status=1,trade_no=?,paid_time=? WHERE id=? AND status=0", tradeNo, time.Now().Unix(), order["id"]); err != nil {
			return err
		}
		userRecord, err := tx.GetOne("SELECT id,points,vip_expire FROM sx_user WHERE id=? FOR UPDATE", order["user_id"])
		if err != nil {
			return err
		}
		if userRecord == nil {
			return errors.New("用户不存在")
		}
		goodsRecord, err := tx.GetOne("SELECT points,days FROM sx_goods WHERE id=?", order["goods_id"])
		if err != nil {
			return err
		}
		if goodsRecord != nil {
			user := gconv.Map(userRecord)
			goods := gconv.Map(goodsRecord)
			days := gconv.Int64(goods["days"])
			if days > 0 {
				base := max(time.Now().Unix(), gconv.Int64(user["vip_expire"]))
				if _, err = tx.Exec("UPDATE sx_user SET vip_expire=? WHERE id=?", base+days*86400, user["id"]); err != nil {
					return err
				}
			}
			points := gconv.Int64(goods["points"])
			if points > 0 {
				if _, err = tx.Exec("UPDATE sx_user SET points=points+? WHERE id=?", points, user["id"]); err != nil {
					return err
				}
			}
		}
		changed = true
		return nil
	})
	return changed, err
}
func (*Payment) BuyVod(ctx context.Context, req *BuyVodReq) (*BuyVodRes, error) {
	r := g.RequestFromCtx(ctx)
	user, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if user == nil {
		badRequest(r, "请先登录")
		return &BuyVodRes{}, nil
	}
	vod, err := one(ctx, "SELECT id,points FROM sx_vod WHERE id=? AND "+publicVodCondition(ctx, "")+" AND points>0", req.VodID)
	if err != nil {
		return nil, err
	}
	if vod == nil {
		badRequest(r, "影片不存在或无需购买")
		return &BuyVodRes{}, nil
	}
	err = g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		purchased, err := tx.GetOne("SELECT id FROM sx_user_vod WHERE user_id=? AND vod_id=?", user["id"], req.VodID)
		if err != nil {
			return err
		}
		if purchased != nil {
			return nil
		}
		record, err := tx.GetOne("SELECT points FROM sx_user WHERE id=? FOR UPDATE", user["id"])
		if err != nil {
			return err
		}
		if record == nil || gconv.Int64(record["points"]) < gconv.Int64(vod["points"]) {
			return errors.New("积分不足")
		}
		if _, err = tx.Exec("UPDATE sx_user SET points=points-? WHERE id=?", vod["points"], user["id"]); err != nil {
			return err
		}
		_, err = tx.Exec("INSERT INTO sx_user_vod(user_id,vod_id,points,created) VALUES(?,?,?,?)", user["id"], req.VodID, vod["points"], time.Now().Unix())
		return err
	})
	if err != nil {
		badRequest(r, err.Error())
		return &BuyVodRes{}, nil
	}
	r.Response.WriteJson(gf.Success().SetData(true))
	return &BuyVodRes{}, nil
}

var _ = fmt.Sprintf
var _ = strings.TrimSpace
