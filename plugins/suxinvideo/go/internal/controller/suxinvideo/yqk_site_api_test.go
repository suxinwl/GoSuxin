package suxinvideo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/suxinwl/GoSuxin/internal/yqksign"
)

func TestYQKSiteCardsKeepRemoteIdentityAndExcludeAppFields(t *testing.T) {
	input := []map[string]any{
		{"vodId": json.Number("9007199254740993"), "vodName": "影视名称", "flags": "2026 / 奈飞Netflix / 韩国", "coverImg": "https://example.com/poster.jpg", "remark": "更10集", "token": "do-not-copy", "actionContent": "https://example.com/ad", "startAd": map[string]any{"title": "advertisement"}},
		{"vodId": "9007199254740993", "vodName": "重复的同一影片"},
		{"vodId": "01", "vodName": "非法远端编号"},
		{"vodId": "2", "vodName": ""},
		{"vodId": "3", "vodName": strings.Repeat("x", 513)},
	}
	items := yqkSiteItems("unit:site-cards", input)
	if len(items) != 1 || items[0]["vodId"] != "9007199254740993" || items[0]["flags"] != input[0]["flags"] || items[0]["coverImg"] != input[0]["coverImg"] {
		t.Fatalf("film identity, provenance or artwork changed: %#v", items)
	}
	for _, key := range []string{"token", "actionContent", "startAd", "id", "type_id"} {
		if _, ok := items[0][key]; ok {
			t.Fatalf("APP or local database field %s entered a remote card", key)
		}
	}
}

func TestYQKSiteHomeKeepsSuccessfulSectionsWhenAnotherFails(t *testing.T) {
	for _, allFailed := range []bool{false, true} {
		t.Run(map[bool]string{false: "one-section-outage", true: "all-sections-outage"}[allFailed], func(t *testing.T) {
			calls := map[string]int{}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls[r.URL.Path]++
				w.Header().Set("Content-Type", "application/json")
				if allFailed || r.URL.Path == "/v1/api/search/queryNow" {
					_ = json.NewEncoder(w).Encode(map[string]any{"result": false, "msg": "temporary outage", "data": nil})
					return
				}
				film := map[string]any{"vodId": "123", "vodName": "测试影视", "flags": "2026 / 动漫 / 大陆", "coverImg": "https://example.com/poster.jpg"}
				var data any
				switch r.URL.Path {
				case "/v2/api/home/header":
					data = map[string]any{"channeList": []map[string]any{{"channelId": 8, "channelName": "动漫"}}}
				case "/v1/api/vodRank/getRankList":
					data = []map[string]any{film}
				case "/v2/api/home/firstScreen":
					data = map[string]any{"hotVodList": []map[string]any{film}, "focusAdList": []map[string]any{{"title": "APP广告"}}}
				default:
					t.Errorf("unexpected endpoint %s", r.URL.Path)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"result": true, "data": data})
			}))
			defer server.Close()
			client, err := yqksign.NewClient(yqksign.ClientOptions{BootstrapConfigURLs: []string{server.URL + "/config.json"}, Common: map[string]any{"appId": yqksign.AppID}, HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			if err = client.Restore(yqksign.ClientState{ActiveBackend: server.URL}); err != nil {
				t.Fatal(err)
			}
			home, err := yqkSiteHomeFromClient(context.Background(), client, server.URL)
			if allFailed {
				if err == nil || len(home.PartialErrors) != 4 {
					t.Fatal("all section failures should be retried, not treated as an empty successful home")
				}
			} else if err != nil || len(home.Channels) != 1 || len(home.Hot) != 1 || len(home.Recommended) != 1 || len(home.Recent) != 0 || len(home.PartialErrors) != 1 {
				t.Fatalf("outage discarded independent successful data: %#v, %v", home, err)
			}
			if len(calls) != 4 {
				t.Fatal("one failed endpoint prevented other home sections from being requested")
			}
		})
	}
}

func TestYQKSiteRejectsInvalidNavigationBeforeNetwork(t *testing.T) {
	for _, test := range []struct {
		channel, topic, page int
		order, cursor        string
	}{
		{999, 0, 1, "time", ""},
		{65, -1, 1, "time", ""},
		{65, 0, 0, "time", ""},
		{65, 0, 10001, "time", ""},
		{65, 0, 1, "unknown", ""},
		{65, 0, 1, "time", "invalid\r\n"},
		{65, 0, 1, "time", strings.Repeat("x", 2049)},
		{0, 5, 1, "time", ""},
		{0, 0, 2, "time", ""},
	} {
		if _, err := fetchYQKSiteChannel(context.Background(), test.channel, test.topic, test.page, test.order, test.cursor); err == nil {
			t.Fatalf("accepted invalid page: %+v", test)
		}
	}
}
