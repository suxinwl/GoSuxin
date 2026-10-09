package suxinvideo

import (
	"encoding/json"
	"strings"
	"testing"

	xq "github.com/suxinwl/GoSuxin/internal/xiaoqiapp"
)

func hongguoSeasonRestoreFixture() ([]hongguoSeasonRepairEvidence, map[string]hongguoSeasonRemoteEvidence) {
	repairs := []hongguoSeasonRepairEvidence{
		{ID: 1311, APIID: 9, APIVID: "100", RemoteTitle: "衍天图录第五季", Verified: true},
		{ID: 1361, APIID: 9, APIVID: "200", RemoteTitle: "吞天剑碑", Verified: true},
		{ID: 1496, APIID: 9, APIVID: "300", RemoteTitle: "灯渡星河第二季", Verified: true},
		{ID: 1541, APIID: 9, APIVID: "400", RemoteTitle: "随母改嫁：我带全家上青云第一季", Verified: true},
		{ID: 6487, APIID: 9, APIVID: "500", RemoteTitle: "纹海潮生第二季", Verified: true},
	}
	remotes := map[string]hongguoSeasonRemoteEvidence{}
	names := []struct {
		parent    int
		id, title string
	}{{0, "101", "衍天图录第一季"}, {0, "102", "衍天图录第二季"}, {0, "103", "衍天图录第三季"}, {0, "104", "衍天图录第四季"},
		{1, "201", "吞天剑碑第二季"}, {2, "301", "灯渡星河"}, {3, "401", "随母改嫁：我带全家上青云"}, {4, "501", "纹海潮生第九季"}, {4, "502", "纹海潮生第八季"},
		{1, "202", "吞天剑碑"}}
	for _, item := range names {
		repairs[item.parent].Proofs = append(repairs[item.parent].Proofs, hongguoSeasonRepairProof{Series: item.id, Title: item.title, Episodes: 1, Error: json.RawMessage("false")})
		var remote hongguoSeasonRemoteEvidence
		remote.Request.Series = item.id
		remote.Drama = xq.Drama{Source: "hongguo", SourceID: item.id, Title: item.title, Category: "玄幻", Cover: "https://cover.example/private?token=secret"}
		remote.Chapters = []xq.Chapter{{Source: "hongguo", Title: "第1集", VideoURL: "hongguo-cenc://11"}}
		remotes[item.id] = remote
	}
	return repairs, remotes
}

func TestHongguoSeasonRestoreLimitsScopeAndPreservesFullSeasonTitles(t *testing.T) {
	repairs, remotes := hongguoSeasonRestoreFixture()
	plans, err := buildHongguoSeasonRestorePlans(repairs, remotes)
	if err != nil || len(plans) != 9 {
		t.Fatalf("verified nine-season restore plan failed: count=%d error=%v", len(plans), err)
	}
	for _, plan := range plans {
		if plan.Entry.Series == "202" || plan.Item["type_name"] != "短剧" || plan.Item["vod_class"] != "玄幻" || plan.Item["vod_year"] != "" || plan.Item["vod_area"] != "" || plan.Item["vod_name"] != plan.Drama.DisplayTitle() {
			t.Fatal("same-title extra native ID selected, season title erased or metadata fabricated")
		}
		if plan.Item["vod_play_url"] != "第1集$hongguo://"+plan.Entry.Series+"/11" || len(plan.Entry.ProofVodIDs) != 1 {
			t.Fatal("restoration lost its native series ownership or reviewed donor evidence")
		}
		encoded, err := json.Marshal(plan.Entry)
		if err != nil || strings.Contains(string(encoded), "https://") || strings.Contains(string(encoded), "hongguo-cenc://") || strings.Contains(string(encoded), "token=") {
			t.Fatal("safe report contains private cover/media addresses")
		}
	}
}

func TestHongguoSeasonRestoreRejectsUnprovenOrMixedChapters(t *testing.T) {
	for _, mode := range []string{"unknown-donor", "unverified-original", "failed-proof", "different-title", "missing-detail", "different-count", "foreign-chapter", "zero-video", "invalid-label", "duplicate-label", "incomplete-scope"} {
		t.Run(mode, func(t *testing.T) {
			repairs, remotes := hongguoSeasonRestoreFixture()
			remote := remotes["101"]
			switch mode {
			case "unknown-donor":
				repairs[0].ID = 99
			case "unverified-original":
				repairs[0].Verified = false
			case "failed-proof":
				repairs[0].Proofs[0].Error = json.RawMessage("true")
			case "different-title":
				remote.Drama.Title = "衍天图录第十季"
			case "missing-detail":
				delete(remotes, "101")
			case "different-count":
				repairs[0].Proofs[0].Episodes = 2
			case "foreign-chapter":
				remote.Chapters[0].VideoURL = "https://cdn.example/private.m3u8?token=secret"
			case "zero-video":
				remote.Chapters[0].VideoURL = "hongguo-cenc://0"
			case "invalid-label":
				remote.Chapters[0].Title = "第1集#另一部剧"
			case "duplicate-label":
				remote.Chapters = append(remote.Chapters, remote.Chapters[0])
				repairs[0].Proofs[0].Episodes = 2
			case "incomplete-scope":
				repairs[0].Proofs = repairs[0].Proofs[1:]
			}
			if mode != "missing-detail" {
				remotes["101"] = remote
			}
			plans, err := buildHongguoSeasonRestorePlans(repairs, remotes)
			if plans != nil || err == nil || strings.Contains(err.Error(), "https://") || strings.Contains(err.Error(), "token=") {
				t.Fatalf("unverified restore accepted or error exposed media address: %s", mode)
			}
		})
	}
}

func TestHongguoSeasonExistingCheckPreservesOtherSeasonIdentity(t *testing.T) {
	films := []row{{"id": 1311, "api_id": 9, "api_vid": "100", "name": "衍天图录第五季", "play_from": "hongguo", "play_url": "第1集$hongguo://100/11"}}
	if reason, _ := hongguoSeasonExistingReason(films, 9, "101", "衍天图录第一季"); reason != "" {
		t.Fatal("coarse season-normalized name prevented an independent season restore")
	}
	for _, mode := range []string{"original-api-binding", "stored-native-binding", "exact-title"} {
		t.Run(mode, func(t *testing.T) {
			film := fourKVMFixtureRowCopy(films[0])
			switch mode {
			case "original-api-binding":
				film["api_vid"] = "101"
			case "stored-native-binding":
				film["play_url"] = "第1集$hongguo://100/11#第2集$hongguo://101/12"
			case "exact-title":
				film["name"] = "衍天图录第一季"
			}
			if reason, id := hongguoSeasonExistingReason([]row{film}, 9, "101", "衍天图录第一季"); reason == "" || id != 1311 {
				t.Fatal("existing same series or exact-title film would be reinserted")
			}
		})
	}
}

func TestHongguoSeasonEvidenceParserSupportsReviewedBoolErrorWithoutExposingFailure(t *testing.T) {
	_, remotes := hongguoSeasonRestoreFixture()
	remote := remotes["101"]
	remote.Error = json.RawMessage("false")
	data, _ := json.Marshal(remote)
	parsed, err := parseHongguoSeasonRemoteEvidence(data)
	if err != nil || len(parsed) != 1 {
		t.Fatal("reviewed boolean false error marker was not accepted")
	}
	remote.Error = json.RawMessage(`"https://failed.example/private?token=secret"`)
	data, _ = json.Marshal(remote)
	parsed, err = parseHongguoSeasonRemoteEvidence(data)
	if err != nil || len(parsed) != 0 {
		t.Fatal("failed remote evidence was not excluded")
	}
}
