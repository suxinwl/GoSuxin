package album

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/suxinwl/GoSuxin/framework/contrib/drivers/mysql"
	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/os/gcfg"
	"github.com/suxinwl/GoSuxin/framework/os/gtime"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

// Opt-in: use an explicitly created, disposable ebook_test_* database.
func TestMySQLAlbumLifecycle(t *testing.T) {
	name := os.Getenv("ALBUM_TEST_DATABASE")
	if !strings.HasPrefix(name, "ebook_test_") {
		t.Skip("set ALBUM_TEST_DATABASE to a disposable ebook_test_* database")
	}
	bin := os.Getenv("ALBUM_TEST_PDFTOPPM")
	if bin == "" {
		t.Fatal("ALBUM_TEST_PDFTOPPM must point to Poppler")
	}
	t.Chdir(t.TempDir())
	err := gdb.SetConfigGroup("default", gdb.ConfigGroup{{Type: "mysql", Host: "127.0.0.1", Port: "3306", User: "root", Pass: os.Getenv("ALBUM_TEST_PASSWORD"), Name: name, Prefix: "gf_", Extra: "charset=utf8mb4&parseTime=True&loc=Local"}})
	if err != nil {
		t.Fatal(err)
	}
	configure := func(binary string, seconds int) {
		adapter, err := gcfg.NewAdapterContent(fmt.Sprintf("{\"album\":{\"pdfToPpmBin\":%q,\"pdfTimeoutSeconds\":%d}}", filepath.ToSlash(binary), seconds))
		if err != nil {
			t.Fatal(err)
		}
		g.Cfg("app").SetAdapter(adapter)
	}
	configure(bin, 30)
	ctx := context.Background()
	if err := Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		"CREATE TABLE gf_admin (id BIGINT PRIMARY KEY, status INT NOT NULL DEFAULT 0)",
		"CREATE TABLE gf_auth_role_access (uid BIGINT, role_id BIGINT)",
		"CREATE TABLE gf_auth_role (id BIGINT, rules VARCHAR(30))",
		"INSERT INTO gf_admin VALUES (1,0),(2,0)",
	} {
		if _, err := g.DB().Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	id, err := g.Model("album").Data(g.Map{"owner_id": 1, "title": "integration", "source_type": "images", "visibility": "private", "status": "ready", "page_count": 1, "cover_url": "/_album/fixture/page.jpg", "createtime": gtime.Now()}).InsertAndGetId()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll("storage/albums/fixture", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("storage/albums/fixture/page.jpg", []byte("private-image"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Model("album_page").Data(g.Map{"album_id": id, "page_no": 1, "image_url": "/_album/fixture/page.jpg", "createtime": gtime.Now()}).Insert(); err != nil {
		t.Fatal(err)
	}
	server := g.Server("album-integration")
	server.SetAddr("127.0.0.1:0")
	server.SetDumpRouterMap(false)
	server.BindHandler("/common/album/file", ServeAlbumFile)
	server.BindHandler("/resource/uploads/*file", ServeLegacyUpload)
	server.BindHandler("/action", func(r *ghttp.Request) {
		r.SetCtxVar("uid", r.Get("uid", 1).Int64())
		var err error
		switch r.Get("action").String() {
		case "publish":
			err = SetPublished(r.Context(), id, true)
		case "offline":
			err = SetPublished(r.Context(), id, false)
		case "upload":
			err = SubmitPDF(r.Context(), id, r.GetUploadFile("file"), false)
		case "retry":
			err = SubmitPDF(r.Context(), id, nil, true)
		case "image":
			_, err = AddImage(r.Context(), id, r.GetUploadFile("file"), 0)
		case "reorder", "delete", "cover", "replace":
			err = EditPages(r.Context(), id, r.Get("action").String(), gconv.Int64s(r.URL.Query()["ids"]), r.GetUploadFile("file"))
		case "share-update":
			_, err = UpdateShare(r.Context(), id, r.Get("shareId").Int64(), r.Get("mode", "keep").String(), r.Get("password").String(), nil, true, r.Get("enabled", true).Bool())
		}
		if err != nil {
			r.Response.WriteStatus(409, err.Error())
			return
		}
		r.Response.Write("ok")
	})
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer server.Shutdown()
	base := fmt.Sprintf("http://127.0.0.1:%d", server.GetListenedPort())
	client := &http.Client{Timeout: 5 * time.Second}
	get := func(path string, want int) {
		t.Helper()
		res, err := client.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("%s: got %d want %d: %s", strings.Split(path, "grant=")[0], res.StatusCode, want, b)
		}
	}
	update := func(data g.Map) {
		t.Helper()
		if _, err := g.Model("album").Where("id", id).Data(data).Update(); err != nil {
			t.Fatal(err)
		}
	}
	fileURL := FileURL(id, "page", 1, "")
	t.Run("private owner and anonymous", func(t *testing.T) {
		get(fileURL, 403)
		get(FileURL(id, "page", 1, NewFileGrant(id, 1, 0)), 200)
		get(FileURL(id, "page", 1, NewFileGrant(id, 2, 0)), 403)
		get("/_album/fixture/page.jpg", 404)
	})
	t.Run("stable page identity retains album scope", func(t *testing.T) {
		page, e := g.Model("album_page").Where("album_id", id).Where("page_no", 1).One()
		if e != nil {
			t.Fatal(e)
		}
		pid := page["id"].Int64()
		stable := pageFileURL(id, pid, 1, NewFileGrant(id, 1, 0))
		get(stable, 200)
		get(pageFileURL(id, pid, 1, NewFileGrant(id, 2, 0)), 403)
		get(pageFileURL(id, pid+100000, 1, NewFileGrant(id, 1, 0)), 404)
		g.Model("album_page").Where("id", pid).Data(g.Map{"page_no": 9}).Update()
		get(stable, 200)
		get(FileURL(id, "page", 1, NewFileGrant(id, 1, 0)), 404)
		g.Model("album_page").Where("id", pid).Data(g.Map{"page_no": 1}).Update()
	})
	t.Run("published public and offline", func(t *testing.T) {
		get("/action?action=publish&uid=2", 409)
		get("/action?action=publish", 200)
		update(g.Map{"visibility": "public"})
		get(fileURL, 200)
		get("/action?action=offline", 200)
		get(fileURL, 403)
		update(g.Map{"visibility": "private", "status": "published"})
	})
	t.Run("share expiry revocation and album scope", func(t *testing.T) {
		share, err := CreateShare(ctx, id, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		url := FileURL(id, "page", 1, NewFileGrant(id, 0, share["id"].Int64()))
		get(url, 200)
		if _, err := g.Model("album_share").Where("id", share["id"]).Data(g.Map{"enabled": 0}).Update(); err != nil {
			t.Fatal(err)
		}
		get(url, 403)
		if _, err := g.Model("album_share").Where("id", share["id"]).Data(g.Map{"enabled": 1, "expires_at": gtime.New(time.Now().Add(-time.Minute))}).Update(); err != nil {
			t.Fatal(err)
		}
		get(url, 403)
		get(FileURL(id, "page", 1, NewFileGrant(id+1, 0, share["id"].Int64())), 403)
	})
	t.Run("legacy direct paths blocked", func(t *testing.T) {
		os.MkdirAll("resource/uploads/custom", 0700)
		os.WriteFile("resource/uploads/custom/old.jpg", []byte("legacy-secret"), 0600)
		update(g.Map{"cover_url": "/resource/uploads/custom/old.jpg"})
		get("/resource/uploads/custom/old.jpg", 403)
		get("/resource/uploads/puty/unknown.jpg", 403)
		get(FileURL(id, "cover", 0, NewFileGrant(id, 1, 0)), 200)
		os.WriteFile("resource/uploads/custom/other.txt", []byte("ordinary attachment"), 0600)
		get("/resource/uploads/custom/other.txt", 200)
	})
	upload := func(content []byte, want int) {
		t.Helper()
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		part, _ := w.CreateFormFile("file", "book.pdf")
		part.Write(content)
		w.Close()
		res, err := client.Post(base+"/action?action=upload", w.FormDataContentType(), &body)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("upload %d want %d: %s", res.StatusCode, want, b)
		}
	}
	waitStatus := func(want string) {
		t.Helper()
		until := time.Now().Add(15 * time.Second)
		for time.Now().Before(until) {
			row, err := g.Model("album").Where("id", id).One()
			if err != nil {
				t.Fatal(err)
			}
			if row["status"].String() == want {
				return
			}
			time.Sleep(30 * time.Millisecond)
		}
		row, _ := g.Model("album").Where("id", id).One()
		t.Fatalf("expected %s: %v", want, row)
	}
	t.Run("concurrent image uploads allocate unique pages", func(t *testing.T) {
		get("/action?action=offline", 200)
		png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aRZkAAAAASUVORK5CYII=")
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		errors := make(chan error, 4)
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var body bytes.Buffer
				w := multipart.NewWriter(&body)
				part, _ := w.CreateFormFile("file", "page.png")
				part.Write(png)
				w.Close()
				res, err := client.Post(base+"/action?action=image", w.FormDataContentType(), &body)
				if err != nil {
					errors <- err
					return
				}
				defer res.Body.Close()
				b, _ := io.ReadAll(res.Body)
				if res.StatusCode != 200 {
					errors <- fmt.Errorf("image upload: %d %s", res.StatusCode, b)
				}
			}()
		}
		wg.Wait()
		close(errors)
		for err := range errors {
			t.Error(err)
		}
		count, err := g.Model("album_page").Where("album_id", id).Count()
		if err != nil || count != 5 {
			t.Fatalf("pages=%d err=%v", count, err)
		}
		get(FileURL(id, "page", 5, NewFileGrant(id, 1, 0)), 200)
	})
	t.Run("invalid uploads and processing guard", func(t *testing.T) {
		upload([]byte("not a PDF"), 409)
		update(g.Map{"status": "processing"})
		upload(minimalPDF(), 409)
		get("/action?action=publish", 409)
		get("/action?action=offline", 409)
		if err := Initialize(ctx); err != nil {
			t.Fatal(err)
		}
		waitStatus("failed")
		get("/action?action=publish", 409)
	})
	t.Run("real PDF failure retry and publish", func(t *testing.T) {
		configure("missing-pdftoppm.exe", 5)
		upload(minimalPDF(), 200)
		waitStatus("failed")
		get("/action?action=publish", 409)
		configure(bin, 30)
		get("/action?action=retry", 200)
		waitStatus("ready")
		row, _ := g.Model("album").Where("id", id).One()
		if row["page_count"].Int() != 1 || row["failure_reason"].String() != "" {
			t.Fatal(row)
		}
		get("/action?action=publish", 200)
		update(g.Map{"visibility": "public"})
		get(FileURL(id, "page", 1, ""), 200)
	})
	t.Run("conversion wait timeout", func(t *testing.T) {
		get("/action?action=offline", 200)
		configure(bin, 1)
		pdfSlots <- struct{}{}
		pdfSlots <- struct{}{}
		upload(minimalPDF(), 200)
		waitStatus("failed")
		<-pdfSlots
		<-pdfSlots
		get("/action?action=publish", 409)
	})
	t.Run("category migration rename delete and repeat", func(t *testing.T) {
		categories, err := Categories(ctx)
		if err != nil || len(categories) != 3 {
			t.Fatalf("categories: %v %v", categories, err)
		}
		categoryID := categories[0]["id"].Int64()
		key := categories[0]["stable_key"].String()
		update(g.Map{"category_id": categoryID, "category": categories[0]["name"]})
		if _, err := SaveCategory(ctx, categoryID, "Renamed", "EN", "description", 5); err != nil {
			t.Fatal(err)
		}
		if err := DeleteCategory(ctx, categoryID); err == nil {
			t.Fatal("deleted occupied category")
		}
		if err := MigrateCenter(ctx); err != nil {
			t.Fatal(err)
		}
		category, _ := g.Model("album_category").Where("id", categoryID).One()
		if category["stable_key"].String() != key || category["name"].String() != "Renamed" {
			t.Fatal(category)
		}
		row, _ := g.Model("album").Where("id", id).One()
		if row["category"].String() != "Renamed" {
			t.Fatal("album name not synchronized")
		}
		newID, err := SaveCategory(ctx, 0, "Temporary", "", "", 10)
		if err != nil {
			t.Fatal(err)
		}
		if err := DeleteCategory(ctx, newID); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("page ownership published guard reorder replace cover and delete", func(t *testing.T) {
		update(g.Map{"status": "published"})
		page, _ := g.Model("album_page").Where("album_id", id).One()
		pid := page["id"].Int64()
		get(fmt.Sprintf("/action?action=cover&ids=%d", pid), 409)
		get("/action?action=offline", 200)
		get(fmt.Sprintf("/action?action=cover&ids=%d&uid=2", pid), 409)
		// Add a second independently addressable page.
		os.WriteFile("storage/albums/fixture/second.jpg", []byte("second"), 0600)
		second, err := g.Model("album_page").Data(g.Map{"album_id": id, "page_no": 2, "image_url": "/_album/fixture/second.jpg", "createtime": gtime.Now()}).InsertAndGetId()
		if err != nil {
			t.Fatal(err)
		}
		get(fmt.Sprintf("/action?action=reorder&ids=%d&ids=%d", second, pid), 200)
		get(fmt.Sprintf("/action?action=reorder&ids=%d&ids=%d", second, second), 409)
		get(fmt.Sprintf("/action?action=cover&ids=%d", second), 200)
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		part, _ := w.CreateFormFile("file", "replace.png")
		png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aRZkAAAAASUVORK5CYII=")
		part.Write(png)
		w.Close()
		res, err := client.Post(fmt.Sprintf("%s/action?action=replace&ids=%d", base, second), w.FormDataContentType(), &body)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatal(res.StatusCode)
		}
		p, _ := g.Model("album_page").Where("id", second).One()
		row, _ := g.Model("album").Where("id", id).One()
		if p["page_no"].Int() != 1 || p["image_url"].String() != row["cover_url"].String() {
			t.Fatal("replacement changed identity or lost cover")
		}
		get(fmt.Sprintf("/action?action=delete&ids=%d", second), 200)
		row, _ = g.Model("album").Where("id", id).One()
		if row["page_count"].Int() != 1 {
			t.Fatal(row)
		}
		get(fmt.Sprintf("/action?action=delete&ids=%d", pid), 200)
		row, _ = g.Model("album").Where("id", id).One()
		if row["cover_url"].String() != "" || row["page_count"].Int() != 0 || row["status"].String() != "draft" {
			t.Fatal(row)
		}
		get("/action?action=publish", 409)
	})
	t.Run("share defaults hashes and version revocation", func(t *testing.T) {
		hash, plain, err := SharePassword("random", "")
		if err != nil || len(plain) != 12 || hash == plain {
			t.Fatal("bad default password")
		}
		expires, err := ShareExpiry(nil, false)
		if err != nil || expires.Time.Before(time.Now().Add(6*24*time.Hour)) {
			t.Fatal("bad default expiry")
		}
		share, err := CreateShare(ctx, id, hash, expires)
		if err != nil {
			t.Fatal(err)
		}
		update(g.Map{"status": "published", "visibility": "private", "cover_url": "/_album/fixture/page.jpg"})
		grant := NewFileGrant(id, 0, share["id"].Int64(), share["auth_version"].Int64())
		url := FileURL(id, "cover", 0, grant)
		get(url, 200)
		get(fmt.Sprintf("/action?action=share-update&shareId=%d&mode=none&uid=2", share["id"].Int64()), 409)
		get(fmt.Sprintf("/action?action=share-update&shareId=%d&mode=custom&password=new-password", share["id"].Int64()), 200)
		get(url, 403)
		fresh, _ := g.Model("album_share").Where("id", share["id"]).One()
		url = FileURL(id, "cover", 0, NewFileGrant(id, 0, share["id"].Int64(), fresh["auth_version"].Int64()))
		get(url, 200)
		get(fmt.Sprintf("/action?action=share-update&shareId=%d&enabled=false", share["id"].Int64()), 200)
		get(url, 403)
		get(fmt.Sprintf("/action?action=share-update&shareId=%d&enabled=true", share["id"].Int64()), 200)
		get(url, 403)
		update(g.Map{"visibility": "public"})
		get(url, 403) // An explicit revoked grant stays revoked even for a public album.
		get(FileURL(id, "cover", 0, ""), 200)
	})
	t.Run("cloud storage mixed access and authorization", func(t *testing.T) { storageDatabaseChecks(t, ctx, id, base, client) })
	t.Run("cloud delete replace reference protection and durable retry", func(t *testing.T) { deleteDatabaseChecks(t, ctx, id, base, client) })
	t.Run("bilingual storage and category covers", func(t *testing.T) { bilingualDatabaseChecks(t, ctx) })
}

func minimalPDF() []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << >> >>"}
	offsets := []int{0}
	for i, obj := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 4\n0000000000 65535 f \n")
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)
	return b.Bytes()
}
