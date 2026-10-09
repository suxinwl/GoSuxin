package suxinvideo

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Opt-in: inserts only a uniquely scoped health record, never changes real films.
func TestSourceHealthPersistence(t *testing.T) {
	if os.Getenv("SUXIN_INTEGRATION") != "1" {
		t.Skip("set SUXIN_INTEGRATION=1 to use the development database")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(filepath.Clean("../../..")); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	ctx := context.Background()
	if setting(ctx, "player_parse", "") != "" {
		t.Skip("global parser is enabled; direct source health is intentionally bypassed")
	}
	if err = prepareSourceHealth(ctx); err != nil {
		t.Fatal(err)
	}
	id := time.Now().UnixNano()
	src := source{Code: "health-test", Episodes: []episode{{Name: "1", URL: "https://example.com/1.m3u8"}}}
	fingerprint := sourceHealthFingerprint(src)
	defer execSQL(ctx, "DELETE FROM sx_vod_source_health WHERE vod_id=?", id)
	if err = execSQL(ctx, "INSERT INTO sx_vod_source_health(vod_id,fingerprint,source_code,fail_rounds,hidden_until) VALUES(?,?,?,2,?)", id, fingerprint, src.Code, time.Now().Add(30*time.Minute).Unix()); err != nil {
		t.Fatal(err)
	}
	// Repeated installation must retain the suppression record.
	if err = prepareSourceHealth(ctx); err != nil {
		t.Fatal(err)
	}
	other := source{Code: "other", Episodes: []episode{{Name: "1", URL: "https://example.org/1.m3u8"}}}
	if got := filterUnhealthySources(ctx, id, []source{src, other}); len(got) != 1 || got[0].Code != "other" {
		t.Fatalf("quarantine was not limited to the broken source: %+v", got)
	}
	if got := filterUnhealthySources(ctx, id-1, []source{src}); len(got) != 1 {
		t.Fatal("quarantine leaked into another film")
	}
	changed := source{Code: src.Code, Episodes: []episode{{Name: "1", URL: "https://example.com/new.m3u8"}}}
	if got := filterUnhealthySources(ctx, id, []source{changed}); len(got) != 1 {
		t.Fatal("updated media URL did not restore the source")
	}
	parsed := source{Code: src.Code, Parse: "https://example.org/?url={url}", Episodes: src.Episodes}
	if got := filterUnhealthySources(ctx, id, []source{parsed}); len(got) != 1 {
		t.Fatal("parser-based source was incorrectly suppressed")
	}
	if err = execSQL(ctx, "UPDATE sx_vod_source_health SET hidden_until=? WHERE vod_id=?", time.Now().Unix()-1, id); err != nil {
		t.Fatal(err)
	}
	if got := filterUnhealthySources(ctx, id, []source{src}); len(got) != 1 {
		t.Fatal("source did not reappear after quarantine expired")
	}
	// Native YQK uses the exact same persisted policy without touching its
	// stable playlist or hiding the same provider on an unrelated film.
	native := source{Code: "yqk_5", Episodes: []episode{{Name: "1", URL: "yqk://781/5/100"}}}
	nativeFingerprint := sourceHealthFingerprint(native)
	report, err := url.Parse(sourceHealthReportURL(ctx, id, native))
	if err != nil || report.Path != "/suxinvideo/source/report" || report.Query().Get("fingerprint") != nativeFingerprint || report.Query().Get("sig") == "" {
		t.Fatal("native YQK was not issued a signed source report URL")
	}
	if err = execSQL(ctx, "INSERT INTO sx_vod_source_health(vod_id,fingerprint,source_code,fail_rounds,hidden_until) VALUES(?,?,?,2,?)", id, nativeFingerprint, native.Code, time.Now().Add(30*time.Minute).Unix()); err != nil {
		t.Fatal(err)
	}
	if got := filterUnhealthySources(ctx, id, []source{native, other}); len(got) != 1 || got[0].Code != other.Code {
		t.Fatal("confirmed native source failure did not isolate this film's source")
	}
	if got := filterUnhealthySources(ctx, id-1, []source{native}); len(got) != 1 {
		t.Fatal("native quarantine leaked into another film")
	}
	nativeUpdated := source{Code: native.Code, Episodes: []episode{{Name: "1", URL: "yqk://781/5/101"}}}
	if got := filterUnhealthySources(ctx, id, []source{nativeUpdated}); len(got) != 1 {
		t.Fatal("updated native marker did not immediately restore source visibility")
	}
	if err = execSQL(ctx, "UPDATE sx_vod_source_health SET hidden_until=? WHERE vod_id=? AND fingerprint=?", time.Now().Unix()-1, id, nativeFingerprint); err != nil {
		t.Fatal(err)
	}
	if got := filterUnhealthySources(ctx, id, []source{native}); len(got) != 1 {
		t.Fatal("native source did not recover after temporary quarantine expired")
	}
}
