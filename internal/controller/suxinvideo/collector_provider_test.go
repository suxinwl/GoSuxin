package suxinvideo

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"
)

func TestNativeCollectorSources(t *testing.T) {
	for _, source := range []struct {
		url   string
		count int
	}{
		{hongguoSourceURL, 3},
		{fourKVMSourceURL, 4},
	} {
		classes, err := fetchCollectSource(context.Background(), source.url, url.Values{"ac": {"list"}})
		if err != nil || classes.Code != 1 || len(classes.Class) != source.count {
			t.Fatalf("%s classes: %+v, %v", source.url, classes, err)
		}
		if _, err := fetchCollectSource(context.Background(), source.url, url.Values{"ac": {"delete"}}); err == nil {
			t.Fatalf("unsupported action accepted for %s", source.url)
		}
	}
	if nativeProvider("https://example.com/api") != "" || onlyDigits("12x") || !onlyDigits("123") {
		t.Fatal("source dispatch or episode ID validation failed")
	}
}

func TestLiveMacSourceClasses(t *testing.T) {
	if os.Getenv("SUXIN_LIVE_SOURCES") != "1" {
		t.Skip("set SUXIN_LIVE_SOURCES=1 to probe upstream providers")
	}
	for _, source := range []struct {
		name string
		url  string
	}{
		{"量子", "https://cj.lziapi.com/api.php/provide/vod/"},
		{"无极", "https://api.wujinapi.me/api.php/provide/vod/"},
		{"暴风", "https://bfzyapi.com/api.php/provide/vod/"},
		{"红牛", "https://www.hongniuzy2.com/api.php/provide/vod/"},
		{"闪电", "https://sdzyapi.com/api.php/provide/vod/"},
		{"迅雷", "https://api.xinlangapi.com/xinlangapi.php/provide//vod/"},
	} {
		t.Run(source.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			data, err := fetchCollectSource(ctx, source.url, url.Values{"ac": {"list"}})
			if err != nil || len(data.Class) == 0 {
				t.Fatalf("分类不可用：%d, %v", len(data.Class), err)
			}
		})
	}
}
