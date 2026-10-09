package suxinvideo

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

// This fixture requires the existing disposable-schema guard and never writes
// a user, SMTP setting or password to the configured development database.
func TestMemberEmailPolicyControlledIntegration(t *testing.T) {
	if os.Getenv("SUXIN_MEMBER_MAIL_INTEGRATION") != "1" {
		t.Skip("run plugins/suxinvideo/tools/test_member_mail_policy.py")
	}
	ctx := yqkIntegrationContext(t)
	database, err := one(ctx, "SELECT DATABASE() name")
	if err != nil || !regexp.MustCompile(`^suxin_member_mail_verify_[0-9]+$`).MatchString(gconv.String(database["name"])) {
		t.Fatal("registration fixture refuses a non-disposable database")
	}
	appSchemaMutex.Lock()
	appSchemaReady = false
	appSchemaMutex.Unlock()
	if err := EnsureAppSchema(ctx); err != nil {
		t.Fatal("fixture app schema initialization failed")
	}
	set := func(key, value string) {
		t.Helper()
		if err := execSQL(ctx, "INSERT INTO sx_config(`key`,value) VALUES(?,?) ON DUPLICATE KEY UPDATE value=VALUES(value)", key, value); err != nil {
			t.Fatal("isolated setting update failed")
		}
	}
	for key, value := range map[string]string{"member_enable": "1", "register_enable": "1", "points_register": "7", "smtp_host": "", "smtp_user": "", "smtp_pass": "", "smtp_port": "465", "captcha_provider": "graph"} {
		set(key, value)
	}
	base := mediaBinaryHTTPServer(t, func(group *ghttp.RouterGroup) {
		group.Group("/suxinvideo", func(public *ghttp.RouterGroup) {
			public.Bind(new(Member))
			public.GET("/app/v1/config", func(r *ghttp.Request) { appWrite(r, appConfig(r.Context()), nil) })
			public.POST("/app/v1/auth/register", func(r *ghttp.Request) { appWrite(r, nil, appRegister(r)) })
			public.POST("/app/v1/auth/send-code", func(r *ghttp.Request) { appWrite(r, nil, appSendCode(r)) })
			public.POST("/app/v1/auth/reset", func(r *ghttp.Request) { appWrite(r, nil, appResetPassword(r)) })
		})
	})
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	post := func(path string, input row, jsonBody bool, want int) {
		t.Helper()
		var body io.Reader
		contentType := "application/x-www-form-urlencoded"
		if jsonBody {
			payload, _ := json.Marshal(input)
			body = bytes.NewReader(payload)
			contentType = "application/json"
		} else {
			values := url.Values{}
			for key, value := range input {
				values.Set(key, gconv.String(value))
			}
			body = strings.NewReader(values.Encode())
		}
		request, _ := http.NewRequest(http.MethodPost, base+path, body)
		request.Header.Set("Content-Type", contentType)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal("isolated registration request failed")
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("%s status=%d want=%d", path, response.StatusCode, want)
		}
	}
	register := func(native bool, email, code string, validCaptcha bool, want int) {
		t.Helper()
		input := row{"email": email, "name": "Fixture", "password": "fixture-password", "email_code": code, "registration_requires_email_code": false, "email_verified": 1}
		path := "/suxinvideo/register"
		if native {
			path = "/suxinvideo/app/v1/auth/register"
			challenge, _ := appToken()
			if err := execSQL(ctx, "INSERT INTO sx_app_captcha(challenge,answer_hash,expire) VALUES(?,?,?)", challenge, appHash(challenge+":abcd"), time.Now().Add(time.Minute).Unix()); err != nil {
				t.Fatal("isolated captcha creation failed")
			}
			input["challenge_id"], input["captcha"] = challenge, "ABCD"
		} else {
			response, err := client.Get(base + "/suxinvideo/captcha")
			if err != nil {
				t.Fatal("web captcha request failed")
			}
			payload, _ := io.ReadAll(response.Body)
			response.Body.Close()
			match := regexp.MustCompile(`fill="#253759">([A-Z0-9]{4})</text>`).FindSubmatch(payload)
			if len(match) != 2 {
				t.Fatal("web captcha fixture image missing")
			}
			input["captcha"] = string(match[1])
		}
		if !validCaptcha {
			input["captcha"] = "wrong"
		}
		post(path, input, native, want)
	}
	assertUser := func(email string, verified int) {
		t.Helper()
		user, err := one(ctx, "SELECT email_verified,points FROM sx_user WHERE email=?", email)
		if err != nil || user == nil || gconv.Int(user["email_verified"]) != verified || gconv.Int(user["points"]) != 7 {
			t.Fatal("member verification or registration points inconsistent")
		}
		tokens, err := AppAuthenticatePassword(ctx, email, "fixture-password", "member-mail-fixture", "fixture native client", "192.0.2.10")
		if err != nil || tokens.AccessToken == "" || tokens.User == nil {
			t.Fatal("registered member could not log in")
		}
	}
	assertConfig := func(required, reset bool) {
		t.Helper()
		response, err := client.Get(base + "/suxinvideo/app/v1/config")
		if err != nil {
			t.Fatal("config request failed")
		}
		var payload struct{ Data row }
		err = json.NewDecoder(response.Body).Decode(&payload)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 || payload.Data["registration_requires_email_code"] != required || payload.Data["password_reset_enabled"] != reset {
			t.Fatal("config email-policy flags inconsistent")
		}
	}
	assertConfig(false, false)
	for _, native := range []bool{false, true} {
		mode, success := "web", 303
		if native {
			mode, success = "app", 200
		}
		email := mode + "-unverified@example.invalid"
		register(native, email, "", false, 400)
		register(native, email, "", true, success)
		assertUser(email, 0)
		duplicate := 400
		if native {
			duplicate = 409
		}
		register(native, email, "", true, duplicate)
	}
	post("/suxinvideo/app/v1/auth/send-code", row{}, true, 503)
	post("/suxinvideo/sendcode", row{}, false, 503)
	post("/suxinvideo/app/v1/auth/reset", row{}, true, 503)
	post("/suxinvideo/forgot", row{}, false, 503)
	set("register_enable", "0")
	register(false, "closed-web@example.invalid", "", true, 400)
	register(true, "closed-app@example.invalid", "", true, 403)
	set("register_enable", "1")
	set("smtp_host", "127.0.0.1")
	set("smtp_user", "sender@example.invalid")
	set("smtp_pass", "fixture")
	set("smtp_port", "1")
	assertConfig(true, true)
	for _, native := range []bool{false, true} {
		mode, success := "web", 303
		if native {
			mode, success = "app", 200
		}
		email := mode + "-verified@example.invalid"
		register(native, email, "", true, 400)
		register(native, email, "999999", true, 400)
		for _, item := range []struct {
			code    string
			expired bool
		}{{"111111", true}, {"222222", false}} {
			expires := time.Now().Add(time.Minute).Unix()
			if item.expired {
				expires = time.Now().Add(-time.Minute).Unix()
			}
			if err := execSQL(ctx, "INSERT INTO sx_email_code(email,code,type,expire,used,created) VALUES(?,?,'register',?,0,?)", email, item.code, expires, time.Now().Unix()); err != nil {
				t.Fatal("isolated email-code creation failed")
			}
		}
		register(native, email, "111111", true, 400)
		register(native, email, "222222", true, success)
		assertUser(email, 1)
		code, _ := one(ctx, "SELECT used FROM sx_email_code WHERE email=? AND code='222222'", email)
		if gconv.Int(code["used"]) != 1 {
			t.Fatal("registration did not atomically consume email code")
		}
		register(native, email, "222222", true, 400)
	}
	challenge, _ := appToken()
	if err := execSQL(ctx, "INSERT INTO sx_app_captcha(challenge,answer_hash,expire) VALUES(?,?,?)", challenge, appHash(challenge+":abcd"), time.Now().Add(time.Minute).Unix()); err != nil {
		t.Fatal("isolated send-code captcha creation failed")
	}
	post("/suxinvideo/app/v1/auth/send-code", row{"email": "delivery-failure@example.invalid", "challenge_id": challenge, "captcha": "abcd"}, true, 502)
	assertConfig(true, true)
	register(true, "delivery-failure@example.invalid", "", true, 400)
	set("member_enable", "0")
	assertConfig(true, false)
	register(true, "members-disabled@example.invalid", "222222", true, 403)
	t.Log("PASS: Web/native registration, mandatory graph captcha, unverified no-SMTP accounts, verified single-use codes, email uniqueness, closed registration, reset availability and no bypass after SMTP failure")
}
