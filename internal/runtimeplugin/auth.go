package runtimeplugin

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	hostauth "github.com/suxinwl/GoSuxin/utility/auth"
	"github.com/suxinwl/GoSuxin/utility/gf"
)

const (
	runtimePluginCookiePrefix  = "suxin_plugin_"
	runtimePluginCookieSuffix  = "_admin"
	runtimePluginCookieTTL     = 5 * time.Minute
	runtimePluginBootstrapPath = "/__host_auth/bootstrap"
)

// runtimePluginRoute identifies only routes owned by the runtime plugin
// authentication groups. The plugin name is checked against the registry's
// package-name grammar before it is used in a cookie or redirect.
func runtimePluginRoute(path string) (plugin string, api bool, ok bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 3 && parts[0] == "plugins" && pluginNamePattern.MatchString(parts[1]) && parts[2] == "admin" {
		return parts[1], false, true
	}
	if len(parts) >= 4 && parts[0] == "plugins" && pluginNamePattern.MatchString(parts[1]) && parts[2] == "api" && parts[3] == "admin" {
		return parts[1], true, true
	}
	return "", false, false
}

func runtimePluginCookieName(plugin string) string {
	return runtimePluginCookiePrefix + plugin + runtimePluginCookieSuffix
}

func runtimePluginCookiePath(plugin string) string {
	return "/plugins/" + plugin + "/"
}

func runtimePluginShellPath(path string) bool {
	_, api, ok := runtimePluginRoute(path)
	if !ok || api {
		return false
	}
	return strings.TrimRight(path, "/") == "/plugins/"+strings.Split(strings.Trim(path, "/"), "/")[1]+"/admin"
}

func runtimePluginSecureRequest(r *ghttp.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func setRuntimePluginCookie(r *ghttp.Request, plugin, token string) {
	if token == "" {
		return
	}
	r.Cookie.SetHttpCookie(&http.Cookie{
		Name:     runtimePluginCookieName(plugin),
		Value:    token,
		Path:     runtimePluginCookiePath(plugin),
		Expires:  time.Now().Add(runtimePluginCookieTTL),
		MaxAge:   int(runtimePluginCookieTTL / time.Second),
		HttpOnly: true,
		Secure:   runtimePluginSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func expireRuntimePluginCookie(r *ghttp.Request, plugin string) {
	r.Cookie.SetHttpCookie(&http.Cookie{
		Name:     runtimePluginCookieName(plugin),
		Value:    "",
		Path:     runtimePluginCookiePath(plugin),
		Expires:  time.Unix(1, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   runtimePluginSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func runtimePluginCookieValue(r *ghttp.Request, plugin string) string {
	if cookie := r.Cookie.Get(runtimePluginCookieName(plugin)); cookie != nil {
		return strings.TrimSpace(cookie.String())
	}
	return ""
}

func runtimePluginRedirectTarget(r *ghttp.Request) string {
	if r.RequestURI != "" {
		if parsed, err := url.ParseRequestURI(r.RequestURI); err == nil && parsed.Path != "" && strings.HasPrefix(parsed.Path, "/plugins/") {
			target := parsed.EscapedPath()
			if parsed.RawQuery != "" {
				target += "?" + parsed.RawQuery
			}
			return target
		}
	}
	path := r.URL.EscapedPath()
	if path == "" {
		path = r.URL.Path
	}
	if path == "" {
		path = "/"
	}
	if r.URL.RawQuery != "" {
		path += "?" + r.URL.RawQuery
	}
	return path
}

func runtimePluginRedirect(r *ghttp.Request) {
	target := runtimePluginRedirectTarget(r)
	r.Response.RedirectTo("/suxinweb/login?redirect="+url.QueryEscape(target), http.StatusFound)
}

func runtimePluginUnauthorized(r *ghttp.Request) {
	r.Response.WriteHeader(http.StatusUnauthorized)
	r.Response.WriteJson(gf.Failed().SetCode(hostauth.JwtTokenInvalid).SetMsg("宿主登录已失效，请返回 GoSuxin 后台重新登录"))
	r.ExitAll()
}

// runtimePluginAuth accepts the host token only long enough to exchange it for
// a short-lived, HttpOnly cookie. Once present, the cookie is made authoritative
// for the request and is still checked through the host token cache on every
// request. A client-supplied plugin identity header is discarded here and again
// by the reverse proxy before the request reaches the plugin.
func runtimePluginAuth(r *ghttp.Request) {
	plugin, api, ok := runtimePluginRoute(r.URL.Path)
	if !ok {
		r.Middleware.Next()
		return
	}
	r.Header.Del("X-Suxin-Plugin-User")

	originalAuthorization := r.Header.Get("Authorization")
	authenticated := false
	// Prefer a current host token whenever the browser supplied one. This lets a
	// user switch GoSuxin accounts without being pinned to an older plugin
	// cookie for five minutes.
	if originalAuthorization != "" {
		r.Header.Set("Authorization", originalAuthorization)
		if _, err := hostauth.ParseToken(r); err == nil {
			setRuntimePluginCookie(r, plugin, hostauth.GetRequestToken(r))
			authenticated = true
		}
	}
	if !authenticated {
		if cookieToken := runtimePluginCookieValue(r, plugin); cookieToken != "" {
			r.Header.Set("Authorization", cookieToken)
			if _, err := hostauth.ParseToken(r); err == nil {
				setRuntimePluginCookie(r, plugin, hostauth.GetRequestToken(r))
				authenticated = true
			} else {
				expireRuntimePluginCookie(r, plugin)
			}
		}
	}
	if !authenticated {
		r.Header.Del("Authorization")
	}

	if !authenticated {
		if !api && (r.Method == http.MethodGet || r.Method == http.MethodHead) && runtimePluginShellPath(r.URL.Path) {
			runtimePluginRedirect(r)
			return
		}
		runtimePluginUnauthorized(r)
		return
	}
	r.Middleware.Next()
}

// runtimePluginBootstrap is intentionally small. The preceding middleware has
// performed the token exchange and the normal host middleware that follows it
// performs the complete RBAC check before this response is returned.
func runtimePluginBootstrap(r *ghttp.Request) {
	plugin, _, ok := runtimePluginRoute(r.URL.Path)
	if !ok || !strings.HasSuffix(strings.TrimRight(r.URL.Path, "/"), runtimePluginBootstrapPath) {
		r.Response.WriteStatus(http.StatusNotFound)
		r.ExitAll()
		return
	}
	r.Response.WriteJson(gf.Success().SetData(gf.Map{"authenticated": true, "plugin": plugin}))
	r.ExitAll()
}
