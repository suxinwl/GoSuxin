package suxinvideo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"github.com/suxinwl/GoSuxin/internal/yqksign"
)

// This acceptance probe reads current upstream pages and local matches only.
// It never imports or edits business records.
func TestAppFeaturedReadOnlyCoverage(t *testing.T) {
	if os.Getenv("SUXIN_FEATURED_READONLY") != "1" {
		t.Skip("set SUXIN_FEATURED_READONLY=1 for the read-only provider coverage probe")
	}
	ctx := yqkIntegrationContext(t)
	ctx, cancel := context.WithTimeout(ctx, 80*time.Second)
	defer cancel()
	for _, channel := range []int{0, 50, 65} {
		started := time.Now()
		page, err := fetchYQKSiteChannelSized(ctx, channel, 0, 1, 30, "time", "")
		if err != nil {
			t.Logf("channel=%d unavailable=%v elapsed=%s", channel, err, time.Since(started).Round(time.Millisecond))
			continue
		}
		visible := yqkSiteRows(ctx, page.Items)
		local := appLocalizeRecommendations(ctx, visible)
		t.Logf("channel=%d remote=%d visible=%d local=%d topics=%d pages=%d elapsed=%s", channel, len(page.Items), len(visible), len(local), len(page.Topics), page.TotalPages, time.Since(started).Round(time.Millisecond))
		featured, err := appFeatured(ctx, channel, 0, 1, 30, "time")
		if err != nil {
			t.Fatalf("native featured contract unavailable for channel=%d: %v", channel, err)
		}
		cards := gconv.Maps(featured["items"])
		pending := 0
		for _, card := range cards {
			if gconv.Int64(card["id"]) > 0 {
				continue
			}
			pending++
			id, token := gconv.String(card["remote_id"]), gconv.String(card["resolve_token"])
			if card["provider"] != "yqk" || !validateAppFeaturedToken(id, token, time.Now().Unix(), func(id string, expires int64) string { return appFeaturedSignature(ctx, id, expires) }) {
				t.Fatal("uncollected card has no valid signed deferred resolver")
			}
		}
		t.Logf("native channel=%d cards=%d local=%d deferred=%d exact_pages=%v total=%v", channel, len(cards), len(cards)-pending, pending, featured["exact_pages"], featured["total"])
	}
}

func TestAppFeaturedCardsKeepCuratedOrderAndOnlyUseVerifiedLocalIDs(t *testing.T) {
	remote := []row{
		{"id": 0, "name": "First remote", "pic": "first.jpg", "score": "8.8", "__featured_remote_id": "101", "link": "signed-web-link"},
		{"id": 0, "name": "Matched film", "__featured_remote_id": "202"},
		{"id": 0, "name": "Hidden film", "__featured_remote_id": "303"},
		{"id": 0, "name": "Last remote", "__featured_remote_id": "404"},
	}
	matched := []row{{"id": 7001, "name": "Matched film", "vip": 1, "points": 12, "__featured_remote_id": "202"}}
	var signed []string
	cards := appFeaturedCards(remote, matched, map[string]bool{"303": true}, func(id string) string { signed = append(signed, id); return "token-for-" + id })
	if len(cards) != 3 || cards[0]["remote_id"] != "101" || gconv.Int64(cards[1]["id"]) != 7001 || cards[2]["remote_id"] != "404" {
		t.Fatalf("curated identity/order/hidden policy changed: %+v", cards)
	}
	if cards[0]["id"] != int64(0) || cards[0]["provider"] != "yqk" || cards[0]["resolve_token"] != "token-for-101" || cards[0]["score"] != "8.8" || cards[0]["pic"] != "first.jpg" {
		t.Fatal("remote film became a local ID or lost its original metadata/resolver")
	}
	if cards[1]["vip"] != 1 || cards[1]["points"] != 12 || len(signed) != 2 || signed[0] != "101" || signed[1] != "404" {
		t.Fatal("known entitlements changed or local/hidden cards were signed as remote")
	}
	for _, item := range cards {
		if _, present := item["__featured_remote_id"]; present {
			t.Fatal("private matching marker leaked")
		}
		if _, present := item["link"]; present {
			t.Fatal("HTML redirect link leaked into native cards")
		}
	}
	if remote[0]["link"] == nil || matched[0]["__featured_remote_id"] == nil {
		t.Fatal("assembling a response mutated its source metadata")
	}
}

func TestAppFeaturedResolveSignatureRejectsReplayExpiryAndMalformedTokens(t *testing.T) {
	now := int64(1800000000)
	goodSignature := strings.Repeat("a", 64)
	otherSignature := strings.Repeat("b", 64)
	signature := func(id string, expiry int64) string {
		if id == "101" && expiry == now+3600 {
			return goodSignature
		}
		return otherSignature
	}
	good := fmt.Sprintf("%d.%s", now+3600, goodSignature)
	if !validateAppFeaturedToken("101", good, now, signature) {
		t.Fatal("valid signature was rejected")
	}
	for _, test := range []struct{ id, token string }{
		{"202", good}, {"01", good}, {"101", good + ".suffix"},
		{"101", fmt.Sprintf("%d.%s", now-1, goodSignature)},
		{"101", fmt.Sprintf("%d.%s", now+7201, goodSignature)},
		{"101", "not-a-time." + goodSignature}, {"101", ""},
		{"101", fmt.Sprintf("%d.%s", now+3600, otherSignature)},
	} {
		if validateAppFeaturedToken(test.id, test.token, now, signature) {
			t.Fatalf("accepted replay, expiry or malformed token for %s", test.id)
		}
	}
}

func appFeaturedFixtureClient(t *testing.T) (*yqksign.Client, string) {
	t.Helper()
	films := func(start, count int) []map[string]any {
		items := make([]map[string]any, 0, count)
		for n := start; n < start+count; n++ {
			items = append(items, map[string]any{"vodId": strconv.Itoa(1000 + n), "vodName": fmt.Sprintf("Featured %d", n), "coverImg": "https://images.example/cover.jpg"})
		}
		return items
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var params map[string]any
		if json.NewDecoder(r.Body).Decode(&params) != nil {
			t.Error("provider call did not have JSON business parameters")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var data any
		switch r.URL.Path {
		case "/v1/api/search/queryNow":
			size := gconv.Int(params["nextCount"])
			if size < 1 || size > 100 {
				t.Errorf("invalid requested capacity %d", size)
			}
			cursor := gconv.String(params["nextVal"])
			start := 0
			if cursor != "" {
				if cursor != fmt.Sprintf("size-%d-next", size) {
					t.Errorf("cursor from another request capacity: %s", cursor)
				}
				start = size
			}
			data = map[string]any{"items": films(start, size), "hasNext": start == 0, "nextVal": fmt.Sprintf("size-%d-next", size)}
		case "/v2/api/channel/topicListView":
			data = map[string]any{"topicList": []map[string]any{
				{"vodTopicId": 201, "topicName": "First", "vodList": films(0, 20)},
				{"vodTopicId": 202, "topicName": "Second", "vodList": films(20, 20)},
			}}
		case "/v1/api/vodTopic/getVodList":
			size, page := gconv.Int(params["pageSize"]), gconv.Int(params["pageIndex"])
			start := (page - 1) * size
			data = map[string]any{"items": films(start, max(0, min(size, 43-start))), "pageIndex": page, "totalPages": (43 + size - 1) / size}
		default:
			t.Errorf("unexpected provider endpoint %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"result": true, "data": data})
	}))
	t.Cleanup(server.Close)
	client, err := yqksign.NewClient(yqksign.ClientOptions{BootstrapConfigURLs: []string{server.URL + "/config.json"}, Common: map[string]any{"appId": yqksign.AppID}, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Restore(yqksign.ClientState{ActiveBackend: server.URL}); err != nil {
		t.Fatal(err)
	}
	return client, server.URL
}

func TestAppFeaturedCapacityControlsPreviewSlicesAndTopicPageSize(t *testing.T) {
	client, address := appFeaturedFixtureClient(t)
	for _, size := range []int{12, 24, 30} {
		for _, topic := range []int{0, 201} {
			first, err := yqkSiteChannelFromClient(context.Background(), client, address, 65, topic, 1, size, "time", "")
			if err != nil || len(first.Items) != size || len(first.Topics) != 2 {
				t.Fatalf("capacity=%d topic=%d first page=%+v error=%v", size, topic, first, err)
			}
			second, err := yqkSiteChannelFromClient(context.Background(), client, address, 65, topic, 2, size, "time", "")
			if err != nil || len(second.Items) != min(size, map[bool]int{true: 43, false: 40}[topic > 0]-size) || second.Items[0]["vodId"] != strconv.Itoa(1000+size) {
				t.Fatalf("capacity=%d topic=%d second page=%+v error=%v", size, topic, second, err)
			}
		}
	}
}

func TestAppFeaturedCursorCacheSeparatesAppCapacityAndWebsitePages(t *testing.T) {
	isolateYQKSiteState(t)
	client, address := appFeaturedFixtureClient(t)
	for _, size := range []int{12, 30} {
		fetch := func(ctx context.Context, channel, topic, page int, order, cursor string) (yqkSiteRemotePage, error) {
			return yqkSiteChannelFromClient(ctx, client, address, channel, topic, page, size, order, cursor)
		}
		key := fmt.Sprintf("fixture:app:%d", size)
		for _, page := range []int{1, 2, 2} {
			data, err := yqkSitePageForKeyWithFetch(context.Background(), key, 0, 0, page, "time", fetch)
			if err != nil || len(data.Items) != size || data.Items[0]["vodId"] != strconv.Itoa(1000+(page-1)*size) {
				t.Fatalf("capacity=%d page=%d data=%+v error=%v", size, page, data, err)
			}
		}
	}
	fetchYQKChannelPage = func(ctx context.Context, channel, topic, page int, order, cursor string) (yqkSiteRemotePage, error) {
		return yqkSiteChannelFromClient(ctx, client, address, channel, topic, page, 24, order, cursor)
	}
	for _, page := range []int{1, 2} {
		data, err := yqkSitePageForKey(context.Background(), "fixture", 0, 0, page, "time")
		if err != nil || len(data.Items) != 24 || data.Items[0]["vodId"] != strconv.Itoa(1000+(page-1)*24) {
			t.Fatalf("website capacity changed: page=%d data=%+v error=%v", page, data, err)
		}
	}
}
