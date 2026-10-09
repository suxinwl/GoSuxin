package album

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/os/gtime"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"testing"
)

func testImageRequest(t *testing.T, client *http.Client, base, action string, payload []byte) int {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	part, _ := w.CreateFormFile("file", "page.png")
	part.Write(payload)
	w.Close()
	r, e := client.Post(base+"/action?action="+action, w.FormDataContentType(), &b)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	body, _ := io.ReadAll(r.Body)
	if r.StatusCode != 200 {
		t.Logf("mutation status=%d message=%s", r.StatusCode, body)
	}
	return r.StatusCode
}
func deleteDatabaseChecks(t *testing.T, ctx context.Context, id int64, base string, client *http.Client) {
	m := installMockPan(t)
	m.next = 100000
	profileID, e := SaveStorageProfile(StorageProfile{Name: "delete test", ClientID: "delete-client", ClientSecret: "delete-secret", CDNKey: "test-key"})
	if e != nil {
		t.Fatal(e)
	}
	p, _ := storageProfile(profileID)
	markStorageVerified(p, 13)
	ActivateStorage(profileID)
	defer ActivateStorage("local")
	g.Model("album_page").Where("album_id", id).Delete()
	g.Model("album").Where("id", id).Data(g.Map{"status": "ready", "cover_url": "", "pending_pdf": "", "original_url": "", "page_count": 0}).Update()
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aZ1sAAAAASUVORK5CYII=")
	upload := func() gdb.Record {
		t.Helper()
		if s := testImageRequest(t, client, base, "image", png); s != 200 {
			t.Fatal(s)
		}
		page, _ := g.Model("album_page").Where("album_id", id).OrderDesc("page_no").One()
		return page
	}
	remove := func(pid int64, status int, uid int64) {
		t.Helper()
		r, e := client.Get(fmt.Sprintf("%s/action?action=delete&ids=%d&uid=%d", base, pid, uid))
		if e != nil {
			t.Fatal(e)
		}
		defer r.Body.Close()
		if r.StatusCode != status {
			b, _ := io.ReadAll(r.Body)
			t.Fatalf("delete=%d want=%d %s", r.StatusCode, status, b)
		}
	}
	trashed := func(fileID int64) bool { m.mu.Lock(); defer m.mu.Unlock(); return m.trashed[fileID] }
	page := upload()
	old, _ := assetRecord(ctx, page["image_url"].String())
	oldID := old["file_id"].Int64()
	remove(page["id"].Int64(), 409, 2)
	if trashed(oldID) {
		t.Fatal("foreign delete reached cloud")
	}
	if s := testImageRequest(t, client, base, fmt.Sprintf("replace&ids=%d", page["id"].Int64()), append(png, []byte("replacement")...)); s != 200 {
		t.Fatal(s)
	}
	fresh, _ := g.Model("album_page").Where("id", page["id"]).One()
	asset, _ := assetRecord(ctx, fresh["image_url"].String())
	newID := asset["file_id"].Int64()
	row, _ := g.Model("album").Where("id", id).One()
	if !trashed(oldID) || trashed(newID) || row["cover_url"].String() != fresh["image_url"].String() {
		t.Fatal("replace did not trash old cloud file or keep new cover")
	}
	// A second configured client of the same UID aliases the same cloud file.
	secondID, _ := SaveStorageProfile(StorageProfile{Name: "same account", ClientID: "second-client", ClientSecret: "s", CDNKey: "test-key"})
	other, _ := storageProfile(secondID)
	markStorageVerified(other, 13)
	aliasID, e := g.Model("album_asset").Data(g.Map{"album_id": id, "provider": "pan123", "profile_id": secondID, "file_id": newID, "size_bytes": asset["size_bytes"], "checksum": asset["checksum"], "createtime": gtime.Now()}).InsertAndGetId()
	if e != nil {
		t.Fatal(e)
	}
	aliasRef := fmt.Sprintf("%s%d", assetPrefix, aliasID)
	aliasPage, e := g.Model("album_page").Data(g.Map{"album_id": id, "page_no": 2, "image_url": aliasRef, "thumbnail_url": aliasRef, "createtime": gtime.Now()}).InsertAndGetId()
	if e != nil {
		t.Fatal(e)
	}
	remove(fresh["id"].Int64(), 200, 1)
	if trashed(newID) {
		t.Fatal("file still used by another page was deleted")
	}
	ActivateStorage("local")
	remove(aliasPage, 200, 1)
	if !trashed(newID) {
		t.Fatal("last reference removal did not trash using original profile")
	}
	row, _ = g.Model("album").Where("id", id).One()
	if row["page_count"].Int() != 0 || row["cover_url"].String() != "" {
		t.Fatal("last page summary incorrect")
	}
	ActivateStorage(profileID)
	page = upload()
	asset, _ = assetRecord(ctx, page["image_url"].String())
	fileID := asset["file_id"].Int64()
	// The outbox rolls back together with the page, so no deleted file can be restored as a DB reference.
	sentinel := fmt.Errorf("rollback")
	err := g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		tx.Model("album_page").Where("id", page["id"]).Delete()
		if e := queueAssetDeletes(ctx, tx, id, []string{page["image_url"].String()}); e != nil {
			return e
		}
		return sentinel
	})
	if err != sentinel {
		t.Fatal(err)
	}
	FlushAssetDeletes(ctx, id, true)
	if trashed(fileID) {
		t.Fatal("rolled-back mutation deleted cloud file")
	}
	m.mu.Lock()
	m.failTrash = true
	m.mu.Unlock()
	remove(page["id"].Int64(), 200, 1)
	status, e := AssetDeleteStatus(ctx, id)
	if e != nil || status["failed"].(int) != 1 || trashed(fileID) {
		t.Fatal("cloud failure not retained", status, e)
	}
	// Persisted jobs survive schema/init and a new cloud client, then retry succeeds.
	if e = Initialize(ctx); e != nil {
		t.Fatal(e)
	}
	p, _ = storageProfile(profileID)
	panClients.Delete(fmt.Sprintf("%s:%d", profileID, p.Revision))
	m.mu.Lock()
	m.failTrash = false
	m.mu.Unlock()
	assetLifecycle.RLock()
	FlushAssetDeletes(ctx, id, true)
	assetLifecycle.RUnlock()
	if trashed(fileID) {
		t.Fatal("cleanup raced with active upload")
	}
	if e = FlushAssetDeletes(ctx, id, true); e != nil {
		t.Fatal(e)
	}
	if !trashed(fileID) {
		t.Fatal("retry did not delete cloud file")
	}
	status, _ = AssetDeleteStatus(ctx, id)
	if status["pending"].(int) != 0 {
		t.Fatal(status)
	}
	// Historic local images are outside the cloud deletion policy.
	local, e := g.Model("album_page").Data(g.Map{"album_id": id, "page_no": 1, "image_url": "/_album/fixture/page.jpg", "createtime": gtime.Now()}).InsertAndGetId()
	if e != nil {
		t.Fatal(e)
	}
	remove(local, 200, 1)
	if _, e = os.Stat("storage/albums/fixture/page.jpg"); e != nil {
		t.Fatal("local legacy file was removed")
	}
}
