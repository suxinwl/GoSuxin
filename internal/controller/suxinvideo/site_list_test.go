package suxinvideo

import (
	"bytes"
	"context"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

func testLocalChannelTaxonomy() localChannelTaxonomy {
	return buildLocalChannelTaxonomy([]row{
		{"id": 1, "pid": 0, "name": "电影", "status": 1},
		{"id": 2, "pid": 0, "name": "剧集", "status": 1},
		{"id": 3, "pid": 0, "name": "动漫", "status": 1},
		{"id": 6, "pid": 0, "name": "短剧", "status": 1},
		{"id": 9, "pid": 1, "name": "剧情片", "status": 1},
		{"id": 10, "pid": 9, "name": "获奖佳片", "status": 1},
		{"id": 12, "pid": 2, "name": "欧美剧", "status": 1},
		{"id": 20, "pid": 0, "name": "篮球", "status": 1},
		{"id": 23, "pid": 2, "name": "韩剧", "status": 1},
		{"id": 26, "pid": 1, "name": "伦理片", "status": 0},
		{"id": 27, "pid": 26, "name": "隐藏分类的子项", "status": 1},
		{"id": 28, "pid": 6, "name": "现代都市", "status": 1},
		{"id": 44, "pid": 0, "name": "足球", "status": 1},
		{"id": 46, "pid": 0, "name": "动漫电影", "status": 1},
		{"id": 53, "pid": 0, "name": "都市", "status": 1},
		{"id": 65, "pid": 0, "name": "日常", "status": 1},
		{"id": 103, "pid": 0, "name": "奈飞Netflix", "status": 1},
		{"id": 104, "pid": 0, "name": "高清韩剧", "status": 1},
		{"id": 105, "pid": 0, "name": "体育", "status": 1},
		{"id": 200, "pid": 201, "name": "循环A", "status": 1},
		{"id": 201, "pid": 200, "name": "循环B", "status": 1},
		{"id": 202, "pid": 999, "name": "失效父分类", "status": 1},
	})
}

func TestLocalChannelTaxonomyUsesCategoryEvidenceAndFullParentTree(t *testing.T) {
	tax := testLocalChannelTaxonomy()
	for _, item := range []struct {
		channel int
		id      int64
	}{{2, 1}, {2, 9}, {2, 10}, {3, 12}, {3, 23}, {56, 23}, {56, 104}, {5, 20}, {5, 44}, {5, 105}, {50, 28}, {2, 46}, {8, 46}, {65, 103}} {
		if !tax.channelContains(item.channel, item.id) {
			t.Errorf("channel %d missing local category %d", item.channel, item.id)
		}
	}
	for _, item := range []struct {
		channel int
		id      int64
	}{{65, 12}, {56, 12}, {2, 53}, {50, 53}, {65, 65}, {2, 26}, {2, 27}} {
		if tax.channelContains(item.channel, item.id) {
			t.Errorf("channel %d incorrectly acquired local category %d", item.channel, item.id)
		}
	}
	for _, id := range []int64{26, 27, 200, 201, 202} {
		if tax.ByID[id] != nil {
			t.Errorf("invalid category %d is available", id)
		}
	}
	if !tax.channelContains(0, 53) || !tax.channelContains(0, 65) {
		t.Fatal("unclassified legacy genres disappeared from the local library")
	}
	if got := tax.descendants(1); !reflect.DeepEqual(got, []int64{1, 9, 10}) {
		t.Fatalf("recursive descendants = %v", got)
	}
}

func TestLocalChannelQueryKeepsSourceIndependentAndBindsFilters(t *testing.T) {
	req := &YQKChannelReq{ID: 5, Type: 44, Order: "hits", Class: "足球' OR 1=1 --", Year: "2026", Area: "中国"}
	query, err := buildLocalChannelQuery(req, testLocalChannelTaxonomy(), "visible-source-and-policy")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(query.Where, "visible-source-and-policy") || !strings.Contains(query.Where, "v.type_id IN") || !strings.Contains(query.Where, "v.class LIKE ?") {
		t.Fatalf("missing visibility/category/filter predicates: %s", query.Where)
	}
	if strings.Contains(query.Where, req.Class) || strings.Contains(query.Where, "v.api_id=?") || query.Order != "v.total_hits DESC,v.id DESC" {
		t.Fatal("the local multisource query leaked input or restricted one provider")
	}
	if got := query.Args[len(query.Args)-3:]; !reflect.DeepEqual(got, []any{"%" + req.Class + "%", "%2026%", "%中国%"}) {
		t.Fatalf("bound filters = %v", got)
	}
	for _, bad := range []*YQKChannelReq{{ID: 5, Type: 1}, {ID: 65, Type: 65}, {ID: 2, Type: 26}, {ID: 999}, {ID: 3, Topic: 123}, {Order: "id;DROP TABLE sx_vod"}, {Page: 10001}, {Area: strings.Repeat("中", 31)}} {
		if _, err := buildLocalChannelQuery(bad, testLocalChannelTaxonomy(), "1=1"); err == nil {
			t.Errorf("invalid request accepted: %+v", bad)
		}
	}
}

func TestLocalChannelShortDramaEvidenceDoesNotReclassifyGenre(t *testing.T) {
	tax := testLocalChannelTaxonomy()
	tax.Evidence[50] = map[int64]bool{53: true}
	query, err := buildLocalChannelQuery(&YQKChannelReq{ID: 50, Type: 53}, tax, "visible")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(query.Where, "v.type_id IN") != 3 || !strings.Contains(query.Where, "hc.api_url='hongguo://app'") || !strings.Contains(query.Where, "hc.status=1") {
		t.Fatalf("missing actual short-drama evidence: %s", query.Where)
	}
	if tax.Channels[50][53] {
		t.Fatal("provider evidence silently changed the generic category's meaning")
	}
	if tax.channelContains(2, 53) {
		t.Fatal("generic urban content became movie")
	}
}

func TestLocalChannelPaginationAndLinksRetainAllFilters(t *testing.T) {
	for _, item := range []struct{ total, requested, page, pages, offset int }{{0, 0, 1, 1, 0}, {25, 2, 2, 2, 24}, {48, 999, 2, 2, 24}, {49, 3, 3, 3, 48}} {
		page, pages, offset := localChannelPagination(item.total, item.requested)
		if page != item.page || pages != item.pages || offset != item.offset {
			t.Fatalf("pagination %+v => %d/%d offset%d", item, page, pages, offset)
		}
	}
	req := &YQKChannelReq{ID: 56, Type: 23, Order: "score", Class: "爱情", Year: "2026", Area: "韩国"}
	link, err := url.Parse(localChannelURL(req, 3))
	if err != nil {
		t.Fatal(err)
	}
	for key, expected := range map[string]string{"id": "56", "type": "23", "order": "score", "class": "爱情", "year": "2026", "area": "韩国", "page": "3", "source": "local"} {
		if link.Query().Get(key) != expected {
			t.Fatalf("%s lost in pagination", key)
		}
	}
	remote, _ := url.Parse(yqkChannelURL(56, 109, 2, "time"))
	if remote.Query().Get("source") != "remote" || remote.Query().Get("topic") != "109" {
		t.Fatal("APP topic pagination entered local lists")
	}
	children := localChannelChildren(testLocalChannelTaxonomy())
	for _, child := range children[5] {
		link, _ := url.Parse(gconvStringForTest(child["url"]))
		if link.Query().Get("id") != "5" {
			t.Fatal("sports navigation conflated local and APP IDs")
		}
	}
}

func gconvStringForTest(value any) string { result, _ := value.(string); return result }

func TestLocalChannelBodyRendersAllThemesWithLocalFilmLinks(t *testing.T) {
	for theme, base := range themePages {
		t.Run(theme, func(t *testing.T) {
			page, err := base.Clone()
			if err != nil {
				t.Fatal(err)
			}
			page, err = page.New("body").Parse(localChannelBody)
			if err != nil {
				t.Fatal(err)
			}
			data := map[string]any{
				"Title": "体育", "Theme": theme, "SiteMode": "cms", "ID": 5, "TypeID": 44, "Order": "hits",
				"Class": "", "Year": "", "Area": "", "Total": 25, "Page": 1, "Pages": 2,
				"Categories": []catalogLink{{Name: "体育", URL: "/suxinvideo/channel?id=5", Active: true}},
				"Sources":    []catalogLink{{Name: "本站片库", URL: localChannelURL(&YQKChannelReq{ID: 5}, 1), Active: true}, {Name: "小柒精选与专题", URL: yqkChannelURL(5, 0, 1, "time")}},
				"Subtypes":   []catalogLink{{Name: "足球", URL: localChannelURL(&YQKChannelReq{ID: 5, Type: 44}, 1), Active: true}},
				"Orders":     []catalogLink{{Name: "热播", URL: localChannelURL(&YQKChannelReq{ID: 5, Order: "hits"}, 1), Active: true}},
				"Vods":       []row{{"id": 1045, "name": strings.Repeat("足球冠军联赛", 8), "pic": "/poster.svg", "remarks": "全场", "year": "2026", "score": "8.0", "area": "中国", "vip": 0}},
				"NextURL":    localChannelURL(&YQKChannelReq{ID: 5, Type: 44, Order: "hits"}, 2),
			}
			var body bytes.Buffer
			if err = page.ExecuteTemplate(&body, "body", data); err != nil {
				t.Fatal(err)
			}
			html := body.String()
			for _, text := range []string{"本站片库", "小柒精选与专题", "足球", "/suxinvideo/detail?id=1045", "source=remote", "min-width:0", "sx-channel-page"} {
				if !strings.Contains(html, text) {
					t.Fatalf("missing %q", text)
				}
			}
			if strings.Contains(html, "#ZgotmplZ") || strings.Contains(html, "<no value>") || strings.Contains(html, "/yqk/vod") {
				t.Fatal("unresolved or remote-only local card")
			}
		})
	}
}

// Read-only acceptance uses the existing catalogue. It never creates a
// provider, film, category, or fixture, and performs no upstream request.
func TestLocalChannelLiveReadOnly(t *testing.T) {
	if os.Getenv("SUXIN_LOCAL_CATALOG_CHECK") != "1" {
		t.Skip("set SUXIN_LOCAL_CATALOG_CHECK=1 for read-only local catalogue acceptance")
	}
	t.Setenv("SUXIN_INTEGRATION", "1")
	ctx, cancel := context.WithTimeout(yqkIntegrationContext(t), 30*time.Second)
	defer cancel()
	tax, err := loadLocalChannelTaxonomy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, channel := range append([]yqkSiteChannel{{ID: 0, Name: "全部"}}, yqkDefaultSiteChannels...) {
		req := &YQKChannelReq{ID: channel.ID, Order: "time"}
		query, err := buildLocalChannelQuery(req, tax, publicVodCondition(ctx, "v"))
		if err != nil {
			t.Fatal(err)
		}
		count, err := one(ctx, "SELECT COUNT(*) n FROM sx_vod v WHERE "+query.Where, query.Args...)
		if err != nil {
			t.Fatalf("channel %d count: %v", channel.ID, err)
		}
		rows, err := all(ctx, "SELECT v.id,v.type_id FROM sx_vod v WHERE "+query.Where+" ORDER BY "+query.Order+" LIMIT 24", query.Args...)
		if err != nil {
			t.Fatalf("channel %d rows: %v", channel.ID, err)
		}
		if len(rows) > 24 {
			t.Fatal("page exceeded 24 titles")
		}
		for _, item := range rows {
			if gconv.Int64(item["id"]) <= 0 {
				t.Fatal("remote ID appeared in local catalogue")
			}
		}
		t.Logf("channel=%d name=%s local_titles=%d page_rows=%d", channel.ID, channel.Name, gconv.Int(count["n"]), len(rows))
	}
	for _, sport := range []string{"篮球", "足球"} {
		for _, item := range tax.Types {
			if gconv.String(item["name"]) == sport && !tax.channelContains(5, gconv.Int64(item["id"])) {
				t.Fatalf("existing sports category %s disappeared", sport)
			}
		}
	}
}
