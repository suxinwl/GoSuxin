package suxinvideo

import (
	"encoding/json"
	"testing"
)

func TestMacSourceClasses(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		want int
	}{
		{"array", `{"code":1,"total":"40","class":[{"type_id":1,"type_name":"电影"},{"type_id":2,"type_name":"剧集"}]}`, 2},
		{"keyed object", `{"code":0,"class":{"2":{"type_id":2,"type_name":"剧集"},"1":{"type_id":1,"type_name":"电影"}}}`, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			var payload macPayload
			if err := json.Unmarshal([]byte(test.body), &payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Class) != test.want || !validMacPayload(payload, "list") {
				t.Fatalf("unexpected classes response: %+v", payload)
			}
			if test.name == "keyed object" && payload.Class[0]["type_name"] != "电影" {
				t.Fatalf("class order changed: %+v", payload.Class)
			}
		})
	}
}

func TestMacVideoListRejectsInvalidStatus(t *testing.T) {
	data := macPayload{Code: 0, Class: []map[string]any{{"type_id": 1}}}
	if validMacPayload(data, "videolist") {
		t.Fatal("video list must not accept a failed response with only classes")
	}
}
