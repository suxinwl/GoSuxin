package suxinvideo

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

func TestThemePagesRender(t *testing.T) {
	for theme, base := range themePages {
		for name, body := range map[string]string{
			"home":      homeThemeBody(theme),
			"play":      playThemeBody,
			"type":      catalogTypeBody,
			"search":    catalogSearchBody,
			"login":     loginThemeBody,
			"register":  registerThemeBody,
			"forgot":    forgotThemeBody,
			"center":    centerThemeBody,
			"history":   historyThemeBody,
			"pay":       payIndexThemeBody,
			"cashier":   payCashierThemeBody,
			"payResult": payResultThemeBody,
			"article":   articleThemeBody,
		} {
			t.Run(theme+"/"+name, func(t *testing.T) {
				if theme == "guoguo" {
					body = guoguoBody(body)
				}
				page, err := base.Clone()
				if err != nil {
					t.Fatal(err)
				}
				page, err = page.New("body").Parse(body)
				if err != nil {
					t.Fatal(err)
				}
				data := map[string]any{
					"Title": "测试", "SiteName": "速信影视CMS", "Theme": theme,
					"SiteMode": "cms", "HeroJSON": template.JS(`[]`), "ThemeJSON": template.JS(`"` + theme + `"`),
					"SiteLogo": "https://example.com/logo.png", "SiteFavicon": "/suxinvideo/asset?theme=" + theme + "&file=favicon.svg",
					"HeroFirst": homeHero{}, "PlayerJSON": template.JS(`{"sources":[]}`),
					"Vod": row{}, "Comments": []row{}, "Sources": []playSource{}, "Related": []row{},
					"User": row{}, "Checkout": map[string]any{}, "SignPoints": "5",
					"ID": 0, "Order": "time", "Page": 1, "Pages": 1,
				}
				if name == "cashier" || name == "payResult" {
					data["Order"] = row{}
				}
				var output bytes.Buffer
				if err = page.ExecuteTemplate(&output, "body", data); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(output.String(), "<no value>") {
					t.Fatalf("unresolved template data: %s", output.String())
				}
				data["Body"] = template.HTML(output.String())
				var whole bytes.Buffer
				if err = page.ExecuteTemplate(&whole, "page", data); err != nil {
					t.Fatal(err)
				}
				headerStart := strings.Index(whole.String(), "<header")
				headerEnd := strings.Index(whole.String(), "</header>")
				if headerStart < 0 || headerEnd < headerStart {
					t.Fatal("theme header missing")
				}
				header := whole.String()[headerStart:headerEnd]
				if !strings.Contains(header, "速信影视CMS") || !strings.Contains(header, "https://example.com/logo.png") {
					t.Fatalf("theme header does not use configured site name and logo: %s", header)
				}
				if !strings.Contains(whole.String(), `rel="icon" href="/suxinvideo/asset?theme=`) {
					t.Fatal("theme favicon missing")
				}
			})
		}
	}
}

func TestFilmDescription(t *testing.T) {
	got := filmDescription("<p>第一集&nbsp;<b>简介</b></p><br>下一行")
	if got != "第一集 简介 下一行" {
		t.Fatalf("unexpected description: %q", got)
	}
}
