package suxinvideo

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func accountLayoutFixture(t *testing.T, theme, body string, data row) string {
	t.Helper()
	page, err := themePages[theme].Clone()
	if err != nil {
		t.Fatal(err)
	}
	page, err = page.New("body").Parse(guoguoBody(body))
	if err != nil {
		t.Fatal(err)
	}
	data["SiteName"], data["Title"], data["Theme"], data["SiteMode"] = "小柒影视", "个人中心", theme, "cms"
	data["SiteLogo"], data["SiteFavicon"], data["Logged"] = defaultLogoURL, defaultFaviconURL, true
	var fragment, output bytes.Buffer
	if err := page.ExecuteTemplate(&fragment, "body", data); err != nil {
		t.Fatal(err)
	}
	data["Body"] = template.HTML(fragment.String())
	if err := page.ExecuteTemplate(&output, "page", data); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

// Check each real account template, navigation and form contract together.
// Opt-in files are controlled HTML fixtures, not authenticated production pages.
func TestAccountLayoutPagesKeepNavigationAndFormContracts(t *testing.T) {
	user := row{"id": 999, "name": "个人中心排版检查账号", "email": "layout." + strings.Repeat("long-address-", 12) + "@example.invalid", "avatar": defaultLogoURL, "points": 1200, "vip_expire": time.Now().Add(72 * time.Hour).Unix()}
	vods := []row{}
	for i := 0; i < 8; i++ {
		vods = append(vods, row{"id": i + 1, "name": "这是一个用于检查个人中心长标题的影片名称", "pic": defaultLogoURL, "episode": 12, "episode_index": 11, "year": 2026, "remarks": "更新至第12集"})
	}
	orders := []row{
		{"order_no": "SX202610031234567890123456789", "title": "年度会员及积分充值套餐", "amount": "199.00", "pay_type": "支付宝", "status": 1, "created": 1791019200},
		{"order_no": "SX202610034567890123456789012", "title": "月度会员", "amount": "19.90", "pay_type": "微信支付", "status": 0, "created": 1791020000},
		{"order_no": "SX202610037890123456789012345", "title": "已取消的订单", "amount": "9.90", "pay_type": "易支付", "status": 2, "created": 1791030000},
	}
	cases := []struct{ name, body, active string }{
		{"center", centerThemeBody, "center"}, {"history", historyThemeBody, "history"},
		{"favorites", favoritesThemeBody, "favorites"}, {"orders", ordersThemeBody, "orders"},
		{"pay", payIndexThemeBody, "pay"}, {"cashier", payCashierThemeBody, "pay"},
		{"result", payResultThemeBody, "orders"},
	}
	for _, theme := range []string{"suxinlite", "suxinpro", "iqiyi", "guoguo"} {
		for _, tc := range cases {
			t.Run(theme+"/"+tc.name, func(t *testing.T) {
				data := row(accountPageData(user, map[string]any{
					"SignPoints": "5", "Records": vods, "Vods": vods, "Orders": orders, "Order": orders[1], "OrderJSON": orders[1]["order_no"],
					"Goods":   []row{{"id": 1, "name": "30 天 VIP", "price": "19.90", "days": 30, "points": 100}, {"id": 2, "name": "年度会员及积分充值套餐", "price": "199.00", "days": 365, "points": 1000}, {"id": 3, "name": "积分充值", "price": "9.90", "points": 200}},
					"Methods": []payMethod{{"alipay", "支付宝（官方）"}, {"wxpay", "微信支付（官方）"}}, "Checkout": row{"qr_image": defaultLogoURL, "qr": "fixture-payment-address"},
				}))
				page := accountLayoutFixture(t, theme, tc.body, data)
				if !strings.Contains(page, `aria-current="page" href="/suxinvideo/`+tc.active+`"`) || strings.Count(page, `aria-current="page"`) != 1 {
					t.Fatal("exactly one current account navigation item is required")
				}
				for _, path := range []string{"center", "history", "favorites", "orders", "pay"} {
					if !accountElement(t, page, "a", "href", "/suxinvideo/"+path) {
						t.Fatalf("account navigation missing %s", path)
					}
				}
				if tc.name == "center" {
					for _, name := range []string{"name", "oldpwd", "newpwd", "repwd"} {
						if !accountElement(t, page, "input", "name", name) {
							t.Fatalf("account form field missing %s", name)
						}
					}
					if !accountElement(t, page, "form", "action", "/suxinvideo/sign") || !accountElement(t, page, "form", "action", "/suxinvideo/account") {
						t.Fatal("account POST actions must remain unchanged")
					}
				}
				if out := os.Getenv("SUXIN_ACCOUNT_FIXTURE_DIR"); out != "" {
					if err := os.MkdirAll(out, 0755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(out, theme+"-"+tc.name+".html"), []byte(page), 0644); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}
