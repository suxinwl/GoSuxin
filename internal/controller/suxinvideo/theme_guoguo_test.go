package suxinvideo

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

// Keep the existing player contract intact when adding Guoguo's presentation.
func TestGuoguoPlaybackContract(t *testing.T) {
	for _, lock := range []string{"", "login", "vip", "buy"} {
		page, err := themePages["guoguo"].Clone()
		if err != nil {
			t.Fatal(err)
		}
		page, err = page.New("body").Parse(guoguoBody(playThemeBody))
		if err != nil {
			t.Fatal(err)
		}
		data := map[string]any{
			"Theme": "guoguo", "Vod": row{"id": 9, "name": "测试影片"},
			"Lock": lock, "PlayerJSON": template.JS(`{"sources":[]}`),
			"Comments": []row{}, "Sources": []playSource{}, "Related": []row{},
			"ThemeJSON": template.JS(`"guoguo"`),
		}
		var output bytes.Buffer
		if err = page.ExecuteTemplate(&output, "body", data); err != nil {
			t.Fatal(err)
		}
		html := output.String()
		for _, fragment := range []string{`id="playerPanel"`, `id="playSidebar"`, `id="epGrid"`, `id="guoguoEpisodeJump"`, `href="/suxinvideo/pay"`, `source-select.js`, `source-discovery.js`} {
			if !strings.Contains(html, fragment) {
				t.Errorf("lock=%q: missing player contract %s", lock, fragment)
			}
		}
		if strings.Contains(html, "/api/ui/") || strings.Contains(html, ":8999") {
			t.Fatal("Guoguo playback depends on the separate source application")
		}
	}
}

func TestGuoguoRankingDoesNotMutateProviderResults(t *testing.T) {
	items := []row{{"id": 3, "pic": "/suxinvideo/image?sig=fixture"}, {"id": 4}, {"id": 5}}
	ranked := guoguoRanked(items, 2)
	if len(ranked) != 2 || ranked[1]["rank"] != 2 || ranked[0]["pic"] != items[0]["pic"] {
		t.Fatalf("ranking did not preserve artwork and order: %#v", ranked)
	}
	ranked[0]["pic"] = "changed"
	if _, exists := items[0]["rank"]; exists || items[0]["pic"] != "/suxinvideo/image?sig=fixture" {
		t.Fatal("ranking changed the shared provider result")
	}
}

func TestGuoguoCardUsesActualMembershipFlag(t *testing.T) {
	for _, value := range []any{0, "0", 1, "1", nil} {
		var output bytes.Buffer
		if err := themePages["guoguo"].ExecuteTemplate(&output, "guoguoCard", row{"id": 3, "name": "影片", "vip": value}); err != nil {
			t.Fatal(err)
		}
		got := strings.Contains(output.String(), `class="badge-4k"`)
		want := value == 1 || value == "1"
		if got != want {
			t.Errorf("membership flag %v: badge=%v, expected %v", value, got, want)
		}
	}
}
