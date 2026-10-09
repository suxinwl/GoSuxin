package runtimeplugin

import (
	"net/http"
	"strings"
)

// CMS member/device credentials belong to the business plugin, whereas host
// administrator tokens must never be forwarded to a worker.
func stripHostCredentials(name, originalPath string, request *http.Request) {
	member := name == "suxinvideo" && (originalPath == "/suxinvideo" || strings.HasPrefix(originalPath, "/suxinvideo/"))
	cookies := request.Cookies()
	if !member {
		request.Header.Del("Authorization")
	}
	request.Header.Del("Cookie")
	if member {
		for _, cookie := range cookies {
			if cookie.Name == "gfsessionid" {
				request.AddCookie(cookie)
			}
		}
	}
}
