package adminsystem

import "net/http"

// Clone first: logging must not remove the credentials needed by the handler.
func redactedLogHeaders(headers http.Header) http.Header {
	clean := headers.Clone()
	for _, name := range []string{"Authorization", "Proxy-Authorization", "Cookie", "Set-Cookie", "Apiverify", "X-Auth-Token", "X-Suxin-Plugin-Secret", "X-Suxin-Plugin-User"} {
		clean.Del(name)
	}
	return clean
}
