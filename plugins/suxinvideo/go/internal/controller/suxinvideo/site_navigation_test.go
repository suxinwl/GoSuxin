package suxinvideo

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestThemeRemoteNavigationAndCards(t *testing.T) {
	names := []string{"短剧", "奈飞Netflix", "电影", "电视剧", "动漫", "综艺", "高清韩剧", "体育"}
	types := make([]row, 0, len(names))
	navigation := make([]map[string]any, 0, len(names))
	for i, name := range names {
		link := "/suxinvideo/channel?id=" + strconv.Itoa(i+1)
		types = append(types, row{"id": i + 1, "name": name, "url": link})
		navigation = append(navigation, map[string]any{"ID": i + 1, "Name": name, "URL": link, "Children": []row{}})
	}
	film := row{"id": 99999999, "name": "远端测试影片", "pic": "/poster.svg", "year": "2026", "remarks": "更新中", "link": "/suxinvideo/provider/detail?id=123456"}
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
				"Title": "首页", "SiteName": "小柒影视", "Theme": theme, "SiteMode": "cms",
				"SiteLogo": "/suxinvideo/asset?theme=suxinlite&file=favicon.svg",
				"NavTypes": types, "NavItems": navigation, "HeroJSON": template.JS(`[]`),
				"ThemeJSON": template.JS(strconv.Quote(theme)), "HeroFirst": homeHero{},
				"Recent": []row{film}, "Hot": []row{film}, "FullWidth": true,
				"RecentMoreURL": "/suxinvideo/channel?id=0&order=time", "HotMoreURL": "/suxinvideo/channel?id=0&order=hits",
			}
			var body bytes.Buffer
			if err = page.ExecuteTemplate(&body, "body", data); err != nil {
				t.Fatal(err)
			}
			data["Body"] = template.HTML(body.String())
			var output bytes.Buffer
			if err = page.ExecuteTemplate(&output, "page", data); err != nil {
				t.Fatal(err)
			}
			html := output.String()
			if !strings.Contains(html, `<span class="sx-brand-name">小柒影视</span>`) || !strings.Contains(html, `aria-controls="sx-mobile-navigation"`) {
				t.Fatal("mobile brand text or accessible navigation controls missing")
			}
			for i, name := range names {
				link := `href="/suxinvideo/channel?id=` + strconv.Itoa(i+1) + `"`
				if strings.Count(html, link) != 2 || strings.Count(html, name) != 2 {
					t.Fatalf("desktop and mobile channel %q not rendered together", name)
				}
			}
			filmLinks := strings.Count(html, `href="/suxinvideo/provider/detail?id=123456"`)
			linksValid := filmLinks == 2
			if theme == "guoguo" {
				// Posters, titles and hover previews each preserve the provider URL.
				linksValid = filmLinks >= 2
			}
			if !strings.Contains(html, "最近更新") || !strings.Contains(html, "热播") || !linksValid {
				t.Fatal("recent/hot cards do not preserve their actual film link")
			}
			if strings.Contains(html, "detail?id=99999999") || strings.Contains(html, "#ZgotmplZ") || strings.Contains(html, "<no value>") {
				t.Fatal("remote ID was treated as a local film or unresolved template URL")
			}
			// Optional rendered fixtures let the browser measure real responsive
			// CSS against the serving theme assets without a DB or server restart.
			if dir := os.Getenv("SUXIN_LAYOUT_FIXTURE_DIR"); dir != "" {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, theme+"-header-layout.html"), output.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestSiteLinksRejectForeignAndAmbiguousPaths(t *testing.T) {
	for _, link := range []string{"https://evil.example/suxinvideo/detail", "//evil.example/suxinvideo/detail", "javascript:alert(1)", "/suxinvideo/../admin", "/suxinvideo/%2e%2e/admin", "/suxinvideo/%2f%2fevil", "/suxinvideo\\admin", "/suxinvideo/%5cadmin", "/suxinvideo/%0aadmin"} {
		if siteVodURL(row{"id": 12, "link": link}) != "/suxinvideo/detail?id=12" {
			t.Fatalf("untrusted card link was accepted: %q", link)
		}
	}
	if siteVodURL(row{"id": 12}) != "/suxinvideo/detail?id=12" || siteNavURL(row{"id": 8}) != "/suxinvideo/type?id=8" {
		t.Fatal("local film/category links no longer work")
	}
}

func TestHomeImagePreparationUsesRequestCopies(t *testing.T) {
	snapshot := []row{{"id": 1, "pic": "https://example.test/original.jpg"}}
	copy := copyHomeVods(snapshot)
	copy[0]["pic"] = "/suxinvideo/image?sig=test"
	if snapshot[0]["pic"] != "https://example.test/original.jpg" {
		t.Fatal("request image changes polluted the shared provider snapshot")
	}
}
