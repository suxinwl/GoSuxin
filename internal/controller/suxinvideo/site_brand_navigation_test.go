package suxinvideo

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func brandNodeAttribute(node *html.Node, name string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == name {
			return attribute.Val
		}
	}
	return ""
}

func brandWalk(node *html.Node, visit func(*html.Node)) {
	visit(node)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		brandWalk(child, visit)
	}
}

func TestAccountHeadingUsesSiteBrandImage(t *testing.T) {
	for _, theme := range []string{"suxinlite", "suxinpro", "iqiyi", "guoguo"} {
		for pageName, body := range map[string]string{"login": loginThemeBody, "register": registerThemeBody} {
			for name, logo := range map[string]string{
				"bundled": defaultLogoURL, "empty-setting": "", "custom-setting": "https://example.test/custom-logo.png",
			} {
				t.Run(theme+"/"+pageName+"/"+name, func(t *testing.T) {
					page := renderAccountTheme(t, theme, body, false, logo)
					document, err := html.Parse(strings.NewReader(page))
					if err != nil {
						t.Fatal(err)
					}
					var images []*html.Node
					brandWalk(document, func(node *html.Node) {
						if node.Type == html.ElementNode && node.Data == "img" && brandNodeAttribute(node, "class") == "sx-auth-brand-logo" {
							images = append(images, node)
						}
					})
					if len(images) != 1 || images[0].Parent == nil || images[0].Parent.Data != "h2" {
						t.Fatal("the account heading must contain one actual site image")
					}
					image := images[0]
					want := logo
					if want == "" {
						want = defaultLogoURL
					}
					if brandNodeAttribute(image, "src") != want || brandNodeAttribute(image, "alt") != "小柒影视 LOGO" {
						t.Fatal("account logo did not preserve its configured image or accessible name")
					}
					if brandNodeAttribute(image, "width") != "32" || brandNodeAttribute(image, "height") != "32" || !strings.Contains(brandNodeAttribute(image, "onerror"), defaultLogoURL) || !strings.Contains(brandNodeAttribute(image, "onerror"), "this.onerror=null") {
						t.Fatal("account logo needs bounded dimensions and a one-time bundled fallback")
					}
					if name == "bundled" {
						if dir := os.Getenv("SUXIN_LAYOUT_FIXTURE_DIR"); dir != "" {
							if err := os.MkdirAll(dir, 0700); err != nil {
								t.Fatal(err)
							}
							if err := os.WriteFile(filepath.Join(dir, theme+"-"+pageName+"-brand.html"), []byte(page), 0600); err != nil {
								t.Fatal(err)
							}
						}
					}
				})
			}
		}
	}
}

func TestThemesHaveNoExtraTopicNavigation(t *testing.T) {
	for _, theme := range []string{"suxinlite", "suxinpro", "iqiyi", "guoguo"} {
		t.Run(theme, func(t *testing.T) {
			// Guoguo's extra topic item was in its home navigation below the
			// banner, so render the actual home body as well as the outer page.
			page := renderAccountTheme(t, theme, homeThemeBody(theme), false)
			document, err := html.Parse(strings.NewReader(page))
			if err != nil {
				t.Fatal(err)
			}
			brandWalk(document, func(node *html.Node) {
				if node.Type != html.ElementNode || node.Data != "nav" {
					return
				}
				brandWalk(node, func(link *html.Node) {
					if link.Type != html.ElementNode || link.Data != "a" {
						return
					}
					u, err := url.Parse(brandNodeAttribute(link, "href"))
					if err == nil && u.Path == "/suxinvideo/topic" {
						t.Error("an extra topic link remains in the desktop or mobile navigation")
					}
				})
			})
			if theme == "guoguo" && !accountElement(t, page, "a", "href", "/suxinvideo/favorites") {
				t.Fatal("the personal film library must remain available")
			}
		})
	}
}
