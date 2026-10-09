package suxinvideo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestLivePagesUseIndependentPlayerInAllThemes(t *testing.T) {
	for _, theme := range []string{"suxinlite", "suxinpro", "iqiyi", "guoguo"} {
		t.Run(theme, func(t *testing.T) {
			page := renderAccountTheme(t, theme, liveSiteBody, false)
			document, err := html.Parse(strings.NewReader(page))
			if err != nil {
				t.Fatal(err)
			}
			var video, stage, options, dock, settings, controls *html.Node
			links, assetScript := 0, false
			brandWalk(document, func(node *html.Node) {
				if node.Type != html.ElementNode {
					return
				}
				if node.Data == "a" && brandNodeAttribute(node, "href") == "/suxinvideo/live" {
					links++
				}
				if brandNodeAttribute(node, "id") == "liveVideo" {
					video = node
				}
				if brandNodeAttribute(node, "id") == "liveStage" {
					stage = node
				}
				switch brandNodeAttribute(node, "id") {
				case "liveOptions":
					options = node
				case "liveOptionsDock":
					dock = node
				case "liveSettings":
					settings = node
				case "liveControls":
					controls = node
				}
				if node.Data == "script" && strings.HasPrefix(brandNodeAttribute(node, "src"), "/suxinvideo/live/asset?file=live.js") {
					assetScript = true
				}
			})
			if video == nil || video.Data != "video" || video.Parent != stage {
				t.Fatal("live video must be inside its own fullscreen stage")
			}
			if options == nil || dock == nil || options.Parent != dock || controls == nil || controls.Parent != stage || settings == nil || settings.Parent != stage {
				t.Fatal("line and quality controls must start outside the video, with a settings panel available inside fullscreen")
			}
			if links < 2 || !assetScript {
				t.Fatal("desktop/mobile navigation and independent live player script are required")
			}
			for _, id := range []string{"suxinplayer", "epGrid", "sx-play-error", "nextEpisode"} {
				if strings.Contains(page, `id="`+id+`"`) {
					t.Fatalf("live page accidentally included VOD control %s", id)
				}
			}
			if dir := os.Getenv("SUXIN_LIVE_FIXTURE_DIR"); dir != "" {
				if err = os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(dir, theme+"-live.html"), []byte(page), 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
