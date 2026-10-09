package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func fourKVMHomeFixture(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile("testdata/4kvm_home.html")
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func Test4KVMHomeActualMarkup(t *testing.T) {
	home := parse4KVMHomeDOM(fourKVMHomeFixture(t), fourKVMBaseURL)
	if len(home.Banners) != 2 || home.Banners[0].Drama.SourceID != "ch4l5b3mn" || home.Banners[0].Drama.Title != "挑情丑闻" || home.Banners[0].Drama.Score != "7.9" {
		t.Fatalf("real hero markup not parsed: %+v", home.Banners)
	}
	if !strings.Contains(home.Banners[0].ImageURL, "/w1280/") || !strings.Contains(home.Banners[0].TitleLogoURL, "/w200/") || home.Banners[0].Drama.Cover != "" {
		t.Fatal("banner background, title logo, and absent poster must remain distinct")
	}
	sections := map[string]CMSHomeSection{}
	for _, section := range home.Sections {
		sections[section.Key] = section
		for _, item := range section.Dramas {
			cover, _ := item.Cover.(string)
			if item.Source != source4KVM || item.SourceID == "" || cover == "" || item.Title == "" || strings.Contains(cover, "/numbers/") || strings.Contains(cover, "placeholder") {
				t.Fatalf("invalid or decorative card escaped: %+v", item)
			}
		}
	}
	for _, key := range []string{"hot", "rank", "recent", "popular", "latest", "tv", "anime"} {
		if len(sections[key].Dramas) != 2 {
			t.Fatalf("section %s missing or mixed with adjacent section: %+v", key, sections[key])
		}
	}
	if sections["hot"].Dramas[0].Title != "兰香如故" || sections["hot"].Dramas[0].SourceID != "ch4k8uf4p" || sections["hot"].Dramas[0].OnlineDate != "2026" || sections["hot"].Dramas[0].Score != "9.3" {
		t.Fatalf("home cards need chapter href and actual metadata: %+v", sections["hot"].Dramas[0])
	}
	if sections["rank"].Dramas[0].Title != "凡人修仙传" || sections["rank"].Dramas[0].Score != "9.3" || sections["popular"].Dramas[0].Title != "兰香如故" {
		t.Fatal("sidebar rank and standalone popularity grid were mixed")
	}
	if sections["anime"].Dramas[0].Category != "动漫" || sections["tv"].Dramas[0].Category != "电视剧" || sections["hot"].Dramas[0].Category != "" {
		t.Fatal("categories must follow explicit editorial section, leave mixed section unknown")
	}
}

func Test4KVMHomeImagesIdentityAndSectionBoundaries(t *testing.T) {
	body := `<section id="hero-banner"><div class="hero-swiper"><a href="/play/real"><div class="slide-inner" data-title="真实影片" data-info="简介" data-rating="NaN"><img src="//images.example/wide.jpg"><img src="https://images.example/title.png"></div></a><a href="https://ads.example/play/foreign"><div class="slide-inner" data-title="外链广告"><img src="https://images.example/ad.jpg"></div></a><a href="/play/bad"><div class="slide-inner" data-title="坏图片"><img src="javascript:alert(1)"></div></a></div></section><main class="space-y-16"><div><div><h2>最近更新</h2><p>最近的影片</p></div><div><article class="movie-card"><a data-vod="film-id-is-not-play-slug" href="/play/real"><img class="ranking-number-svg" src="/static/images/numbers/1.svg"><img data-src="/uploads/poster.jpg" src="/static/images/placeholder-dark.svg"><h3>真实影片</h3><p>影片1999年的剧情</p></a></article><article class="movie-card"><a data-vod="foreign" href="https://ads.example/play/foreign"><img src="https://images.example/ad.jpg"><h3>外链广告</h3></a></article></div></div><div><h2>今天热播</h2><article class="movie-card"><a href="/play/other"><img src="https://images.example/other.jpg"><h3>另一部</h3></a></article></div><div><h2>榜单排行</h2><p>空排行不读取其他板块影片</p></div></main><script>never execute me</script>`
	home := parse4KVMHomeDOM(body, fourKVMBaseURL)
	if len(home.Banners) != 1 || len(home.Sections) != 2 {
		t.Fatalf("untrusted anchors or empty section accepted: %+v", home)
	}
	banner := home.Banners[0]
	if banner.ImageURL != "https://images.example/wide.jpg" || banner.Drama.Cover != fourKVMBaseURL+"/uploads/poster.jpg" || banner.Drama.Score != "" {
		t.Fatalf("wide image and associated poster were confused: %+v", banner)
	}
	if home.Sections[0].Key != "recent" || home.Sections[0].Subtitle != "最近的影片" || len(home.Sections[0].Dramas) != 1 || home.Sections[0].Dramas[0].OnlineDate != "" || home.Sections[1].Dramas[0].Title != "另一部" {
		t.Fatal("adjacent editorial sections or synopsis year leaked")
	}
	for _, raw := range []string{"javascript:alert(1)", "data:image/png;base64,abc", "file:///temp.jpg", "https://user:secret@images.example/a.jpg", "", "/static/images/numbers/1.svg"} {
		if fourKVMHomeImage(raw, fourKVMBaseURL) != "" {
			t.Fatalf("unsafe/decorative image accepted: %s", raw)
		}
	}
}

func Test4KVMHomePosterUsesExplicitMovieRecordAcrossChapters(t *testing.T) {
	body := `<section id="hero-banner"><div class="hero-swiper"><a data-vod="movie-record" href="/play/banner-chapter"><div class="slide-inner" data-title="同一影片"><img src="https://images.example/wide.jpg"></div></a><a data-vod="unrelated-record" href="/play/unrelated"><div class="slide-inner" data-title="同一影片"><img src="https://images.example/other-wide.jpg"></div></a></div></section><div><h2>最近更新</h2><div class="movie-card"><a data-vod="movie-record" href="/play/newest-chapter"><img src="https://images.example/poster.jpg"><h3>同一影片</h3></a></div></div>`
	home := parse4KVMHomeDOM(body, fourKVMBaseURL)
	if len(home.Banners) != 2 || home.Banners[0].Drama.SourceID != "banner-chapter" || home.Banners[0].Drama.Cover != "https://images.example/poster.jpg" || home.Banners[1].Drama.Cover != "" {
		t.Fatal("banner poster must match explicit movie identity, never a coincidental title")
	}
}

func TestCMS4KVMHomeFetchAndFailure(t *testing.T) {
	bridge := NewCMSBridge()
	bridge.downloader.cfg.RequestIntervalMS = 1
	calls := 0
	body := fourKVMHomeFixture(t)
	bridge.downloader.client.Transport = cmsRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.URL.Path != "/" || request.URL.RawQuery != "" || request.Header.Get("Referer") == "" {
			t.Fatalf("home must use the real public root endpoint: %s", request.URL.Redacted())
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})
	home, err := bridge.Home(context.Background(), source4KVM)
	if err != nil || len(home.Banners) != 2 || calls != 1 {
		t.Fatalf("homepage fetch: %v, calls=%d", err, calls)
	}
	if _, err = bridge.Home(context.Background(), sourceHongguo); err == nil || calls != 1 {
		t.Fatal("unsupported provider must not make a network request")
	}
	body = `<html><h2>暂无数据</h2><script>ad()</script></html>`
	if _, err = bridge.Home(context.Background(), source4KVM); err == nil {
		t.Fatal("empty upstream/blocked page must not become a successful snapshot")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = bridge.Home(ctx, source4KVM); !errors.Is(err, context.Canceled) {
		t.Fatalf("request cancellation lost: %v", err)
	}
}

func TestCMS4KVMHomeLiveInspect(t *testing.T) {
	output := os.Getenv("SUXIN_4KVM_HOME_INSPECT")
	if output == "" {
		t.Skip("live provider inspection is opt-in")
	}
	home, err := NewCMSBridge().Home(context.Background(), source4KVM)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.MarshalIndent(home, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(output, body, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("banners=%d sections=%d", len(home.Banners), len(home.Sections))
	for _, section := range home.Sections {
		t.Logf("section=%s items=%d", section.Key, len(section.Dramas))
	}
}
