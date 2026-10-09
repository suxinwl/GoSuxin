package suxinvideo

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
)

func captchaProvider(ctx context.Context) string {
	provider := setting(ctx, "captcha_provider", "graph")
	switch provider {
	case "turnstile":
		if setting(ctx, "turnstile_site_key", "") != "" && setting(ctx, "turnstile_secret", "") != "" {
			return provider
		}
	case "geetest":
		if setting(ctx, "geetest_id", "") != "" && setting(ctx, "geetest_key", "") != "" {
			return provider
		}
	}
	return "graph"
}

func captchaWidget(ctx context.Context) template.HTML {
	switch captchaProvider(ctx) {
	case "turnstile":
		key := html.EscapeString(setting(ctx, "turnstile_site_key", ""))
		return template.HTML(fmt.Sprintf(`<div class="cf-turnstile" data-sitekey="%s"></div><script src="https://challenges.cloudflare.com/turnstile/v0/api.js" async defer></script>`, key))
	case "geetest":
		id := html.EscapeString(setting(ctx, "geetest_id", ""))
		return template.HTML(fmt.Sprintf(`<div id="sx-geetest" data-captcha-id="%s"></div><input type="hidden" name="lot_number"><input type="hidden" name="captcha_output"><input type="hidden" name="pass_token"><input type="hidden" name="gen_time"><script src="https://static.geetest.com/v4/gt4.js"></script><script>initGeetest4({captchaId:document.getElementById('sx-geetest').dataset.captchaId,product:'float'},function(c){window.sxGeetest=c;c.appendTo('#sx-geetest');c.onSuccess(function(){var v=c.getValidate();if(!v)return;var f=document.getElementById('sx-geetest').closest('form');['lot_number','captcha_output','pass_token','gen_time'].forEach(function(k){f.elements[k].value=v[k]||''})})})</script>`, id))
	default:
		return template.HTML(`<div class="sx-verify"><div style="display:flex;gap:8px;align-items:center"><input type="text" name="captcha" class="sx-input" placeholder="请输入验证码" autocomplete="off" maxlength="4" required><img id="sx-captcha-img" class="sx-captcha-img" src="/suxinvideo/captcha" alt="验证码" title="看不清？点击刷新" style="height:42px;cursor:pointer" onclick="this.src='/suxinvideo/captcha?t='+Date.now()"></div></div>`)
	}
}

func verifyCMSCaptcha(r *ghttp.Request, graphInput string) bool {
	ctx := r.Context()
	switch captchaProvider(ctx) {
	case "turnstile":
		token := strings.TrimSpace(r.Get("cf-turnstile-response").String())
		if token == "" || len(token) > 2048 {
			return false
		}
		values := url.Values{"secret": {setting(ctx, "turnstile_secret", "")}, "response": {token}, "remoteip": {cmsClientIP(ctx, r)}}
		response, err := (&http.Client{Timeout: 8 * time.Second}).PostForm("https://challenges.cloudflare.com/turnstile/v0/siteverify", values)
		if err != nil {
			return false
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return false
		}
		var result struct {
			Success bool `json:"success"`
		}
		return json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&result) == nil && result.Success
	case "geetest":
		lot := strings.TrimSpace(r.Get("lot_number").String())
		output := strings.TrimSpace(r.Get("captcha_output").String())
		pass := strings.TrimSpace(r.Get("pass_token").String())
		gen := strings.TrimSpace(r.Get("gen_time").String())
		if lot == "" || output == "" || pass == "" || gen == "" || len(lot) > 200 || len(output) > 4000 || len(pass) > 200 || len(gen) > 50 {
			return false
		}
		mac := hmac.New(sha256.New, []byte(setting(ctx, "geetest_key", "")))
		_, _ = mac.Write([]byte(lot))
		values := url.Values{"lot_number": {lot}, "captcha_output": {output}, "pass_token": {pass}, "gen_time": {gen}, "sign_token": {hex.EncodeToString(mac.Sum(nil))}}
		endpoint := "https://gcaptcha4.geetest.com/validate?captcha_id=" + url.QueryEscape(setting(ctx, "geetest_id", ""))
		response, err := (&http.Client{Timeout: 8 * time.Second}).PostForm(endpoint, values)
		if err != nil {
			return false
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return false
		}
		var result struct {
			Result string `json:"result"`
		}
		return json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&result) == nil && result.Result == "success"
	default:
		return verifyCaptcha(r, graphInput)
	}
}
