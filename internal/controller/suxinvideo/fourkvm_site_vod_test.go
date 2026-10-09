package suxinvideo

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

func TestFourKVMSiteSignedLinkRejectsTampering(t *testing.T) {
	const now int64 = 1_800_000_000
	sign := func(value string, expires int64) string {
		mac := hmac.New(sha256.New, []byte("test-only-provider-link-key"))
		_, _ = fmt.Fprintf(mac, "%d:%s", expires, value)
		return hex.EncodeToString(mac.Sum(nil))
	}
	good := FourKVMSiteVodReq{ID: "ch1o9kkyn", Exp: now + 3600}
	good.Sig = sign(fourKVMSiteVodSignInput(good.ID), good.Exp)
	for _, play := range []int{0, 1} {
		req := good
		req.Play = play
		if !validFourKVMSiteVodLinkAt(&req, now, sign) {
			t.Fatalf("signed CMS destination play=%d was rejected", play)
		}
	}
	for name, mutate := range map[string]func(*FourKVMSiteVodReq){
		"different film":   func(r *FourKVMSiteVodReq) { r.ID = "ch1o9kkyx" },
		"foreign provider": func(r *FourKVMSiteVodReq) { r.Sig = sign("yqk-site-vod:"+r.ID, r.Exp) },
		"expired":          func(r *FourKVMSiteVodReq) { r.Exp = now - 1; r.Sig = sign(fourKVMSiteVodSignInput(r.ID), r.Exp) },
		"excess lifetime": func(r *FourKVMSiteVodReq) {
			r.Exp = now + 4*3600 + 1
			r.Sig = sign(fourKVMSiteVodSignInput(r.ID), r.Exp)
		},
		"tampered expiry":      func(r *FourKVMSiteVodReq) { r.Exp++ },
		"invalid action":       func(r *FourKVMSiteVodReq) { r.Play = 2 },
		"missing signature":    func(r *FourKVMSiteVodReq) { r.Sig = "" },
		"upper case signature": func(r *FourKVMSiteVodReq) { r.Sig = strings.ToUpper(r.Sig) },
	} {
		t.Run(name, func(t *testing.T) {
			req := good
			mutate(&req)
			if validFourKVMSiteVodLinkAt(&req, now, sign) {
				t.Fatal("forged provider link was accepted")
			}
		})
	}
	for _, slug := range []string{"", "../private", "ch1/other", "https://private.example", "ch1?url=private", "ch1\n", strings.Repeat("a", 33), "%2e%2e", "中文"} {
		req := good
		req.ID = slug
		req.Sig = sign(fourKVMSiteVodSignInput(slug), req.Exp)
		if validFourKVMSiteVodLinkAt(&req, now, sign) {
			t.Fatalf("unsafe or unrepresentable remote slug %q was accepted", slug)
		}
	}
}

func TestFourKVMSiteBindingUsesExactNativeFilmIdentity(t *testing.T) {
	for _, test := range []struct {
		name string
		film row
		want bool
	}{
		{"merged provider", row{"api_id": 25, "api_vid": "123", "play_from": "yqk_1$$$4kvm", "play_url": "第1集$yqk://123/1/1$$$第1集$4kvm://ch_movie?dataid=1&quality=1080&chapter=ch_episode"}, true},
		{"primary provider", row{"api_id": 10, "api_vid": "ch_movie", "play_from": "4kvm", "play_url": "正片$4kvm://ch_movie?dataid=1&quality=1080"}, true},
		{"prefix collision", row{"play_from": "4kvm", "play_url": "正片$4kvm://ch_movie_other?dataid=1&quality=1080"}, false},
		{"foreign source code", row{"play_from": "hnm3u8", "play_url": "正片$4kvm://ch_movie?dataid=1&quality=1080"}, false},
		{"marker inside label", row{"play_from": "4kvm", "play_url": "4kvm://ch_movie?正片$https://media.example/video.mp4"}, false},
		{"wrong chapter cannot select film", row{"play_from": "4kvm", "play_url": "正片$4kvm://other_movie?dataid=1&quality=1080&chapter=ch_movie"}, false},
		{"remote media URL", row{"play_from": "4kvm", "play_url": "正片$https://www.4kvm.net/play/ch_movie"}, false},
		{"invalid native media ID", row{"play_from": "4kvm", "play_url": "正片$4kvm://ch_movie?dataid=0&quality=1080"}, false},
		{"unregistered extra parameter", row{"play_from": "4kvm", "play_url": "正片$4kvm://ch_movie?dataid=1&quality=1080&url=https://private.example"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := fourKVMSiteHasSlug(test.film, "ch_movie"); got != test.want {
				t.Fatalf("exact provider binding = %v, want %v", got, test.want)
			}
		})
	}
}
