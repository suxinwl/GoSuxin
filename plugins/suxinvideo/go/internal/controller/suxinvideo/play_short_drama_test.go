package suxinvideo

import "testing"

func TestShortDramaPlaybackMetadata(t *testing.T) {
	for _, test := range []struct {
		name    string
		vod     row
		sources []source
		want    bool
	}{
		{"short drama", row{"class": "短剧大全"}, nil, true},
		{"imported metadata", row{"class": "Short"}, nil, true},
		{"hongguo evidence", row{"class": "古装穿越", "type_id": 999}, []source{{Code: "hongguo"}}, true},
		{"movie", row{"class": "剧情片"}, []source{{Code: "hnm3u8"}}, false},
		{"APP series", row{"class": "电视剧"}, []source{{Code: "yqk_1"}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := shortDramaMetadata(test.vod, test.sources); got != test.want {
				t.Fatalf("preload evidence=%v, want %v", got, test.want)
			}
		})
	}
}
