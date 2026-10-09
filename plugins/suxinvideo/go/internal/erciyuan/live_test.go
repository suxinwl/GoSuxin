package erciyuan

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// Explicit opt-in only: normal package tests never contact external services.
// Logs contain counts and public CDN hostnames, never signed URLs or material.
func TestLiveOriginalClientProtocol(t *testing.T) {
	if os.Getenv("SUXINVIDEO_LIVE_ERCIYUAN") != "1" {
		t.Skip("live source test requires explicit opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client := NewClient(nil)
	categories, err := client.Categories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("categories=%d", len(categories))
	page, err := client.List(ctx, 2, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("category=2 page=%d count=%d total=%d pages=%d", page.Page, len(page.Items), page.Total, page.PageCount)
	rows, err := client.Search(ctx, "仙逆")
	if err != nil {
		t.Fatal(err)
	}
	filmID := ""
	for _, row := range rows {
		if stringValue(row["vod_name"]) == "仙逆" {
			filmID = stringValue(row["vod_id"])
			break
		}
	}
	if filmID == "" {
		t.Fatal("exact search match missing")
	}
	detail, err := client.Detail(ctx, filmID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("film=%s lines=%d", filmID, len(detail.Lines))
	marker := ""
	for _, line := range detail.Lines {
		t.Logf("line=%s episodes=%d", line.Code, len(line.Episodes))
		if line.Code == "ecy_aa03" && len(line.Episodes) > 0 {
			marker = line.Episodes[len(line.Episodes)-1].URL
		}
	}
	if marker == "" {
		t.Fatal("reviewed telecom line missing")
	}
	item, err := client.detailItem(ctx, filmID)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range extractLines(item) {
		if line.key != "aa03" {
			continue
		}
		for _, parser := range line.parsers {
			address, _ := url.Parse(stringValue(parser["url"]))
			if address != nil {
				t.Logf("parser type=%s scheme=%s host=%s port=%s path=%s field=%s query_placeholder=%t userinfo=%t fragment=%t", stringValue(parser["type"]), address.Scheme, address.Hostname(), address.Port(), address.Path, stringValue(parser["jsonPlayUrl"]), strings.Contains(address.RawQuery, "{url}"), address.User != nil, address.Fragment != "")
			}
		}
	}
	media, err := client.Resolve(ctx, marker)
	if err != nil {
		t.Fatal(err)
	}
	address, _ := url.Parse(media.URL)
	_, line, index, _ := ParseMarker(marker)
	t.Logf("resolved line=%s episode_index=%d media_host=%s", line, index, address.Hostname())
}
