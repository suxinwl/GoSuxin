package suxinvideo

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestLiveProviderCanonicalMapping(t *testing.T) {
	for _, tc := range []struct{ name, id string }{{"CCTV1综合", "CCTV1.cn"}, {"CCTV-5+ 体育赛事", "CCTV5plus.cn"}, {"凤凰中文台", "PhoenixChineseChannel.hk"}, {"翡翠台", "Jade.hk"}, {"黄金翡翠台", ""}} {
		if got := liveCanonicalProviderID("unknown.epg", tc.name); !strings.EqualFold(got, tc.id) {
			t.Errorf("canonical station %q: got %q", tc.name, got)
		}
	}
	if liveCanonicalProviderID("Jade.hk", "任意上游显示名") != "Jade.hk" {
		t.Fatal("canonical TVG ID lost")
	}
	for _, name := range []string{"CCTV-4 欧洲", "CCTV4 America", "CCTV4 中文国际（亚洲）"} {
		if liveCanonicalProviderID("unknown.epg", name) != "" {
			t.Errorf("regional broadcast merged with generic CCTV4: %s", name)
		}
	}
	if !strings.EqualFold(liveCanonicalProviderID("CCTV4Europe.cn", "CCTV4 欧洲"), "CCTV4Europe.cn") {
		t.Fatal("explicit canonical ID was overridden by a display name")
	}
	if liveProviderPublicLogo("http://127.0.0.1/private") != "" || liveProviderPublicLogo("http://user:password@public.example/logo") != "" {
		t.Fatal("private logo exposed")
	}
}
func TestLiveProviderProgrammeCapabilities(t *testing.T) {
	future := liveProgrammeFromRow(row{"id": 1, "start_at": time.Now().Unix() + 60, "end_at": time.Now().Unix() + 3600, "replay_kind": "catchup", "catchup_enabled": 1})
	if future.CanReplay {
		t.Fatal("future schedule was represented as replay")
	}
	past := liveProgrammeFromRow(row{"id": 2, "start_at": time.Now().Unix() - 3600, "end_at": time.Now().Unix() - 60, "replay_kind": "", "catchup_enabled": 0})
	if past.CanReplay {
		t.Fatal("EPG alone fabricated replay capability")
	}
	event := liveProgrammeFromRow(row{"id": 3, "replay_kind": "event_replay", "source_mode": "event_replay", "catchup_enabled": 0})
	if !event.CanReplay {
		t.Fatal("real independent event replay was treated as television catchup")
	}
	futureEvent := liveProgrammeFromRow(row{"id": 4, "end_at": time.Now().Unix() + 60, "replay_kind": "event_replay", "source_mode": "event_replay"})
	if futureEvent.CanReplay {
		t.Fatal("unfinished event was represented as replay")
	}
	parsed, e := liveXMLTime("20261003213000 +0800")
	if e != nil || parsed.In(liveShanghai).Hour() != 21 {
		t.Fatal("XMLTV timezone lost")
	}
}

func TestLiveProviderSecretsRemainPrivate(t *testing.T) {
	config := map[string]any{"nested": map[string]any{"cookie": "fixture-cookie", "quality": 3}, "entries": []any{map[string]any{"token": "fixture-token"}}}
	if !liveConfigContainsSecrets(config) {
		t.Fatal("nested credential bypasses public profile isolation")
	}
	masked, _ := json.Marshal(liveMaskConfig(config))
	if strings.Contains(string(masked), "fixture-cookie") || strings.Contains(string(masked), "fixture-token") {
		t.Fatal("nested credential exposed in status")
	}
}

func liveProviderControlledIntegration(t *testing.T, ctx context.Context) {
	t.Helper()
	now := time.Now().Unix()
	result, e := g.DB().Exec(ctx, "INSERT INTO sx_user(email,name,pwd,status,vip_expire) VALUES('live-provider-fixture@example.invalid','Provider fixture','fixture-only',1,?)", now+86400)
	if e != nil {
		t.Fatal(e)
	}
	member, _ := result.LastInsertId()
	id, _, e := liveUpsertImport(ctx, liveImportItem{Name: "Provider fixture channel", Group: "Provider fixtures", URL: "provider:controlled-reference", Headers: map[string]string{}}, 0)
	if e != nil {
		t.Fatal(e)
	}
	if e = execSQL(ctx, "UPDATE sx_live_stream SET source_kind='provider',provider_key='member',module_key='fixture',provider_ref='stable-fixture',url='',access_level='member',catchup_enabled=1,health='healthy',last_checked=? WHERE id=?", now, id); e != nil {
		t.Fatal(e)
	}
	if e = execSQL(ctx, "INSERT INTO sx_live_module(provider_key,module_key,name,enabled,config_json,secrets_cipher,created,updated) VALUES('member','fixture','Controlled module',1,'{}','',?,?)", now, now); e != nil {
		t.Fatal(e)
	}
	stream, e := liveStream(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	channel := stream.ChannelID
	if e = execSQL(ctx, "INSERT INTO sx_live_module(provider_key,module_key,name,enabled,config_json,secrets_cipher,created,updated) VALUES('public','fixture','Public module',1,'{}','',?,?)", now, now); e != nil {
		t.Fatal(e)
	}
	publicID, _, e := liveUpsertImport(ctx, liveImportItem{Name: "Public provider fixture", Group: "Provider fixtures", URL: "provider:public-controlled-reference", Headers: map[string]string{}}, 0)
	if e != nil {
		t.Fatal(e)
	}
	if e = execSQL(ctx, "UPDATE sx_live_stream SET source_kind='provider',provider_key='public',module_key='fixture',provider_ref='stable-public',url='',access_level='public' WHERE id=?", publicID); e != nil {
		t.Fatal(e)
	}
	public, e := liveStream(ctx, publicID)
	if e != nil {
		t.Fatal(e)
	}
	if e = liveStreamAccess(ctx, public, LiveViewer{}); e != nil {
		t.Fatal("guest public provider rejected", e)
	}
	viewer := LiveViewer{MemberID: member}
	if _, _, e = liveStreamAccessWhere(liveContextWithViewer(ctx, viewer)); e != nil {
		t.Fatal(e)
	}
	if e = liveStreamAccess(ctx, stream, LiveViewer{}); e == nil {
		t.Fatal("guest reached restricted source")
	}
	if e = liveStreamAccess(ctx, stream, viewer); e == nil {
		t.Fatal("VIP implied a module whitelist")
	}
	if e = execSQL(ctx, "INSERT INTO sx_live_member_access(member_id,module_key,enabled,created,updated) VALUES(?,'fixture',1,?,?)", member, now, now); e != nil {
		t.Fatal(e)
	}
	if e = liveStreamAccess(ctx, stream, viewer); e != nil {
		t.Fatal("explicit member whitelist rejected", e)
	}
	if e = execSQL(ctx, "UPDATE sx_live_stream SET source_mode='event_replay',catchup_enabled=0 WHERE id=?", id); e != nil {
		t.Fatal(e)
	}
	if e = liveUpsertEventProgramme(ctx, "member", liveProviderChannel{ModuleKey: "fixture", Ref: "stable-fixture", Name: "Controlled ended event", Mode: "event_replay"}, id); e != nil {
		t.Fatal(e)
	}
	stream, e = liveStream(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	programme, eventStream, e := liveReplayProgramme(liveContextWithViewer(ctx, viewer), stream.ProgrammeID, id)
	if e != nil || !programme.CanReplay || programme.Kind != "event_replay" || eventStream.SourceMode != "event_replay" {
		t.Fatal("actual event replay contract rejected", e)
	}
	guest, e := liveStreams(ctx, channel)
	if e != nil || len(guest) != 0 {
		t.Fatal("restricted stream appeared in guest directory")
	}
	allowed, e := liveStreams(liveContextWithViewer(ctx, viewer), channel)
	if e != nil || len(allowed) != 1 {
		t.Fatal("authorized stream missing from directory")
	}
	result, e = g.DB().Exec(ctx, "INSERT INTO sx_live_profile(name,modules_json,created,updated) VALUES('Controlled distribution','[\"member:fixture\"]',?,?)", now, now)
	if e != nil {
		t.Fatal(e)
	}
	profile, _ := result.LastInsertId()
	token, _ := appToken()
	if e = execSQL(ctx, "INSERT INTO sx_live_distribution(member_id,profile_id,name,token_hash,created,updated) VALUES(?,?,'Controlled subscription',?,?,?)", member, profile, appHash(token), now, now); e != nil {
		t.Fatal(e)
	}
	subscription, e := liveDistributionViewer(ctx, token)
	if e != nil {
		t.Fatal(e)
	}
	if e = liveStreamAccess(ctx, stream, subscription); e != nil {
		t.Fatal("subscription ACL differs from member ACL")
	}
	if e = execSQL(ctx, "UPDATE sx_live_distribution SET token_hash=? WHERE id=?", appHash("reset-controlled-token"), subscription.SubscriptionID); e != nil {
		t.Fatal(e)
	}
	if e = liveValidateViewer(ctx, subscription); e == nil {
		t.Fatal("token reset did not revoke existing media viewer")
	}
	if e = execSQL(ctx, "UPDATE sx_live_member_access SET enabled=0 WHERE member_id=? AND module_key='fixture'", member); e != nil {
		t.Fatal(e)
	}
	if e = liveStreamAccess(ctx, stream, viewer); e == nil {
		t.Fatal("whitelist revocation did not revoke streaming")
	}
	encrypted, e := liveSealModuleConfig(ctx, map[string]any{"token": "fixture-sensitive-value", "rateType": 3})
	if e != nil || strings.Contains(encrypted, "fixture-sensitive-value") {
		t.Fatal("module secret stored as plaintext")
	}
	decoded, e := liveUnsealModuleConfig(ctx, encrypted)
	if e != nil || decoded["token"] != "fixture-sensitive-value" {
		t.Fatal("encrypted config does not survive read")
	}
	masked, _ := json.Marshal(liveMaskConfig(decoded))
	if strings.Contains(string(masked), "fixture-sensitive-value") {
		t.Fatal("module status leaked secret")
	}
	p, e := one(ctx, "SELECT COUNT(*) n FROM sx_live_provider_binding")
	if e != nil || gconv.Int(p["n"]) < 0 {
		t.Fatal("binding schema missing")
	}
	t.Log("PASS: guest/VIP cannot access account source; explicit module whitelist, directory ACL, immediate whitelist/token reset revocation and encrypted masked configuration")
	liveProviderScheduleControlledIntegration(t, ctx, publicID)
	t.Run("MemberStartupAndLogin", func(t *testing.T) { liveProviderStartupControlledIntegration(t, ctx) })
}

type liveProviderFixtureTransport func(*http.Request) (*http.Response, error)

func (f liveProviderFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func liveProviderScheduleControlledIntegration(t *testing.T, ctx context.Context, streamID int64) {
	t.Helper()
	now := time.Now().Unix()
	if err := execSQL(ctx, "UPDATE sx_live_module SET last_sync=?,last_error=''", now); err != nil {
		t.Fatal(err)
	}
	if err := execSQL(ctx, "UPDATE sx_live_module SET last_sync=? WHERE provider_key='public' AND module_key='fixture'", now-int64((6*time.Hour).Seconds())-1); err != nil {
		t.Fatal(err)
	}
	due, err := liveDueProviders(ctx, now)
	if err != nil || len(due) != 1 || gconv.String(due[0]["provider_key"]) != "public" {
		t.Fatal("six-hour module timestamp did not trigger its instance", err)
	}
	if err = execSQL(ctx, "UPDATE sx_live_stream SET enabled=1,url_hash=?,epg_id='fixture.epg' WHERE id=?", liveIdentity("provider:"+liveIdentity("provider:public:fixture:stable-public")), streamID); err != nil {
		t.Fatal(err)
	}
	original := liveProviderTransport
	defer func() { liveProviderTransport = original }()
	failEPG := false
	catchup := true
	liveProviderTransport = liveProviderFixtureTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "127.0.0.1:9180" || request.Header.Get("X-IPTV-Secret") == "" {
			return nil, fmt.Errorf("invalid fixed provider request")
		}
		status := 200
		body := ""
		switch request.URL.Path {
		case "/internal/health":
			body = `{"ok":true,"ready":true,"refreshing":false}`
		case "/internal/catalog":
			data, _ := json.Marshal(liveProviderCatalog{OK: true, Profile: "public", Channels: []liveProviderChannel{{ModuleKey: "fixture", Ref: "stable-public", Name: "Public provider fixture", Group: "Provider fixtures", EpgID: "fixture.epg", Format: "hls", Catchup: catchup}}})
			body = string(data)
		case "/internal/epg":
			if failEPG {
				status = 502
				body = "unavailable"
			} else {
				body = fmt.Sprintf(`<tv><programme channel="fixture.epg" start="%s" stop="%s"><title>Actual fixture programme</title></programme></tv>`, time.Unix(now-1800, 0).UTC().Format(time.RFC3339), time.Unix(now+1800, 0).UTC().Format(time.RFC3339))
			}
		default:
			return nil, fmt.Errorf("automatic catalogue pull unexpectedly mutated configuration or probed media")
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})
	if err = liveProviderPull(ctx, &liveProgress{}, "public"); err != nil {
		t.Fatal("automatic provider pull failed", err)
	}
	stream, err := liveStream(ctx, streamID)
	if err != nil || !stream.Catchup {
		t.Fatal("catalogue updates did not reach Go", err)
	}
	programme, err := one(ctx, "SELECT id FROM sx_live_programme WHERE provider_key='public' AND module_key='fixture' AND title='Actual fixture programme'")
	if err != nil || programme == nil {
		t.Fatal("actual XMLTV programme did not reach Go", err)
	}
	module, err := one(ctx, "SELECT last_sync,last_error,status FROM sx_live_module WHERE provider_key='public' AND module_key='fixture'")
	if err != nil || gconv.Int64(module["last_sync"]) < now || gconv.String(module["last_error"]) != "" {
		t.Fatal("successful provider pull did not advance last_sync", err)
	}
	oldSuccess := now - int64((7 * time.Hour).Seconds())
	if err = execSQL(ctx, "UPDATE sx_live_module SET last_sync=? WHERE provider_key='public' AND module_key='fixture'", oldSuccess); err != nil {
		t.Fatal(err)
	}
	failEPG = true
	catchup = false
	if err = liveProviderPull(ctx, &liveProgress{}, "public"); err == nil {
		t.Fatal("EPG failure was marked as synchronized")
	}
	module, err = one(ctx, "SELECT last_sync,last_error FROM sx_live_module WHERE provider_key='public' AND module_key='fixture'")
	if err != nil || gconv.Int64(module["last_sync"]) != oldSuccess || gconv.String(module["last_error"]) == "" {
		t.Fatal("failed sync lost success timestamp or retry status", err)
	}
	retained, err := one(ctx, "SELECT id FROM sx_live_programme WHERE id=?", programme["id"])
	if err != nil || retained == nil {
		t.Fatal("failed XMLTV refresh removed saved programme", err)
	}
	stream, err = liveStream(ctx, streamID)
	if err != nil || stream.Catchup {
		t.Fatal("EPG failure discarded successful catalogue update", err)
	}
	due, err = liveDueProviders(ctx, time.Now().Unix())
	if err != nil || len(due) != 0 {
		t.Fatal("failed sync did not back off", err)
	}
	due, err = liveDueProviders(ctx, time.Now().Add(16*time.Minute).Unix())
	if err != nil || len(due) != 1 {
		t.Fatal("failed sync was never eligible for retry", err)
	}
	if err = execSQL(ctx, "UPDATE sx_live_module SET last_error='',updated=0 WHERE provider_key='public' AND module_key='fixture'"); err != nil {
		t.Fatal(err)
	}
	before, _ := one(ctx, "SELECT COUNT(*) n FROM sx_live_job")
	liveJobs.Lock()
	active := liveJobs.active
	liveJobs.active = 987654
	liveJobs.Unlock()
	started, startErr := liveScheduleProviderSync(ctx)
	liveJobs.Lock()
	liveJobs.active = active
	liveJobs.Unlock()
	after, _ := one(ctx, "SELECT COUNT(*) n FROM sx_live_job")
	if started || startErr == nil || gconv.Int(before["n"]) != gconv.Int(after["n"]) {
		t.Fatal("provider timer queued a duplicate live job")
	}
	t.Log("PASS: six-hour provider catalogue/XMLTV synchronization, same-job deduplication, safe read-only engine requests, retained successful data and failed-sync cooldown")
}
