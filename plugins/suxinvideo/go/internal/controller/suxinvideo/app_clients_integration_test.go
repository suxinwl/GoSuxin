package suxinvideo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"
)

// This suite can only run inside the runner's disposable schema. It uses real
// HTTP/WebSocket transports, persisted device credentials and a controlled
// media service; it neither restarts nor talks to the development :8600 server.
func TestAppClientsControlledIntegration(t *testing.T) {
	if os.Getenv("SUXIN_APP_CLIENTS_INTEGRATION") != "1" {
		t.Skip("run plugins/suxinvideo/tools/test_app_clients.py")
	}
	ctx := yqkIntegrationContext(t)
	db, err := one(ctx, "SELECT DATABASE() name")
	if err != nil || !regexp.MustCompile(`^suxin_app_clients_verify_[0-9]+$`).MatchString(gconv.String(db["name"])) {
		t.Fatal("client integration refuses a non-disposable database")
	}
	appSchemaMutex.Lock()
	appSchemaReady = false
	appSchemaMutex.Unlock()
	if err = EnsureAppSchema(ctx); err != nil {
		t.Fatal("native schema initialization failed")
	}
	if err = PrepareClientIntegrations(ctx); err != nil {
		t.Fatal("client schema initialization failed")
	}
	if err = PrepareClientIntegrations(ctx); err != nil {
		t.Fatal("client schema upgrade is not idempotent")
	}
	for _, tc := range []struct {
		uid     int
		action  string
		allowed bool
	}{
		{71, "clients/releases", true}, {71, "clients/publish", false}, {71, "clients/status", false},
		{72, "clients/releases", true}, {72, "clients/publish", true}, {72, "clients/status", true},
		{73, "clients/releases", false}, {73, "clients/publish", false}, {74, "clients/publish", true},
	} {
		allowed, e := resourceAllowed(context.WithValue(ctx, "uid", tc.uid), "", tc.action)
		if e != nil || allowed != tc.allowed {
			t.Fatalf("role migration/permission mismatch uid=%d action=%s", tc.uid, tc.action)
		}
	}
	rules, _ := all(ctx, "SELECT id,title,path FROM gf_auth_rule WHERE routename='host_fixture'")
	if len(rules) != 1 || gconv.Int(rules[0]["id"]) != 4 || gconv.String(rules[0]["path"]) != "/admin/host-fixture" || gconv.String(rules[0]["title"]) != "Host sentinel" {
		t.Fatal("client upgrade changed the existing host route")
	}
	newPermissions, err := one(ctx, "SELECT COUNT(*) n FROM gf_auth_rule WHERE path LIKE '/admin/suxinvideo/clients/%' AND des='' AND locale='' AND icon='' AND component='' AND redirect=''")
	if err != nil || gconv.Int(newPermissions["n"]) != 3 {
		t.Fatal("client permission insert omitted mandatory host strings")
	}
	for _, id := range []int{1, 2, 3} {
		role, _ := one(ctx, "SELECT rules,btns FROM gf_auth_role WHERE id=?", id)
		if !strings.Contains(","+gconv.String(role["btns"])+",", ",4,") {
			t.Fatal("client upgrade discarded an unrelated host role grant")
		}
	}
	t.Log("PASS: repeated schema upgrade, inherited reader/editor rights, denied unrelated role, host permissions preserved")

	password, _ := appToken()
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	for _, email := range []string{"client-fixture@example.invalid", "other-client-fixture@example.invalid"} {
		if err = execSQL(ctx, "INSERT INTO sx_user(email,name,pwd,status,points) VALUES(?,'client fixture',?,1,100)", email, string(hash)); err != nil {
			t.Fatal("member fixture initialization failed")
		}
	}
	mobile, err := AppAuthenticatePassword(ctx, "client-fixture@example.invalid", password, "fixture-mobile", "fixture mobile", "192.0.2.11")
	if err != nil {
		t.Fatal("fixture login failed")
	}
	other, err := AppAuthenticatePassword(ctx, "other-client-fixture@example.invalid", password, "fixture-other", "fixture other", "192.0.2.12")
	if err != nil {
		t.Fatal("second fixture login failed")
	}
	unauthorizedDevice, err := AppIssueDeviceSession(ctx, gconv.Int64(mobile.User["id"]), "fixture-unpaired", "unpaired fixture")
	if err != nil {
		t.Fatal("unpaired fixture login failed")
	}
	nativeURL := mediaBinaryHTTPServer(t, func(group *ghttp.RouterGroup) {
		group.Group("/suxinvideo", func(public *ghttp.RouterGroup) { public.Bind(new(AppClients)) })
	})
	client := &http.Client{Timeout: 5 * time.Second}
	call := func(method, path, token string, input any, status int) row {
		t.Helper()
		body, _ := json.Marshal(input)
		r, e := http.NewRequest(method, nativeURL+path, bytes.NewReader(body))
		if e != nil {
			t.Fatal("fixture request construction failed")
		}
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		response, e := client.Do(r)
		if e != nil {
			t.Fatal("native fixture transport failed")
		}
		defer response.Body.Close()
		if response.StatusCode != status {
			t.Fatalf("native route %s returned %d, expected %d", strings.Split(path, "?")[0], response.StatusCode, status)
		}
		var envelope struct {
			Code int
			Data json.RawMessage
		}
		if e = json.NewDecoder(response.Body).Decode(&envelope); e != nil {
			t.Fatal("native route returned invalid JSON")
		}
		if status == 200 && envelope.Code != 0 {
			t.Fatal("native success envelope contains failure")
		}
		var data row
		if len(envelope.Data) > 0 && envelope.Data[0] == '{' {
			if json.Unmarshal(envelope.Data, &data) != nil {
				t.Fatal("native route returned invalid object data")
			}
		}
		return data
	}
	pair := call("POST", "/suxinvideo/app/v1/devices/pair/create", "", row{"device_id": "fixture-tv", "name": "Fixture TV", "platform": "tv"}, 200)
	code, pairID, poll := gconv.String(pair["code"]), gconv.String(pair["pair_id"]), gconv.String(pair["poll_token"])
	if !appPairCode(code) || len(pairID) != 48 || len(poll) != 48 {
		t.Fatal("pairing lacks isolated short code and polling credential")
	}
	statusPath := "/suxinvideo/app/v1/devices/pair/status?pair_id=" + url.QueryEscape(pairID) + "&poll_token=" + url.QueryEscape(poll)
	if gconv.String(call("GET", statusPath, "", nil, 200)["status"]) != "pending" {
		t.Fatal("pairing not pending")
	}
	call("POST", "/suxinvideo/app/v1/devices/pair/approve", "", row{"code": code}, 401)
	call("POST", "/suxinvideo/app/v1/devices/pair/approve", mobile.AccessToken, row{"code": code}, 200)
	approved := call("GET", statusPath, "", nil, 200)
	var tv AppTokens
	data, _ := json.Marshal(approved["tokens"])
	_ = json.Unmarshal(data, &tv)
	if tv.DeviceID != "fixture-tv" || len(tv.AccessToken) != 48 {
		t.Fatal("approved TV did not receive its own persistent session")
	}
	stored, _ := one(ctx, "SELECT poll_hash FROM sx_app_pairing WHERE id=?", pairID)
	if gconv.String(stored["poll_hash"]) == poll || gconv.String(stored["poll_hash"]) != appHash(poll) {
		t.Fatal("polling secret persisted in plaintext")
	}
	t.Log("PASS: anonymous pairing, authenticated approval, TV-bound token delivery and hash-only polling secret")

	cast := call("POST", "/suxinvideo/app/v1/devices/cast/create", mobile.AccessToken, row{"device_id": "fixture-tv"}, 200)
	castAgain := call("POST", "/suxinvideo/app/v1/devices/cast/create", mobile.AccessToken, row{"device_id": "fixture-tv"}, 200)
	if gconv.String(cast["session_id"]) != gconv.String(castAgain["session_id"]) {
		t.Fatal("same controller created a second TV room")
	}
	current := call("GET", "/suxinvideo/app/v1/devices/cast/current", tv.AccessToken, nil, 200)
	if current["active"] != true || gconv.String(current["session_id"]) != gconv.String(cast["session_id"]) {
		t.Fatal("TV could not discover mobile's room")
	}
	wsURL := gconv.String(cast["ws_url"])
	dial := func(token string, status int) *websocket.Conn {
		t.Helper()
		c, response, e := websocket.DefaultDialer.Dial(wsURL, http.Header{"Authorization": []string{"Bearer " + token}})
		if status != 101 {
			if e == nil {
				c.Close()
				t.Fatal("unauthorized WebSocket joined room")
			}
			if response == nil || response.StatusCode != status {
				t.Fatal("WebSocket authorization status mismatch")
			}
			return nil
		}
		if e != nil {
			t.Fatal("authorized WebSocket connection failed")
		}
		t.Cleanup(func() { c.Close() })
		return c
	}
	dial(other.AccessToken, 403)
	dial(unauthorizedDevice.AccessToken, 403)
	mobileWS := dial(mobile.AccessToken, 101)
	if mobileWS.WriteJSON(row{"type": "command", "id": "pending-play", "action": "play", "payload": row{"vod_id": 1001}}) != nil {
		t.Fatal("WebSocket play write failed")
	}
	deadline := time.Now().Add(time.Second)
	for {
		castHub.Lock()
		ready := len(castHub.commands[gconv.String(cast["session_id"])]) > 0
		castHub.Unlock()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("play sent before TV connection was lost")
		}
		time.Sleep(10 * time.Millisecond)
	}
	tvWS := dial(tv.AccessToken, 101)
	read := func(c *websocket.Conn, wantID string) {
		t.Helper()
		c.SetReadDeadline(time.Now().Add(3 * time.Second))
		var msg row
		if c.ReadJSON(&msg) != nil || gconv.String(msg["id"]) != wantID {
			t.Fatalf("WebSocket delivery mismatch for %s", wantID)
		}
	}
	read(tvWS, "pending-play")
	_ = mobileWS.WriteJSON(row{"type": "command", "id": "forbidden", "action": "delete"})
	_ = mobileWS.WriteJSON(row{"type": "command", "id": "pause-1", "action": "pause"})
	read(tvWS, "pause-1")
	_ = tvWS.WriteJSON(row{"type": "command", "id": "forbidden-tv", "action": "play"})
	_ = tvWS.WriteJSON(row{"type": "status", "id": "status-1", "payload": row{"playing": false, "position_ms": 12000}})
	read(mobileWS, "status-1")
	t.Log("PASS: cast room reuse/discovery, pending command replay, mobile controls, TV status, forbidden user/device/action rejected")
	pageRequest, _ := http.NewRequest("GET", nativeURL+"/suxinvideo/app-download?pair="+code, nil)
	pageResponse, e := client.Do(pageRequest)
	if e != nil {
		t.Fatal("download page fixture failed")
	}
	page, _ := io.ReadAll(pageResponse.Body)
	pageResponse.Body.Close()
	if pageResponse.StatusCode != 200 || !bytes.Contains(page, []byte("xiaoqi://pair?code="+code)) || bytes.Contains(page, []byte("ZgotmplZ")) {
		t.Fatal("pair approval deep link omitted or rejected by template")
	}
	t.Log("PASS: download page renders actual native pairing deep link")

	// The media service is controlled: it applies the same live CMS auth and
	// film rights, and returns signed media. No external supplier is contacted.
	for _, query := range []string{
		"INSERT INTO sx_type(id,pid,name,sort,status) VALUES(9001,0,'\u77ed\u5267',0,1),(9003,0,'\u7535\u5f71',0,1)",
		"INSERT INTO sx_collect_api(id,name,api_url,status,collect_auto) VALUES(901,'controlled fixture','https://fixture.invalid/api',1,0)",
		"UPDATE sx_player SET status=1 WHERE code='hnm3u8'",
		"INSERT INTO sx_vod(id,type_id,api_id,api_vid,name,name_norm,play_from,play_url,status,year,pic) VALUES(1001,9003,901,'client-1','Client fixture movie','client fixture movie','hnm3u8','feature$https://fixture.invalid/movie.mp4',1,'2026','/suxinvideo/image/local?id=fixture')",
		"INSERT INTO sx_vod(id,type_id,api_id,api_vid,name,name_norm,play_from,play_url,status) VALUES(1002,9001,901,'client-2','Client fixture series','client fixture series','hnm3u8','01$https://fixture.invalid/one.mp4#02$https://fixture.invalid/two.mp4',1)",
		"INSERT INTO sx_vod(id,type_id,api_id,api_vid,name,name_norm,play_from,play_url,status,vip) VALUES(1003,9003,901,'client-3','Client locked movie','client locked movie','hnm3u8','feature$https://fixture.invalid/locked.mp4',1,1)",
	} {
		if err = execSQL(ctx, query); err != nil {
			t.Fatal("media catalogue fixture initialization failed")
		}
	}
	// A controlled completed MP4 checks the real cast delivery/cancellation
	// routes. Transcoding itself is exercised separately by the media engine.
	jobID, _ := appToken()
	if err = os.MkdirAll(castMediaDirectory(), 0700); err != nil {
		t.Fatal("cast delivery fixture directory unavailable")
	}
	jobBytes := bytes.Repeat([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p'}, 256)
	if err = os.WriteFile(castMP4Path(jobID), jobBytes, 0600); err != nil {
		t.Fatal("cast delivery fixture file unavailable")
	}
	t.Cleanup(func() { _ = os.Remove(castMP4Path(jobID)) })
	if err = execSQL(ctx, "INSERT INTO sx_app_cast_job(id,user_id,device_id,vod_id,status,progress,duration_ms,size,created,expires) VALUES(?,?,?,1001,'completed',100,60000,?,?,?)", jobID, mobile.User["id"], mobile.DeviceID, len(jobBytes), time.Now().Unix(), time.Now().Add(time.Minute).Unix()); err != nil {
		t.Fatal("cast delivery fixture job unavailable")
	}
	jobStatusPath := "/suxinvideo/app/v1/cast/prepare/" + jobID
	jobStatus := call("GET", jobStatusPath, mobile.AccessToken, nil, 200)
	jobURL := gconv.String(jobStatus["url"])
	if jobURL == "" || gconv.Int(jobStatus["progress"]) != 100 {
		t.Fatal("completed cast job did not expose its granted media URL")
	}
	call("GET", jobStatusPath, other.AccessToken, nil, 404)
	call("GET", jobStatusPath, unauthorizedDevice.AccessToken, nil, 404)
	wrongPrincipal, _ := AppUserForAccessToken(ctx, mobile.AccessToken)
	wrongGrant, _ := AppCreateMediaGrant(AppWithPrincipal(ctx, wrongPrincipal), 1002, time.Now().Add(time.Minute))
	call("GET", "/suxinvideo/app/v1/cast/file/"+jobID+"?app_grant="+url.QueryEscape(wrongGrant), "", nil, 403)
	jobRequest, _ := http.NewRequest("GET", jobURL, nil)
	jobRequest.Header.Set("Range", "bytes=8-15")
	jobResponse, e := client.Do(jobRequest)
	if e != nil {
		t.Fatal("cast granted media transport failed")
	}
	jobRange, _ := io.ReadAll(jobResponse.Body)
	jobResponse.Body.Close()
	if jobResponse.StatusCode != 206 || !bytes.Equal(jobRange, jobBytes[8:16]) {
		t.Fatal("cast byte range payload or status changed")
	}
	call("POST", jobStatusPath+"/cancel", mobile.AccessToken, nil, 200)
	jobResponse, e = client.Get(jobURL)
	if e != nil {
		t.Fatal("cast revocation transport failed")
	}
	jobResponse.Body.Close()
	if jobResponse.StatusCode != 404 {
		t.Fatal("cancelled completed job retained an active media grant")
	}
	t.Log("PASS: DLNA preparation status owner/device scope, wrong-film grant denial, exact MP4 byte range and completed-job revocation")
	var upstream *httptest.Server
	upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/suxinvideo/app/v1/playback/resolve" {
			p, e := AppUserForAccessToken(r.Context(), strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			if e != nil {
				w.WriteHeader(401)
				return
			}
			r = r.WithContext(AppWithPrincipal(r.Context(), p))
			var input AppPlaybackRequest
			_ = json.NewDecoder(r.Body).Decode(&input)
			film, e := appVisibleFilm(r.Context(), input.VodID)
			if e != nil {
				w.WriteHeader(404)
				return
			}
			if _, e = AppAuthorizeFilm(r.Context(), film); e != nil {
				w.WriteHeader(403)
				return
			}
			grant, e := AppCreateMediaGrant(r.Context(), input.VodID, time.Now().Add(time.Minute))
			if e != nil {
				w.WriteHeader(500)
				return
			}
			srcs, _ := hydratePlayers(r.Context(), film, playlist(film))
			src := srcs[0]
			if input.Episode < 0 || input.Episode >= len(src.Episodes) {
				w.WriteHeader(400)
				return
			}
			d := AppPlaybackDescriptor{VodID: input.VodID, Line: src.Code, VersionKey: src.VersionKey, EpisodeKey: episodeKey(src.Episodes[input.Episode]), Episode: input.Episode, Type: "hls", DurationMS: 60000, URL: "/suxinvideo/app/v1/media?app_grant=" + url.QueryEscape(grant)}
			_ = json.NewEncoder(w).Encode(row{"code": 0, "data": d})
			return
		}
		if r.URL.Path == "/suxinvideo/app/v1/media" || r.URL.Path == "/suxinvideo/native/fixture.ts" {
			p, e := appPrincipalByGrant(r.Context(), r.URL.Query().Get("app_grant"))
			if e != nil {
				w.WriteHeader(401)
				return
			}
			film, e := appVisibleFilm(AppWithPrincipal(r.Context(), *p), p.GrantVodID)
			if e != nil {
				w.WriteHeader(404)
				return
			}
			if _, e = AppAuthorizeFilm(AppWithPrincipal(r.Context(), *p), film); e != nil {
				w.WriteHeader(403)
				return
			}
			if r.URL.Path == "/suxinvideo/native/fixture.ts" {
				w.Header().Set("Content-Type", "video/mp2t")
				w.Write([]byte{0x47, 0, 0, 0, 0x47, 0, 0, 0})
				return
			}
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprintf(w, "#EXTM3U\n#EXTINF:60,\n%s/suxinvideo/native/fixture.ts?app_grant=%s\n#EXT-X-ENDLIST\n", upstream.URL, url.QueryEscape(r.URL.Query().Get("app_grant")))
			return
		}
		if r.URL.Path == "/suxinvideo/image/local" {
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("image fixture"))
			return
		}
		w.WriteHeader(404)
	}))
	defer upstream.Close()
	j := &jellyfinGateway{client: upstream.Client(), backend: upstream.URL}
	if err = prepareJellyfinServerID(ctx); err != nil {
		t.Fatal("Jellyfin server identity initialization failed")
	}
	lan := httptest.NewServer(j)
	defer lan.Close()
	lanClient := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	jcall := func(method, path, token string, input any, status int) row {
		t.Helper()
		data, _ := json.Marshal(input)
		r, _ := http.NewRequest(method, lan.URL+path, bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("X-Emby-Token", token)
		} else {
			r.Header.Set("X-Emby-Authorization", `MediaBrowser DeviceId="controlled-tv"`)
		}
		response, e := lanClient.Do(r)
		if e != nil {
			t.Fatal("Jellyfin transport failed")
		}
		defer response.Body.Close()
		if response.StatusCode != status {
			t.Fatalf("Jellyfin route %s returned %d, expected %d", strings.Split(path, "?")[0], response.StatusCode, status)
		}
		var value row
		if status == 200 {
			if json.NewDecoder(response.Body).Decode(&value) != nil {
				t.Fatal("Jellyfin returned invalid JSON")
			}
		}
		return value
	}
	public := jcall("GET", "/System/Info/Public", "", nil, 200)
	if gconv.String(public["Id"]) == "" {
		t.Fatal("server ID absent")
	}
	jcall("GET", "/Items", "", nil, 401)
	login := jcall("POST", "/Users/AuthenticateByName", "", row{"Username": "client-fixture@example.invalid", "Pw": password}, 200)
	jToken := gconv.String(login["AccessToken"])
	jUser := gconv.String(gconv.Map(login["User"])["Id"])
	if len(jToken) != 48 || jUser != jellyfinUserID(gconv.Int64(mobile.User["id"])) {
		t.Fatal("Jellyfin auth did not map CMS member")
	}
	assertGUID := func(raw string) {
		t.Helper()
		id, err := uuid.Parse(raw)
		if err != nil || id.Variant() != uuid.RFC4122 || id.Version() != 5 {
			t.Fatal("Jellyfin returned an ID rejected by a strict UUID client")
		}
	}
	assertGUID(jUser)
	if gconv.String(gconv.Map(login["SessionInfo"])["UserId"]) != jUser {
		t.Fatal("login session references a different/non-UUID member")
	}
	jcall("GET", "/Users/"+jUser, jToken, nil, 200)
	expiry, _ := one(ctx, "SELECT access_expire,refresh_expire FROM sx_app_session WHERE access_hash=?", appHash(jToken))
	if gconv.Int64(expiry["access_expire"])-time.Now().Unix() < 29*24*3600 {
		t.Fatal("Jellyfin token still uses native one-hour TTL")
	}
	views := jcall("GET", "/Users/"+jUser+"/Views", jToken, nil, 200)
	if gconv.Int(views["TotalRecordCount"]) < 1 {
		t.Fatal("Jellyfin views empty")
	}
	for _, view := range gconv.SliceAny(views["Items"]) {
		assertGUID(gconv.String(gconv.Map(view)["Id"]))
	}
	libraryID := gconv.String(gconv.Map(gconv.SliceAny(views["Items"])[0])["Id"])
	jcall("GET", "/Users/"+jUser+"/Items/"+libraryID, jToken, nil, 200)
	items := jcall("GET", "/Users/"+jUser+"/Items?ParentId="+libraryID+"&SearchTerm=Client%20fixture", jToken, nil, 200)
	if gconv.Int(items["TotalRecordCount"]) != 2 {
		t.Fatal("Jellyfin list/search omitted controlled catalogue")
	}
	var movieID, seriesID string
	for _, value := range gconv.SliceAny(items["Items"]) {
		item := gconv.Map(value)
		id := gconv.String(item["Id"])
		assertGUID(id)
		if gconv.String(item["Type"]) == "Movie" {
			movieID = id
		} else if gconv.String(item["Type"]) == "Series" {
			seriesID = id
		}
	}
	if movieID == "" || seriesID == "" {
		t.Fatal("strict client movie/series identity missing")
	}
	locked := jcall("GET", "/Items?SearchTerm=Client%20locked", jToken, nil, 200)
	lockedID := gconv.String(gconv.Map(gconv.SliceAny(locked["Items"])[0])["Id"])
	assertGUID(lockedID)
	hints := jcall("GET", "/Search/Hints?SearchTerm=Client%20fixture", jToken, nil, 200)
	if gconv.Int(hints["TotalRecordCount"]) != 2 {
		t.Fatal("search hints absent")
	}
	jcall("GET", "/Users/"+jellyfinUserID(999999)+"/Items", jToken, nil, 403)
	jcall("POST", "/Users/"+jUser+"/FavoriteItems/"+movieID, jToken, nil, 200)
	item := jcall("GET", "/Users/"+jUser+"/Items/"+movieID, jToken, nil, 200)
	if gconv.Map(item["UserData"])["IsFavorite"] != true {
		t.Fatal("favorite did not persist")
	}
	jcall("DELETE", "/Users/"+jUser+"/FavoriteItems/"+movieID, jToken, nil, 200)
	seasons := jcall("GET", "/Shows/"+seriesID+"/Seasons", jToken, nil, 200)
	if gconv.Int(seasons["TotalRecordCount"]) != 1 {
		t.Fatal("stable season missing")
	}
	seasonID := gconv.String(gconv.Map(gconv.SliceAny(seasons["Items"])[0])["Id"])
	assertGUID(seasonID)
	jcall("GET", "/Items/"+seasonID, jToken, nil, 200)
	jcall("GET", "/Items?ParentId="+seriesID, jToken, nil, 200)
	jcall("GET", "/Items?ParentId="+seasonID, jToken, nil, 200)
	episodes := jcall("GET", "/Shows/"+seriesID+"/Episodes?SeasonId="+seasonID, jToken, nil, 200)
	if gconv.Int(episodes["TotalRecordCount"]) != 2 {
		t.Fatal("stable episodes missing")
	}
	episodeID := gconv.String(gconv.Map(gconv.SliceAny(episodes["Items"])[0])["Id"])
	assertGUID(episodeID)
	for _, value := range gconv.SliceAny(episodes["Items"]) {
		episode := gconv.Map(value)
		assertGUID(gconv.String(episode["Id"]))
		assertGUID(gconv.String(episode["SeriesId"]))
		assertGUID(gconv.String(episode["SeasonId"]))
	}
	jcall("GET", "/Items/"+episodeID, jToken, nil, 200)
	jcall("POST", "/Items/"+episodeID+"/PlaybackInfo", jToken, nil, 200)
	jcall("GET", "/Items/"+strings.ReplaceAll(movieID, "-", ""), jToken, nil, 200)
	jcall("GET", "/Items/m1001", jToken, nil, 200) // Existing bookmarks remain readable.
	jcall("POST", "/Sessions/Playing/Progress", jToken, row{"ItemId": episodeID, "PositionTicks": int64(120000000)}, 204)
	history, _ := one(ctx, "SELECT position,episode_key,version_key FROM sx_play_record WHERE user_id=? AND vod_id=1002", mobile.User["id"])
	seriesFilm, _ := appVisibleFilm(ctx, 1002)
	seriesSources, _ := hydratePlayers(ctx, seriesFilm, playlist(seriesFilm))
	if len(seriesSources) == 0 || gconv.Int(history["position"]) != 12 || gconv.String(history["episode_key"]) != episodeKey(seriesSources[0].Episodes[0]) || gconv.String(history["version_key"]) != seriesSources[0].VersionKey {
		t.Fatal("history lost stable episode identity or position")
	}
	jcall("POST", "/Items/"+lockedID+"/PlaybackInfo", jToken, nil, 403)
	info := jcall("POST", "/Items/"+movieID+"/PlaybackInfo", jToken, nil, 200)
	mediaURL := gconv.String(gconv.Map(gconv.SliceAny(info["MediaSources"])[0])["DirectStreamUrl"])
	if !strings.HasPrefix(mediaURL, lan.URL+"/suxinvideo/") {
		t.Fatal("Jellyfin stream escaped LAN HTTP listener")
	}
	response, e := lanClient.Get(mediaURL)
	if e != nil {
		t.Fatal("signed LAN media transport failed")
	}
	manifest, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !bytes.Contains(manifest, []byte(lan.URL+"/suxinvideo/native/")) || bytes.Contains(manifest, []byte(upstream.URL)) {
		t.Fatal("LAN recursive playlist rewrite failed")
	}
	var segmentURL string
	for _, line := range strings.Split(string(manifest), "\n") {
		if strings.HasPrefix(line, "http://") {
			segmentURL = line
		}
	}
	response, e = lanClient.Get(segmentURL)
	if e != nil {
		t.Fatal("LAN media segment transport failed")
	}
	segment, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || len(segment) != 8 || segment[0] != 0x47 {
		t.Fatal("binary media segment changed")
	}
	t.Log("PASS: strict UUID login/library/movie/series/season/episode DTOs, returned-ID routing, compact/legacy IDs, favorites/progress and authorized recursive LAN media")
	jcall("POST", "/Sessions/Logout", jToken, nil, 204)
	jcall("GET", "/Items", jToken, nil, 401)
	response, e = lanClient.Get(mediaURL)
	if e != nil {
		t.Fatal("revoked media fixture transport failed")
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("logout retained a granted stream")
	}
	call("POST", "/suxinvideo/app/v1/devices/pair/revoke", mobile.AccessToken, row{"device_id": "fixture-tv"}, 200)
	call("GET", "/suxinvideo/app/v1/devices/cast/current", tv.AccessToken, nil, 401)
	if _, err = AppUserForAccessToken(ctx, tv.AccessToken); err == nil {
		t.Fatal("TV pairing revocation retained credential")
	}
	t.Log("PASS: Jellyfin logout and TV unpair revoke sessions and granted streams")

	// Prove the schema initializer did not alter untouched film/platform rows.
	keep, _ := one(ctx, "SELECT name FROM sx_vod WHERE id=800001")
	config, _ := one(ctx, "SELECT value FROM sx_config WHERE `key`='client_fixture_preservation'")
	if gconv.String(keep["name"]) != "preserved fixture" || gconv.String(config["value"]) != "keep" {
		t.Fatal("client initializer overwrote prior data")
	}
}
