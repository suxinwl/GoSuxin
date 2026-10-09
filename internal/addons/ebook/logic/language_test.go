package album

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/os/gtime"
)

func fixturePNG() []byte {
	b, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aRZkAAAAASUVORK5CYII=")
	return b
}
func fixtureMultipart(t *testing.T, name string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	b := new(bytes.Buffer)
	w := multipart.NewWriter(b)
	f, e := w.CreateFormFile("file", name)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Write(data); e != nil {
		t.Fatal(e)
	}
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
	return b, w.FormDataContentType()
}

func TestLanguageAndStorageRoots(t *testing.T) {
	for host, want := range map[string]string{EnglishDomain: "en", "EN.ALBUMS.LOCALHOST:443": "en", EnglishDomain + ".": "en", ChineseDomain: "zh", "127.0.0.1:8600": "zh", EnglishDomain + ".evil.test": "zh"} {
		if LanguageForHost(host) != want {
			t.Fatalf("host %s", host)
		}
	}
	english := int64(200)
	p := StorageProfile{ID: "cloud", ParentID: 100, EnglishParentID: &english}
	zh, err := targetForLanguage(p, "zh")
	if err != nil || zh.ParentID != 100 {
		t.Fatal("Chinese target")
	}
	en, err := targetForLanguage(p, "en")
	if err != nil || en.ParentID != 200 || p.ParentID != 100 {
		t.Fatal("English target must not mutate shared configuration")
	}
	p.EnglishParentID = nil
	if _, err := targetForLanguage(p, "en"); err == nil {
		t.Fatal("English upload must not fall back to Chinese root")
	}
	if _, err := targetForLanguage(StorageProfile{}, "en"); err != nil {
		t.Fatal("local English upload")
	}
}

func bilingualDatabaseChecks(t *testing.T, ctx context.Context) {
	category, err := SaveCategory(ctx, 0, "双语测试", "Bilingual collection", "中文简介", 99, "English description")
	if err != nil {
		t.Fatal(err)
	}
	id, err := g.Model("album").Data(g.Map{"owner_id": 1, "title": "English catalogue", "category_id": category, "language": "en", "source_type": "images", "status": "ready", "visibility": "public", "createtime": gtime.Now()}).InsertAndGetId()
	if err != nil {
		t.Fatal(err)
	}
	m := installMockPan(t)
	english := int64(202)
	profileID, err := SaveStorageProfile(StorageProfile{ClientID: "bilingual", ClientSecret: "test", CDNKey: "test-key", ParentID: 101, EnglishParentID: &english})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := storageProfile(profileID)
	if err := markStorageVerified(p, 13); err != nil {
		t.Fatal(err)
	}
	if err := ActivateStorage(profileID); err != nil {
		t.Fatal(err)
	}
	defer ActivateStorage("local")
	server := g.Server("bilingual-test")
	server.SetAddr("127.0.0.1:0")
	server.SetDumpRouterMap(false)
	server.BindHandler("/cover", func(r *ghttp.Request) {
		r.SetCtxVar("uid", 1)
		e := SetCategoryCover(r.Context(), category, r.Get("lang").String(), r.GetUploadFile("file"), r.Get("remove").Bool())
		if e != nil {
			r.Response.WriteStatus(400, e.Error())
			return
		}
		r.Response.Write("ok")
	})
	server.BindHandler("/image", func(r *ghttp.Request) {
		r.SetCtxVar("uid", 1)
		_, e := AddImage(r.Context(), id, r.GetUploadFile("file"), 0)
		if e != nil {
			r.Response.WriteStatus(400, e.Error())
			return
		}
		r.Response.Write("ok")
	})
	server.BindHandler("/pdf", func(r *ghttp.Request) {
		r.SetCtxVar("uid", 1)
		if e := SubmitPDF(r.Context(), id, r.GetUploadFile("file"), false); e != nil {
			r.Response.WriteStatus(400, e.Error())
			return
		}
		r.Response.Write("ok")
	})
	server.BindHandler("/detail", func(r *ghttp.Request) {
		a, e := PublicAlbum(r.Context(), id)
		if e != nil || a.IsEmpty() {
			r.Response.WriteStatus(404)
			return
		}
		r.Response.Write("ok")
	})
	server.BindHandler("/common/album/category-cover", ServeCategoryCover)
	if e := server.Start(); e != nil {
		t.Fatal(e)
	}
	defer server.Shutdown()
	base := fmt.Sprintf("http://127.0.0.1:%d", server.GetListenedPort())
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	post := func(path string, pdf ...bool) {
		name, data := "page.png", fixturePNG()
		if len(pdf) > 0 {
			name, data = "source.pdf", storageProbePDF()
		}
		body, content := fixtureMultipart(t, name, data)
		req, _ := http.NewRequest("POST", base+path, body)
		req.Header.Set("Content-Type", content)
		res, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("%s: %d", path, res.StatusCode)
		}
	}
	post("/image")
	post("/cover?lang=zh")
	post("/cover?lang=en")
	m.mu.Lock()
	for _, file := range m.files {
		filename := fmt.Sprint(file["filename"])
		parent := fmt.Sprint(file["parentFileID"])
		if strings.HasPrefix(filename, fmt.Sprint(id)+"/") && parent != "202" {
			t.Error("English album in wrong root")
		}
		if !strings.HasPrefix(filename, fmt.Sprint(id)+"/") && !strings.HasPrefix(filename, fmt.Sprintf("_categories/%d/", category)) {
			t.Error("wrong folder layout", filename)
		}
	}
	m.mu.Unlock()
	cats, _ := Categories(ctx)
	PresentCategories(cats, "en")
	for _, row := range cats {
		if row["id"].Int64() == category && (row["name"].String() != "Bilingual collection" || row["description"].String() != "English description" || !strings.Contains(row["cover_url"].String(), "language=en")) {
			t.Fatal("English category presentation")
		}
	}
	for _, lang := range []string{"zh", "en"} {
		res, e := client.Get(fmt.Sprintf("%s/common/album/category-cover?id=%d&language=%s", base, category, lang))
		if e != nil {
			t.Fatal(e)
		}
		res.Body.Close()
		if res.StatusCode != 200 || res.Header.Get("ETag") == "" || res.Header.Get("Location") != "" {
			t.Fatal("category cover cache response invalid")
		}
	}
	post("/cover?lang=en&remove=true")
	res, _ := client.Get(fmt.Sprintf("%s/common/album/category-cover?id=%d&language=en", base, category))
	res.Body.Close()
	if res.StatusCode != 404 {
		t.Fatal("removed cover still accessible")
	}
	if m.trashCalls != 1 {
		t.Fatal("removed cloud cover not trashed")
	}
	post("/pdf", true)
	deadline := time.Now().Add(10 * time.Second)
	for {
		row, e := g.Model("album").Where("id", id).One()
		if e != nil {
			t.Fatal(e)
		}
		if row["status"].String() == "ready" {
			if row["page_count"].Int() != 1 || row["storage_parent_id"].Int64() != english {
				t.Fatal("English PDF target or pages")
			}
			break
		}
		if row["status"].String() == "failed" || time.Now().After(deadline) {
			t.Fatal("English PDF failed", row["failure_reason"].String())
		}
		time.Sleep(25 * time.Millisecond)
	}
	m.mu.Lock()
	for _, file := range m.files {
		if strings.HasPrefix(fmt.Sprint(file["filename"]), fmt.Sprint(id)+"/") && fmt.Sprint(file["parentFileID"]) != "202" {
			t.Error("English PDF source/page in wrong root")
		}
	}
	m.mu.Unlock()
	g.Model("album").Where("id", id).Data(g.Map{"status": "published"}).Update()
	for host, status := range map[string]int{ChineseDomain: 404, EnglishDomain: 200} {
		req, _ := http.NewRequest("GET", base+"/detail?language=en", nil)
		req.Host = host
		res, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		res.Body.Close()
		if res.StatusCode != status {
			t.Fatalf("host separation %s %d", host, res.StatusCode)
		}
	}
}
