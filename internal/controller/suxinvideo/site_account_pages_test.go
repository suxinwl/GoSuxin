package suxinvideo

import (
	"bytes"
	"html/template"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// Render the actual account body inside each shipped theme, including Guoguo's
// normal body transformation, so the SMTP gate cannot be bypassed by a theme.
func renderAccountTheme(t *testing.T, theme, body string, emailEnabled bool, logos ...string) string {
	t.Helper()
	page, err := themePages[theme].Clone()
	if err != nil {
		t.Fatal(err)
	}
	if theme == "guoguo" {
		body = guoguoBody(body)
	}
	page, err = page.New("body").Parse(body)
	if err != nil {
		t.Fatal(err)
	}
	data := row{
		"SiteName": "小柒影视", "Title": "账户", "Theme": theme, "SiteMode": "cms",
		"SiteLogo": defaultLogoURL, "SiteFavicon": defaultFaviconURL,
		"RegistrationEnabled": true, "RegistrationRequiresEmailCode": emailEnabled,
		"PasswordResetEnabled": emailEnabled,
		"CaptchaWidget":        template.HTML(`<div class="captcha"><input name="captcha" required><img id="sx-captcha-img" src="/suxinvideo/captcha" alt="图形验证码"></div>`),
	}
	if len(logos) > 0 {
		data["SiteLogo"] = logos[0]
	}
	var fragment bytes.Buffer
	if err = page.ExecuteTemplate(&fragment, "body", data); err != nil {
		t.Fatal(err)
	}
	data["Body"] = template.HTML(fragment.String())
	var output bytes.Buffer
	if err = page.ExecuteTemplate(&output, "page", data); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func accountElement(t *testing.T, document, tag, attribute, value string) bool {
	t.Helper()
	node, err := html.Parse(strings.NewReader(document))
	if err != nil {
		t.Fatal(err)
	}
	var contains func(*html.Node) bool
	contains = func(node *html.Node) bool {
		if node.Type == html.ElementNode && node.Data == tag {
			for _, attr := range node.Attr {
				if attr.Key == attribute && attr.Val == value {
					return true
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if contains(child) {
				return true
			}
		}
		return false
	}
	return contains(node)
}

func TestAccountTemplatesSMTPAvailability(t *testing.T) {
	for _, theme := range []string{"suxinlite", "suxinpro", "iqiyi", "guoguo"} {
		for _, enabled := range []bool{false, true} {
			name := "without-smtp"
			if enabled {
				name = "with-smtp"
			}
			t.Run(theme+"/"+name+"/register", func(t *testing.T) {
				page := renderAccountTheme(t, theme, registerThemeBody, enabled)
				for _, required := range []struct{ tag, attribute, value string }{
					{"form", "id", "register-form"}, {"input", "name", "email"},
					{"input", "name", "password"}, {"input", "name", "captcha"},
					{"img", "id", "sx-captcha-img"}, {"button", "type", "submit"},
				} {
					if !accountElement(t, page, required.tag, required.attribute, required.value) {
						t.Errorf("required registration element missing: %+v", required)
					}
				}
				if accountElement(t, page, "input", "name", "email_code") != enabled || accountElement(t, page, "button", "id", "send-code") != enabled {
					t.Fatal("email code and send button do not match SMTP availability")
				}
				if strings.Contains(page, "/suxinvideo/sendcode") != enabled {
					t.Fatal("the send-code request script must only exist with email capability")
				}
				if !enabled && (strings.Contains(page, "getElementById('send-code')") || strings.Contains(page, "发送验证码")) {
					t.Fatal("disabled email verification must not retain orphan controls or script bindings")
				}
			})
			t.Run(theme+"/"+name+"/login", func(t *testing.T) {
				page := renderAccountTheme(t, theme, loginThemeBody, enabled)
				if accountElement(t, page, "a", "href", "/suxinvideo/forgot") != enabled {
					t.Fatal("email password recovery link does not match SMTP availability")
				}
				if !accountElement(t, page, "form", "action", "/suxinvideo/login") || !accountElement(t, page, "input", "name", "captcha") || !accountElement(t, page, "a", "href", "/suxinvideo/register") {
					t.Fatal("ordinary login, graph captcha and registration must remain available")
				}
			})
			t.Run(theme+"/"+name+"/forgot", func(t *testing.T) {
				page := renderAccountTheme(t, theme, forgotThemeBody, enabled)
				if accountElement(t, page, "form", "id", "reset-form") != enabled || accountElement(t, page, "button", "id", "reset-code") != enabled {
					t.Fatal("password reset form does not match SMTP availability")
				}
				if !accountElement(t, page, "a", "href", "/suxinvideo/login") {
					t.Fatal("the recovery page must always provide a return-to-login link")
				}
				if enabled && !accountElement(t, page, "input", "name", "captcha") {
					t.Fatal("enabled email recovery requires graph captcha")
				}
				if !enabled && (!strings.Contains(page, "联系管理员") || strings.Contains(page, "/suxinvideo/sendcode") || accountElement(t, page, "input", "name", "password")) {
					t.Fatal("unavailable recovery must explain administrator assistance and provide no submit inputs")
				}
			})
		}
	}
}
