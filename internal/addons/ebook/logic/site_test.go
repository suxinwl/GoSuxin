package album

import (
	"os"
	"strings"
	"testing"
)

func TestSiteSettingsValidationAndPersistence(t *testing.T) {
	t.Chdir(t.TempDir())
	config, err := SiteSettings()
	if err != nil {
		t.Fatal(err)
	}
	if config.Chinese.Website != "" || strings.Contains(config.EnglishURL, "suxin") {
		t.Fatal("unexpected deployment branding")
	}
	config.ChineseURL = "https://catalog.example.com/zh"
	config.EnglishURL = "https://catalog.example.com/en/"
	config.Chinese.Name = "用户品牌 {{7*7}}"
	config.Chinese.Website = "https://www.example.com"
	if err := SaveSiteSettings(config); err != nil {
		t.Fatal(err)
	}
	saved, err := SiteSettings()
	if err != nil {
		t.Fatal(err)
	}
	if saved.ChineseURL != "https://catalog.example.com/zh/" || saved.Chinese.Name != config.Chinese.Name {
		t.Fatal("configuration changed unexpectedly")
	}
	for _, bad := range []string{"javascript:alert(1)", "//evil.test/", "/admin/", "/suxinweb/", "/common/album/", "/x/../y/", "/x/%2e%2e/y/", "https://user:pass@example.com/", "https://example.com/#/", "/foo?lang=en"} {
		next := saved
		next.EnglishURL = bad
		if SaveSiteSettings(next) == nil {
			t.Fatalf("accepted invalid entry %q", bad)
		}
	}
	next := saved
	next.EnglishURL = next.ChineseURL
	if SaveSiteSettings(next) == nil {
		t.Fatal("accepted duplicate language entries")
	}
	next = saved
	next.Chinese.Logo = "javascript:alert(1)"
	if SaveSiteSettings(next) == nil {
		t.Fatal("accepted unsafe logo")
	}
	after, _ := SiteSettings()
	if after != saved {
		t.Fatal("failed saves altered stored configuration")
	}
	if err := os.WriteFile(siteConfigFile, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := SiteSettings(); err == nil {
		t.Fatal("silently ignored invalid configuration")
	}
}
func TestSitePathAndHostLanguage(t *testing.T) {
	cfg := DefaultSiteConfig()
	cfg.ChineseURL = "/catalog/zh/"
	cfg.EnglishURL = "/catalog/en/"
	if configuredLanguage(cfg, "example.com", "/catalog/en/") != "en" {
		t.Fatal("same-host English path")
	}
	if configuredLanguage(cfg, "example.com", "/catalog/zh/") != "zh" {
		t.Fatal("same-host Chinese path")
	}
	cfg.EnglishURL = "https://en.example.com/books/"
	if configuredLanguage(cfg, "en.example.com", "/books/") != "en" {
		t.Fatal("English deployment domain")
	}
	if configuredLanguage(cfg, "evil.test", "/books/") == "en" {
		t.Fatal("unmatched host accepted")
	}
	if entryMatches(cfg.EnglishURL, "en.example.com", "/books/private/file") {
		t.Fatal("unexpected static directory expansion")
	}
}
