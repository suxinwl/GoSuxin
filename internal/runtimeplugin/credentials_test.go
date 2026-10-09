package runtimeplugin

import (
	"net/http/httptest"
	"testing"
)

func TestProxySeparatesHostAndCMSCredentials(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		member     bool
	}{{"suxinvideo", "/suxinvideo/app/v1/me", true}, {"suxinvideo", "/suxinvideo/center", true}, {"suxinvideo", "/admin/suxinvideo/dashboard", false}, {"suxinvideo", "/plugins/suxinvideo/admin/", false}, {"ebook", "/common/album/file", false}} {
		request := httptest.NewRequest("GET", tc.path, nil)
		request.Header.Set("Authorization", "Bearer member-token")
		request.Header.Set("Cookie", "gfsessionid=member-session; suxin_plugin_suxinvideo_admin=host-token; biztoken=host-token")
		stripHostCredentials(tc.name, tc.path, request)
		if tc.member {
			if request.Header.Get("Authorization") != "Bearer member-token" || request.Header.Get("Cookie") != "gfsessionid=member-session" {
				t.Fatalf("member credentials lost: %s", tc.path)
			}
		} else if request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "" {
			t.Fatalf("host credentials leaked: %s", tc.path)
		}
	}
}
