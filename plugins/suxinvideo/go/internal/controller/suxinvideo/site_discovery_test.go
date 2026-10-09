package suxinvideo

import "testing"

func TestRequestedPlaybackKeySurvivesHiddenSource(t *testing.T) {
	original := []source{{Code: "disabled", Episodes: []episode{{Name: "第01集"}, {Name: "第160集"}}}}
	visible := []playSource{{Code: "new", Episodes: []playEpisode{{Name: "第1集", Key: "episode:1", Number: 1}}}}
	req := &PlayReq{Line: "disabled", Episode: 1}
	if got := requestedPlaybackKey(req, original, visible, 0, false, "series"); got != "episode:160" {
		t.Fatalf("hidden original chapter identity lost: %q", got)
	}
	if got := requestedPlaybackKey(&PlayReq{Episode: 159}, nil, nil, 0, false, "series"); got != "episode:160" {
		t.Fatalf("empty catalogue must retain requested episode: %q", got)
	}
}

func TestRequestedPlaybackKeyPreservesMovieAndSpecialIdentity(t *testing.T) {
	original := []source{{Code: "disabled", Episodes: []episode{{Name: "HD国语"}}}}
	if got := requestedPlaybackKey(&PlayReq{Line: "disabled"}, original, nil, 0, true, "电影"); got != "movie:feature" {
		t.Fatalf("single feature label must match later providers: %q", got)
	}
	original[0].Episodes[0].Name = "回顾特辑"
	if got := requestedPlaybackKey(&PlayReq{Line: "disabled"}, original, nil, 0, false, "series"); got != "label:回顾特辑" {
		t.Fatalf("special must not become episode one: %q", got)
	}
}
