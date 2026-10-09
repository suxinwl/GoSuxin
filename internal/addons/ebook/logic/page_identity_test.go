package album

import (
	"github.com/suxinwl/GoSuxin/framework/container/gvar"
	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"net/url"
	"testing"
)

func TestPageURLStableAcrossReorder(t *testing.T) {
	before := pageFileURL(12, 101, 1, "grant")
	after := pageFileURL(12, 101, 9, "grant")
	if before != after {
		t.Fatal("sorting changed page identity URL")
	}
	u, _ := url.Parse(before)
	if u.Query().Get("pageId") != "101" || u.Query().Has("pageNo") {
		t.Fatal(u)
	}
	if before == pageFileURL(12, 102, 1, "grant") {
		t.Fatal("different images share a URL")
	}
	if pageFileURL(12, 0, 1, "grant") != FileURL(12, "page", 1, "grant") {
		t.Fatal("legacy URL broken")
	}
	row := gdb.Record{"id": gvar.New(12), "cover_url": gvar.New("")}
	pages := gdb.Result{{"id": gvar.New(101), "page_no": gvar.New(9), "image_url": gvar.New("private-path")}}
	PresentAlbum(row, pages, "grant")
	if pages[0]["image_url"].String() != after+imageRevision("private-path") || pages[0]["thumbnail_url"].String() != after+imageRevision("private-path") {
		t.Fatal("API did not use stable identity")
	}
	if imageRevision("private-path") == imageRevision("replacement-path") {
		t.Fatal("replacement did not change version")
	}
}
