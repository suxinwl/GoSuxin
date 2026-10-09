package suxinvideo

import "testing"

func TestNativeYQKSeriesRejectsWrongLine(t *testing.T) {
	for _, test := range []struct {
		code, marker string
		valid        bool
	}{
		{"yqk_34", "yqk://129431/34/27714327", true},
		{"yqk_19", "yqk://129431/34/27714327", false},
		{"hongguo", "yqk://129431/34/27714327", false},
		{"yqk_34", "https://example.com/video.m3u8", false},
		{"yqk_34", "yqk://129431/34/../../../private", false},
	} {
		series, err := nativeYQKSeries(test.code, test.marker)
		if (err == nil) != test.valid || (test.valid && series != "129431") {
			t.Errorf("native series validation: code=%s valid=%t series=%s error=%v", test.code, test.valid, series, err)
		}
	}
}

func TestNative4KVMSlug(t *testing.T) {
	for _, test := range []struct {
		marker string
		slug   string
		ok     bool
	}{
		{"4kvm://ch27q13xa?dataid=23149&quality=1080", "ch27q13xa", true},
		{"4kvm://", "", false},
		{"4kvm://../private", "", false},
		{"https://example.com/video", "", false},
	} {
		slug, ok := native4KVMSlug(test.marker)
		if slug != test.slug || ok != test.ok {
			t.Errorf("native4KVMSlug(%q) = %q, %v; want %q, %v", test.marker, slug, ok, test.slug, test.ok)
		}
	}
}
