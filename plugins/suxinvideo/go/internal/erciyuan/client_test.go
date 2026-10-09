package erciyuan

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func encodeFixture(raw string) string {
	padding := aes.BlockSize - len(raw)%aes.BlockSize
	plain := append([]byte(raw), []byte(strings.Repeat(string(byte(padding)), padding))...)
	block, _ := aes.NewCipher(originalKey[:])
	encrypted := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, originalIV[:]).CryptBlocks(encrypted, plain)
	return base64.StdEncoding.EncodeToString(encrypted)
}

// Only tests replace the transport; production request validation remains
// enabled and sees the actual, narrowly approved upstream origin.
type testTransport struct {
	base     *url.URL
	delegate http.RoundTripper
}

func (tr testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	copyURL := *req.URL
	copyURL.Scheme = tr.base.Scheme
	copyURL.Host = tr.base.Host
	clone.URL = &copyURL
	clone.Host = req.URL.Host
	return tr.delegate.RoundTrip(clone)
}
func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	base, _ := url.Parse(server.URL)
	return NewClient(&http.Client{Transport: testTransport{base: base, delegate: http.DefaultTransport}, Timeout: time.Second})
}

func TestOriginalCategoryCiphertext(t *testing.T) {
	raw, err := os.ReadFile("testdata/categories.encrypted.txt")
	if err != nil {
		t.Fatal(err)
	}
	document, err := decodeDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	rows := objectRows(document.(map[string]any)["list"])
	if len(rows) != 6 || intValue(rows[1]["type_id"]) != 2 || stringValue(rows[1]["type_name"]) != "国漫" {
		t.Fatal("original API category ciphertext did not decode to its verified schema")
	}
}

func TestDecodeRejectsMalformedCiphertext(t *testing.T) {
	block, _ := aes.NewCipher(originalKey[:])
	invalidPad := make([]byte, 16)
	encrypted := make([]byte, 16)
	cipher.NewCBCEncrypter(block, originalIV[:]).CryptBlocks(encrypted, invalidPad)
	for _, raw := range []string{"", "not-base64!", base64.StdEncoding.EncodeToString([]byte("short")), base64.StdEncoding.EncodeToString(encrypted), encodeFixture("not JSON"), `{"a":1}{"b":2}`} {
		if _, err := decodeDocument([]byte(raw)); err == nil {
			t.Fatal("malformed encrypted or JSON response accepted")
		}
	}
	wrapped := `{"data":{"encrypted":"` + encodeFixture(`{"access_granted":true}`) + `"}}`
	doc, err := decodeDocument([]byte(wrapped))
	if err != nil || doc.(map[string]any)["data"].(map[string]any)["access_granted"] != true {
		t.Fatal("encrypted data wrapper failed")
	}
}

func TestMarkers(t *testing.T) {
	id, line, index, ok := ParseMarker("erciyuan://35604/aa03/159")
	if !ok || id != "35604" || line != "aa03" || index != 159 {
		t.Fatal("valid marker rejected")
	}
	for _, marker := range []string{"erciyuan://0/aa03/0", "erciyuan://35604/aa03/-1", "erciyuan://35604/aa03/01", "erciyuan://35604/aa03/0?url=http://localhost", "erciyuan://35604/unknown/0", "erciyuan://35604/aa03/9999999", "erciyuan://35604/aa03/0/other"} {
		if _, _, _, ok := ParseMarker(marker); ok {
			t.Fatal("malformed marker accepted")
		}
	}
	if !ValidLineCode("ecy_aa03") || ValidLineCode("aa03") || ValidLineCode("ecy_unknown") {
		t.Fatal("line validation failed")
	}
}

func TestDialAddressPolicy(t *testing.T) {
	for _, row := range []struct {
		host, ip string
		allowed  bool
	}{
		{"sh13.fannaz.top", "198.18.0.42", true},
		{"198.18.0.42", "198.18.0.42", false},
		{"unreviewed.example", "198.18.0.42", false},
		{"sh13.fannaz.top", "192.168.10.10", false},
		{"sh13.fannaz.top", "127.0.0.1", false},
		{"183.131.206.132", "183.131.206.132", true},
		{"cdn.example", "100.64.0.1", false},
	} {
		if actual := allowedDialIP(row.host, net.ParseIP(row.ip)); actual != row.allowed {
			t.Fatal("DNS target policy mismatch")
		}
	}
}

func TestBootstrapSignatureCachingAndGrant(t *testing.T) {
	var mu sync.Mutex
	primary, launch := 0, 0
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/xcy.txt":
			mu.Lock()
			primary++
			mu.Unlock()
			fmt.Fprint(w, "https://sh13.fannaz.top/app-config/launch/json")
		case "/app-config/launch/json":
			mu.Lock()
			launch++
			mu.Unlock()
			query := r.URL.Query()
			if query.Get("version") != "10000" || query.Get("channel") != "selfOperated" {
				t.Error("original client identity was not used")
			}
			message := "v1|10000|selfOperated|" + originalSigner + "|" + query.Get("ts") + "|" + query.Get("nonce")
			mac := hmac.New(sha256.New, originalKey[:])
			mac.Write([]byte(message))
			if !hmac.Equal([]byte(query.Get("sig")), []byte(hex.EncodeToString(mac.Sum(nil)))) {
				t.Error("launch HMAC mismatch")
			}
			fmt.Fprint(w, `{"data":{"access_granted":true,"base_url":"https://sh13.fannaz.top"}}`)
		case "/filter/nav":
			fmt.Fprint(w, encodeFixture(`{"code":1,"list":[{"type_id":2,"type_name":"国漫"}]}`))
		default:
			t.Error("unexpected request path")
			w.WriteHeader(404)
		}
	})
	var group sync.WaitGroup
	for i := 0; i < 5; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := client.Categories(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	group.Wait()
	if primary != 1 || launch != 1 {
		t.Fatal("bootstrap was not coalesced and cached")
	}
	denied := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/xcy.txt" {
			fmt.Fprint(w, "https://sh13.fannaz.top/app-config/launch/json")
		} else {
			fmt.Fprint(w, `{"data":{"access_granted":false,"base_url":"https://sh13.fannaz.top"}}`)
		}
	})
	if _, err := denied.Categories(context.Background()); err == nil {
		t.Fatal("explicit denied grant ignored")
	}
}

func TestGlobalPaginationPreservesAllCategories(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/filter/nav" {
			fmt.Fprint(w, `{"code":1,"list":[{"type_id":1,"type_name":"日漫"},{"type_id":2,"type_name":"国漫"},{"type_id":3,"type_name":"剧场版"},{"type_id":4,"type_name":"经典番剧"},{"type_id":21,"type_name":"特摄"},{"type_id":27,"type_name":"动态漫画"}]}`)
			return
		}
		if r.URL.Path != "/filter/qjvideo" {
			w.WriteHeader(404)
			return
		}
		id := r.URL.Query().Get("tid")
		page := r.URL.Query().Get("pg")
		total, count := 2, 1
		if id == "1" {
			total, count = 3, 2
		}
		fmt.Fprintf(w, `{"code":1,"page":%s,"pagecount":%d,"limit":2,"total":%d,"list":[{"vod_id":%s00,"vod_name":"sample","vod_year":"2026","vod_area":"中国"}]}`, page, count, total, id)
	})
	client.base = "https://sh13.fannaz.top"
	client.baseUntil = time.Now().Add(time.Hour)
	for page, category := range []int{1, 1, 2, 3, 4, 21, 27} {
		result, err := client.List(context.Background(), 0, page+1, 0)
		if err != nil {
			t.Fatal(err)
		}
		if result.Page != page+1 || result.PageCount != 7 || result.Total != 13 || intValue(result.Items[0]["type_id"]) != category {
			t.Fatal("global page does not preserve category page cursors")
		}
	}
	for page, category := range []int{1, 2, 3, 4, 21, 27} {
		result, err := client.List(context.Background(), 0, page+1, 24)
		if err != nil {
			t.Fatal(err)
		}
		if result.PageCount != 6 || intValue(result.Items[0]["type_id"]) != category {
			t.Fatal("recent collection omitted a source category")
		}
	}
	last, err := client.List(context.Background(), 0, 8, 0)
	if err != nil || len(last.Items) != 0 || last.PageCount != 7 {
		t.Fatal("past-end page lost pagination metadata")
	}
	if _, err := client.List(context.Background(), 999, 1, 0); err == nil {
		t.Fatal("unknown category accepted")
	}
}

func TestDynamicParserAndMetadata(t *testing.T) {
	detailCalls, parserCalls := 0, 0
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/vod/search":
			fmt.Fprint(w, `{"code":0,"data":[{"vod_id":"35604","vod_name":"仙逆","vod_pic":"https://poster.example/xianni.jpg","type_id":"2","type_name":"国漫"}]}`)
		case "/vod/play":
			detailCalls++
			fmt.Fprint(w, `{"code":1,"list":[{"vod_id":35604,"vod_name":"仙逆","type_id":2,"vod_play_from":"AA-02[极速]$$$AA-03[电信]$$$DD-02[有广]$$$4K-01[测试中]","vod_play_url":"第01集$https://v1-ad.video.yximgs.com/bs2/adVideoLp/material.mp4$$$第01集$ali_a&other=secret$$$第01集$https://cdn.example/film.m3u8$$$第70集$https://cdn.example/film4k.m3u8","play_from_parsers":[[],[{"type":"json","url":"http://183.131.206.132:9999/json5.php?url={url}","jsonPlayUrl":"url"}],[],[]]}]}`)
		case "/json5.php":
			parserCalls++
			if r.URL.Query().Get("url") != "ali_a&other=secret" || r.URL.Query().Has("other") {
				t.Error("parser episode query was not safely encoded")
			}
			fmt.Fprint(w, `{"code":200,"url":"https://media.example/film.mp4?expires=123"}`)
		default:
			w.WriteHeader(404)
		}
	})
	client.base = "https://sh13.fannaz.top"
	client.baseUntil = time.Now().Add(time.Hour)
	if _, err := client.Search(context.Background(), "仙逆"); err != nil {
		t.Fatal(err)
	}
	client.remember([]map[string]any{{"vod_id": "35604", "vod_year": "2023", "vod_area": "中国"}})
	detail, err := client.Detail(context.Background(), "35604")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Lines) != 4 || detail.Lines[1].Episodes[0].URL != "erciyuan://35604/aa03/0" || detail.Item["vod_year"] != "2023" || detail.Item["vod_pic"] == nil || detail.Item["vod_play_url"] != nil {
		t.Fatal("persistent metadata or marker contract failed")
	}
	media, err := client.Resolve(context.Background(), detail.Lines[1].Episodes[0].URL)
	if err != nil || !strings.HasPrefix(media.URL, "https://media.example/") || parserCalls != 1 || detailCalls != 2 {
		t.Fatal("dynamic JSON parser was not applied to refreshed episode")
	}
	if _, err := client.Resolve(context.Background(), "erciyuan://35604/aa02/0"); err == nil || !strings.Contains(err.Error(), "广告") {
		t.Fatal("advertisement asset accepted as a film")
	}
	if _, err := client.Resolve(context.Background(), "erciyuan://35604/aa03/160"); err == nil {
		t.Fatal("out-of-range episode accepted")
	}
}

func TestParserSSRFAndContextCancellation(t *testing.T) {
	requests := 0
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		fmt.Fprint(w, `{"url":"https://media.example/video.mp4"}`)
	})
	for _, address := range []string{"http://127.0.0.1/json5.php?url={url}", "http://183.131.206.132/admin?url={url}", "http://183.131.206.132:8080/json5.php?url={url}", "http://183.131.206.132/json5.php/{url}", "file:///tmp/{url}"} {
		_, err := client.parseMedia(context.Background(), map[string]any{"type": "json", "url": address, "jsonPlayUrl": "url"}, "item")
		if err == nil {
			t.Fatal("unapproved parser target accepted")
		}
	}
	if requests != 0 {
		t.Fatal("unsafe parser performed a request")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Categories(ctx); err == nil {
		t.Fatal("canceled upstream request proceeded")
	}
	for _, address := range []string{"http://127.0.0.1/video.mp4", "http://192.168.10.10/video.mp4", "http://user:password@cdn.example/a.mp4", "javascript:alert(1)"} {
		if validateMedia(address) == nil {
			t.Fatal("unsafe media URL accepted")
		}
	}
}
