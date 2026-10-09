package suxinvideo

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
)

func TestMediaRefererByDestination(t *testing.T) {
	for _, test := range []struct {
		host  string
		strip bool
	}{
		{"sns-open-qc.xhscdn.com", true},
		{"xhscdn.com", true},
		{"oss.douyinbit.com", false},
		{"xhscdn.com.example.org", false},
	} {
		request, _ := http.NewRequest(http.MethodGet, "https://"+test.host+"/media", nil)
		request.Header.Set("Origin", "https://www.4kvm.net")
		request.Header.Set("Range", "bytes=0-65535")
		applyMediaReferer(request, "https://www.4kvm.net/")
		if test.strip {
			if request.Header.Get("Referer") != "" || request.Header.Get("Origin") != "" {
				t.Fatalf("foreign headers kept for %s", test.host)
			}
		} else if request.Header.Get("Referer") != "https://www.4kvm.net/" || request.Header.Get("Origin") == "" {
			t.Fatalf("provider headers lost for %s", test.host)
		}
		if request.Header.Get("Range") != "bytes=0-65535" {
			t.Fatal("range header changed")
		}
	}
}

func TestFourKVMChinaRegionAlias(t *testing.T) {
	if discoveryRegion("中国") != discoveryRegion("中国大陆") || discoveryRegion("China") != "cn" {
		t.Fatal("4KVM country alias did not normalize")
	}
	if discoveryRegion("中国香港") == "cn" || discoveryRegion("中国台湾") == "cn" {
		t.Fatal("distinct regions were collapsed")
	}
}

func TestPNGWrappedTransportStreamHTTP(t *testing.T) {
	prefix := append([]byte{0x89, 'P', 'N', 'G', 13, 10, 26, 10}, bytes.Repeat([]byte{0}, 65)...)
	packet := bytes.Repeat([]byte{0xff}, 188)
	copy(packet, []byte{0x47, 0x40, 0x00, 0x10})
	stream := bytes.Repeat(packet, 500)
	wrapped := append(append([]byte{}, prefix...), stream...)
	image := append(append([]byte{}, prefix...), bytes.Repeat([]byte{0xaa}, 3000)...)
	key := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	base := mediaBinaryHTTPServer(t, func(group *ghttp.RouterGroup) {
		for _, path := range []string{"/wrapped", "/image", "/key", "/range"} {
			group.GET(path, func(r *ghttp.Request) {
				body, status := wrapped, http.StatusOK
				r.Response.Header().Set("Content-Type", "image/png")
				switch r.URL.Path {
				case "/image":
					body = image
				case "/key":
					body = key
					r.Response.Header().Set("Content-Type", "application/octet-stream")
				case "/range":
					status = http.StatusPartialContent
					r.Response.Header().Set("Content-Range", "bytes 0-94072/94073")
				}
				_, _ = copyMediaBinary(r.Response, status, bytes.NewReader(body))
				r.ExitAll()
			})
		}
	})
	for _, test := range []struct {
		path   string
		status int
		body   []byte
		mime   string
	}{
		{"/wrapped", 200, stream, "video/mp2t"},
		{"/image", 200, image, "image/png"},
		{"/key", 200, key, "application/octet-stream"},
		{"/range", 206, wrapped, "image/png"},
	} {
		response, err := http.Get(base + test.path)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != test.status || !bytes.Equal(body, test.body) || response.Header.Get("Content-Type") != test.mime {
			t.Fatalf("%s: response mismatch status=%d bytes=%d mime=%s", test.path, response.StatusCode, len(body), response.Header.Get("Content-Type"))
		}
		if test.path == "/range" && response.Header.Get("Content-Range") != "bytes 0-94072/94073" {
			t.Fatal("partial representation offsets changed")
		}
	}
}

func TestHealthProbePNGWrappedTransportStream(t *testing.T) {
	packet := bytes.Repeat([]byte{0xff}, 188)
	copy(packet, []byte{0x47, 0x40, 0x00, 0x10})
	prefix := append([]byte{0x89, 'P', 'N', 'G', 13, 10, 26, 10}, bytes.Repeat([]byte{0}, 65)...)
	wrapped := append(append([]byte{}, prefix...), bytes.Repeat(packet, 20)...)
	image := append(append([]byte{}, prefix...), bytes.Repeat([]byte{0xaa}, 3000)...)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("User-Agent"), "Mozilla/") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/valid.m3u8", "/image.m3u8":
			file := "valid.png"
			if r.URL.Path == "/image.m3u8" {
				file = "image.png"
			}
			_, _ = io.WriteString(w, "#EXTM3U\n#EXTINF:5,\n"+file+"\n#EXT-X-ENDLIST\n")
		case "/valid.png", "/image.png":
			if r.Header.Get("Range") != "bytes=0-4095" {
				t.Error("media probe lost its bounded range")
			}
			w.Header().Set("Content-Type", "image/png")
			w.WriteHeader(http.StatusPartialContent)
			body := wrapped
			if r.URL.Path == "/image.png" {
				body = image
			}
			_, _ = w.Write(body)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	probe := sourceHealthProbe{client: server.Client(), checkURL: func(context.Context, string) error { return nil }}
	if err := probe.media(context.Background(), server.URL+"/valid.m3u8", 0, map[string]bool{}); err != nil {
		t.Fatalf("playable PNG-wrapped TS was rejected: %v", err)
	}
	if err := probe.media(context.Background(), server.URL+"/image.m3u8", 0, map[string]bool{}); !errors.Is(err, errSourceProbeUnsupported) {
		t.Fatalf("actual image was accepted as video: %v", err)
	}
}
