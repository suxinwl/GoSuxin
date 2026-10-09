package suxinvideo

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"golang.org/x/crypto/bcrypt"
)

func TestAppGenrePreference(t *testing.T) {
	for _, item := range []struct {
		short, anime bool
		want         string
	}{{true, false, "hongguo"}, {false, true, "ecy_1"}, {false, false, "yqk_1"}} {
		codes := []string{"hnm3u8", "yqk_4", "yqk_1", "hongguo", "ecy_1"}
		best, rank := "", 100
		for _, code := range codes {
			if r := appProviderRank(code, item.short, item.anime); r < rank {
				best, rank = code, r
			}
		}
		if best != item.want {
			t.Fatalf("genre preference got %s want %s", best, item.want)
		}
	}
}

func TestAppEpisodeSelectionKeepsVersionAndManualChoice(t *testing.T) {
	views := []row{
		{"code": "yqk_1", "version_key": "season1", "episodes": []row{{"key": "episode:1"}}},
		{"code": "ecy_1", "version_key": "season2", "episodes": []row{{"key": "episode:2"}}},
		{"code": "hnm3u8", "version_key": "season1", "episodes": []row{{"key": "episode:1"}, {"key": "episode:2"}}},
	}
	si, ei, err := appFindEpisode(views, "yqk_1", &AppPlaybackRequest{EpisodeKey: "episode:2", VersionKey: "season1"})
	if err != nil || si != 2 || ei != 1 {
		t.Fatal("preferred missing episode did not select same-version alternate")
	}
	if _, _, err = appFindEpisode(views, "yqk_1", &AppPlaybackRequest{EpisodeKey: "episode:2", VersionKey: "season1", Manual: true}); err == nil {
		t.Fatal("manual choice silently changed lines")
	}
	if _, _, err = appFindEpisode(views, "ecy_1", &AppPlaybackRequest{EpisodeKey: "episode:2", VersionKey: "season3"}); err == nil {
		t.Fatal("episode crossed an unmatched season")
	}
}

func TestAppOfflineEntitlementExpiryAndGrantScope(t *testing.T) {
	now := time.Unix(1700000000, 0)
	if got := appOfflineExpiry(now, row{"vip": 0}, row{}); got != now.Add(7*24*time.Hour).Unix() {
		t.Fatal("ordinary offline license is not seven days")
	}
	end := now.Add(12 * time.Hour).Unix()
	if got := appOfflineExpiry(now, row{"vip": 1}, row{"vip_expire": end}); got != end {
		t.Fatal("offline license outlives membership")
	}
	ctx := AppWithPrincipal(context.Background(), AppPrincipal{SessionID: 1, User: row{"id": 3}, GrantVodID: 7})
	if AppAuthorizeGrantVod(ctx, 7) != nil {
		t.Fatal("authorized film rejected")
	}
	if AppAuthorizeGrantVod(ctx, 8) == nil {
		t.Fatal("media grant unlocked another film")
	}
	if _, err := appRequirePrincipal(ctx); err == nil {
		t.Fatal("film-scoped media grant became a full account token")
	}
}

func TestAppMediaURLPreservesStableDownloadIdentity(t *testing.T) {
	u, err := url.Parse(AppAppendMediaAuthorization("/suxinvideo/native/hls?token=session-one&segment=17", "grant-one", "stable-content"))
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("app_grant") != "grant-one" || u.Query().Get("download_key") != "stable-content" || u.Query().Get("segment") != "17" {
		t.Fatal("media identity or authorization was dropped")
	}
	a, err := appToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := appToken()
	if err != nil {
		t.Fatal(err)
	}
	if a == b || len(a) != 48 || len(appHash(a)) != 64 || strings.Contains(appHash(a), a) {
		t.Fatal("device tokens lack independent hashed identities")
	}
}

func TestAppDeviceAuthAndCaptchaIntegration(t *testing.T) {
	ctx := hongguoAuditFixtureContext(t)
	appSchemaMutex.Lock()
	appSchemaReady = false
	appSchemaMutex.Unlock()
	if err := EnsureAppSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if err := EnsureAppSchema(ctx); err != nil {
		t.Fatal("schema upgrade is not idempotent", err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("native-fixture-password"), bcrypt.MinCost)
	if err := execSQL(ctx, "DELETE FROM sx_user WHERE email='app-fixture@example.invalid'"); err != nil {
		t.Fatal(err)
	}
	if err := execSQL(ctx, "INSERT INTO sx_user(email,name,pwd,status,points) VALUES('app-fixture@example.invalid','Native test',?,1,100)", string(hash)); err != nil {
		t.Fatal(err)
	}
	user, err := one(ctx, "SELECT id FROM sx_user WHERE email='app-fixture@example.invalid'")
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := AppAuthenticatePassword(ctx, "app-fixture@example.invalid", "native-fixture-password", "fixture-android", "fixture mobile", "192.0.2.11")
	if err != nil {
		t.Fatal(err)
	}
	p, err := AppUserForAccessToken(ctx, tokens.AccessToken)
	if err != nil || p.DeviceID != "fixture-android" || gconv.Int64(p.User["id"]) != gconv.Int64(user["id"]) {
		t.Fatal("persistent app session is not recognized", err)
	}
	pctx := AppWithPrincipal(ctx, p)
	grant, err := AppCreateMediaGrant(pctx, 1001, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	gp, err := appPrincipalByGrant(ctx, grant)
	if err != nil || gp.GrantVodID != 1001 {
		t.Fatal("media grant lost film/device scope", err)
	}
	if _, err = appPrincipalByGrant(ctx, grant+"x"); err == nil {
		t.Fatal("tampered media grant accepted")
	}
	if _, err = appRefresh(ctx, tokens.RefreshToken, "different-device"); err == nil {
		t.Fatal("refresh credential transferred to another device")
	}
	next, err := appRefresh(ctx, tokens.RefreshToken, "fixture-android")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = appRefresh(ctx, tokens.RefreshToken, "fixture-android"); err == nil {
		t.Fatal("rotated refresh token was replayable")
	}
	if _, err = AppUserForAccessToken(ctx, tokens.AccessToken); err == nil {
		t.Fatal("rotating credentials retained the old access token")
	}
	if _, err = AppUserForAccessToken(ctx, next.AccessToken); err != nil {
		t.Fatal(err)
	}
	if err = execSQL(ctx, "UPDATE sx_app_session SET revoked=1 WHERE id=?", p.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err = appPrincipalByGrant(ctx, grant); err == nil {
		t.Fatal("logout did not revoke streaming media")
	}
	challenge, _ := appToken()
	if err = execSQL(ctx, "INSERT INTO sx_app_captcha(challenge,answer_hash,expire) VALUES(?,?,?)", challenge, appHash(challenge+":abcd"), time.Now().Add(time.Minute).Unix()); err != nil {
		t.Fatal(err)
	}
	if err = appConsumeCaptcha(ctx, challenge, "wrong"); err == nil {
		t.Fatal("wrong captcha accepted")
	}
	if err = appConsumeCaptcha(ctx, challenge, "ABCD"); err == nil {
		t.Fatal("incorrect attempt did not consume challenge")
	}
	challenge, _ = appToken()
	_ = execSQL(ctx, "INSERT INTO sx_app_captcha(challenge,answer_hash,expire) VALUES(?,?,?)", challenge, appHash(challenge+":abcd"), time.Now().Add(time.Minute).Unix())
	if err = appConsumeCaptcha(ctx, challenge, "ABCD"); err != nil {
		t.Fatal("captcha comparison should be case-insensitive", err)
	}
	var known *AppError
	if err = appConsumeCaptcha(ctx, challenge, "ABCD"); !errors.As(err, &known) || known.Status != 400 {
		t.Fatal("captcha replay did not produce actionable error")
	}
}

func TestAppContentEntitlementsAndAtomicUnlockIntegration(t *testing.T) {
	ctx := hongguoAuditFixtureContext(t)
	appSchemaMutex.Lock()
	appSchemaReady = false
	appSchemaMutex.Unlock()
	if err := EnsureAppSchema(ctx); err != nil {
		t.Fatal(err)
	}
	hongguoAuditFixtureInsert(t, ctx, 1001, 9001, 901, "fixture-entitlement", "原生权限测试短剧", "2026", "中国大陆", "hnm3u8", "第1集$https://fixture.invalid/one.m3u8", 1)
	_ = execSQL(ctx, "UPDATE sx_vod SET vip=0,points=12 WHERE id=1001")
	film, err := appVisibleFilm(ctx, 1001)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = AppAuthorizeFilm(ctx, film); err == nil {
		t.Fatal("guest opened points-gated film")
	}
	_ = execSQL(ctx, "DELETE FROM sx_user WHERE email='app-entitlement@example.invalid'")
	_ = execSQL(ctx, "INSERT INTO sx_user(email,name,pwd,status,points) VALUES('app-entitlement@example.invalid','Entitlement','unused',1,100)")
	user, _ := one(ctx, "SELECT id,email,name,points,vip_expire FROM sx_user WHERE email='app-entitlement@example.invalid'")
	pctx := AppWithPrincipal(ctx, AppPrincipal{User: user, SessionID: 1, DeviceID: "entitlement-fixture"})
	if _, err = AppAuthorizeFilm(pctx, film); err == nil {
		t.Fatal("member bypassed points purchase")
	}
	if err = appUnlock(pctx, 1001); err != nil {
		t.Fatal(err)
	}
	if err = appUnlock(pctx, 1001); err != nil {
		t.Fatal("repeat unlock should succeed without payment", err)
	}
	updated, _ := one(ctx, "SELECT points FROM sx_user WHERE id=?", user["id"])
	if gconv.Int(updated["points"]) != 88 {
		t.Fatal("repeat unlock charged points twice")
	}
	if _, err = AppAuthorizeFilm(pctx, film); err != nil {
		t.Fatal("purchased film is still locked", err)
	}
	film["vip"] = 1
	if _, err = AppAuthorizeFilm(pctx, film); err == nil {
		t.Fatal("points purchase bypassed VIP gate")
	}
	film["status"] = 0
	if _, err = AppAuthorizeFilm(pctx, film); err == nil {
		t.Fatal("unlisted content remained playable")
	}
}
