package suxinvideo

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestValidBrandURL(t *testing.T) {
	for _, value := range []string{
		"", "https://example.com/logo.png", "/suxinvideo/brand?file=" + strings.Repeat("a", 32) + ".png",
		defaultLogoURL, defaultFaviconURL, defaultAvatarURL,
	} {
		if !validBrandURL(value) {
			t.Errorf("expected valid brand URL: %q", value)
		}
	}
	for _, value := range []string{
		"javascript:alert(1)", "http://example.com/logo.png", "/suxinvideo/brand?file=../secret.png",
		"/suxinvideo/brand?file=" + strings.Repeat("a", 32) + ".svg",
		"/suxinvideo/brand?file=../app-icon.png", "/suxinvideo/brand?file=secret.png",
		defaultLogoURL + "&file=secret.png", defaultLogoURL + "&other=1",
	} {
		if validBrandURL(value) {
			t.Errorf("expected invalid brand URL: %q", value)
		}
	}
}

func TestDefaultBrandSetting(t *testing.T) {
	if defaultBrandSetting("site_logo") != defaultLogoURL || defaultBrandSetting("site_favicon") != defaultFaviconURL || defaultBrandSetting("user_default_avatar") != defaultAvatarURL {
		t.Fatal("bundled logo, ICO and avatar must remain the default branding")
	}
	if defaultBrandSetting("unrelated_setting") != "" || validBrandFile("../favicon.ico") || validBrandFile("private.key") {
		t.Fatal("unknown settings and files must not become brand resources")
	}
	custom := row{"avatar": "https://example.com/member.png"}
	if applyDefaultUserAvatar(nil, custom)["avatar"] != "https://example.com/member.png" {
		t.Fatal("a member's custom avatar must be preserved")
	}
}

func TestBrandImageFormat(t *testing.T) {
	var buffer bytes.Buffer
	imageData := image.NewRGBA(image.Rect(0, 0, 2, 2))
	imageData.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&buffer, imageData); err != nil {
		t.Fatal(err)
	}
	extension, mime, err := brandImageFormat(buffer.Bytes())
	if err != nil || extension != "png" || mime != "image/png" {
		t.Fatalf("valid PNG: extension=%q mime=%q err=%v", extension, mime, err)
	}
	if _, _, err := brandImageFormat([]byte("<svg onload=alert(1)></svg>")); err == nil {
		t.Fatal("SVG script must be rejected")
	}
	if _, _, err := brandImageFormat(make([]byte, (2<<20)+1)); err == nil {
		t.Fatal("oversized upload must be rejected")
	}
}
