package album

import (
	"context"
	"encoding/base64"
	"fmt"
	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/os/gtime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Explicit opt-in: creates and recycles only tiny files uploaded by THIS test.
// All album/page rows and configuration changes are in a disposable DB/directory.
func TestLiveCloudImageDeletion(t *testing.T) {
	root := os.Getenv("ALBUM_LIVE_DELETE_ROOT")
	name := os.Getenv("ALBUM_LIVE_DELETE_DATABASE")
	if root == "" || !strings.HasPrefix(name, "ebook_test_") {
		t.Skip("explicit isolated live deletion test only")
	}
	source := filepath.Join(root, "storage/config")
	config, e := os.ReadFile(filepath.Join(source, "album-storage.json"))
	if e != nil {
		t.Fatal(e)
	}
	t.Chdir(t.TempDir())
	os.MkdirAll("storage/config", 0700)
	os.WriteFile(storageConfigPath, config, 0600)
	tokens, _ := filepath.Glob(filepath.Join(source, "token-*.json"))
	for _, f := range tokens {
		b, e := os.ReadFile(f)
		if e == nil {
			os.WriteFile(filepath.Join("storage/config", filepath.Base(f)), b, 0600)
		}
	}
	cfg, e := readStorageConfig()
	if e != nil {
		t.Fatal(e)
	}
	var profile StorageProfile
	for _, p := range cfg.Profiles {
		if p.VerifiedAt > 0 {
			profile = p
		}
	}
	if profile.ID == "" {
		t.Fatal("no verified cloud profile")
	}
	if e = ActivateStorage(profile.ID); e != nil {
		t.Fatal(e)
	}
	if e = gdb.SetConfigGroup("default", gdb.ConfigGroup{{Type: "mysql", Host: "127.0.0.1", Port: "3306", User: "root", Pass: os.Getenv("ALBUM_TEST_PASSWORD"), Name: name, Prefix: "gf_", Extra: "charset=utf8mb4&parseTime=True&loc=Local"}}); e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	if e = Initialize(ctx); e != nil {
		t.Fatal(e)
	}
	for _, sql := range []string{"CREATE TABLE IF NOT EXISTS gf_admin(id BIGINT PRIMARY KEY,status INT DEFAULT 0)", "CREATE TABLE IF NOT EXISTS gf_auth_role_access(uid BIGINT,role_id BIGINT)", "CREATE TABLE IF NOT EXISTS gf_auth_role(id BIGINT,rules VARCHAR(30))", "INSERT IGNORE INTO gf_admin VALUES(1,0)"} {
		if _, e = g.DB().Exec(ctx, sql); e != nil {
			t.Fatal(e)
		}
	}
	id, e := g.Model("album").Data(g.Map{"owner_id": 1, "title": "Live cloud deletion fixture", "source_type": "images", "status": "draft", "visibility": "private", "createtime": gtime.Now()}).InsertAndGetId()
	if e != nil {
		t.Fatal(e)
	}
	server := g.Server("album-live-delete")
	server.SetAddr("127.0.0.1:0")
	server.SetDumpRouterMap(false)
	server.BindHandler("/common/album/file", ServeAlbumFile)
	server.BindHandler("/action", func(r *ghttp.Request) {
		r.SetCtxVar("uid", int64(1))
		var err error
		switch r.Get("action").String() {
		case "image":
			_, err = AddImage(r.Context(), id, r.GetUploadFile("file"), 0)
		case "replace":
			err = EditPages(r.Context(), id, "replace", []int64{r.Get("ids").Int64()}, r.GetUploadFile("file"))
		case "delete":
			err = EditPages(r.Context(), id, "delete", []int64{r.Get("ids").Int64()}, nil)
		}
		if err != nil {
			r.Response.WriteStatus(409, err.Error())
			return
		}
		r.Response.Write("ok")
	})
	if e = server.Start(); e != nil {
		t.Fatal(e)
	}
	defer server.Shutdown()
	base := fmt.Sprintf("http://127.0.0.1:%d", server.GetListenedPort())
	client := &http.Client{Timeout: 90 * time.Second}
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aZ1sAAAAASUVORK5CYII=")
	if s := testImageRequest(t, client, base, "image", png); s != 200 {
		t.Fatal(s)
	}
	page, _ := g.Model("album_page").Where("album_id", id).One()
	old, e := assetRecord(ctx, page["image_url"].String())
	if e != nil {
		t.Fatal(e)
	}
	// Every file eligible for removal is recorded from the upload just performed.
	if old["profile_id"].String() != profile.ID {
		t.Fatal("unexpected upload profile")
	}
	preview := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	url := base + FileURL(id, "cover", 0, NewFileGrant(id, 1, 0))
	r, e := preview.Get(url)
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal("cloud preview", r.StatusCode)
	}
	t.Log("PASS actual cloud upload and protected cached image response")
	if s := testImageRequest(t, client, base, fmt.Sprintf("replace&ids=%d", page["id"].Int64()), append(png, []byte("replacement-live")...)); s != 200 {
		t.Fatal(s)
	}
	d, e := clientFor(profile).detail(ctx, old["file_id"].Int64())
	if e != nil || panNumber(d, "trashed") != 1 {
		t.Fatal("old image not recycled", e)
	}
	fresh, _ := g.Model("album_page").Where("id", page["id"]).One()
	current, e := assetRecord(ctx, fresh["image_url"].String())
	if e != nil {
		t.Fatal(e)
	}
	if current["file_id"].Int64() == old["file_id"].Int64() {
		t.Fatal("replacement reused trashed file identity")
	}
	row, _ := g.Model("album").Where("id", id).One()
	if row["cover_url"].String() != fresh["image_url"].String() {
		t.Fatal("replacement cover lost")
	}
	t.Log("PASS actual replacement preserved page/cover and recycled old cloud image")
	// Changing the default in this isolated configuration must not affect deletion.
	ActivateStorage("local")
	r, e = client.Get(fmt.Sprintf("%s/action?action=delete&ids=%d", base, page["id"].Int64()))
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal(r.StatusCode)
	}
	d, e = clientFor(profile).detail(ctx, current["file_id"].Int64())
	if e != nil || panNumber(d, "trashed") != 1 {
		t.Fatal("deleted image not recycled", e)
	}
	row, _ = g.Model("album").Where("id", id).One()
	if row["page_count"].Int() != 0 || row["cover_url"].String() != "" {
		t.Fatal("deleted last page retained cover")
	}
	r, e = preview.Get(url)
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.StatusCode != 404 {
		t.Fatal("deleted cover still signed", r.StatusCode)
	}
	status, e := AssetDeleteStatus(ctx, id)
	if e != nil || status["pending"].(int) != 0 {
		t.Fatal("pending cleanup", e, status)
	}
	t.Log("PASS actual delete recycled replacement; empty album cannot serve cover; no pending jobs")
	// Recycle references left by an earlier failed invocation in this explicitly disposable DB only.
	fixtures, e := g.Model("album").Where("title", "Live cloud deletion fixture").Where("owner_id", 1).All()
	if e != nil {
		t.Fatal(e)
	}
	for _, fixture := range fixtures {
		fid := fixture["id"].Int64()
		e = g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
			pages, err := tx.Model("album_page").Where("album_id", fid).All()
			if err != nil {
				return err
			}
			if err = queueAssetDeletes(ctx, tx, fid, pageLocations(pages)); err != nil {
				return err
			}
			if _, err = tx.Model("album_page").Where("album_id", fid).Delete(); err != nil {
				return err
			}
			_, err = tx.Model("album").Where("id", fid).Data(g.Map{"cover_url": "", "page_count": 0}).Update()
			return err
		})
		if e != nil {
			t.Fatal(e)
		}
		if e = FlushAssetDeletes(ctx, fid, true); e != nil {
			t.Fatal(e)
		}
		state, e := AssetDeleteStatus(ctx, fid)
		if e != nil || state["pending"].(int) != 0 {
			t.Fatal("fixture cleanup pending", state, e)
		}
	}
	t.Log("PASS prior disposable test fixture references also recycled")
}
