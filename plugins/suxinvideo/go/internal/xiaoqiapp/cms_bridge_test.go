package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

type cmsRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn cmsRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestCMS4KVMSearchAndDetailMarkup(t *testing.T) {
	bridge := NewCMSBridge()
	bridge.downloader.cfg.RequestIntervalMS = 1
	search := `<div class="group"><a data-vod="movie-record" href="/play/chapter-real"><img data-src="https://images.example/cover.jpg"><div>2023</div><h3>仙逆</h3><p>剧情简介</p></a></div>`
	detail := `<meta property="og:title" content="仙逆 - 第1集"><meta property="og:image" content="https://images.example/real.jpg"><meta name="description" content="真实简介"><h1>观看历史</h1><div data-classify-id="3"></div><div class="col-span-1">地区</div><div class="col-span-2">中国大陆</div><div class="col-span-1">上映</div><div class="col-span-2">2023-09-25</div><div class="col-span-1">类型</div><div class="col-span-2">动画 / 奇幻</div><div x-data="episodeManager(2, 1, [])"><a href="/play/chapter-real" dataid="123" data-line="2" data-episode="1" x-effect="if (isActive(2, 1)) { nextTick(() => scrollIntoView()) }"><span>正片</span></a><a href="/play/chapter-other" dataid="124" data-line="1" data-episode="1"><span>预告</span></a></div>`
	requests := 0
	bridge.downloader.client.Transport = cmsRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		body := detail
		if request.URL.Path == "/search" {
			if request.URL.Query().Get("q") != "仙逆" {
				t.Fatal("search keyword was not preserved")
			}
			body = search
		} else if request.URL.Path != "/play/chapter-real" {
			t.Fatalf("unexpected detail path: %s", request.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})
	items, err := bridge.Search(context.Background(), source4KVM, "仙逆")
	if err != nil || len(items) != 1 || items[0].SourceID != "chapter-real" || items[0].OnlineDate != "2023" || items[0].Category != "" || requests != 1 {
		t.Fatalf("search results must use the real chapter href and leave unknown category empty: %+v, %v", items, err)
	}
	drama, chapters, err := bridge.Detail(context.Background(), source4KVM, items[0].SourceID)
	if err != nil || drama.Title != "仙逆" || drama.Category != "动漫" || drama.Area != "中国大陆" || drama.OnlineDate != "2023" || len(chapters) != 2 {
		t.Fatalf("detail metadata: %+v, chapters=%d, err=%v", drama, len(chapters), err)
	}
	if chapters[0].Title != "正片" || chapters[0].SourceLine != "2" || !chapters[0].DefaultLine || chapters[1].DefaultLine || chapters[1].Title != "预告" {
		t.Fatalf("episode HTML attributes must not become labels: %+v", chapters)
	}
	for index, expectedChapter := range []string{"chapter-real", "chapter-other"} {
		ref, valid := parse4KVMPlaybackRef(chapters[index].VideoURL)
		if !valid || ref.FilmSlug != "chapter-real" || ref.ChapterSlug != expectedChapter || !strings.HasSuffix(chapters[index].PageURL, "/play/"+expectedChapter) {
			t.Fatal("collection entry identity and actual episode page were mixed")
		}
	}
	if _, _, err = bridge.Detail(context.Background(), source4KVM, "../../admin"); err == nil {
		t.Fatal("invalid remote identifier accepted")
	}
	if _, err = bridge.Search(context.Background(), "unsupported", "仙逆"); err == nil {
		t.Fatal("unsupported search silently returned data")
	}
}

func TestCMSHongguoSearchUsesRealEndpointAndValidatesResults(t *testing.T) {
	for _, valid := range []bool{true, false} {
		t.Run(strconv.FormatBool(valid), func(t *testing.T) {
			bridge := NewCMSBridge()
			bridge.downloader.cfg.RequestIntervalMS = 1
			calls := 0
			bridge.downloader.client.Transport = cmsRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if request.URL.Path != "/search/冲喜赘婿竟是绝世神医" {
					t.Fatal("Hongguo search did not preserve the keyword")
				}
				query := "冲喜赘婿竟是绝世神医"
				if !valid {
					query = "其它搜索"
				}
				payload, _ := json.Marshal(map[string]any{"loaderData": map[string]any{"search_(keyword)/page": map[string]any{"isSuccess": true, "query": query, "totalCount": "1", "searchList": []any{map[string]any{"video_data": map[string]any{"series_id": "7684943327614995518", "series_title": "冲喜赘婿竟是绝世神医", "episode_cnt": 98}}}}}})
				body := "<script>window._ROUTER_DATA=" + string(payload) + ";</script>"
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
			})
			items, err := bridge.Search(context.Background(), sourceHongguo, "冲喜赘婿竟是绝世神医")
			if calls != 1 || valid && (err != nil || len(items) != 1 || items[0].SourceID != "7684943327614995518" || items[0].TotalEpisode != "98") || !valid && err == nil {
				t.Fatalf("unvalidated or unrelated search data: count=%d calls=%d error=%v", len(items), calls, err)
			}
		})
	}
}

func Test4KVMCatalogDOMKeepsCardsSeparate(t *testing.T) {
	body := `<div class="movie-card" data-vod-id="movie-one"><a href="/play/chapter-one"><img data-src="https://images.example/first.jpg"><h3>第一部</h3><span>2024</span><p>第一简介</p></a></div><div class="movie-card" data-vod-id="movie-two"><a href="/play/chapter-two"><h3>第二部</h3><p>故事发生于1999年</p></a></div>`
	items := parse4KVMCards(body, 1)
	if len(items) != 2 || items[0].SourceID != "chapter-one" || items[1].SourceID != "chapter-two" || items[0].OnlineDate != "2024" || items[1].OnlineDate != "" || items[1].Cover != "" || items[1].Category != "电影" {
		t.Fatalf("catalog cards leaked metadata across card boundaries: %+v", items)
	}
	unknown := parse4KVMDetailDOM(fourKVMParseHTML(`<meta property="og:title" content="未标注"><meta name="description" content="1999年的故事">`), "unknown")
	if unknown.OnlineDate != "" || unknown.Area != "" || unknown.Category != "" {
		t.Fatalf("missing metadata must not be guessed: %+v", unknown)
	}
}

func Test4KVMKeepsWorkingPrimaryAndExcludesVIP(t *testing.T) {
	bridge := NewCMSBridge()
	bridge.downloader.cfg.RequestIntervalMS = 1
	hosts, _ := json.Marshal([]string{"oss.douyinbit.com", "myoss.douyinbit.top"})
	page := `userlink:'anonymous'; window._pdf='` + base64.StdEncoding.EncodeToString(hosts) + `';`
	primary := "https://oss.douyinbit.com/play/one.m3u8?token=fixture"
	fallbackRequests := 0
	bridge.downloader.client.Transport = cmsRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := page
		switch {
		case request.URL.Path == "/video/play":
			body = `{"code":200,"data":{"quality_urls":[{"url":"` + primary + `","title":"1080p"},{"url":"https://oss.douyinbit.com/play/vip.m3u8","title":"2160p","isvip":true},{"url":"https://oss.douyinbit.com/play/locked.m3u8","locked":true}]}}`
		case request.URL.Hostname() == "myoss.douyinbit.top":
			fallbackRequests++
			body = "<html>parking page</html>"
		case request.URL.Hostname() == "oss.douyinbit.com":
			body = "#EXTM3U\n#EXTINF:12.5,\none.ts\n#EXT-X-ENDLIST\n"
			if strings.HasSuffix(request.URL.Path, ".ts") {
				body = fourKVMTestTSPayload()
			}
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})
	media, err := bridge.Resolve(context.Background(), source4KVM, "chapter-one", "4kvm://chapter-one?dataid=123&quality=1080")
	if err != nil || media.URL != primary || media.Duration != 12500*time.Millisecond || media.Quality != 1080 || len(media.Variants) != 1 || fallbackRequests != 0 {
		t.Fatalf("working API host was replaced or VIP exposed: host=%s duration=%v quality=%d variants=%d fallbackRequests=%d err=%v", safeTest4KVMHost(media.URL), media.Duration, media.Quality, len(media.Variants), fallbackRequests, err)
	}
}

func safeTest4KVMHost(raw string) string {
	u, _ := url.Parse(raw)
	if u == nil {
		return ""
	}
	return u.Hostname()
}

func fourKVMTestTSPayload() string {
	packet := append([]byte{0x47}, make([]byte, 187)...)
	return string(append(append(append([]byte{}, packet...), packet...), packet...))
}

func Test4KVMFallbackMustBeActualHLS(t *testing.T) {
	for _, validFallback := range []bool{true, false} {
		t.Run(strconv.FormatBool(validFallback), func(t *testing.T) {
			bridge := NewCMSBridge()
			bridge.downloader.cfg.RequestIntervalMS = 1
			unexpected := 0
			bridge.downloader.client.Transport = cmsRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				body := "<html>not media</html>"
				if request.URL.Hostname() == "myoss.douyinbit.top" && validFallback {
					body = "#EXTM3U\n#EXTINF:5,\none.ts\n#EXT-X-ENDLIST\n"
					if strings.HasSuffix(request.URL.Path, ".ts") {
						body = fourKVMTestTSPayload()
					}
				} else if !fourKVMKnownMediaHost(request.URL.Hostname()) {
					unexpected++
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
			})
			media, err := bridge.downloader.fourKVMSelectMedia(context.Background(), "https://oss.douyinbit.com/one.m3u8", []string{"127.0.0.1", "localhost:8000", "myoss.douyinbit.top"}, fourKVMBaseURL+"/")
			if unexpected != 0 || (validFallback && (err != nil || safeTest4KVMHost(media.URL) != "myoss.douyinbit.top")) || (!validFallback && err == nil) {
				t.Fatalf("unvalidated fallback accepted: valid=%v host=%s unexpected=%d err=%v", validFallback, safeTest4KVMHost(media.URL), unexpected, err)
			}
		})
	}
}

func Test4KVMFirstSegmentAndImageCDNHeaders(t *testing.T) {
	for _, imageCDN := range []bool{false, true} {
		t.Run(strconv.FormatBool(imageCDN), func(t *testing.T) {
			bridge := NewCMSBridge()
			bridge.downloader.cfg.RequestIntervalMS = 1
			cdnChecked := false
			bridge.downloader.client.Transport = cmsRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				body, status := "#EXTM3U\n#EXTINF:5,\none.ts\n#EXT-X-ENDLIST\n", 200
				if imageCDN {
					body = "#EXTM3U\n#EXTINF:5,\nhttps://sns-open-qc.xhscdn.com/image-chunk\n#EXT-X-ENDLIST\n"
				}
				if strings.HasSuffix(request.URL.Path, ".ts") {
					if request.URL.Hostname() == "oss.douyinbit.com" {
						body, status = "denied", 403
					} else {
						body = fourKVMTestTSPayload()
					}
				}
				if fourKVMImageCDN(request.URL.Hostname()) {
					cdnChecked = true
					if request.Header.Get("Referer") != "" || request.Header.Get("Origin") != "" || request.Header.Get("Range") != "bytes=0-16383" {
						t.Fatal("image CDN probe sent forbidden referer/origin or unbounded range")
					}
					body = string(append(make([]byte, 73), []byte(fourKVMTestTSPayload())...))
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
			})
			media, err := bridge.downloader.fourKVMSelectMedia(context.Background(), "https://oss.douyinbit.com/one.m3u8", []string{"myoss.douyinbit.top"}, fourKVMBaseURL+"/")
			wantHost := "myoss.douyinbit.top"
			if imageCDN {
				wantHost = "oss.douyinbit.com"
			}
			if err != nil || safeTest4KVMHost(media.URL) != wantHost || cdnChecked != imageCDN {
				t.Fatalf("manifest status hid a first-segment failure: host=%s imageCDN=%v checked=%v err=%v", safeTest4KVMHost(media.URL), imageCDN, cdnChecked, err)
			}
		})
	}
}

func Test4KVMTokenQueryEscapingAndMarkerValidation(t *testing.T) {
	raw, err := build4KVMPlayURL(context.Background(), "123", "chapter-real", "1080", "nbS")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(raw)
	if u.Query().Get("k") != "AAA+" || !strings.Contains(u.RawQuery, "%2B") {
		t.Fatal("base64 play token did not survive URL query encoding")
	}
	for _, marker := range []string{"4kvm://chapter-real?dataid=123&quality=1080", "4kvm://chapter-real?dataid=123"} {
		if _, _, _, ok := parse4KVMMarker(marker); !ok {
			t.Fatal("valid provider marker rejected")
		}
	}
	for _, marker := range []string{"4kvm://../../admin?dataid=123", "4kvm://chapter-real?dataid=0", "4kvm://chapter-real?dataid=123&dataid=456", "4kvm://chapter-real?dataid=123&redirect=http://localhost", "4kvm://chapter-real?dataid=123&quality=invalid"} {
		if _, _, _, ok := parse4KVMMarker(marker); ok {
			t.Fatal("invalid provider marker accepted")
		}
	}
}

func Test4KVMCollectionMarkerUsesActualChapterAndKeepsLegacy(t *testing.T) {
	bridge := NewCMSBridge()
	bridge.downloader.cfg.RequestIntervalMS = 1
	requests, pageRequests := 0, 0
	primary := "https://oss.douyinbit.com/ep2.m3u8"
	bridge.downloader.client.Transport = cmsRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		body := ""
		switch request.URL.Path {
		case "/play/episode-2":
			pageRequests++
			body = `userlink:'anonymous-fixture';`
		case "/video/play":
			if request.URL.Query().Get("v") != "episode-2" || request.URL.Query().Get("p") != "102" {
				t.Fatal("playback was signed with the collection slug instead of the real chapter")
			}
			body = `{"code":200,"data":{"quality_urls":[{"url":"` + primary + `","title":"1080p"}]}}`
		case "/ep2.m3u8":
			body = "#EXTM3U\n#EXTINF:5,\nep2.ts\n#EXT-X-ENDLIST\n"
		case "/ep2.ts":
			body = fourKVMTestTSPayload()
		default:
			t.Fatalf("resolver fetched the collection or an unrelated episode page: %s", request.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})
	newMarker := "4kvm://film-entry?dataid=102&quality=1080&chapter=episode-2"
	for _, test := range []struct{ sourceID, marker string }{
		{"film-entry", newMarker},
		{"episode-2", "4kvm://episode-2?dataid=102&quality=1080"},
	} {
		media, err := bridge.Resolve(context.Background(), source4KVM, test.sourceID, test.marker)
		if err != nil || media.URL != primary {
			t.Fatalf("new collection marker or legacy chapter marker failed: source=%s err=%v", test.sourceID, err)
		}
	}
	// An older task can retain its collection PageURL, but the marker still
	// identifies which episode page supplies the anonymous signing token.
	_, err := bridge.downloader.resolve4KVMChapter(context.Background(), Task{}, Chapter{VideoURL: newMarker, PageURL: fourKVMBaseURL + "/play/film-entry"})
	if err != nil || pageRequests != 3 {
		t.Fatal("stale collection page overrode the actual chapter signing page")
	}
	before := requests
	if _, err = bridge.Resolve(context.Background(), source4KVM, "other-film", newMarker); err == nil || requests != before {
		t.Fatal("marker collection identity mismatch was accepted or triggered a request")
	}
	if _, err = bridge.Resolve(context.Background(), source4KVM, "film-entry", "4kvm://episode-2?dataid=102&quality=1080"); err == nil || requests != before {
		t.Fatal("legacy marker bypassed the source identity check")
	}
	for _, query := range []string{"chapter=", "chapter=../../admin", "chapter=%2Fprivate", "chapter=ep2&chapter=ep3", "chapter=" + strings.Repeat("a", 65)} {
		if _, ok := parse4KVMPlaybackRef("4kvm://film-entry?dataid=102&quality=1080&" + query); ok {
			t.Fatal("invalid or duplicate chapter identifier accepted")
		}
	}
}

func Test4KVMMasterAndAESKeyProbeIsBounded(t *testing.T) {
	bridge := NewCMSBridge()
	bridge.downloader.cfg.RequestIntervalMS = 1
	paths := []string{}
	bridge.downloader.client.Transport = cmsRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		paths = append(paths, request.URL.Path)
		body := ""
		switch request.URL.Path {
		case "/master.m3u8":
			body = "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=2000000\nchild.m3u8\n"
		case "/child.m3u8":
			body = "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\n#EXTINF:5,\none.ts\n#EXT-X-ENDLIST\n"
		case "/key.bin":
			if request.Header.Get("Range") != "bytes=0-16" {
				t.Fatal("AES key probe was not bounded")
			}
			body = strings.Repeat("k", 16)
		case "/one.ts":
			if request.Header.Get("Range") != "bytes=0-16383" {
				t.Fatal("encrypted chunk probe was not bounded")
			}
			body = strings.Repeat("encrypted", 50)
		default:
			t.Fatalf("unexpected probe path: %s", request.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})
	_, err := bridge.downloader.fourKVMSelectMedia(context.Background(), "https://oss.douyinbit.com/master.m3u8", nil, fourKVMBaseURL+"/")
	if err != nil || len(paths) != 4 {
		t.Fatalf("expected exactly master, child, AES key and first chunk: paths=%v err=%v", paths, err)
	}
}

func Test4KVMProbeRedirectClearsImageCDNHeadersAndUsesFinalBase(t *testing.T) {
	bridge := NewCMSBridge()
	bridge.downloader.cfg.RequestIntervalMS = 1
	checks, requests := 0, 0
	bridge.downloader.client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		checks++
		// A preexisting policy can recreate both headers during a redirect.
		next.Header.Set("Referer", "https://www.4kvm.net/")
		next.Header.Set("Origin", "https://www.4kvm.net")
		return nil
	}
	bridge.downloader.client.Transport = cmsRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		status, body, header := 200, "", http.Header{}
		switch request.URL.Hostname() + request.URL.Path {
		case "oss.douyinbit.com/start.m3u8":
			if request.Header.Get("Referer") != fourKVMBaseURL+"/" {
				t.Fatal("primary media lost its provider referer")
			}
			status = http.StatusFound
			header.Set("Location", "https://sns-open-qc.xhscdn.com/nested/index.m3u8")
		case "sns-open-qc.xhscdn.com/nested/index.m3u8":
			body = "#EXTM3U\n#EXTINF:5,\npart.ts\n#EXT-X-ENDLIST\n"
		case "sns-open-qc.xhscdn.com/nested/part.ts":
			if request.Header.Get("Range") != "bytes=0-16383" {
				t.Fatal("redirected media probe lost its bounded range")
			}
			body = fourKVMTestTSPayload()
		default:
			t.Fatalf("relative media URL did not use the final manifest base: host=%s path=%s", request.URL.Hostname(), request.URL.Path)
		}
		if fourKVMImageCDN(request.URL.Hostname()) && (request.Header.Get("Referer") != "" || request.Header.Get("Origin") != "") {
			t.Fatal("automatic or inherited redirect headers reached the image CDN")
		}
		return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})
	primary := "https://oss.douyinbit.com/start.m3u8"
	media, err := bridge.downloader.fourKVMSelectMedia(context.Background(), primary, nil, fourKVMBaseURL+"/")
	if err != nil || media.URL != primary || media.Duration != 5*time.Second || checks != 1 || requests != 3 {
		t.Fatalf("redirected probe did not preserve original URL/policy or validate the relative chunk: checks=%d requests=%d duration=%v err=%v", checks, requests, media.Duration, err)
	}
	if _, unchanged := bridge.downloader.client.Transport.(cmsRoundTripFunc); !unchanged {
		t.Fatal("4KVM-specific guard modified the shared catalog/API client")
	}
	bridge.downloader.limiter.mu.Lock()
	active := bridge.downloader.limiter.active
	bridge.downloader.limiter.mu.Unlock()
	if active != 0 {
		t.Fatal("media probe leaked a request limiter slot")
	}
}

func Test4KVMProbeRedirectRejectsUnsafeDestinations(t *testing.T) {
	for _, destination := range []string{
		"http://127.0.0.1/private", "http://localhost/private",
		"https://xhscdn.com.example.org/media", "https://user:pass@oss.douyinbit.com/media",
		"https://oss.douyinbit.com:8000/media",
	} {
		t.Run(destination, func(t *testing.T) {
			bridge := NewCMSBridge()
			bridge.downloader.cfg.RequestIntervalMS = 1
			requests := 0
			bridge.downloader.client.Transport = cmsRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				requests++
				return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {destination}}, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
			})
			_, _, err := bridge.downloader.fourKVMProbePlaylist(context.Background(), "https://oss.douyinbit.com/one.m3u8", fourKVMBaseURL+"/")
			if err == nil || requests != 1 {
				t.Fatalf("probe fetched a prohibited redirected destination: requests=%d", requests)
			}
		})
	}
}

func Test4KVMProbeKeepsPreviousRedirectRejection(t *testing.T) {
	bridge := NewCMSBridge()
	bridge.downloader.cfg.RequestIntervalMS = 1
	denied := errors.New("existing redirect policy rejected the request")
	checks, requests := 0, 0
	bridge.downloader.client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		checks++
		return denied
	}
	bridge.downloader.client.Transport = cmsRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {"https://sns-open-qc.xhscdn.com/one.m3u8"}}, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
	})
	request, _ := http.NewRequest(http.MethodGet, "https://oss.douyinbit.com/one.m3u8", nil)
	_, err := bridge.downloader.do4KVMProbeRequest(request, time.Second)
	if !errors.Is(err, denied) || checks != 1 || requests != 1 {
		t.Fatalf("original HTTP client's redirect rejection was bypassed: checks=%d requests=%d", checks, requests)
	}
	bridge.downloader.limiter.mu.Lock()
	active := bridge.downloader.limiter.active
	bridge.downloader.limiter.mu.Unlock()
	if active != 0 {
		t.Fatal("redirect rejection leaked a request limiter slot")
	}
}

func TestCMSHongguoCatalogCursor(t *testing.T) {
	bridge := NewCMSBridge()
	bridge.downloader.cfg.RequestIntervalMS = 1
	offsets := []int{}
	bridge.downloader.client.Transport = cmsRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/reading/distribution/category/landpage/v/" || request.Header.Get("X-Gorgon") == "" {
			t.Fatalf("wrong Hongguo request: %s", request.URL)
		}
		var payload struct {
			Offset int `json:"offset"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		offsets = append(offsets, payload.Offset)
		body := `{"data":{"video_data":[{"series_id":"` + strconv.Itoa(700000+payload.Offset) + `","series_title":"测试短剧"}],"has_more":` + strconv.FormatBool(payload.Offset == 0) + `,"next_offset":18,"session_id":"session-1"}}`
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})
	first, err := bridge.List(context.Background(), sourceHongguo, 1, 1)
	if err != nil || len(first.Dramas) != 1 || !first.HasMore {
		t.Fatalf("first page: %+v, %v", first, err)
	}
	second, err := bridge.List(context.Background(), sourceHongguo, 1, 2)
	if err != nil || len(second.Dramas) != 1 || second.HasMore || second.Dramas[0].SourceID != "700018" {
		t.Fatalf("second page: %+v, %v", second, err)
	}
	if len(offsets) != 2 || offsets[0] != 0 || offsets[1] != 18 {
		t.Fatalf("pagination offsets: %v", offsets)
	}
}
