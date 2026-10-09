package suxinvideo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

func liveStartupFixtureResponse(request *http.Request, value any) *http.Response {
	data, _ := json.Marshal(value)
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(data)), Request: request}
}

func liveStartupMockTransport(t *testing.T, handler liveProviderFixtureTransport) {
	t.Helper()
	original := liveProviderTransport
	liveProviderTransport = handler
	t.Cleanup(func() { liveProviderTransport = original })
}

func liveStartupIsolate(t *testing.T) {
	t.Helper()
	t.Setenv("SUXIN_IPTV_DISABLED", "1")
	t.Chdir(t.TempDir())
}

func TestLiveWaitProviderAcceptsEmptyCatalogueAfterConnectionRefusal(t *testing.T) {
	liveStartupIsolate(t)
	calls := 0
	liveStartupMockTransport(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "127.0.0.1:9181" || request.URL.Path != "/internal/health" || request.Method != http.MethodGet || request.Header.Get("X-IPTV-Secret") == "" {
			t.Fatal("health wait escaped the authenticated fixed member route")
		}
		calls++
		if calls == 1 {
			return nil, errors.New("controlled connection refused")
		}
		return liveStartupFixtureResponse(request, row{"ok": true, "profile": "member", "ready": false, "channel_count": 0}), nil
	})
	started := time.Now()
	if err := liveWaitProvider(context.Background(), "member", 2*time.Second); err != nil {
		t.Fatal("empty catalogue blocked account management readiness", err)
	}
	if calls != 2 || time.Since(started) < 200*time.Millisecond {
		t.Fatal("connection refusal did not use the bounded startup polling interval")
	}
}

func TestLiveWaitProviderDeadlineAndCancellation(t *testing.T) {
	liveStartupIsolate(t)
	t.Run("StartupDeadline", func(t *testing.T) {
		calls := 0
		liveStartupMockTransport(t, func(request *http.Request) (*http.Response, error) {
			calls++
			return nil, errors.New("controlled connection refused")
		})
		started := time.Now()
		err := liveWaitProvider(context.Background(), "member", 70*time.Millisecond)
		if err == nil || calls != 1 || time.Since(started) > time.Second {
			t.Fatal("startup timeout was not bounded")
		}
	})
	t.Run("CallerCancellationDuringRequest", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		liveStartupMockTransport(t, func(request *http.Request) (*http.Response, error) {
			cancel()
			<-request.Context().Done()
			return nil, request.Context().Err()
		})
		started := time.Now()
		err := liveWaitProvider(ctx, "member", 2*time.Second)
		if !errors.Is(err, context.Canceled) || time.Since(started) > time.Second {
			t.Fatal("caller cancellation did not stop the health wait promptly")
		}
	})
	t.Run("RequestCannotOutliveStartupDeadline", func(t *testing.T) {
		liveStartupMockTransport(t, func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, request.Context().Err()
		})
		started := time.Now()
		if err := liveWaitProvider(context.Background(), "member", 70*time.Millisecond); err == nil || time.Since(started) > time.Second {
			t.Fatal("in-flight health request ignored startup deadline")
		}
	})
}

func TestLiveWaitProviderRejectsWrongIdentity(t *testing.T) {
	liveStartupIsolate(t)
	for _, tc := range []struct {
		name string
		data row
	}{
		{"OtherProfile", row{"ok": true, "profile": "public", "ready": true}},
		{"MissingProfile", row{"ok": true, "ready": false}},
		{"UnsuccessfulHealth", row{"ok": false, "profile": "member"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			liveStartupMockTransport(t, func(request *http.Request) (*http.Response, error) {
				calls++
				return liveStartupFixtureResponse(request, tc.data), nil
			})
			if err := liveWaitProvider(context.Background(), "member", time.Second); err == nil || calls != 1 {
				t.Fatal("invalid engine identity was accepted or retried")
			}
		})
	}
	t.Run("InvalidProviderNeverMakesRequest", func(t *testing.T) {
		liveStartupMockTransport(t, func(*http.Request) (*http.Response, error) {
			t.Fatal("invalid profile reached the transport")
			return nil, errors.New("unexpected request")
		})
		var app *AppError
		if err := liveWaitProvider(context.Background(), "arbitrary", time.Second); !errors.As(err, &app) || app.Status != 400 {
			t.Fatal("invalid profile was not rejected")
		}
	})
}

func TestLiveProviderLoginSucceededUsesActionAndActualStatus(t *testing.T) {
	for _, tc := range []struct {
		name, action string
		data         map[string]any
		want         bool
	}{
		{"BilibiliPollOK", "poll", map[string]any{"status": "ok"}, true},
		{"CaseInsensitivePollOK", "poll", map[string]any{"status": "OK"}, true},
		{"StartIsNotAccountSuccess", "start", map[string]any{"status": "ok"}, false},
		{"BrowserStatusOKIsNotAccountSuccess", "browserStatus", map[string]any{"status": "ok"}, false},
		{"Pending", "poll", map[string]any{"status": "pending"}, false},
		{"Scanned", "poll", map[string]any{"status": "scanned"}, false},
		{"Expired", "poll", map[string]any{"status": "expired"}, false},
		{"AuthenticatedBrowser", "browserCheck", map[string]any{"authenticated": true}, true},
		{"BrowserImport", "browserImport", map[string]any{}, true},
		{"ExplicitSuccess", "browserCheck", map[string]any{"status": "success"}, true},
		{"ExplicitAuthenticated", "browserStatus", map[string]any{"status": "authenticated"}, true},
		{"ExplicitLoggedIn", "browserStatus", map[string]any{"status": "logged_in"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := liveProviderLoginSucceeded(tc.action, tc.data); got != tc.want {
				t.Fatal("login action/status completion contract changed")
			}
		})
	}
}

func TestLiveProviderStartupRecognizesSessdataAsSecret(t *testing.T) {
	config := map[string]any{"sessdata": "controlled-sensitive-session", "rooms": "13", "nested": map[string]any{"SESSDATA": "controlled-nested-session", "preferAvc": true}}
	if !liveConfigContainsSecrets(config) {
		t.Fatal("Bilibili account session bypassed public profile credential isolation")
	}
	masked, err := json.Marshal(liveMaskConfig(config))
	if err != nil || strings.Contains(string(masked), "controlled-sensitive-session") || strings.Contains(string(masked), "controlled-nested-session") {
		t.Fatal("Bilibili account session was exposed in administrator configuration")
	}
	cleaned := liveConfigWithoutSecrets(config)
	if cleaned["sessdata"] != nil || gconv.Map(cleaned["nested"])["SESSDATA"] != nil || cleaned["rooms"] != "13" || !gconv.Bool(gconv.Map(cleaned["nested"])["preferAvc"]) {
		t.Fatal("newly captured Bilibili login could be overwritten by a stale stored account session")
	}
}

// Called only by the existing disposable-MySQL backend gate. No real Node
// process, production account directory, or provider network endpoint is used.
func liveProviderStartupControlledIntegration(t *testing.T, ctx context.Context) {
	t.Helper()
	liveStartupIsolate(t)
	const qrModule, browserModule, tokenModule = "startup-qr", "startup-browser", "startup-token"
	const oldSession = "controlled-old-sessdata"
	config := map[string]any{"sessdata": oldSession, "rooms": "13", "preferAvc": true}
	sealed, err := liveSealModuleConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	if err = execSQL(ctx, "INSERT INTO sx_live_module(provider_key,module_key,name,enabled,config_json,secrets_cipher,source_revision,created,updated) VALUES('member',?,'Startup QR fixture',0,'{}',?,8,?,?)", qrModule, sealed, time.Now().Unix(), time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	memberReady := false
	healthCalls, loginCalls, configCalls := 0, 0, 0
	lastConfig := row{}
	liveStartupMockTransport(t, func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("X-IPTV-Secret") == "" {
			return nil, errors.New("fixture request missing internal authentication")
		}
		if request.URL.Host == "127.0.0.1:9180" && request.URL.Path == "/internal/modules" && request.Method == http.MethodGet {
			return liveStartupFixtureResponse(request, row{"ok": true, "profile": "public", "modules": []row{
				{"id": qrModule, "login": true, "browser_login": false},
				{"id": browserModule, "login": false, "browser_login": true},
				{"id": tokenModule, "login": false, "browser_login": false},
			}}), nil
		}
		if request.URL.Host != "127.0.0.1:9181" {
			return nil, errors.New("fixture request escaped fixed account origin")
		}
		switch request.URL.Path {
		case "/internal/health":
			healthCalls++
			if healthCalls == 1 {
				return nil, errors.New("controlled initial connection refused")
			}
			memberReady = true
			return liveStartupFixtureResponse(request, row{"ok": true, "profile": "member", "ready": false, "channel_count": 0}), nil
		case "/internal/modules/config":
			if !memberReady {
				return nil, errors.New("controlled engine not listening yet")
			}
			if json.NewDecoder(request.Body).Decode(&lastConfig) != nil {
				return nil, errors.New("invalid controlled configuration request")
			}
			configCalls++
			return liveStartupFixtureResponse(request, row{"ok": true}), nil
		case "/internal/modules/login":
			if !memberReady || configCalls == 0 {
				return nil, errors.New("login was sent before account engine configuration")
			}
			var input row
			if json.NewDecoder(request.Body).Decode(&input) != nil {
				return nil, errors.New("invalid controlled login request")
			}
			loginCalls++
			switch gconv.String(input["action"]) {
			case "start":
				return liveStartupFixtureResponse(request, row{"ok": true, "data": row{"status": "ok", "key": "controlled-qr-key", "image": "data:image/png;base64,AA=="}}), nil
			case "poll":
				if gconv.String(input["key"]) != "controlled-qr-key" {
					return nil, errors.New("QR login flow identity was lost")
				}
				return liveStartupFixtureResponse(request, row{"ok": true, "data": row{"status": "ok", "message": "登录成功，凭据已保存"}}), nil
			case "browserImport":
				if gconv.String(input["payload"]) != "controlled-browser-import" {
					return nil, errors.New("browser import payload was lost")
				}
				return liveStartupFixtureResponse(request, row{"ok": true, "data": row{"authenticated": true}}), nil
			default:
				return nil, errors.New("unexpected fixture login action")
			}
		default:
			return nil, errors.New("startup fixture unexpectedly fetched catalogue or media")
		}
	})
	assertAppStatus := func(err error, status int) {
		t.Helper()
		var app *AppError
		if !errors.As(err, &app) || app.Status != status {
			t.Fatalf("expected controlled login error status %d", status)
		}
	}
	for _, tc := range []struct{ module, action string }{{qrModule, "browserStart"}, {browserModule, "start"}, {tokenModule, "browserImport"}} {
		_, err = liveProviderLogin(ctx, tc.module, tc.action, "", nil, true)
		assertAppStatus(err, 400)
	}
	for _, tc := range []struct{ module, action string }{{qrModule, "poll"}, {browserModule, "browserCheck"}} {
		_, err = liveProviderLogin(ctx, tc.module, tc.action, "", nil, true)
		assertAppStatus(err, 409)
	}
	_, err = liveProviderLogin(ctx, qrModule, "start", "", nil, false)
	assertAppStatus(err, 403)
	setting, err := one(ctx, "SELECT enabled FROM sx_live_module WHERE provider_key='member' AND module_key=?", qrModule)
	if err != nil || gconv.Bool(setting["enabled"]) || healthCalls != 0 || loginCalls != 0 {
		t.Fatal("unsupported or non-start login enabled an account module")
	}
	role, err := one(ctx, "SELECT rules,btns FROM gf_auth_role WHERE id=2")
	if err != nil {
		t.Fatal(err)
	}
	permission, err := one(ctx, "SELECT id FROM gf_auth_rule WHERE path='/admin/suxinvideo/live/provider/login'")
	if err != nil || permission == nil {
		t.Fatal("login action permission is missing")
	}
	t.Cleanup(func() {
		_ = execSQL(ctx, "UPDATE gf_auth_role SET rules=?,btns=? WHERE id=2", role["rules"], role["btns"])
	})
	if err = execSQL(ctx, "UPDATE gf_auth_role SET rules=?,btns=? WHERE id=2", fmt.Sprintf("%s,%v", role["rules"], permission["id"]), fmt.Sprintf("%s,%v", role["btns"], permission["id"])); err != nil {
		t.Fatal(err)
	}
	base := mediaBinaryHTTPServer(t, func(group *ghttp.RouterGroup) {
		group.Group("/admin/suxinvideo", func(admin *ghttp.RouterGroup) {
			admin.Middleware(func(r *ghttp.Request) {
				r.SetCtx(context.WithValue(r.Context(), "uid", int64(201)))
				r.Middleware.Next()
			}, cmsAudit)
			RegisterLiveProviderAdminRoutes(admin)
			admin.Bind(new(Admin))
		})
	})
	input, _ := json.Marshal(row{"module_key": qrModule, "action": "start"})
	request, _ := http.NewRequest(http.MethodPost, base+"/admin/suxinvideo/live/provider/login", bytes.NewReader(input))
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		t.Fatal("controlled permission request failed", err)
	}
	var envelope row
	err = json.NewDecoder(response.Body).Decode(&envelope)
	response.Body.Close()
	if err != nil || gconv.Int(envelope["code"]) == 0 || !strings.Contains(gconv.String(envelope["message"]), "配置权限") || healthCalls != 0 || loginCalls != 0 {
		t.Fatal("login-only administrator enabled a module without provider/save permission")
	}
	out, err := liveProviderLogin(ctx, qrModule, "start", "", nil, true)
	if err != nil || gconv.String(gconv.Map(out["data"])["key"]) != "controlled-qr-key" || healthCalls != 2 || loginCalls != 1 {
		t.Fatal("first account login did not survive deferred engine startup", err)
	}
	setting, err = one(ctx, "SELECT enabled,secrets_cipher,source_revision FROM sx_live_module WHERE provider_key='member' AND module_key=?", qrModule)
	retained, decodeErr := liveUnsealModuleConfig(ctx, gconv.String(setting["secrets_cipher"]))
	posted := gconv.Map(lastConfig["config"])
	if err != nil || decodeErr != nil || !gconv.Bool(setting["enabled"]) || retained["sessdata"] != oldSession || retained["rooms"] != "13" || posted["sessdata"] != oldSession || !gconv.Bool(lastConfig["enabled"]) {
		t.Fatal("initial enable/configuration discarded existing encrypted account settings")
	}
	startRevision := gconv.Int64(setting["source_revision"])
	streamID, _, err := liveUpsertImport(ctx, liveImportItem{Name: "Startup account fixture", Group: "Provider fixtures", URL: "provider:startup-account-reference", Headers: map[string]string{}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = execSQL(ctx, "UPDATE sx_live_stream SET source_kind='provider',provider_key='member',module_key=?,provider_ref='controlled-startup-ref',access_level='member',source_revision=?,url='',health='healthy' WHERE id=?", qrModule, startRevision, streamID); err != nil {
		t.Fatal(err)
	}
	stream, err := liveStream(ctx, streamID)
	member, memberErr := one(ctx, "SELECT id FROM sx_user WHERE email='live-provider-fixture@example.invalid'")
	if err != nil || memberErr != nil || member == nil {
		t.Fatal("startup media identity fixture missing")
	}
	if err = execSQL(ctx, "INSERT INTO sx_live_member_access(member_id,module_key,enabled,created,updated) VALUES(?,?,1,?,?)", member["id"], qrModule, time.Now().Unix(), time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	viewer := LiveViewer{MemberID: gconv.Int64(member["id"])}
	item := &liveMediaSession{ChannelID: stream.ChannelID, StreamID: stream.ID, Viewer: viewer, RuntimeStream: stream, SourceRevision: startRevision}
	if _, err = checkLiveSession(ctx, item); err != nil {
		t.Fatal("controlled authorized media session was not valid before login", err)
	}
	if _, err = liveProviderLogin(ctx, qrModule, "poll", "controlled-qr-key", nil, false); err != nil {
		t.Fatal("existing account QR poll failed", err)
	}
	setting, err = one(ctx, "SELECT secrets_cipher,source_revision FROM sx_live_module WHERE provider_key='member' AND module_key=?", qrModule)
	updated, decodeErr := liveUnsealModuleConfig(ctx, gconv.String(setting["secrets_cipher"]))
	streamAfter, streamErr := liveStream(ctx, streamID)
	if err != nil || decodeErr != nil || streamErr != nil || gconv.Int64(setting["source_revision"]) != startRevision+1 || streamAfter.SourceRevision != startRevision+1 || updated["sessdata"] != nil || updated["rooms"] != "13" {
		t.Fatal("Bilibili poll status=ok did not revoke old account revision and stale form credentials")
	}
	_, err = checkLiveSession(ctx, item)
	assertAppStatus(err, 403)
	if _, err = liveProviderLogin(ctx, browserModule, "browserImport", "", "controlled-browser-import", true); err != nil {
		t.Fatal("supported browser import did not bootstrap its member module", err)
	}
	browser, err := one(ctx, "SELECT enabled FROM sx_live_module WHERE provider_key='member' AND module_key=?", browserModule)
	token, tokenErr := one(ctx, "SELECT enabled FROM sx_live_module WHERE provider_key='member' AND module_key=?", tokenModule)
	if err != nil || tokenErr != nil || !gconv.Bool(browser["enabled"]) || (token != nil && gconv.Bool(token["enabled"])) {
		t.Fatal("login capability did not control account module activation")
	}
	t.Log("PASS: deferred member startup with empty catalogue; login-only administrator denied activation; capability/non-start guards; preserved encrypted settings; Bilibili QR completion revokes old media revision; browser import bootstraps only its module")
}
