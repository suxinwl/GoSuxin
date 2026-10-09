package suxinvideo

import (
	"reflect"
	"testing"
)

func TestCastFFmpegVerifiesSiteNameOnLoopback(t *testing.T) {
	for _, test := range []struct {
		name, source string
		verify       bool
	}{
		{"site", "https://127.0.0.1:8600/suxinvideo/media/stream", true},
		{"external", "https://cdn.example/stream.m3u8", false},
		{"http", "http://127.0.0.1:8600/media", false},
		{"different port", "https://127.0.0.1:443/media", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			want := []string{"-tls_verify", "1", "-ca_file", "manifest/cert/fullchain.pem"}
			if test.verify {
				want = append(want, "-verifyhost", "xq.suxinwl.com")
			}
			want = append(want, "-i", test.source)
			if got := castFFmpegInputArgs(test.source, "https://xq.suxinwl.com:8600"); !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v; want %v", got, want)
			}
		})
	}
}
