package suxinvideo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

// Only the explicit runner's disposable database is eligible. This test cannot
// create fixtures in the configured development movie/member/admin database.
func TestLiveBackendControlledIntegration(t *testing.T) {
	if os.Getenv("SUXIN_LIVE_BACKEND_INTEGRATION") != "1" {
		t.Skip("run plugins/suxinvideo/tools/test_live_backend.py")
	}
	ctx := yqkIntegrationContext(t)
	database, err := one(ctx, "SELECT DATABASE() name")
	if err != nil || !regexp.MustCompile(`^suxin_live_verify_[0-9]+$`).MatchString(gconv.String(database["name"])) {
		t.Fatal("live tests refuse non-disposable database")
	}
	liveSchemaLock.Lock()
	liveSchemaReady = false
	liveSchemaLock.Unlock()
	if err = ensureLiveSchema(ctx); err != nil {
		t.Fatal("live schema initialization failed", err)
	}
	if err = ensureLiveSchema(ctx); err != nil {
		t.Fatal("live schema repeated initialization failed", err)
	}
	if err = upgradeLivePermissions(ctx); err != nil {
		t.Fatal("live permission repeat upgrade failed", err)
	}
	count, err := one(ctx, "SELECT COUNT(*) n FROM sx_live_subscription")
	if err != nil || gconv.Int(count["n"]) != 4 {
		t.Fatal("default subscriptions duplicated")
	}
	permissions, err := all(ctx, "SELECT id,path FROM gf_auth_rule WHERE path LIKE '/admin/suxinvideo/live/%' ORDER BY id")
	if err != nil || len(permissions) != 10+len(liveProviderAdminActions) {
		t.Fatal("live permissions missing or duplicated")
	}
	ids := map[string]int64{}
	for _, r := range permissions {
		ids[strings.TrimPrefix(gconv.String(r["path"]), "/admin/suxinvideo/")] = gconv.Int64(r["id"])
	}
	reader := fmt.Sprintf("%d,%d,%d,%d", ids["live/channels"], ids["live/groups"], ids["live/streams"], ids["live/job"])
	if err = execSQL(ctx, "UPDATE gf_auth_role SET rules=?,btns=? WHERE id=2", reader, reader); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		uid     int
		action  string
		allowed bool
	}{{201, "live/channels", true}, {201, "live/save", false}, {202, "live/channels", false}, {203, "live/save", true}} {
		ok, e := resourceAllowed(context.WithValue(ctx, "uid", tc.uid), "", tc.action)
		if e != nil || ok != tc.allowed {
			t.Fatalf("incorrect role permission uid=%d action=%s", tc.uid, tc.action)
		}
	}
	sentinel, _ := one(ctx, "SELECT title,path FROM gf_auth_rule WHERE routename='host_fixture'")
	if gconv.String(sentinel["title"]) != "Host sentinel" || gconv.String(sentinel["path"]) != "/admin/host-fixture" {
		t.Fatal("host permissions were changed")
	}
	t.Log("PASS: repeated schema/menu installation, 4 subscriptions, exact live/provider permissions, restricted roles and host sentinel preserved")

	oldClient := liveHTTPClient
	t.Cleanup(func() { liveHTTPClient = oldClient })
	var obsoleteChecks atomic.Int64
	liveHTTPClient = &http.Client{Transport: liveFixtureTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/jade/obsolete.m3u8" {
			obsoleteChecks.Add(1)
			response := liveFixtureResponse(r, "temporarily unavailable", "text/plain")
			response.StatusCode = http.StatusServiceUnavailable
			return response, nil
		}
		if strings.HasSuffix(r.URL.Path, ".m3u8") {
			return liveFixtureResponse(r, "#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXT-X-MEDIA-SEQUENCE:600\n#EXTINF:6,\nfragment.ts\n", "application/vnd.apple.mpegurl"), nil
		}
		return liveFixtureResponse(r, strings.Repeat("media", 100), "video/mp2t"), nil
	})}
	raw := `#EXTM3U
#EXTINF:-1 tvg-id="Jade.hk" group-title="香港",Jade
#EXTHTTP:{"Authorization":"Bearer controlled-secret"}
https://8.8.8.8/jade/first.m3u8
#EXTINF:-1 tvg-id="Jade.hk" group-title="香港",Jade backup
https://8.8.4.4/jade/backup.m3u8
#EXTINF:-1 tvg-id="GoldenJade.hk" group-title="香港",Golden Jade
https://8.8.8.8/golden/first.m3u8
`
	items, _, err := liveParseImport(raw, "")
	if err != nil {
		t.Fatal(err)
	}
	newProgress := func() *liveProgress {
		t.Helper()
		r, e := g.DB().Exec(ctx, "INSERT INTO sx_live_job(status,kind,created,updated) VALUES('completed','fixture',?,?)", time.Now().Unix(), time.Now().Unix())
		if e != nil {
			t.Fatal(e)
		}
		id, e := r.LastInsertId()
		if e != nil {
			t.Fatal(e)
		}
		return &liveProgress{ID: id}
	}
	for range 2 {
		if err = liveImportItems(ctx, newProgress(), items, 1); err != nil {
			t.Fatal("controlled import failed", err)
		}
	}
	channels, err := all(ctx, "SELECT id,tvg_id,name,group_id FROM sx_live_channel ORDER BY id")
	if err != nil || len(channels) != 2 {
		t.Fatal("same-channel lines duplicated or Jade merged with Golden Jade")
	}
	streams, err := all(ctx, "SELECT id,channel_id,health FROM sx_live_stream ORDER BY id")
	if err != nil || len(streams) != 3 {
		t.Fatal("repeat import duplicated live streams")
	}
	for _, r := range streams {
		if gconv.String(r["health"]) != "healthy" {
			t.Fatalf("verified media not marked healthy: %s", gconv.String(r["health"]))
		}
	}
	jadeID, goldenID := int64(0), int64(0)
	for _, r := range channels {
		switch gconv.String(r["name"]) {
		case "翡翠台":
			jadeID = gconv.Int64(r["id"])
		case "黄金翡翠台":
			goldenID = gconv.Int64(r["id"])
		}
	}
	if jadeID < 1 || goldenID < 1 || jadeID == goldenID {
		t.Fatal("Jade identity mapping failed")
	}
	first, _ := one(ctx, "SELECT * FROM sx_live_stream WHERE url='https://8.8.8.8/jade/first.m3u8'")
	streamID := gconv.Int64(first["id"])
	if _, err = liveSaveData(ctx, "channel", jadeID, row{"name": "自定义翡翠台", "sort": 87, "enabled": false, "aliases": []string{"TVB Jade"}}); err != nil {
		t.Fatal("manual channel update failed", err)
	}
	if _, err = liveSaveData(ctx, "stream", streamID, row{"priority": 900, "enabled": false, "headers": map[string]string{"Authorization": "", "User-Agent": "controlled-player"}}); err != nil {
		t.Fatal("manual source update failed", err)
	}
	if err = liveImportItems(ctx, newProgress(), items, 1); err != nil {
		t.Fatal(err)
	}
	updated, _ := one(ctx, "SELECT name,sort,enabled,aliases_json FROM sx_live_channel WHERE id=?", jadeID)
	if gconv.String(updated["name"]) != "自定义翡翠台" || gconv.Int(updated["sort"]) != 87 || gconv.Int(updated["enabled"]) != 0 || !strings.Contains(gconv.String(updated["aliases_json"]), "TVB Jade") {
		t.Fatal("subscription overwrote manually edited channel")
	}
	updatedStream, _ := one(ctx, "SELECT priority,enabled,headers_json FROM sx_live_stream WHERE id=?", streamID)
	if gconv.Int(updatedStream["priority"]) != 900 || gconv.Int(updatedStream["enabled"]) != 0 || !strings.Contains(gconv.String(updatedStream["headers_json"]), "controlled-secret") || !strings.Contains(gconv.String(updatedStream["headers_json"]), "controlled-player") {
		t.Fatal("subscription overwrote manual source or cleared blank credential")
	}
	aliasBefore, _ := one(ctx, "SELECT identity_key,tvg_id,name,group_id,enabled FROM sx_live_channel WHERE id=?", jadeID)
	aliasItem := liveImportItem{Name: " t V b  J a d e ", Group: "unrelated upstream group", URL: "https://8.8.8.8/jade/first.m3u8"}
	aliasID, created, aliasErr := liveUpsertImport(ctx, aliasItem, 1)
	aliasAfter, _ := one(ctx, "SELECT identity_key,tvg_id,name,group_id,enabled FROM sx_live_channel WHERE id=?", jadeID)
	aliasSource, _ := one(ctx, "SELECT enabled,priority,headers_json FROM sx_live_stream WHERE id=?", streamID)
	if aliasErr != nil || created || aliasID != streamID || !reflect.DeepEqual(aliasBefore, aliasAfter) || gconv.Bool(aliasSource["enabled"]) || gconv.Int(aliasSource["priority"]) != 900 || !strings.Contains(gconv.String(aliasSource["headers_json"]), "controlled-secret") {
		t.Fatal("missing-ID normalized alias did not reuse disabled channel and existing source safely")
	}
	aliasSeed, err := SeedLiveChannels(ctx, "#EXTM3U\n#EXTINF:-1,T V B JADE\nhttps://8.8.8.8/jade/first.m3u8\n", "unrelated seed group")
	if err != nil || aliasSeed.Added != 0 || len(aliasSeed.StreamIDs) != 1 || aliasSeed.StreamIDs[0] != streamID {
		t.Fatal("local seed could not identify a line imported through a missing-ID alias")
	}
	if _, err = liveSaveData(ctx, "channel", goldenID, row{"aliases": []string{"TV B JADE"}}); err != nil {
		t.Fatal(err)
	}
	aliasItem.URL = "https://8.8.8.8/ambiguous/new.m3u8"
	if _, _, err = liveUpsertImport(ctx, aliasItem, 1); err == nil {
		t.Fatal("ambiguous administrator aliases merged an unrelated channel")
	}
	ambiguous, _ := one(ctx, "SELECT COUNT(*) n FROM sx_live_stream WHERE url_hash=?", liveIdentity(aliasItem.URL))
	if gconv.Int(ambiguous["n"]) != 0 {
		t.Fatal("ambiguous alias created a stream under an arbitrary channel")
	}
	if _, err = liveSaveData(ctx, "channel", goldenID, row{"aliases": []string{}}); err != nil {
		t.Fatal(err)
	}
	t.Log("PASS: missing TVG-ID uses case/whitespace-normalized administrator aliases without re-enabling or changing identity; ambiguous aliases rejected")
	// Force a genuine second startup pass, rather than merely exercising the
	// schema-ready shortcut. Existing manual values and permission IDs survive.
	liveSchemaLock.Lock()
	liveSchemaReady = false
	liveSchemaLock.Unlock()
	if err = ensureLiveSchema(ctx); err != nil {
		t.Fatal("repeated startup schema failed", err)
	}
	secondChannel, _ := one(ctx, "SELECT name,sort,enabled,aliases_json FROM sx_live_channel WHERE id=?", jadeID)
	secondStream, _ := one(ctx, "SELECT priority,enabled,headers_json FROM sx_live_stream WHERE id=?", streamID)
	secondPermissions, _ := all(ctx, "SELECT id,path FROM gf_auth_rule WHERE path LIKE '/admin/suxinvideo/live/%' ORDER BY id")
	if !reflect.DeepEqual(updated, secondChannel) || !reflect.DeepEqual(updatedStream, secondStream) || !reflect.DeepEqual(permissions, secondPermissions) {
		t.Fatal("repeated startup overwrote manual settings or duplicated permission IDs")
	}
	if _, err = liveSaveData(ctx, "channel", jadeID, row{"enabled": true}); err != nil {
		t.Fatal(err)
	}
	if _, err = liveSaveData(ctx, "stream", streamID, row{"enabled": true}); err != nil {
		t.Fatal(err)
	}
	_ = liveSetHealth(ctx, streamID, false, "暂时超时")
	public, err := liveStreams(ctx, jadeID)
	if err != nil || len(public) != 2 {
		t.Fatal("temporary failure removed a previously verified fallback")
	}
	publicList, total, err := liveChannels(ctx, 0, "", 1, 30)
	if err != nil || total != 2 || len(publicList) != 2 {
		t.Fatal("channel vanished because one stream failed")
	}
	t.Log("PASS: repeated multi-line import, distinct Jade/Golden Jade, manual name/sort/enable/header protection and failed-source fallback retained")

	// The current successful list does not contain the old URL. The periodic
	// sweep must still recheck that retained line; a disabled subscription or
	// manually disabled line must not become an automatic network request.
	staleTime := time.Now().Add(-7 * time.Hour).Unix()
	if err = execSQL(ctx, "UPDATE sx_live_subscription SET last_refresh=?,enabled=1", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if err = execSQL(ctx, "UPDATE sx_live_subscription SET enabled=0 WHERE id=2"); err != nil {
		t.Fatal(err)
	}
	retained, _, err := liveUpsertImport(ctx, liveImportItem{TVGID: "Jade.hk", Name: "retained line", Group: "香港", URL: "https://8.8.8.8/jade/obsolete.m3u8", Headers: map[string]string{"Authorization": "Bearer controlled-secret"}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	manual, _, err := liveUpsertImport(ctx, liveImportItem{TVGID: "Jade.hk", Name: "manual line", Group: "香港", URL: "https://8.8.8.8/jade/manual.m3u8"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	disabledSubscription, _, err := liveUpsertImport(ctx, liveImportItem{TVGID: "Jade.hk", Name: "disabled subscription line", Group: "香港", URL: "https://8.8.8.8/jade/disabled-sub.m3u8"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	disabledLine, _, err := liveUpsertImport(ctx, liveImportItem{TVGID: "Jade.hk", Name: "disabled manual line", Group: "香港", URL: "https://8.8.8.8/jade/disabled-line.m3u8"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = execSQL(ctx, "UPDATE sx_live_stream SET health='healthy',last_checked=?,manual_edited=1,priority=777,quality='自定义清晰度' WHERE id IN (?,?,?,?)", staleTime, retained, manual, disabledSubscription, disabledLine); err != nil {
		t.Fatal(err)
	}
	if err = execSQL(ctx, "UPDATE sx_live_stream SET enabled=0 WHERE id=?", disabledLine); err != nil {
		t.Fatal(err)
	}
	if err = liveImportItems(ctx, newProgress(), items, 1); err != nil {
		t.Fatal(err)
	}
	due, err := liveStaleStreams(ctx, true)
	if err != nil || len(due) != 1 {
		t.Fatal("scheduler cannot discover old retained or manual lines without a due subscription")
	}
	sweep := newProgress()
	if err = liveRefreshSubscriptions(ctx, sweep, 0, false); err != nil {
		t.Fatal(err)
	}
	retainedRow, _ := one(ctx, "SELECT name,priority,quality,enabled,headers_json,health,last_checked FROM sx_live_stream WHERE id=?", retained)
	manualRow, _ := one(ctx, "SELECT health,last_checked FROM sx_live_stream WHERE id=?", manual)
	disabledRows, _ := all(ctx, "SELECT last_checked FROM sx_live_stream WHERE id IN (?,?)", disabledSubscription, disabledLine)
	if obsoleteChecks.Load() < 1 || sweep.Total != 2 || sweep.Processed != 2 || sweep.Failed != 1 || gconv.String(retainedRow["health"]) != "unavailable" || gconv.Int64(retainedRow["last_checked"]) <= staleTime || gconv.String(retainedRow["name"]) != "retained line" || gconv.Int(retainedRow["priority"]) != 777 || gconv.String(retainedRow["quality"]) != "自定义清晰度" || !gconv.Bool(retainedRow["enabled"]) || !strings.Contains(gconv.String(retainedRow["headers_json"]), "controlled-secret") {
		t.Fatal("obsolete subscription line was not rechecked or manual fields were overwritten")
	}
	if gconv.String(manualRow["health"]) != "healthy" || gconv.Int64(manualRow["last_checked"]) <= staleTime {
		t.Fatal("manual line was not periodically rechecked")
	}
	for _, row := range disabledRows {
		if gconv.Int64(row["last_checked"]) != staleTime {
			t.Fatal("disabled line or disabled subscription was automatically probed")
		}
	}
	t.Log("PASS: omitted subscription line rechecked after six hours, manual health refreshed, disabled sources skipped and manual metadata preserved")
	seed, err := SeedLiveChannels(ctx, raw, "备用默认分组")
	if err != nil || seed.Added != 0 || seed.Streams != 3 || len(seed.StreamIDs) != 3 {
		t.Fatal("local vetted seed duplicated lines or failed normal import/probe rules")
	}
	seededChannel, _ := one(ctx, "SELECT name,sort,group_id FROM sx_live_channel WHERE id=?", jadeID)
	seededManual, _ := one(ctx, "SELECT priority,headers_json FROM sx_live_stream WHERE id=?", streamID)
	seededBackup, _ := one(ctx, "SELECT priority FROM sx_live_stream WHERE url_hash=?", liveIdentity("https://8.8.4.4/jade/backup.m3u8"))
	if gconv.String(seededChannel["name"]) != "自定义翡翠台" || gconv.Int(seededChannel["sort"]) != 87 || gconv.Int64(seededChannel["group_id"]) != gconv.Int64(channels[0]["group_id"]) || gconv.Int(seededManual["priority"]) != 900 || !strings.Contains(gconv.String(seededManual["headers_json"]), "controlled-secret") || gconv.Int(seededBackup["priority"]) != 100 {
		t.Fatal("local vetted seed overwrote administrator properties or moved existing group")
	}
	t.Log("PASS: local vetted seed uses normal probes, retains existing channel groups/manual values, and prefers verified AVC backups")

	before, _ := all(ctx, "SELECT id,name,enabled FROM sx_live_channel ORDER BY id")
	if err = execSQL(ctx, "UPDATE sx_live_subscription SET enabled=0"); err != nil {
		t.Fatal(err)
	}
	result, err := g.DB().Exec(ctx, "INSERT INTO sx_live_subscription(name,url,url_hash,created,updated) VALUES('failed fixture','http://127.0.0.1/no.m3u',?,?,?)", liveIdentity("failed-fixture"), time.Now().Unix(), time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	subID, _ := result.LastInsertId()
	progress := newProgress()
	if err = liveRefreshSubscriptions(ctx, progress, subID, true); err != nil {
		t.Fatal(err)
	}
	after, _ := all(ctx, "SELECT id,name,enabled FROM sx_live_channel ORDER BY id")
	if !reflect.DeepEqual(before, after) || progress.Failed == 0 {
		t.Fatal("failed refresh deleted or rewrote channel data")
	}
	failedSub, _ := one(ctx, "SELECT last_error FROM sx_live_subscription WHERE id=?", subID)
	if gconv.String(failedSub["last_error"]) == "" {
		t.Fatal("refresh failure not visible")
	}
	t.Log("PASS: inaccessible subscription does not overwrite existing channels; failure is persisted")

	base := mediaBinaryHTTPServer(t, func(group *ghttp.RouterGroup) {
		group.Group("/suxinvideo", RegisterLiveRoutes)
		group.Group("/admin/suxinvideo", func(admin *ghttp.RouterGroup) {
			admin.Middleware(func(r *ghttp.Request) {
				if value := r.Header.Get("X-Controlled-UID"); value != "" {
					r.SetCtx(context.WithValue(r.Context(), "uid", gconv.Int64(value)))
				}
				r.Middleware.Next()
			}, cmsAudit)
			admin.Bind(new(Admin))
		})
	})
	client := &http.Client{Timeout: 5 * time.Second}
	call := func(method, path string, uid int, body any) (row, string) {
		t.Helper()
		encoded, _ := json.Marshal(body)
		request, e := http.NewRequest(method, base+path, bytes.NewReader(encoded))
		if e != nil {
			t.Fatal(e)
		}
		request.Header.Set("Content-Type", "application/json")
		if uid > 0 {
			request.Header.Set("X-Controlled-UID", fmt.Sprint(uid))
		}
		response, e := client.Do(request)
		if e != nil {
			t.Fatal("controlled admin transport failed")
		}
		defer response.Body.Close()
		var envelope row
		if json.NewDecoder(response.Body).Decode(&envelope) != nil {
			t.Fatal("admin response not valid JSON")
		}
		encoded, _ = json.Marshal(envelope)
		return envelope, string(encoded)
	}
	for _, uid := range []int{0, 202} {
		response, _ := call("GET", "/admin/suxinvideo/live/channels", uid, nil)
		if gconv.Int(response["code"]) == 0 {
			t.Fatal("unauthorized live list accepted")
		}
	}
	response, _ := call("POST", "/admin/suxinvideo/live/save", 201, row{"kind": "channel", "id": jadeID, "data": row{"name": "should not change"}})
	if gconv.Int(response["code"]) == 0 {
		t.Fatal("reader changed live channel")
	}
	response, encoded := call("GET", fmt.Sprintf("/admin/suxinvideo/live/streams?channel_id=%d", jadeID), 201, nil)
	if gconv.Int(response["code"]) != 0 || strings.Contains(encoded, "controlled-secret") || !strings.Contains(encoded, "••••••") {
		t.Fatal("source credential redaction failed")
	}
	response, _ = call("GET", "/admin/suxinvideo/capabilities", 201, nil)
	caps := gconv.Map(response["data"])
	if !gconv.Bool(gconv.Map(caps["pages"])["content/live"]) || gconv.Bool(gconv.Map(caps["actions"])["live/save"]) {
		t.Fatal("live page/action capabilities incorrect")
	}
	response, _ = call("POST", "/admin/suxinvideo/live/save", 203, row{"kind": "group", "id": 0, "data": row{"name": "Controlled group", "enabled": true}})
	if gconv.Int(response["code"]) != 0 {
		t.Fatal("authorized group creation failed")
	}
	logs, _ := all(ctx, "SELECT action FROM sx_admin_log")
	if len(logs) != 1 || strings.Contains(gconv.String(logs[0]["action"]), "secret") {
		t.Fatal("audit recorded denied action or credential")
	}
	t.Log("PASS: real local admin HTTP routes deny anonymous/unrelated/editor escalation, redact secrets, expose exact capabilities and audit only allowed summaries")

	// Swap only the controlled upstream transport. Neither media nor native API
	// requests contact a real station or the production development service.
	var sequence atomic.Int64
	keyBytes := []byte("0123456789abcdef")
	fragmentBytes := bytes.Repeat([]byte{0x47, 0x40, 0x11, 0x10}, 300)
	liveHTTPClient = &http.Client{Transport: liveFixtureTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/jade/first.m3u8":
			return liveFixtureResponse(r, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=5000000,RESOLUTION=1920x1080\nchild.m3u8\n", "application/vnd.apple.mpegurl"), nil
		case "/jade/child.m3u8":
			body := fmt.Sprintf("#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXT-X-MEDIA-SEQUENCE:%d\n#EXT-X-PROGRAM-DATE-TIME:2026-10-03T12:00:00Z\n#EXT-X-KEY:METHOD=AES-128,URI=\"key\"\n#EXTINF:6,\npart.ts\n", 700+sequence.Add(1))
			return liveFixtureResponse(r, body, "application/vnd.apple.mpegurl"), nil
		case "/jade/key":
			return liveFixtureResponse(r, string(keyBytes), "application/octet-stream"), nil
		case "/jade/part.ts":
			response := liveFixtureResponse(r, string(fragmentBytes), "video/mp2t")
			if r.Header.Get("Range") == "bytes=0-15" {
				response.StatusCode = http.StatusPartialContent
				response.Body = io.NopCloser(bytes.NewReader(fragmentBytes[:16]))
				response.Header.Set("Content-Range", fmt.Sprintf("bytes 0-15/%d", len(fragmentBytes)))
			}
			return response, nil
		default:
			t.Fatal("controlled live requested an unexpected resource")
			return nil, fmt.Errorf("unsupported fixture resource")
		}
	})}
	response, encoded = call("POST", "/suxinvideo/app/v1/live/resolve", 0, row{"channel_id": jadeID, "stream_id": streamID})
	if gconv.Int(response["code"]) != 0 || strings.Contains(encoded, "8.8.8.8") || strings.Contains(encoded, "controlled-secret") {
		t.Fatal("guest live resolution failed or upstream leaked")
	}
	play := gconv.Map(response["data"])
	token, mediaURL := gconv.String(play["session_id"]), gconv.String(play["url"])
	if token == "" || !strings.HasPrefix(mediaURL, "/suxinvideo/live/media?") || !gconv.Bool(play["is_live"]) {
		t.Fatal("native live descriptor lacks session or opaque media URL")
	}
	t.Cleanup(func() { liveSessions.Lock(); delete(liveSessions.Items, token); liveSessions.Unlock() })
	getBytes := func(path, byteRange string) ([]byte, int, http.Header) {
		t.Helper()
		request, e := http.NewRequest("GET", base+path, nil)
		if e != nil {
			t.Fatal(e)
		}
		if byteRange != "" {
			request.Header.Set("Range", byteRange)
		}
		response, e := client.Do(request)
		if e != nil {
			t.Fatal("controlled live media transport failed")
		}
		defer response.Body.Close()
		body, e := io.ReadAll(response.Body)
		if e != nil {
			t.Fatal(e)
		}
		return body, response.StatusCode, response.Header
	}
	master, status, headers := getBytes(mediaURL, "")
	if status != 200 || headers.Get("Cache-Control") != "private, no-store" || !bytes.HasPrefix(master, []byte("#EXTM3U")) {
		t.Fatal("live master response was cached, JSON wrapped or invalid")
	}
	lines := strings.Split(string(master), "\n")
	childURL := ""
	for _, line := range lines {
		if strings.HasPrefix(line, "/suxinvideo/live/media?") {
			childURL = line
		}
	}
	if childURL == "" || strings.Contains(string(master), "8.8.8.8") {
		t.Fatal("master child was not opaque")
	}
	child, childStatus, _ := getBytes(childURL, "")
	if childStatus != 200 || !bytes.Contains(child, []byte("#EXT-X-MEDIA-SEQUENCE:")) || !bytes.Contains(child, []byte("#EXT-X-PROGRAM-DATE-TIME:")) || bytes.Contains(child, []byte("#EXT-X-ENDLIST")) {
		t.Fatal("dynamic live window was converted or lost")
	}
	keyURL, partURL := "", ""
	for _, line := range strings.Split(string(child), "\n") {
		if strings.HasPrefix(line, "#EXT-X-KEY:") {
			attrs := liveAttributes(line)
			keyURL = attrs["URI"]
		}
		if strings.HasPrefix(line, "/suxinvideo/live/media?") {
			partURL = line
		}
	}
	key, status, _ := getBytes(keyURL, "")
	if status != 200 || !bytes.Equal(key, keyBytes) {
		t.Fatal("AES key was altered or wrapped")
	}
	part, status, headers := getBytes(partURL, "bytes=0-15")
	if status != 206 || !bytes.Equal(part, fragmentBytes[:16]) || headers.Get("Content-Range") != fmt.Sprintf("bytes 0-15/%d", len(fragmentBytes)) {
		t.Fatal("live fragment Range semantics changed")
	}
	secondChild, status, _ := getBytes(childURL, "")
	if status != 200 || bytes.Equal(child, secondChild) {
		t.Fatal("live manifest was frozen between refreshes")
	}
	response, _ = call("POST", "/suxinvideo/app/v1/live/renew", 0, row{"session_id": token})
	if gconv.Int(response["code"]) != 0 || gconv.String(gconv.Map(response["data"])["session_id"]) != token {
		t.Fatal("renewal changed or rejected live session")
	}
	if _, status, _ = getBytes(mediaURL, ""); status != 200 {
		t.Fatal("original opaque media URL stopped working after renewal")
	}
	response, _ = call("GET", "/suxinvideo/live/media?session="+token+"&asset=arbitrary", 0, nil)
	if gconv.Int(response["code"]) != 403 {
		t.Fatal("unregistered media asset accepted")
	}
	if _, err = liveSaveData(ctx, "channel", jadeID, row{"enabled": false}); err != nil {
		t.Fatal(err)
	}
	response, _ = call("GET", mediaURL, 0, nil)
	if gconv.Int(response["code"]) != 403 {
		t.Fatal("disabled channel retained media authorization")
	}
	if _, err = liveSaveData(ctx, "channel", jadeID, row{"enabled": true}); err != nil {
		t.Fatal(err)
	}
	if _, err = liveSaveData(ctx, "stream", streamID, row{"enabled": false}); err != nil {
		t.Fatal(err)
	}
	response, _ = call("GET", mediaURL, 0, nil)
	if gconv.Int(response["code"]) != 403 {
		t.Fatal("disabled stream retained media authorization")
	}
	response, _ = call("POST", "/admin/suxinvideo/live/save", 203, row{"kind": "stream", "id": 0, "data": row{"name": "private fixture", "channel_id": jadeID, "url": "http://127.0.0.1/private.m3u8"}})
	if gconv.Int(response["code"]) == 0 {
		t.Fatal("private media URL was admitted")
	}
	response, _ = call("POST", "/admin/suxinvideo/live/import", 203, row{"url": "http://127.0.0.1/private.m3u", "preview": true})
	if gconv.Int(response["code"]) == 0 {
		t.Fatal("private subscription URL was fetched")
	}
	response, _ = call("POST", "/admin/suxinvideo/live/import", 203, row{"content": "invalid,file:///private", "preview": true})
	if gconv.Int(response["code"]) == 0 {
		t.Fatal("invalid import accepted")
	}
	t.Log("PASS: guest live resolve, opaque master/child, moving window, exact AES and Range bytes, stable renewal, revoked channel/stream/asset and private/invalid URL rejection")
	t.Run("ChineseCatalogueUpgrade", func(t *testing.T) { liveLocalizedCatalogueIntegration(t, ctx) })
	t.Run("RequestedLiveGroupLayout", func(t *testing.T) { liveGroupLayoutIntegration(t, ctx) })
	t.Run("ProviderAccessAndSubscriptions", func(t *testing.T) { liveProviderControlledIntegration(t, ctx) })
}

func liveGroupLayoutIntegration(t *testing.T, ctx context.Context) {
	t.Helper()
	groupRows, _ := all(ctx, "SELECT id,name,identity_key,sort,enabled,manual_edited FROM sx_live_group ORDER BY sort,id")
	if len(groupRows) < 2 || gconv.String(groupRows[0]["identity_key"]) != liveIdentity(liveRegionalGroupKey) || gconv.String(groupRows[1]["identity_key"]) != liveIdentity(liveCCTVGroupKey) {
		t.Fatal("regional and CCTV groups did not precede legacy groups")
	}
	regionalID := gconv.Int64(groupRows[0]["id"])
	cctvID := gconv.Int64(groupRows[1]["id"])
	// Existing Golden Jade is edited by earlier tests. It must remain an
	// independent, preserved channel throughout this default-layout upgrade.
	goldenBefore, _ := one(ctx, "SELECT * FROM sx_live_channel WHERE tvg_id='GoldenJade.hk'")
	fixtures := []string{"PhoenixChineseChannel.hk", "PhoenixInfoNewsChannel.hk", "PhoenixHongKongChannel.hk", "LotusTV.mo", "TVBSAsia.tw", "CCTV13.cn", "CCTV1.cn", "CCTV5Plus.cn", "CCTV5.cn"}
	for _, id := range fixtures {
		_, _, err := liveUpsertImport(ctx, liveImportItem{TVGID: id, Name: id, Group: "Unrelated upstream genre", URL: "https://8.8.8.8/layout/" + id + ".m3u8"}, 1)
		if err != nil {
			t.Fatal(err)
		}
	}
	// A pre-upgrade catalogue can contain arbitrary original genre groups.
	// Move one fixture back there directly to exercise genuine migration.
	legacy, _ := one(ctx, "SELECT id FROM sx_live_group WHERE identity_key=?", liveIdentity("news"))
	phoenix, _ := one(ctx, "SELECT * FROM sx_live_channel WHERE tvg_id='PhoenixChineseChannel.hk'")
	phoenixID := gconv.Int64(phoenix["id"])
	if err := execSQL(ctx, "UPDATE sx_live_channel SET group_id=?,sort=287,enabled=0,aliases_json='[\"Phoenix fixture\"]' WHERE id=?", legacy["id"], phoenixID); err != nil {
		t.Fatal(err)
	}
	phoenixBefore, _ := one(ctx, "SELECT * FROM sx_live_channel WHERE id=?", phoenixID)
	for range 2 {
		if err := UpgradeLiveChannelGroups(ctx); err != nil {
			t.Fatal(err)
		}
	}
	phoenixAfter, _ := one(ctx, "SELECT * FROM sx_live_channel WHERE id=?", phoenixID)
	if gconv.Int64(phoenixAfter["group_id"]) != regionalID || gconv.Int(phoenixAfter["sort"]) != -9999 {
		t.Fatal("existing Phoenix did not migrate from its original genre")
	}
	for _, field := range []string{"id", "name", "tvg_id", "identity_key", "aliases_json", "logo", "enabled", "manual_edited", "created"} {
		if !reflect.DeepEqual(phoenixBefore[field], phoenixAfter[field]) {
			t.Fatal("layout migration altered protected channel data", field)
		}
	}
	groupRowsAfter, _ := all(ctx, "SELECT id,name,identity_key,sort,enabled,manual_edited FROM sx_live_group ORDER BY sort,id")
	if !reflect.DeepEqual(groupRows, groupRowsAfter) {
		t.Fatal("repeat upgrade deleted/duplicated groups or changed their administrative values")
	}
	goldenAfter, _ := one(ctx, "SELECT * FROM sx_live_channel WHERE tvg_id='GoldenJade.hk'")
	if !reflect.DeepEqual(goldenBefore, goldenAfter) {
		t.Fatal("default layout changed an administrator-edited Golden Jade channel")
	}
	for _, id := range fixtures {
		channel, _ := one(ctx, "SELECT group_id,sort FROM sx_live_channel WHERE tvg_id=?", id)
		layout, _ := liveSystemChannelLayout(id)
		wantGroup := regionalID
		if layout.Key == liveCCTVGroupKey {
			wantGroup = cctvID
		}
		if gconv.Int64(channel["group_id"]) != wantGroup || gconv.Int(channel["sort"]) != layout.Sort {
			t.Fatal("new import classification/sort incorrect", id)
		}
	}
	// Refreshing a known source must keep its system grouping. Once an admin
	// deliberately moves/sorts it, both subsequent imports and upgrades yield.
	item := liveImportItem{TVGID: "PhoenixChineseChannel.hk", Name: "Upstream Phoenix", Group: "News", URL: "https://8.8.8.8/layout/PhoenixChineseChannel.hk.m3u8"}
	if _, _, err := liveUpsertImport(ctx, item, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := liveSaveData(ctx, "channel", phoenixID, row{"name": "自定义凤凰台", "group_id": legacy["id"], "sort": 77, "enabled": false}); err != nil {
		t.Fatal(err)
	}
	manualBefore, _ := one(ctx, "SELECT group_id,sort,name,enabled,identity_key,aliases_json FROM sx_live_channel WHERE id=?", phoenixID)
	if _, _, err := liveUpsertImport(ctx, item, 1); err != nil {
		t.Fatal(err)
	}
	if err := UpgradeLiveChannelGroups(ctx); err != nil {
		t.Fatal(err)
	}
	manualAfter, _ := one(ctx, "SELECT group_id,sort,name,enabled,identity_key,aliases_json FROM sx_live_channel WHERE id=?", phoenixID)
	if !reflect.DeepEqual(manualBefore, manualAfter) {
		t.Fatal("import or upgrade replaced later administrator group/order/name/disable changes")
	}
	// Simulate the requested name having existed before this system layout:
	// it has a legacy identity and manual presentation. Adopt its stable role
	// without duplicating it, then retain it through later rename/disable.
	if err := execSQL(ctx, "UPDATE sx_live_group SET identity_key=?,manual_edited=1,sort=-5000 WHERE id=?", liveIdentity("legacy-regional-group"), regionalID); err != nil {
		t.Fatal(err)
	}
	legacyGroupBefore, _ := one(ctx, "SELECT id,name,sort,enabled,manual_edited FROM sx_live_group WHERE id=?", regionalID)
	if err := UpgradeLiveChannelGroups(ctx); err != nil {
		t.Fatal(err)
	}
	legacyGroupAfter, _ := one(ctx, "SELECT id,name,sort,enabled,manual_edited FROM sx_live_group WHERE id=?", regionalID)
	bound, _ := one(ctx, "SELECT identity_key FROM sx_live_group WHERE id=?", regionalID)
	if !reflect.DeepEqual(legacyGroupBefore, legacyGroupAfter) || gconv.String(bound["identity_key"]) != liveIdentity(liveRegionalGroupKey) {
		t.Fatal("same-name legacy group was not bound stably without changing manual fields")
	}
	if _, err := liveSaveData(ctx, "group", regionalID, row{"name": "自定义港澳台", "sort": -5000, "enabled": false}); err != nil {
		t.Fatal(err)
	}
	manualGroupBefore, _ := one(ctx, "SELECT * FROM sx_live_group WHERE id=?", regionalID)
	if err := UpgradeLiveChannelGroups(ctx); err != nil {
		t.Fatal(err)
	}
	manualGroupAfter, _ := one(ctx, "SELECT * FROM sx_live_group WHERE id=?", regionalID)
	if !reflect.DeepEqual(manualGroupBefore, manualGroupAfter) {
		t.Fatal("system group identity did not preserve later manual rename/order/enable")
	}
	boundGroups, _ := one(ctx, "SELECT COUNT(*) n FROM sx_live_group WHERE identity_key IN (?,?)", liveIdentity(liveRegionalGroupKey), liveIdentity(liveCCTVGroupKey))
	if gconv.Int(boundGroups["n"]) != 2 {
		t.Fatal("renaming an adopted system group recreated a duplicate")
	}
	t.Log("PASS: authority-based HK/MO/TW+CCTV grouping, Jade/Phoenix pinning, numeric CCTV sort, repeat upgrade/import, legacy groups and later admin overrides preserved")
}

func liveLocalizedCatalogueIntegration(t *testing.T, ctx context.Context) {
	t.Helper()
	now := time.Now().Unix()
	if err := execSQL(ctx, "INSERT INTO sx_live_group(name,identity_key,sort,enabled,created,updated) VALUES('News',?,44,1,?,?)", liveIdentity("news"), now, now); err != nil {
		t.Fatal(err)
	}
	groupBefore, _ := one(ctx, "SELECT * FROM sx_live_group WHERE identity_key=?", liveIdentity("news"))
	groupID := gconv.Int64(groupBefore["id"])
	item := liveImportItem{TVGID: "BeijingSatelliteTV.cn", Name: "Beijing Satellite TV (720p)", Group: "News", Quality: "720p", URL: "https://8.8.8.8/localized/first.m3u8"}
	firstID, _, err := liveUpsertImport(ctx, item, 1)
	if err != nil {
		t.Fatal(err)
	}
	channelBefore, _ := one(ctx, "SELECT * FROM sx_live_channel WHERE tvg_id='BeijingSatelliteTV.cn'")
	channelID := gconv.Int64(channelBefore["id"])
	if err = execSQL(ctx, "UPDATE sx_live_channel SET sort=31,aliases_json='[\"Beijing TV\"]',enabled=0 WHERE id=?", channelID); err != nil {
		t.Fatal(err)
	}
	channelBefore, _ = one(ctx, "SELECT * FROM sx_live_channel WHERE id=?", channelID)
	if err = LocalizeLiveCatalog(ctx); err != nil {
		t.Fatal(err)
	}
	groupAfter, _ := one(ctx, "SELECT * FROM sx_live_group WHERE id=?", groupID)
	channelAfter, _ := one(ctx, "SELECT * FROM sx_live_channel WHERE id=?", channelID)
	if gconv.String(groupAfter["name"]) != "新闻" || gconv.String(channelAfter["name"]) != "北京卫视" {
		t.Fatal("stored catalogue did not receive Chinese labels")
	}
	for _, key := range []string{"id", "identity_key", "sort", "enabled", "manual_edited"} {
		if !reflect.DeepEqual(groupBefore[key], groupAfter[key]) {
			t.Fatal("group upgrade changed protected property", key)
		}
	}
	for _, key := range []string{"id", "identity_key", "tvg_id", "aliases_json", "group_id", "sort", "enabled", "manual_edited"} {
		if !reflect.DeepEqual(channelBefore[key], channelAfter[key]) {
			t.Fatal("channel upgrade changed protected property", key)
		}
	}
	if err = LocalizeLiveCatalog(ctx); err != nil {
		t.Fatal(err)
	}
	groupRepeat, _ := one(ctx, "SELECT * FROM sx_live_group WHERE id=?", groupID)
	channelRepeat, _ := one(ctx, "SELECT * FROM sx_live_channel WHERE id=?", channelID)
	if !reflect.DeepEqual(groupAfter, groupRepeat) || !reflect.DeepEqual(channelAfter, channelRepeat) {
		t.Fatal("display upgrade is not idempotent")
	}
	items, _, err := liveParseImport("#EXTM3U\n#EXTINF:-1 tvg-id=\"BeijingSatelliteTV.cn\" group-title=\"News\",Beijing Satellite TV (1080p)\nhttps://8.8.4.4/localized/backup.m3u8\n", "")
	if err != nil {
		t.Fatal(err)
	}
	secondID, _, err := liveUpsertImport(ctx, items[0], 1)
	if err != nil {
		t.Fatal(err)
	}
	count, _ := one(ctx, "SELECT COUNT(*) n FROM sx_live_group WHERE name IN ('News','新闻')")
	channelAfter, _ = one(ctx, "SELECT group_id,enabled FROM sx_live_channel WHERE id=?", channelID)
	if gconv.Int(count["n"]) != 1 || gconv.Int64(channelAfter["group_id"]) != groupID || gconv.Bool(channelAfter["enabled"]) {
		t.Fatal("subscription repeated after localization duplicated a category or enabled its channel")
	}
	if _, err = liveSaveData(ctx, "group", groupID, row{"name": "自定义新闻分组", "sort": 91, "enabled": false}); err != nil {
		t.Fatal(err)
	}
	if _, err = liveSaveData(ctx, "channel", channelID, row{"name": "自定义频道", "enabled": false}); err != nil {
		t.Fatal(err)
	}
	if _, err = liveSaveData(ctx, "stream", firstID, row{"name": "管理员线路", "priority": 700, "enabled": false}); err != nil {
		t.Fatal(err)
	}
	if err = LocalizeLiveCatalog(ctx); err != nil {
		t.Fatal(err)
	}
	// An exact known channel imported by the local Seed retains its actual ID,
	// even if an administrator has renamed/disabled its translated group.
	seed, err := SeedLiveChannels(ctx, "#EXTM3U\n#EXTINF:-1 tvg-id=\"BeijingSatelliteTV.cn\" group-title=\"News\",Beijing Satellite TV\nhttps://8.8.8.8/localized/first.m3u8\n", "News")
	if err != nil || len(seed.StreamIDs) != 1 || seed.StreamIDs[0] != firstID {
		t.Fatal("seed after manual group localization failed", err)
	}
	groupAfter, _ = one(ctx, "SELECT name,sort,enabled FROM sx_live_group WHERE id=?", groupID)
	channelAfter, _ = one(ctx, "SELECT name,group_id,enabled,aliases_json FROM sx_live_channel WHERE id=?", channelID)
	if gconv.String(groupAfter["name"]) != "自定义新闻分组" || gconv.Int(groupAfter["sort"]) != 91 || gconv.Bool(groupAfter["enabled"]) || gconv.String(channelAfter["name"]) != "自定义频道" || gconv.Bool(channelAfter["enabled"]) || gconv.Int64(channelAfter["group_id"]) != groupID || !strings.Contains(gconv.String(channelAfter["aliases_json"]), "Beijing TV") {
		t.Fatal("upgrade or seed overwrote manual group/channel settings")
	}
	first, _ := liveStream(ctx, firstID)
	second, _ := liveStream(ctx, secondID)
	if first.Name != "管理员线路" || second.Name != "备用2 · 1080p" || first.Enabled || first.Priority != 700 {
		t.Fatal("manual source label/priority/disabled flag or stable backup label lost")
	}
	t.Log("PASS: Chinese labels preserve identities/aliases/groups/order/disabled/manual values; repeated import and local Seed reuse groups; backups stay distinct")
}
