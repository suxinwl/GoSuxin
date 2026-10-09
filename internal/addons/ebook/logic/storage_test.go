package album

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/os/gtime"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type mockPan struct {
	mu           sync.Mutex
	files        map[int64]g.Map
	chunks       map[int64]map[int][]byte
	next         int64
	reuse        bool
	expireOnce   bool
	throttleOnce bool
	failCreate   bool
	failAt       int64
	failTrash    bool
	trashed      map[int64]bool
	trashCalls   int
	corrupt      bool
	tokens       int
	slices       int
	gate         chan struct{}
	started      chan struct{}
	unsignedCDN  bool
	cdnReads     int
}

func mockResponse(status int, data any) *http.Response {
	b, _ := json.Marshal(data)
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(b))}
}
func (m *mockPan) RoundTrip(r *http.Request) (*http.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ok := func(d any) (*http.Response, error) { return mockResponse(200, g.Map{"code": 0, "data": d}), nil }
	if strings.Contains(r.URL.Host, "cdn.") {
		valid := m.unsignedCDN || validMockSignature(r.URL, "test-key")
		if !valid {
			return mockResponse(403, nil), nil
		}
		if r.Header.Get("Range") != "" {
			return &http.Response{StatusCode: 206, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("x"))}, nil
		}
		m.cdnReads++
		parts := strings.Split(r.URL.Path, "/")
		id, _ := strconv.ParseInt(parts[2], 10, 64)
		var content []byte
		for no := 1; no <= len(m.chunks[id]); no++ {
			content = append(content, m.chunks[id][no]...)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(content))}, nil
	}
	if r.Header.Get("Platform") != "open_platform" {
		return nil, fmt.Errorf("missing platform")
	}
	if strings.HasSuffix(r.URL.Path, "access_token") {
		m.tokens++
		return ok(g.Map{"accessToken": fmt.Sprintf("token-%d", m.tokens), "expiredAt": time.Now().Add(time.Hour).Format(time.RFC3339)})
	}
	if r.Header.Get("Authorization") == "" {
		return nil, fmt.Errorf("missing bearer")
	}
	if m.expireOnce {
		m.expireOnce = false
		return mockResponse(401, g.Map{"code": 401}), nil
	}
	if m.throttleOnce {
		m.throttleOnce = false
		return mockResponse(429, g.Map{"code": 429}), nil
	}
	var body g.Map
	if r.Method == "POST" && !strings.HasSuffix(r.URL.Path, "slice") {
		json.NewDecoder(r.Body).Decode(&body)
	}
	switch r.URL.Path {
	case "/api/v1/user/info":
		return ok(g.Map{"uid": 13})
	case "/upload/v2/file/create":
		if m.failCreate || m.failAt == m.next+1 {
			return mockResponse(400, g.Map{"code": 400}), nil
		}
		m.next++
		id := m.next
		m.files[id] = body
		m.chunks[id] = map[int][]byte{}
		if m.reuse {
			return ok(g.Map{"reuse": true, "fileID": id})
		}
		return ok(g.Map{"preuploadID": strconv.FormatInt(id, 10), "sliceSize": 128, "servers": []string{"https://upload.123pan.com"}})
	case "/upload/v2/file/slice":
		if m.gate != nil {
			gate := m.gate
			m.gate = nil
			close(m.started)
			m.mu.Unlock()
			<-gate
			m.mu.Lock()
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			return nil, err
		}
		defer r.MultipartForm.RemoveAll()
		id, _ := strconv.ParseInt(r.FormValue("preuploadID"), 10, 64)
		no, _ := strconv.Atoi(r.FormValue("sliceNo"))
		f, _, err := r.FormFile("slice")
		if err != nil {
			return nil, err
		}
		b, _ := io.ReadAll(f)
		f.Close()
		h := md5.Sum(b)
		if r.FormValue("sliceMD5") != hex.EncodeToString(h[:]) {
			return nil, fmt.Errorf("bad slice checksum")
		}
		m.chunks[id][no] = b
		m.slices++
		return ok(nil)
	case "/upload/v2/file/upload_complete":
		id, _ := strconv.ParseInt(fmt.Sprint(body["preuploadID"]), 10, 64)
		parts := m.chunks[id]
		var b []byte
		for no := 1; no <= len(parts); no++ {
			b = append(b, parts[no]...)
		}
		h := md5.Sum(b)
		if fmt.Sprint(m.files[id]["etag"]) != hex.EncodeToString(h[:]) {
			return nil, fmt.Errorf("bad assembled checksum")
		}
		return ok(g.Map{"completed": true, "fileID": id})
	case "/api/v1/file/trash":
		if m.failTrash {
			return mockResponse(400, g.Map{"code": 400}), nil
		}
		for _, v := range body["fileIDs"].([]interface{}) {
			id := int64(v.(float64))
			if m.trashed == nil {
				m.trashed = map[int64]bool{}
			}
			m.trashed[id] = true
			m.trashCalls++
		}
		return ok(nil)
	case "/api/v1/file/detail":
		id, _ := strconv.ParseInt(r.URL.Query().Get("fileID"), 10, 64)
		f := m.files[id]
		if f == nil {
			return ok(g.Map{"type": 1, "trashed": 0})
		}
		etag := f["etag"]
		if m.corrupt {
			etag = "bad"
		}
		trashed := 0
		if m.trashed[id] {
			trashed = 1
		}
		return ok(g.Map{"size": f["size"], "etag": etag, "fileID": id, "type": 0, "trashed": trashed})
	case "/api/v1/direct-link/url":
		return ok(g.Map{"url": "https://13.cdn.123clouddisk.com/13/" + r.URL.Query().Get("fileID") + "/%E5%9B%BE.png"})
	}
	return nil, fmt.Errorf("unexpected request %s", r.URL.Path)
}
func validMockSignature(u *url.URL, key string) bool {
	fields := strings.Split(u.Query().Get("auth_key"), "-")
	if len(fields) != 4 {
		return false
	}
	ts, e := strconv.ParseInt(fields[0], 10, 64)
	if e != nil || ts <= time.Now().Unix() {
		return false
	}
	h := md5.Sum([]byte(u.EscapedPath() + "-" + strings.Join(fields[:3], "-") + "-" + key))
	return fields[3] == hex.EncodeToString(h[:])
}
func installMockPan(t *testing.T) *mockPan {
	t.Helper()
	m := &mockPan{files: map[int64]g.Map{}, chunks: map[int64]map[int][]byte{}}
	old := panHTTP
	panHTTP = &http.Client{Transport: m}
	t.Cleanup(func() { panHTTP = old })
	return m
}
func TestPanStorageConfigurationAndProtocol(t *testing.T) {
	englishParent := int64(8)
	t.Chdir(t.TempDir())
	m := installMockPan(t)
	target, e := CurrentStorageTarget()
	if e != nil || target.ID != "" {
		t.Fatal("must default local")
	}
	id, e := SaveStorageProfile(StorageProfile{Name: "draft"})
	if e != nil {
		t.Fatal(e)
	}
	if ActivateStorage(id) == nil {
		t.Fatal("unverified activation succeeded")
	}
	_, e = SaveStorageProfile(StorageProfile{ID: id, Name: "cloud", ClientID: "client", ClientSecret: "secret", CDNKey: "test-key", ParentID: 7, EnglishParentID: &englishParent})
	if e != nil {
		t.Fatal(e)
	}
	data, e := StorageSettings()
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(data)
	if bytes.Contains(b, []byte(`"secret"`)) || bytes.Contains(b, []byte(`"test-key"`)) {
		t.Fatal("secret leaked")
	}
	p, _ := storageProfile(id)
	client := clientFor(p)
	m.expireOnce = true
	m.throttleOnce = true
	if e = CheckStorage(context.Background(), id); e != nil {
		t.Fatal(e)
	}
	if m.tokens != 2 || m.slices < 2 {
		t.Fatal("refresh or slices not exercised")
	}
	if e = ActivateStorage(id); e != nil {
		t.Fatal(e)
	}
	p, _ = CurrentStorageTarget()
	if p.UID != 13 {
		t.Fatal("UID not discovered")
	}
	if _, e = SaveStorageProfile(StorageProfile{ID: id, Name: "rename", ClientID: p.ClientID, ParentID: 7, EnglishParentID: &englishParent}); e != nil {
		t.Fatal(e)
	}
	p, _ = CurrentStorageTarget()
	if p.CDNKey != "test-key" || p.VerifiedAt == 0 {
		t.Fatal("blank secret or name edit broke connection")
	}
	m.reuse = true
	os.WriteFile("reuse.pdf", storageProbePDF(), 0600)
	if _, e = client.upload(context.Background(), "reuse.pdf", "reuse.pdf", 7); e != nil {
		t.Fatal(e)
	}
	m.corrupt = true
	if _, e = client.upload(context.Background(), "reuse.pdf", "corrupt.pdf", 7); e == nil {
		t.Fatal("corrupt file accepted")
	}
	m.corrupt = false
	if e = ActivateStorage("local"); e != nil {
		t.Fatal(e)
	}
	if _, e = SaveStorageProfile(StorageProfile{ID: id, ClientID: "other"}); e == nil {
		t.Fatal("old account overwritten")
	}
	if _, e = storageProfile(id); e != nil {
		t.Fatal("old connection lost")
	}
	// Read persisted configuration and token with fresh objects (restart semantics).
	again := &panClient{p: p, http: panHTTP}
	n := m.tokens
	if _, e = again.accessToken(context.Background(), ""); e != nil || m.tokens != n {
		t.Fatal("token not reused after restart")
	}
}
func TestPanSigningAndExpiry(t *testing.T) {
	for _, raw := range []string{"http://13.cdn.123clouddisk.com/a", "https://123pan.com.evil.test/a", "https://user:pass@123pan.com/a", "https://127.0.0.1/a", "https://123pan.com:8600/a"} {
		if allowedPanURL(raw) {
			t.Fatal(raw)
		}
	}
	raw := "https://13.cdn.123clouddisk.com/13/%E5%9B%BE.png"
	signed, e := signPanURL(raw, 13, "test-key", time.Now().Add(60*time.Second))
	if e != nil {
		t.Fatal(e)
	}
	u, _ := url.Parse(signed)
	if !validMockSignature(u, "test-key") || validMockSignature(u, "wrong") {
		t.Fatal("signature mismatch")
	}
	// Published vendor example validates exact URI/field ordering independently.
	h := md5.Sum([]byte("/13/files/1.txt-1689220731-123-13-289ds32418bxdba"))
	if hex.EncodeToString(h[:]) != "3bdacc0e031fd67fe829152f37c8fbad" {
		t.Fatal("vendor vector mismatch")
	}
}

func storageDatabaseChecks(t *testing.T, ctx context.Context, id int64, base string, client *http.Client) {
	m := installMockPan(t)
	profileID, e := SaveStorageProfile(StorageProfile{Name: "mock cloud", ClientID: "db-client", ClientSecret: "db-secret", CDNKey: "test-key"})
	if e != nil {
		t.Fatal(e)
	}
	p, _ := storageProfile(profileID)
	if e = markStorageVerified(p, 13); e != nil {
		t.Fatal(e)
	}
	if e = ActivateStorage(profileID); e != nil {
		t.Fatal(e)
	}
	defer ActivateStorage("local")
	p, _ = CurrentStorageTarget()
	os.WriteFile("storage/albums/fixture/cloud.png", []byte("cloud-image"), 0600)
	cloud, e := PutAsset(ctx, id, "/_album/fixture/cloud.png", p)
	if e != nil {
		t.Fatal(e)
	}
	g.Model("album").Where("id", id).Data(g.Map{"status": "published", "visibility": "private", "cover_url": cloud}).Update()
	share, e := CreateShare(ctx, id, "", gtime.New(time.Now().Add(20*time.Second)))
	if e != nil {
		t.Fatal(e)
	}
	token := NewFileGrant(id, 0, share["id"].Int64(), share["auth_version"].Int64())
	file := base + FileURL(id, "cover", 0, token)
	reader := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	check := func(status int) *http.Response {
		t.Helper()
		r, e := reader.Get(file)
		if e != nil {
			t.Fatal(e)
		}
		r.Body.Close()
		if r.StatusCode != status {
			fresh, _ := g.Model("album").Where("id", id).One()
			current, _ := g.Model("album_share").Where("id", share["id"]).One()
			t.Fatalf("file status %d expected %d; state=%s share=%s now=%v file=%s", r.StatusCode, status, fresh.Json(), current.Json(), time.Now(), file)
		}
		return r
	}
	r := check(200)
	if r.Header.Get("Location") != "" || r.Header.Get("Cache-Control") != imageCacheControl || r.Header.Get("ETag") == "" {
		t.Fatal("cloud image was not served with browser validation")
	}
	etag := r.Header.Get("ETag")
	req, _ := http.NewRequest("GET", file, nil)
	req.Header.Set("If-None-Match", etag)
	cached, e := reader.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	cached.Body.Close()
	if cached.StatusCode != 304 {
		t.Fatal("cloud image not reused")
	}
	ActivateStorage("local")
	check(200)
	off := false
	p.URLAuth = &off
	if _, e = SaveStorageProfile(p); e != nil {
		t.Fatal(e)
	}
	check(200)
	if m.cdnReads != 1 {
		t.Fatal("repeated browser read downloaded CDN again", m.cdnReads)
	}
	g.Model("album_share").Where("id", share["id"]).Data(g.Map{"enabled": 0, "auth_version": gdb.Raw("auth_version+1")}).Update()
	check(403)
	cached, e = reader.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	cached.Body.Close()
	if cached.StatusCode != 403 {
		t.Fatal("revoked share cache bypass")
	}
	g.Model("album_share").Where("id", share["id"]).Data(g.Map{"enabled": 1}).Update()
	check(403)
	file = base + FileURL(id, "cover", 0, NewFileGrant(id, 1, 0, 0))
	check(200)
	g.Model("album").Where("id", id).Data(g.Map{"cover_url": "", "status": "offline"}).Update()
	check(404)
	on := true
	p.URLAuth = &on
	if _, e = SaveStorageProfile(p); e != nil {
		t.Fatal(e)
	}
	p, _ = storageProfile(profileID)
	if e = markStorageVerified(p, 13); e != nil {
		t.Fatal(e)
	}
	// Cloud upload failure leaves the album references untouched.
	ActivateStorage(profileID)
	m.mu.Lock()
	m.failCreate = true
	m.mu.Unlock()
	if _, e = PutAsset(ctx, id, "/_album/fixture/cloud.png", p); e == nil {
		t.Fatal("cloud failure silently fell back")
	}
	// Final mutation checks must happen after network IO, without retaining a row lock.
	m.mu.Lock()
	m.failCreate = false
	m.gate = make(chan struct{})
	gate := m.gate
	m.started = make(chan struct{})
	started := m.started
	m.mu.Unlock()

	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aZ1sAAAAASUVORK5CYII=")
	upload := func(action, name string, data []byte) int {
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		part, _ := w.CreateFormFile("file", name)
		part.Write(data)
		w.Close()
		res, e := client.Post(base+"/action?action="+action, w.FormDataContentType(), &body)
		if e != nil {
			return 0
		}
		defer res.Body.Close()
		return res.StatusCode
	}
	completed := make(chan int, 1)
	go func() { completed <- upload("image", "page.png", png) }()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("upload did not start")
	}
	changed := make(chan error, 1)
	go func() {
		_, e := g.Model("album").Where("id", id).Data(g.Map{"edit_version": gdb.Raw("edit_version+1")}).Update()
		changed <- e
	}()
	select {
	case e := <-changed:
		if e != nil {
			t.Error(e)
		}
	case <-time.After(3 * time.Second):
		close(gate)
		t.Fatal("remote upload held SQL row lock")
	}
	close(gate)
	select {
	case status := <-completed:
		if status != 409 {
			t.Fatalf("stale upload accepted: %d", status)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("upload stuck")
	}
	// The default switch cannot move an in-flight upload to local storage.
	m.mu.Lock()
	m.gate = make(chan struct{})
	gate = m.gate
	m.started = make(chan struct{})
	started = m.started
	m.mu.Unlock()
	go func() { completed <- upload("image", "page.png", png) }()
	<-started
	ActivateStorage("local")
	close(gate)
	if status := <-completed; status != 200 {
		t.Fatalf("frozen target upload %d", status)
	}
	page, _ := g.Model("album_page").Where("album_id", id).OrderDesc("page_no").One()
	asset, e := assetRecord(ctx, page["image_url"].String())
	if e != nil || asset["provider"].String() != "pan123" {
		t.Fatal("in-flight target changed")
	}
	// Real Poppler with mocked remote file transfer: atomic replacement and cleanup.
	ActivateStorage(profileID)
	if status := upload("upload", "test.pdf", minimalPDF()); status != 200 {
		t.Fatalf("PDF submit %d", status)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		row, _ := g.Model("album").Where("id", id).One()
		if row["status"].String() == "ready" {
			if row["pending_pdf"].String() != "" || row["processing_stage"].String() != "" {
				t.Fatal("PDF stage not cleared")
			}
			original, e := assetRecord(ctx, row["original_url"].String())
			if e != nil || original["provider"].String() != "pan123" {
				t.Fatal("original not cloud")
			}
			break
		}
		if row["status"].String() == "failed" || time.Now().After(deadline) {
			t.Fatal("cloud PDF", row["failure_reason"])
		}
		time.Sleep(100 * time.Millisecond)
	}
	before, _ := g.Model("album_page").Where("album_id", id).One()
	m.mu.Lock()
	m.failAt = m.next + 2 // original succeeds, first converted page fails
	m.mu.Unlock()
	if status := upload("upload", "fail.pdf", minimalPDF()); status != 200 {
		t.Fatal(status)
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		row, _ := g.Model("album").Where("id", id).One()
		if row["status"].String() == "failed" {
			after, _ := g.Model("album_page").Where("album_id", id).One()
			if before["id"].Int64() != after["id"].Int64() {
				t.Fatal("failed PDF replaced old pages")
			}
			if _, e := StoredFile(row["pending_pdf"].String()); e != nil {
				t.Fatal("retry source removed")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("failure not recorded")
		}
		time.Sleep(100 * time.Millisecond)
	}
	m.mu.Lock()
	m.failCreate = false
	m.failAt = 0
	m.mu.Unlock()
	ActivateStorage("local")
	res, e := client.Get(base + "/action?action=retry")
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal("retry rejected")
	}
	deadline = time.Now().Add(20 * time.Second)
	for {
		row, _ := g.Model("album").Where("id", id).One()
		if row["status"].String() == "ready" {
			a, e := assetRecord(ctx, row["cover_url"].String())
			if e != nil || a["provider"].String() != "pan123" {
				t.Fatal("retry changed original destination")
			}
			break
		}
		if row["status"].String() == "failed" || time.Now().After(deadline) {
			t.Fatal("retry failed", row["failure_reason"])
		}
		time.Sleep(100 * time.Millisecond)
	}
}

type panRoundTripFunc func(*http.Request) (*http.Response, error)

func (f panRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestPanNetworkRetriesAreBoundedAndRedacted(t *testing.T) {
	calls := 0
	client := &panClient{http: &http.Client{Transport: panRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return nil, fmt.Errorf("transport secret-marker")
	})}}
	_, err := client.request(context.Background(), "GET", "https://open-api.123pan.com/api/v1/user/info", nil, false)
	if err == nil || calls != 4 || strings.Contains(err.Error(), "secret-marker") {
		t.Fatalf("bounded sanitized retry failed, calls=%d err=%v", calls, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	before := calls
	_, err = client.request(cancelled, "GET", "https://open-api.123pan.com/api/v1/user/info", nil, false)
	if err == nil || calls != before {
		t.Fatal("cancelled request reached network")
	}
}

func TestPanCustomCDNDomains(t *testing.T) {
	custom := "https://down.szputy.com/13/%E5%9B%BE.png"
	if allowedPanURL(custom) {
		t.Fatal("custom CDN must not become an API/upload destination")
	}
	if !validPanCDNURL(custom) {
		t.Fatal("officially returned custom CDN rejected")
	}
	signed, err := signPanURL(custom, 13, "test-key", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(signed)
	if !validMockSignature(u, "test-key") {
		t.Fatal("custom CDN signature invalid")
	}
	for _, raw := range []string{"http://down.szputy.com/a", "https://user:pass@down.szputy.com/a", "https://127.0.0.1/a", "https://[::1]/a", "https://10.0.0.1/a", "https://localhost/a", "https://host.local/a", "https://host.internal/a", "https://down.szputy.com:8600/a"} {
		if validPanCDNURL(raw) {
			t.Fatal("unsafe CDN", raw)
		}
	}
	first, _ := http.NewRequest("GET", custom, nil)
	cdn := panCDNClient(panHTTP)
	for _, raw := range []string{"https://down.szputy.com/next", "https://13.cdn.123clouddisk.com/a"} {
		r, _ := http.NewRequest("GET", raw, nil)
		if err = cdn.CheckRedirect(r, []*http.Request{first}); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{"https://other.example.com/a", "http://down.szputy.com/a", "https://127.0.0.1/a"} {
		r, _ := http.NewRequest("GET", raw, nil)
		if cdn.CheckRedirect(r, []*http.Request{first}) == nil {
			t.Fatal("unsafe redirect", raw)
		}
	}
	// Keep network interception at the transport boundary; direct() must accept the official response.
	c := &panClient{p: StorageProfile{ID: "custom-domain-test"}, token: "cached", expires: time.Now().Add(time.Hour), http: &http.Client{Transport: panRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return mockResponse(200, g.Map{"code": 0, "data": g.Map{"url": custom}}), nil
	})}}
	raw, err := c.direct(context.Background(), 123)
	if err != nil || raw != custom {
		t.Fatal("custom direct-link response", err)
	}
}
