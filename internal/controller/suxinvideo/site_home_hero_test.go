package suxinvideo

import (
	"bytes"
	"encoding/json"
	"html/template"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestThemeHomeHeroArtwork(t *testing.T) {
	// Three distinct inputs cover a poster-only provider, a managed banner
	// linking to a category, and an image failure. Optional HTML fixtures let
	// a browser verify intrinsic aspect ratios, fallbacks and carousel races.
	heroes := []homeHero{
		{Name: "兰香如故", Pic: "/hero-portrait.jpg", Poster: "/hero-portrait.jpg", Link: "/suxinvideo/detail?id=12", Button: "立即播放", Category: "短剧", Year: "2026", Remarks: "全集", Description: "海报应完整显示，背景柔化处理。", Film: true},
		{Name: "动漫精选", Pic: "/hero-wide.svg", Link: "/suxinvideo/type?id=4", Button: "查看详情"},
		{Name: "备用横幅", Pic: "/hero-missing.jpg", Link: "/suxinvideo/type?id=1", Button: "查看详情"},
	}
	heroJSON, err := json.Marshal(heroes)
	if err != nil {
		t.Fatal(err)
	}
	for theme, base := range themePages {
		t.Run(theme, func(t *testing.T) {
			page, err := base.Clone()
			if err != nil {
				t.Fatal(err)
			}
			page, err = page.New("body").Parse(homeThemeBody(theme))
			if err != nil {
				t.Fatal(err)
			}
			data := map[string]any{
				"Title": "首页", "SiteName": "小柒影视", "Theme": theme, "SiteMode": "cms", "FullWidth": true,
				"NavTypes": []row{}, "NavItems": []map[string]any{}, "Hero": heroes, "HeroFirst": heroes[0],
				"HeroJSON": template.JS(heroJSON), "ThemeJSON": template.JS(strconv.Quote(theme)),
				"Recent": []row{}, "Hot": []row{}, "HomeBlocks": []row{}, "Links": []row{},
				"RecentMoreURL": "/suxinvideo/search?order=time", "HotMoreURL": "/suxinvideo/search?order=hits",
			}
			var body bytes.Buffer
			if err := page.ExecuteTemplate(&body, "body", data); err != nil {
				t.Fatal(err)
			}
			data["Body"] = template.HTML(body.String())
			var output bytes.Buffer
			if err := page.ExecuteTemplate(&output, "page", data); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(output.String(), "#ZgotmplZ") || strings.Contains(output.String(), "<no value>") || strings.Contains(output.String(), "????") {
				t.Fatal("hero output has an unresolved URL, value or broken Chinese label")
			}
			status, background := `class="sx-hero-image-notice"`, `sx-hero-background`
			if theme == "guoguo" {
				status, background = `class="guoguo-hero-image-notice"`, `reference-hero__shade`
			}
			if !strings.Contains(output.String(), status) || !strings.Contains(output.String(), background) {
				t.Fatal("hero has no independent background layer or image failure status")
			}
			if dir := os.Getenv("SUXIN_HERO_FIXTURE_DIR"); dir != "" {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, theme+"-hero.html"), output.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
