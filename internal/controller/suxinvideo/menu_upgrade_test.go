package suxinvideo

import "testing"

func TestExpandRoleButtons(t *testing.T) {
	mapping := map[int64]int64{4: 10}
	extras := map[int64][]int64{4: {11, 12}, 10: {11, 12}}
	for _, tc := range []struct{ before, want string }{
		{"4,8", "8,10,11,12"},
		{"10", "10,11,12"},
	} {
		got, changed := expandRoleButtons(tc.before, mapping, extras)
		if !changed || got != tc.want {
			t.Fatalf("%s => %s, changed=%v; want %s", tc.before, got, changed, tc.want)
		}
	}
	if got, changed := expandRoleButtons("8", mapping, extras); changed || got != "8" {
		t.Fatalf("unrelated grant changed: %s, %v", got, changed)
	}
}
