package suxinvideo

import (
	"context"
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

const tronUSDTContract = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"

var paymentHTTP = &http.Client{Timeout: 15 * time.Second}

func signedFields(fields url.Values, key string, wx bool) string {
	keys := make([]string, 0, len(fields))
	for k, vals := range fields {
		if k != "sign" && k != "sign_type" && len(vals) > 0 && vals[0] != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+fields.Get(k))
	}
	base := strings.Join(parts, "&") + key
	if wx {
		base = strings.Join(parts, "&") + "&key=" + key
	}
	digest := md5.Sum([]byte(base))
	sign := hex.EncodeToString(digest[:])
	if wx {
		return strings.ToUpper(sign)
	}
	return sign
}

func createProviderPayment(ctx context.Context, channel, orderNo, title string, amount float64, notify, back, clientIP, userAgent string) (map[string]any, error) {
	switch channel {
	case "epay", "codepay":
		prefix := channel + "_"
		gateway := setting(ctx, prefix+"gateway", "")
		if channel == "codepay" && gateway == "" {
			gateway = "https://xpay.shw1.com/xpay/epay/submit.php"
		}
		pid, key := setting(ctx, prefix+"pid", ""), setting(ctx, prefix+"key", "")
		parsed, err := url.Parse(gateway)
		if err != nil || parsed == nil || parsed.Scheme != "https" || parsed.Host == "" || pid == "" || key == "" {
			return nil, errors.New("支付通道未完整配置 HTTPS 网关、商户 ID 和密钥")
		}
		fields := url.Values{"pid": {pid}, "type": {setting(ctx, prefix+"channel", "alipay")}, "out_trade_no": {orderNo}, "notify_url": {notify}, "return_url": {back}, "name": {title}, "money": {fmt.Sprintf("%.2f", amount)}, "sitename": {setting(ctx, "site_name", "速信影视CMS")}}
		fields.Set("sign", signedFields(fields, key, false))
		fields.Set("sign_type", "MD5")
		parsed.RawQuery = fields.Encode()
		return map[string]any{"type": "redirect", "url": parsed.String()}, nil
	case "usdt":
		address := setting(ctx, "usdt_address", "")
		rate := gconv.Float64(setting(ctx, "usdt_rate", "0"))
		if address == "" || rate <= 0 {
			return nil, errors.New("USDT 收款地址或汇率未配置")
		}
		order, err := one(ctx, "SELECT id FROM sx_order WHERE order_no=?", orderNo)
		if err != nil {
			return nil, err
		}
		if order == nil {
			return nil, errors.New("订单不存在")
		}
		value := math.Round((amount*rate+float64(gconv.Int64(order["id"])%40)*0.01)*100) / 100
		if err := execSQL(ctx, "UPDATE sx_order SET usdt_amount=? WHERE order_no=?", value, orderNo); err != nil {
			return nil, err
		}
		return map[string]any{"type": "qrcode", "qr": address, "address": address, "amount": fmt.Sprintf("%.2f", value), "tip": fmt.Sprintf("向此 TRC20 地址转入 %.2f USDT", value)}, nil
	case "wxpay":
		appid, mchid, key := setting(ctx, "wxpay_appid", ""), setting(ctx, "wxpay_mchid", ""), setting(ctx, "wxpay_key", "")
		if appid == "" || mchid == "" || key == "" {
			return nil, errors.New("微信支付未配置")
		}
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		fields := url.Values{"appid": {appid}, "mch_id": {mchid}, "nonce_str": {hex.EncodeToString(b)}, "body": {title}, "out_trade_no": {orderNo}, "total_fee": {strconv.Itoa(int(math.Round(amount * 100)))}, "spbill_create_ip": {clientIP}, "notify_url": {notify}, "trade_type": {"NATIVE"}}
		fields.Set("sign", signedFields(fields, key, true))
		body, err := wxRequest(fields)
		if err != nil {
			return nil, err
		}
		resp, err := paymentHTTP.Post("https://api.mch.weixin.qq.com/pay/unifiedorder", "text/xml", strings.NewReader(body))
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("微信支付网关 HTTP %d", resp.StatusCode)
		}
		payload, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		if err != nil {
			return nil, err
		}
		data, err := wxFields(payload)
		if err != nil {
			return nil, err
		}
		if data.Get("return_code") != "SUCCESS" || data.Get("result_code") != "SUCCESS" || data.Get("code_url") == "" {
			return nil, fmt.Errorf("微信下单失败: %s %s", data.Get("return_msg"), data.Get("err_code_des"))
		}
		return map[string]any{"type": "qrcode", "qr": data.Get("code_url"), "tip": "请使用微信扫码支付"}, nil
	case "alipay", "alipayf2f":
		appid, private := setting(ctx, "alipay_appid", ""), setting(ctx, "alipay_private_key", "")
		if appid == "" || private == "" {
			return nil, errors.New("支付宝未配置")
		}
		method := "alipay.trade.page.pay"
		productCode := "FAST_INSTANT_TRADE_PAY"
		if channel == "alipay" {
			agent := strings.ToLower(userAgent)
			if strings.Contains(agent, "mobile") || strings.Contains(agent, "android") || strings.Contains(agent, "iphone") || strings.Contains(agent, "ipad") {
				method = "alipay.trade.wap.pay"
				productCode = "QUICK_WAP_WAY"
			}
		}
		if channel == "alipayf2f" {
			method = "alipay.trade.precreate"
		}
		biz := map[string]string{"out_trade_no": orderNo, "total_amount": fmt.Sprintf("%.2f", amount), "subject": title}
		if channel == "alipay" {
			biz["product_code"] = productCode
		}
		encoded, _ := json.Marshal(biz)
		fields := url.Values{"app_id": {appid}, "method": {method}, "format": {"JSON"}, "charset": {"utf-8"}, "sign_type": {"RSA2"}, "timestamp": {time.Now().Format("2006-01-02 15:04:05")}, "version": {"1.0"}, "notify_url": {notify}, "biz_content": {string(encoded)}}
		if channel == "alipay" {
			fields.Set("return_url", back)
		}
		sign, err := aliSign(fields, private)
		if err != nil {
			return nil, err
		}
		fields.Set("sign", sign)
		gateway := "https://openapi.alipay.com/gateway.do"
		if channel == "alipay" {
			return map[string]any{"type": "redirect", "url": gateway + "?" + fields.Encode()}, nil
		}
		resp, err := paymentHTTP.PostForm(gateway, fields)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		payload, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		if err != nil {
			return nil, err
		}
		var result struct {
			Precreate struct {
				Code    string `json:"code"`
				Message string `json:"sub_msg"`
				QR      string `json:"qr_code"`
			} `json:"alipay_trade_precreate_response"`
		}
		if err := json.Unmarshal(payload, &result); err != nil {
			return nil, err
		}
		if result.Precreate.Code != "10000" || result.Precreate.QR == "" {
			return nil, fmt.Errorf("支付宝下单失败: %s", result.Precreate.Message)
		}
		return map[string]any{"type": "qrcode", "qr": result.Precreate.QR, "tip": "请使用支付宝扫码支付"}, nil
	}
	return nil, errors.New("不支持的支付方式")
}

func checkUSDT(ctx context.Context, order row) string {
	address := setting(ctx, "usdt_address", "")
	expected := gconv.Float64(order["usdt_amount"])
	if address == "" || expected <= 0 {
		return ""
	}
	query := url.Values{"limit": {"100"}, "only_to": {"true"}, "contract_address": {tronUSDTContract}, "min_timestamp": {strconv.FormatInt((gconv.Int64(order["created"])-300)*1000, 10)}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.trongrid.io/v1/accounts/"+url.PathEscape(address)+"/transactions/trc20?"+query.Encode(), nil)
	if err != nil {
		return ""
	}
	if key := setting(ctx, "usdt_trongrid_key", ""); key != "" {
		request.Header.Set("TRON-PRO-API-KEY", key)
	}
	resp, err := paymentHTTP.Do(request)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ""
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
	if err != nil {
		return ""
	}
	var result struct {
		Data []struct {
			To            string `json:"to"`
			Value         string `json:"value"`
			TransactionID string `json:"transaction_id"`
			Token         struct {
				Address  string `json:"address"`
				Decimals int    `json:"decimals"`
			} `json:"token_info"`
		} `json:"data"`
	}
	if json.Unmarshal(payload, &result) != nil {
		return ""
	}
	for _, item := range result.Data {
		if !strings.EqualFold(item.To, address) || item.Token.Address != tronUSDTContract || item.Token.Decimals != 6 || item.TransactionID == "" {
			continue
		}
		units, err := strconv.ParseFloat(item.Value, 64)
		if err != nil || math.Abs(units/1e6-expected) >= 0.001 {
			continue
		}
		used, err := one(ctx, "SELECT id FROM sx_order WHERE trade_no=? AND order_no<>? LIMIT 1", item.TransactionID, order["order_no"])
		if err == nil && used == nil {
			return item.TransactionID
		}
	}
	return ""
}

func verifyProviderNotify(ctx context.Context, channel string, r *ghttp.Request) (string, string, float64, bool) {
	if channel == "epay" || channel == "codepay" || channel == "alipay" || channel == "alipayf2f" {
		if r.Request.ParseForm() != nil {
			return "", "", 0, false
		}
		fields := r.PostForm
		if channel == "epay" || channel == "codepay" {
			key := setting(ctx, channel+"_key", "")
			if key == "" || !strings.EqualFold(fields.Get("sign"), signedFields(fields, key, false)) || fields.Get("trade_status") != "TRADE_SUCCESS" || fields.Get("pid") != setting(ctx, channel+"_pid", "") {
				return "", "", 0, false
			}
			money, err := strconv.ParseFloat(fields.Get("money"), 64)
			return fields.Get("out_trade_no"), fields.Get("trade_no"), money, err == nil
		}
		if fields.Get("app_id") != setting(ctx, "alipay_appid", "") || (fields.Get("trade_status") != "TRADE_SUCCESS" && fields.Get("trade_status") != "TRADE_FINISHED") || !aliVerify(fields, setting(ctx, "alipay_public_key", "")) {
			return "", "", 0, false
		}
		money, err := strconv.ParseFloat(fields.Get("total_amount"), 64)
		return fields.Get("out_trade_no"), fields.Get("trade_no"), money, err == nil
	}
	if channel == "wxpay" {
		payload, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
		if err != nil {
			return "", "", 0, false
		}
		fields, err := wxFields(payload)
		if err != nil || fields.Get("return_code") != "SUCCESS" || fields.Get("result_code") != "SUCCESS" || fields.Get("appid") != setting(ctx, "wxpay_appid", "") || fields.Get("mch_id") != setting(ctx, "wxpay_mchid", "") {
			return "", "", 0, false
		}
		key := setting(ctx, "wxpay_key", "")
		if key == "" || fields.Get("sign") != signedFields(fields, key, true) {
			return "", "", 0, false
		}
		cents, err := strconv.Atoi(fields.Get("total_fee"))
		return fields.Get("out_trade_no"), fields.Get("transaction_id"), float64(cents) / 100, err == nil
	}
	return "", "", 0, false
}

func aliText(fields url.Values) string {
	keys := make([]string, 0, len(fields))
	for k, vals := range fields {
		if k != "sign" && k != "sign_type" && len(vals) > 0 && vals[0] != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+fields.Get(k))
	}
	return strings.Join(parts, "&")
}
func pemKey(raw, kind string) []byte {
	if strings.Contains(raw, "-----BEGIN") {
		return []byte(raw)
	}
	raw = strings.ReplaceAll(strings.ReplaceAll(raw, "\r", ""), "\n", "")
	return pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: mustBase64(raw)})
}
func mustBase64(raw string) []byte { data, _ := base64.StdEncoding.DecodeString(raw); return data }
func aliSign(fields url.Values, raw string) (string, error) {
	block, _ := pem.Decode(pemKey(raw, "RSA PRIVATE KEY"))
	if block == nil {
		return "", errors.New("支付宝私钥无效")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		parsed, e := x509.ParsePKCS8PrivateKey(block.Bytes)
		if e != nil {
			return "", err
		}
		var ok bool
		key, ok = parsed.(*rsa.PrivateKey)
		if !ok {
			return "", errors.New("支付宝私钥类型无效")
		}
	}
	digest := sha256.Sum256([]byte(aliText(fields)))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}
func aliVerify(fields url.Values, raw string) bool {
	signature, err := base64.StdEncoding.DecodeString(fields.Get("sign"))
	if err != nil {
		return false
	}
	block, _ := pem.Decode(pemKey(raw, "PUBLIC KEY"))
	if block == nil {
		return false
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return false
	}
	key, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return false
	}
	digest := sha256.Sum256([]byte(aliText(fields)))
	return rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature) == nil
}

func wxRequest(fields url.Values) (string, error) {
	var b strings.Builder
	b.WriteString("<xml>")
	for k, vals := range fields {
		if !identifier.MatchString(k) || len(vals) == 0 {
			return "", errors.New("微信参数无效")
		}
		b.WriteString("<" + k + ">")
		if err := xml.EscapeText(&b, []byte(vals[0])); err != nil {
			return "", err
		}
		b.WriteString("</" + k + ">")
	}
	b.WriteString("</xml>")
	return b.String(), nil
}
func wxFields(payload []byte) (url.Values, error) {
	var root struct {
		XMLName xml.Name `xml:"xml"`
		Fields  []struct {
			XMLName xml.Name
			Value   string `xml:",chardata"`
		} `xml:",any"`
	}
	if err := xml.Unmarshal(payload, &root); err != nil {
		return nil, err
	}
	if root.XMLName.Local != "xml" {
		return nil, errors.New("微信 XML 格式无效")
	}
	v := url.Values{}
	for _, f := range root.Fields {
		v.Set(f.XMLName.Local, f.Value)
	}
	return v, nil
}
