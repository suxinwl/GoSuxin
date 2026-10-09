package suxinvideo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

var jellyfinServerID string

func prepareJellyfinServerID(ctx context.Context) error {
	item, err := one(ctx, "SELECT value FROM sx_config WHERE `key`='app_jellyfin_server_id'")
	if err != nil {
		return err
	}
	if item == nil || !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(gconv.String(item["value"])) {
		value, err := appToken()
		if err != nil {
			return err
		}
		if err = execSQL(ctx, "INSERT INTO sx_config (`key`,value) VALUES('app_jellyfin_server_id',?) ON DUPLICATE KEY UPDATE value=VALUES(value)", value[:32]); err != nil {
			return err
		}
		item, err = one(ctx, "SELECT value FROM sx_config WHERE `key`='app_jellyfin_server_id'")
		if err != nil {
			return err
		}
	}
	jellyfinServerID = gconv.String(item["value"])
	return nil
}

var jellyfinStartMutex sync.Mutex
var jellyfinRunning *http.Server

func stopJellyfinListener() {
	jellyfinStartMutex.Lock()
	defer jellyfinStartMutex.Unlock()
	if jellyfinRunning != nil {
		_ = jellyfinRunning.Close()
		jellyfinRunning = nil
	}
}

type jellyfinGateway struct {
	client  *http.Client
	backend string
}

func jellyfinLocalBackend() string {
	host := "xq.suxinwl.com"
	if value, err := g.Cfg().Get(context.Background(), "server.publicHost"); err == nil && strings.TrimSpace(value.String()) != "" {
		host = strings.TrimSpace(value.String())
	}
	return "https://" + net.JoinHostPort(host, "8600")
}

// StartJellyfinListener deliberately refuses wildcard/public binds. The main
// native clients keep HTTPS; this HTTP adapter is solely for trusted LAN TVs.
func StartJellyfinListener(ctx context.Context, address string) error {
	jellyfinStartMutex.Lock()
	defer jellyfinStartMutex.Unlock()
	if jellyfinRunning != nil {
		return nil
	}
	if address == "" {
		address = "192.168.10.10:8601"
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || (!ip.IsPrivate() && !ip.IsLoopback()) {
		return errors.New("Jellyfin 兼容服务只能绑定局域网或回环 IP")
	}
	if err = EnsureAppSchema(ctx); err != nil {
		return err
	}
	if err = prepareJellyfinServerID(ctx); err != nil {
		return err
	}
	gateway, err := newJellyfinGateway()
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: gateway, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 32 << 10}
	jellyfinRunning = server
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			g.Log().Error(context.Background(), "Jellyfin LAN service:", err)
		}
	}()
	g.Log().Info(ctx, "Jellyfin compatible LAN endpoint:", address)
	return nil
}
func newJellyfinGateway() (*jellyfinGateway, error) {
	backend, err := url.Parse(jellyfinLocalBackend())
	if err != nil {
		return nil, err
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	pem, err := os.ReadFile("manifest/cert/fullchain.pem")
	if err != nil {
		return nil, err
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("本站 TLS 证书无效")
	}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: backend.Hostname(), MinVersion: tls.VersionTLS12}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, network, "127.0.0.1:8600")
	}, MaxIdleConns: 64, MaxIdleConnsPerHost: 32, ResponseHeaderTimeout: 45 * time.Second}
	return &jellyfinGateway{client: &http.Client{Transport: transport, Timeout: 0}, backend: backend.String()}, nil
}
func jellyfinLANRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsPrivate() || ip.IsLoopback())
}
func jellyfinOrigin(r *http.Request) string { return "http://" + r.Host }
func jellyfinToken(r *http.Request) string {
	for _, header := range []string{"X-Emby-Token", "X-MediaBrowser-Token"} {
		if v := strings.TrimSpace(r.Header.Get(header)); v != "" {
			return v
		}
	}
	authorization := r.Header.Get("X-Emby-Authorization")
	if authorization == "" {
		authorization = r.Header.Get("Authorization")
	}
	if len(authorization) > 7 && strings.EqualFold(authorization[:7], "Bearer ") {
		return strings.TrimSpace(authorization[7:])
	}
	if match := regexp.MustCompile(`(?i)\bToken\s*=\s*"([^"]+)"`).FindStringSubmatch(authorization); len(match) == 2 {
		return match[1]
	}
	return r.URL.Query().Get("api_key")
}
func jellyfinDeviceID(r *http.Request) string {
	header := r.Header.Get("X-Emby-Authorization")
	if header == "" {
		header = r.Header.Get("Authorization")
	}
	match := regexp.MustCompile(`(?i)\bDeviceId\s*=\s*"([^"]+)"`).FindStringSubmatch(header)
	raw := r.Header.Get("X-Emby-Device-Id")
	if len(match) == 2 {
		raw = match[1]
	}
	if raw == "" {
		raw = r.UserAgent() + "|" + strings.Split(r.RemoteAddr, ":")[0]
	}
	return "jellyfin-" + appHash(raw)[:32]
}
func jellyfinJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func jellyfinFailure(w http.ResponseWriter, err error) {
	status, message := 500, "服务器暂时不可用"
	var appErr *AppError
	if errors.As(err, &appErr) {
		status, message = appErr.Status, appErr.Message
	}
	jellyfinJSON(w, status, row{"StatusCode": status, "Message": message})
}
func jellyfinUser(user row) row {
	return row{"Id": jellyfinUserID(gconv.Int64(user["id"])), "Name": gconv.String(user["email"]), "ServerId": jellyfinServerID, "HasPassword": true, "HasConfiguredPassword": true, "EnableAutoLogin": false, "Policy": row{"IsAdministrator": false, "IsDisabled": false, "EnableMediaPlayback": true, "EnableContentDownloading": false, "EnableAllFolders": true}, "Configuration": row{"EnableNextEpisodeAutoPlay": true, "RememberAudioSelections": true, "RememberSubtitleSelections": true}}
}

func (j *jellyfinGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !jellyfinLANRequest(r) {
		jellyfinJSON(w, 403, row{"Message": "此服务仅供局域网访问"})
		return
	}
	if len(r.Host) > 250 || strings.ContainsAny(r.Host, "/\\\r\n") {
		w.WriteHeader(400)
		return
	}
	path := strings.TrimSuffix(r.URL.Path, "/")
	path = strings.TrimPrefix(path, "/jellyfin")
	lower := strings.ToLower(path)
	if lower == "/system/info/public" {
		jellyfinJSON(w, 200, row{"LocalAddress": jellyfinOrigin(r), "ServerName": setting(r.Context(), "site_name", "小柒影视"), "Version": "10.10.0", "ProductName": "Jellyfin", "OperatingSystem": "CMS", "Id": jellyfinServerID, "StartupWizardCompleted": true})
		return
	}
	if lower == "/branding/configuration" {
		jellyfinJSON(w, 200, row{"LoginDisclaimer": "使用本站会员邮箱和密码登录。影片权限沿用 CMS。"})
		return
	}
	if lower == "/users/public" {
		jellyfinJSON(w, 200, []row{})
		return
	}
	if lower == "/users/authenticatebyname" && r.Method == "POST" {
		j.authenticate(w, r)
		return
	}
	token := jellyfinToken(r)
	principal, err := AppUserForAccessToken(r.Context(), token)
	if token == "" && jellyfinProxyPathAllowed(r.URL.Path) {
		if grant := r.URL.Query().Get("app_grant"); grant != "" {
			var p *AppPrincipal
			p, err = appPrincipalByGrant(r.Context(), grant)
			if err == nil {
				principal = *p
			}
		}
	}
	if err != nil {
		jellyfinFailure(w, err)
		return
	}
	ctx := AppWithPrincipal(r.Context(), principal)
	r = r.WithContext(ctx)
	if strings.HasPrefix(lower, "/suxinvideo/") {
		j.proxyMedia(w, r, token)
		return
	}
	if lower == "/users/me" || strings.HasPrefix(lower, "/users/") && len(strings.Split(strings.Trim(path, "/"), "/")) == 2 && j.allowedUserPath(path, principal.User) {
		jellyfinJSON(w, 200, jellyfinUser(principal.User))
		return
	}
	if lower == "/system/info" {
		jellyfinJSON(w, 200, row{"Id": jellyfinServerID, "ServerName": setting(ctx, "site_name", "小柒影视"), "Version": "10.10.0", "ProductName": "Jellyfin", "LocalAddress": jellyfinOrigin(r)})
		return
	}
	if lower == "/sessions/logout" {
		err = execSQL(ctx, "UPDATE sx_app_session SET revoked=1 WHERE id=? AND user_id=?", principal.SessionID, principal.User["id"])
		if err != nil {
			jellyfinFailure(w, err)
		} else {
			w.WriteHeader(204)
		}
		return
	}
	if strings.HasPrefix(lower, "/users/") && !j.allowedUserPath(path, principal.User) {
		jellyfinJSON(w, 403, row{"Message": "无权访问其他用户"})
		return
	}
	userParts := strings.Split(strings.Trim(path, "/"), "/")
	if len(userParts) >= 4 && strings.EqualFold(userParts[0], "Users") && strings.EqualFold(userParts[2], "Items") {
		path = "/Items/" + strings.Join(userParts[3:], "/")
		lower = strings.ToLower(path)
	}
	if strings.HasSuffix(lower, "/views") || lower == "/library/virtualfolders" {
		j.views(w, r)
		return
	}
	if lower == "/sessions/playing" || lower == "/sessions/playing/progress" || lower == "/sessions/playing/stopped" {
		j.progress(w, r, principal)
		return
	}
	if lower == "/sessions" {
		jellyfinJSON(w, 200, []row{})
		return
	}
	if strings.HasPrefix(lower, "/displaypreferences/") {
		if r.Method == "POST" {
			w.WriteHeader(204)
		} else {
			jellyfinJSON(w, 200, row{"Id": "usersettings", "SortBy": "SortName", "RememberIndexing": true, "Client": "emby", "CustomPrefs": row{}})
		}
		return
	}
	if strings.Contains(lower, "/favoriteitems/") {
		j.favorite(w, r, principal)
		return
	}
	if strings.HasPrefix(lower, "/items/") {
		j.itemRoute(w, r, path, principal, token)
		return
	}
	if strings.HasPrefix(lower, "/shows/") {
		j.showRoute(w, r, path, principal)
		return
	}
	if strings.HasPrefix(lower, "/videos/") {
		j.stream(w, r, path, principal, token)
		return
	}
	if lower == "/items" || strings.HasSuffix(lower, "/items") || strings.HasSuffix(lower, "/items/latest") || strings.HasSuffix(lower, "/items/resume") {
		j.items(w, r, path, principal)
		return
	}
	if lower == "/search/hints" {
		j.searchHints(w, r)
		return
	}
	if lower == "/genres" || lower == "/studios" {
		jellyfinJSON(w, 200, row{"Items": []row{}, "TotalRecordCount": 0, "StartIndex": 0})
		return
	}
	jellyfinJSON(w, 404, row{"Message": "当前兼容接口不支持此操作"})
}
func (*jellyfinGateway) allowedUserPath(path string, user row) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) < 2 || jellyfinUserMatches(parts[1], gconv.Int64(user["id"]))
}
func (j *jellyfinGateway) authenticate(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string
		Pw       string
		Password string
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&input); err != nil {
		jellyfinFailure(w, appError(400, "登录信息无效"))
		return
	}
	if input.Pw == "" {
		input.Pw = input.Password
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	tokens, err := AppAuthenticatePassword(r.Context(), input.Username, input.Pw, jellyfinDeviceID(r), "网易爆米花／Jellyfin", ip)
	if err != nil {
		jellyfinFailure(w, err)
		return
	}
	// The Jellyfin protocol has no refresh exchange. Its own revocable session
	// uses a 30-day access lifetime, independently of one-hour native app tokens.
	expires := time.Now().Add(30 * 24 * time.Hour).Unix()
	if err = execSQL(r.Context(), "UPDATE sx_app_session SET access_expire=? WHERE access_hash=? AND user_id=? AND device_id=?", expires, appHash(tokens.AccessToken), tokens.User["id"], tokens.DeviceID); err != nil {
		jellyfinFailure(w, err)
		return
	}
	jellyfinJSON(w, 200, row{"User": jellyfinUser(tokens.User), "SessionInfo": row{"Id": tokens.DeviceID, "UserId": jellyfinUserID(gconv.Int64(tokens.User["id"])), "DeviceId": tokens.DeviceID, "DeviceName": "网易爆米花", "Client": "Jellyfin", "ServerId": jellyfinServerID, "SupportsMediaControl": true}, "AccessToken": tokens.AccessToken, "ServerId": jellyfinServerID})
}
func (j *jellyfinGateway) views(w http.ResponseWriter, r *http.Request) {
	items := []row{{"Id": jellyfinLibraryID(0), "Name": "全部影片", "Type": "CollectionFolder", "CollectionType": "mixed", "IsFolder": true, "ServerId": jellyfinServerID}}
	categories, err := appCategories(r.Context())
	if err != nil {
		jellyfinFailure(w, err)
		return
	}
	for _, category := range categories {
		if gconv.Int64(category["pid"]) != 0 {
			continue
		}
		items = append(items, row{"Id": jellyfinLibraryID(gconv.Int64(category["id"])), "Name": category["name"], "Type": "CollectionFolder", "CollectionType": "mixed", "IsFolder": true, "ServerId": jellyfinServerID})
	}
	jellyfinJSON(w, 200, row{"Items": items, "TotalRecordCount": len(items), "StartIndex": 0})
}

func jellyfinFilmIsSeries(ctx context.Context, film row) bool {
	movie, _ := moviePlaybackCategory(ctx, gconv.Int64(film["type_id"]))
	return !movie
}
func jellyfinFilmID(ctx context.Context, film row) string {
	kind := byte('m')
	if jellyfinFilmIsSeries(ctx, film) {
		kind = 's'
	}
	return jellyfinTypedGUID(kind, gconv.Int64(film["id"]), "film")
}
func jellyfinEpisodeID(vodID int64, key, version string) string {
	return jellyfinTypedGUID('e', vodID, version+"\x00"+key)
}
func jellyfinSeasonID(vodID int64, version string) string {
	return jellyfinTypedGUID('n', vodID, version)
}
func jellyfinSeasonNumber(src source) int {
	if match := regexp.MustCompile(`(?:^|:)season:([0-9]+)(?:$|:)`).FindStringSubmatch(src.VersionKey); len(match) == 2 {
		return max(1, gconv.Int(match[1]))
	}
	if match := vodAliasSeasonPattern.FindStringSubmatch(src.VersionLabel); len(match) == 2 {
		return max(1, vodAliasSeasonNumber(match[1]))
	}
	return 1
}
func jellyfinSeasonItem(film row, src source) row {
	name := strings.TrimSpace(src.VersionLabel)
	if name == "" {
		name = fmt.Sprintf("第 %d 季", jellyfinSeasonNumber(src))
	}
	return row{"Id": jellyfinSeasonID(gconv.Int64(film["id"]), src.VersionKey), "Name": name, "Type": "Season", "IsFolder": true, "IndexNumber": jellyfinSeasonNumber(src), "SeriesId": jellyfinTypedGUID('s', gconv.Int64(film["id"]), "film"), "SeriesName": film["name"], "ServerId": jellyfinServerID, "ImageTags": row{"Primary": appHash(gconv.String(film["pic"]))[:16]}}
}
func jellyfinVodID(id string) (int64, error) {
	kind, n, valid := jellyfinParseID(id)
	if !valid || (kind != 'm' && kind != 's' && kind != 'e' && kind != 'n') {
		return 0, appError(400, "媒体编号无效")
	}
	return n, nil
}
func jellyfinMediaSourceID(src source) string {
	hash := sha256.Sum256([]byte(src.Code + "\x00" + src.VersionKey))
	return hex.EncodeToString(hash[:12])
}

func (j *jellyfinGateway) filmItem(ctx context.Context, r *http.Request, film row, user row) row {
	id := jellyfinFilmID(ctx, film)
	kind := "Movie"
	if jellyfinIDKind(id) == 's' {
		kind = "Series"
	}
	score := gconv.Float64(film["score"])
	if hydrateVodSourceScores(ctx, []row{film}) == nil {
		score = gconv.Float64(film["score"])
	}
	item := row{"Id": id, "Name": film["name"], "Type": kind, "IsFolder": kind == "Series", "ServerId": jellyfinServerID, "Overview": filmDescription(gconv.String(film["content"])), "CommunityRating": score, "ProductionYear": gconv.Int(film["year"]), "ImageTags": row{"Primary": appHash(gconv.String(film["pic"]))[:16]}, "BackdropImageTags": []string{appHash(gconv.String(film["pic"]))[:16]}, "Genres": strings.FieldsFunc(gconv.String(film["class"]), func(c rune) bool { return c == ',' || c == '、' || c == '/' }), "LocationType": "Remote", "MediaType": "Video", "CanDownload": false, "UserData": row{"IsFavorite": false, "PlaybackPositionTicks": 0, "Played": false, "PlayCount": 0, "Key": id}}
	if user != nil {
		favorite, _ := one(ctx, "SELECT id FROM sx_fav WHERE user_id=? AND vod_id=?", user["id"], film["id"])
		progress, _ := one(ctx, "SELECT position,updated FROM sx_play_record WHERE user_id=? AND vod_id=?", user["id"], film["id"])
		state := item["UserData"].(row)
		state["IsFavorite"] = favorite != nil
		if progress != nil {
			state["PlaybackPositionTicks"] = gconv.Int64(progress["position"]) * 10000000
			state["LastPlayedDate"] = time.Unix(gconv.Int64(progress["updated"]), 0).UTC().Format(time.RFC3339)
		}
	}
	return item
}
func (j *jellyfinGateway) items(w http.ResponseWriter, r *http.Request, path string, p AppPrincipal) {
	query := r.URL.Query()
	start := gconv.Int(query.Get("StartIndex"))
	if start < 0 {
		start = 0
	}
	limit := gconv.Int(query.Get("Limit"))
	if limit < 1 || limit > 100 {
		limit = 30
	}
	parent := query.Get("ParentId")
	typeID := int64(0)
	if parent != "" {
		kind, id, valid := jellyfinParseID(parent)
		if !valid {
			jellyfinFailure(w, appError(400, "媒体库编号无效"))
			return
		}
		switch kind {
		case 'l':
			typeID = id
		case 's':
			j.showRoute(w, r, "/Shows/"+parent+"/Seasons", p)
			return
		case 'n':
			clone := r.Clone(r.Context())
			values := clone.URL.Query()
			values.Set("SeasonId", parent)
			clone.URL.RawQuery = values.Encode()
			j.showRoute(w, clone, "/Shows/"+jellyfinTypedGUID('s', id, "film")+"/Episodes", p)
			return
		default:
			jellyfinFailure(w, appError(400, "该媒体不是影片目录"))
			return
		}
	}
	where := publicVodListingCondition(r.Context(), "v")
	args := []any{}
	join := ""
	if typeID > 0 {
		where += " AND (v.type_id=? OR v.type_id IN (SELECT id FROM sx_type WHERE pid=?))"
		args = append(args, typeID, typeID)
	}
	search := strings.TrimSpace(query.Get("SearchTerm"))
	if search != "" {
		if len([]rune(search)) > 100 {
			jellyfinFailure(w, appError(400, "搜索词过长"))
			return
		}
		where += " AND (v.name LIKE ? OR v.sub LIKE ?)"
		args = append(args, "%"+search+"%", "%"+search+"%")
	}
	if strings.EqualFold(query.Get("IsFavorite"), "true") || strings.Contains(strings.ToLower(query.Get("Filters")), "isfavorite") {
		join = " JOIN sx_fav f ON f.vod_id=v.id"
		where += " AND f.user_id=?"
		args = append(args, p.User["id"])
	}
	if strings.HasSuffix(strings.ToLower(path), "/resume") {
		join = " JOIN sx_play_record pr ON pr.vod_id=v.id"
		where += " AND pr.user_id=? AND pr.position>0"
		args = append(args, p.User["id"])
	}
	count, err := one(r.Context(), "SELECT COUNT(*) n FROM sx_vod v"+join+" WHERE "+where, args...)
	if err != nil {
		jellyfinFailure(w, err)
		return
	}
	order := "v.updatetime DESC,v.id DESC"
	if strings.Contains(strings.ToLower(query.Get("SortBy")), "sortname") {
		order = "v.name ASC,v.id ASC"
	}
	if strings.HasSuffix(strings.ToLower(path), "/resume") {
		order = "pr.updated DESC,v.id DESC"
	}
	itemsArgs := append(append([]any{}, args...), limit, start)
	films, err := all(r.Context(), "SELECT v.* FROM sx_vod v"+join+" WHERE "+where+" ORDER BY "+order+" LIMIT ? OFFSET ?", itemsArgs...)
	if err != nil {
		jellyfinFailure(w, err)
		return
	}
	items := make([]row, 0, len(films))
	for _, film := range films {
		items = append(items, j.filmItem(r.Context(), r, film, p.User))
	}
	if strings.HasSuffix(strings.ToLower(path), "/latest") {
		jellyfinJSON(w, 200, items)
	} else {
		jellyfinJSON(w, 200, row{"Items": items, "TotalRecordCount": gconv.Int64(count["n"]), "StartIndex": start})
	}
}
func (j *jellyfinGateway) searchHints(w http.ResponseWriter, r *http.Request) {
	term := strings.TrimSpace(r.URL.Query().Get("SearchTerm"))
	if len([]rune(term)) > 100 {
		jellyfinFailure(w, appError(400, "搜索词过长"))
		return
	}
	films, err := all(r.Context(), "SELECT id,name,type_id FROM sx_vod WHERE "+publicVodListingCondition(r.Context(), "")+" AND name LIKE ? ORDER BY total_hits DESC LIMIT 30", "%"+term+"%")
	if err != nil {
		jellyfinFailure(w, err)
		return
	}
	items := make([]row, 0, len(films))
	for _, film := range films {
		items = append(items, row{"Id": jellyfinFilmID(r.Context(), film), "Name": film["name"], "Type": map[bool]string{true: "Series", false: "Movie"}[jellyfinFilmIsSeries(r.Context(), film)], "MediaType": "Video"})
	}
	jellyfinJSON(w, 200, row{"SearchHints": items, "TotalRecordCount": len(items)})
}

func (j *jellyfinGateway) itemRoute(w http.ResponseWriter, r *http.Request, path string, p AppPrincipal, token string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		jellyfinFailure(w, appError(404, "媒体不存在"))
		return
	}
	id := parts[1]
	if kind, libraryID, valid := jellyfinParseID(id); valid && kind == 'l' {
		if len(parts) != 2 {
			jellyfinFailure(w, appError(404, "媒体库操作不存在"))
			return
		}
		item, err := jellyfinLibraryItem(r.Context(), libraryID)
		if err != nil {
			jellyfinFailure(w, err)
			return
		}
		jellyfinJSON(w, 200, item)
		return
	}
	vodID, err := jellyfinVodID(id)
	if err != nil {
		jellyfinFailure(w, err)
		return
	}
	film, err := appVisibleFilm(r.Context(), vodID)
	if err != nil {
		jellyfinFailure(w, err)
		return
	}
	if len(parts) >= 3 && strings.EqualFold(parts[2], "Images") {
		j.image(w, r, film, token)
		return
	}
	if len(parts) >= 3 && strings.EqualFold(parts[2], "PlaybackInfo") {
		j.playbackInfo(w, r, id, film, p, token)
		return
	}
	if len(parts) >= 3 && strings.EqualFold(parts[2], "Ancestors") {
		jellyfinJSON(w, 200, []row{{"Id": jellyfinLibraryID(0), "Name": "全部影片", "Type": "CollectionFolder", "IsFolder": true}})
		return
	}
	if len(parts) == 2 {
		item := j.filmItem(r.Context(), r, film, p.User)
		if jellyfinIDKind(id) == 'e' {
			episode, source, err := j.resolveEpisode(r.Context(), film, id, "")
			if err != nil {
				jellyfinFailure(w, err)
				return
			}
			item = j.episodeItem(r.Context(), r, film, source, episode, p.User)
		}
		if jellyfinIDKind(id) == 'n' {
			sources, err := hydratePlayers(r.Context(), film, playlist(film))
			if err != nil {
				jellyfinFailure(w, err)
				return
			}
			found := false
			for _, src := range sources {
				if jellyfinSeasonMatches(vodID, src.VersionKey, id) {
					item = jellyfinSeasonItem(film, src)
					found = true
					break
				}
			}
			if !found {
				jellyfinFailure(w, appError(404, "分季不存在"))
				return
			}
		}
		jellyfinJSON(w, 200, item)
		return
	}
	jellyfinJSON(w, 404, row{"Message": "媒体操作不存在"})
}
func (j *jellyfinGateway) showRoute(w http.ResponseWriter, r *http.Request, path string, p AppPrincipal) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 3 {
		jellyfinFailure(w, appError(404, "分集操作不存在"))
		return
	}
	id, err := jellyfinVodID(parts[1])
	if err != nil {
		jellyfinFailure(w, err)
		return
	}
	film, err := appVisibleFilm(r.Context(), id)
	if err != nil {
		jellyfinFailure(w, err)
		return
	}
	if !strings.EqualFold(parts[2], "Episodes") && !strings.EqualFold(parts[2], "Seasons") {
		jellyfinFailure(w, appError(404, "分集操作不存在"))
		return
	}
	sources, err := hydratePlayers(r.Context(), film, playlist(film))
	if err != nil {
		jellyfinFailure(w, err)
		return
	}
	preferred := appPreferredLine(r.Context(), film, sources)
	sort.SliceStable(sources, func(a, b int) bool { return sources[a].Code == preferred && sources[b].Code != preferred })
	if strings.EqualFold(parts[2], "Seasons") {
		items := []row{}
		seen := map[string]bool{}
		for _, src := range sources {
			if seen[src.VersionKey] {
				continue
			}
			seen[src.VersionKey] = true
			items = append(items, jellyfinSeasonItem(film, src))
		}
		jellyfinJSON(w, 200, row{"Items": items, "TotalRecordCount": len(items), "StartIndex": 0})
		return
	}
	seasonID := r.URL.Query().Get("SeasonId")
	seen := map[string]bool{}
	items := make([]row, 0)
	base := j.filmItem(r.Context(), r, film, p.User)
	resume, _ := one(r.Context(), "SELECT episode_key,version_key FROM sx_play_record WHERE user_id=? AND vod_id=?", p.User["id"], film["id"])
	for _, source := range sources {
		if seasonID != "" && !jellyfinSeasonMatches(gconv.Int64(film["id"]), source.VersionKey, seasonID) {
			continue
		}
		for index := range source.Episodes {
			id := jellyfinEpisodeID(gconv.Int64(film["id"]), episodeKey(source.Episodes[index]), source.VersionKey)
			if seen[id] {
				continue
			}
			seen[id] = true
			items = append(items, jellyfinEpisodeItem(film, source, index, base, resume))
		}
	}
	jellyfinJSON(w, 200, row{"Items": items, "TotalRecordCount": len(items), "StartIndex": 0})
}
func episodeKey(ep episode) string { key, _ := playbackEpisodeIdentity(ep.Name); return key }
func (j *jellyfinGateway) episodeItem(ctx context.Context, r *http.Request, film row, src source, index int, user row) row {
	base := j.filmItem(ctx, r, film, user)
	resume, _ := one(ctx, "SELECT episode_key,version_key FROM sx_play_record WHERE user_id=? AND vod_id=?", user["id"], film["id"])
	return jellyfinEpisodeItem(film, src, index, base, resume)
}
func jellyfinEpisodeItem(film row, src source, index int, base, resume row) row {
	key, number := playbackEpisodeIdentity(src.Episodes[index].Name)
	item := row{}
	for field, value := range base {
		item[field] = value
	}
	item["Id"] = jellyfinEpisodeID(gconv.Int64(film["id"]), key, src.VersionKey)
	item["Name"] = src.Episodes[index].Name
	item["Type"] = "Episode"
	item["IsFolder"] = false
	item["IndexNumber"] = number
	item["ParentIndexNumber"] = jellyfinSeasonNumber(src)
	item["SeriesId"] = jellyfinTypedGUID('s', gconv.Int64(film["id"]), "film")
	item["SeriesName"] = film["name"]
	item["SeasonId"] = jellyfinSeasonID(gconv.Int64(film["id"]), src.VersionKey)
	state := row{}
	if original, ok := base["UserData"].(row); ok {
		for field, value := range original {
			state[field] = value
		}
	}
	state["Key"] = item["Id"]
	if resume == nil || gconv.String(resume["episode_key"]) != key || gconv.String(resume["version_key"]) != src.VersionKey {
		state["PlaybackPositionTicks"] = 0
		delete(state, "LastPlayedDate")
	}
	item["UserData"] = state
	return item
}
func (*jellyfinGateway) resolveEpisode(ctx context.Context, film row, id, mediaSourceID string) (int, source, error) {
	sources, err := hydratePlayers(ctx, film, playlist(film))
	if err != nil {
		return 0, source{}, err
	}
	preferred := appPreferredLine(ctx, film, sources)
	sort.SliceStable(sources, func(a, b int) bool { return sources[a].Code == preferred && sources[b].Code != preferred })
	for _, src := range sources {
		if mediaSourceID != "" && jellyfinMediaSourceID(src) != mediaSourceID {
			continue
		}
		for index, ep := range src.Episodes {
			if jellyfinIDKind(id) != 'e' || jellyfinEpisodeMatches(gconv.Int64(film["id"]), episodeKey(ep), src.VersionKey, id) {
				return index, src, nil
			}
		}
	}
	return 0, source{}, appError(404, "当前分集暂无可用线路")
}

func (j *jellyfinGateway) backendJSON(r *http.Request, token, path string, input any, output any) error {
	data, _ := json.Marshal(input)
	request, err := http.NewRequestWithContext(r.Context(), "POST", j.backend+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	request.Host = r.Host
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := j.client.Do(request)
	if err != nil {
		return appError(502, "播放服务暂时不可用")
	}
	defer response.Body.Close()
	var envelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&envelope); err != nil {
		return appError(502, "播放服务返回无效响应")
	}
	if response.StatusCode >= 400 || envelope.Code != 0 {
		return appError(max(response.StatusCode, 400), envelope.Message)
	}
	return json.Unmarshal(envelope.Data, output)
}
func (j *jellyfinGateway) playbackInfo(w http.ResponseWriter, r *http.Request, id string, film row, p AppPrincipal, token string) {
	if _, err := AppAuthorizeFilm(r.Context(), film); err != nil {
		jellyfinFailure(w, err)
		return
	}
	mediaSourceID := r.URL.Query().Get("MediaSourceId")
	if r.Method == "POST" {
		var input struct {
			MediaSourceID string `json:"MediaSourceId"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&input)
		if input.MediaSourceID != "" {
			mediaSourceID = input.MediaSourceID
		}
	}
	index, src, err := j.resolveEpisode(r.Context(), film, id, mediaSourceID)
	if err != nil {
		jellyfinFailure(w, err)
		return
	}
	var descriptor AppPlaybackDescriptor
	err = j.backendJSON(r, token, "/suxinvideo/app/v1/playback/resolve", AppPlaybackRequest{VodID: gconv.Int64(film["id"]), Line: src.Code, Episode: index, EpisodeKey: episodeKey(src.Episodes[index]), VersionKey: src.VersionKey, Manual: mediaSourceID != ""}, &descriptor)
	if err != nil {
		jellyfinFailure(w, err)
		return
	}
	streamURL := j.toLAN(r, descriptor.URL)
	allSources, _ := hydratePlayers(r.Context(), film, playlist(film))
	for _, candidate := range allSources {
		if candidate.Code == descriptor.Line && candidate.VersionKey == descriptor.VersionKey {
			src = candidate
			break
		}
	}
	container := "mp4"
	if descriptor.Type == "hls" || strings.Contains(descriptor.Type, "m3u8") {
		container = "m3u8"
	}
	media := row{"Id": jellyfinMediaSourceID(src), "Name": src.Name, "Protocol": "Http", "Path": streamURL, "Container": container, "Type": "Default", "IsRemote": true, "SupportsDirectPlay": true, "SupportsDirectStream": true, "SupportsTranscoding": false, "DirectStreamUrl": streamURL, "RequiresOpening": false, "RequiresClosing": false, "ReadAtNativeFramerate": false, "MediaStreams": []row{{"Type": "Video", "Index": 0, "IsDefault": true, "IsExternal": false}, {"Type": "Audio", "Index": 1, "IsDefault": true, "IsExternal": false}}, "DefaultAudioStreamIndex": 1}
	if descriptor.DurationMS > 0 {
		media["RunTimeTicks"] = descriptor.DurationMS * 10000
	}
	if len(descriptor.Qualities) > 0 {
		quality := descriptor.Qualities[0]
		media["Bitrate"] = quality.Bitrate
	}
	mediaSources := []row{media}
	// Additional lines stay lazy. Resolving every source here would reproduce
	// the long first-play delay that the native source policy removes.
	for _, candidate := range allSources {
		if candidate.Code == src.Code && candidate.VersionKey == src.VersionKey {
			continue
		}
		if candidate.VersionKey != descriptor.VersionKey {
			continue
		}
		if candidate.Parse != "" && !strings.HasPrefix(strings.ToLower(candidate.Parse), "m3u8:") {
			continue
		}
		matched := false
		for _, ep := range candidate.Episodes {
			if episodeKey(ep) == descriptor.EpisodeKey {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		address := jellyfinOrigin(r) + "/Videos/" + url.PathEscape(id) + "/stream?MediaSourceId=" + url.QueryEscape(jellyfinMediaSourceID(candidate)) + "&api_key=" + url.QueryEscape(token)
		alternative := row{}
		for key, value := range media {
			alternative[key] = value
		}
		alternative["Id"] = jellyfinMediaSourceID(candidate)
		alternative["Name"] = candidate.Name
		alternative["Path"] = address
		alternative["DirectStreamUrl"] = address
		delete(alternative, "Bitrate")
		mediaSources = append(mediaSources, alternative)
	}
	jellyfinJSON(w, 200, row{"MediaSources": mediaSources, "PlaySessionId": appHash(token + "|" + id)[:32]})
}
func (j *jellyfinGateway) stream(w http.ResponseWriter, r *http.Request, path string, p AppPrincipal, token string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 {
		jellyfinFailure(w, appError(404, "媒体不存在"))
		return
	}
	id, err := jellyfinVodID(parts[1])
	if err != nil {
		jellyfinFailure(w, err)
		return
	}
	film, err := appVisibleFilm(r.Context(), id)
	if err != nil {
		jellyfinFailure(w, err)
		return
	}
	if _, err = AppAuthorizeFilm(r.Context(), film); err != nil {
		jellyfinFailure(w, err)
		return
	}
	index, src, err := j.resolveEpisode(r.Context(), film, parts[1], r.URL.Query().Get("MediaSourceId"))
	if err != nil {
		jellyfinFailure(w, err)
		return
	}
	var descriptor AppPlaybackDescriptor
	err = j.backendJSON(r, token, "/suxinvideo/app/v1/playback/resolve", AppPlaybackRequest{VodID: id, Line: src.Code, Episode: index, EpisodeKey: episodeKey(src.Episodes[index]), VersionKey: src.VersionKey, PositionMS: gconv.Int64(r.URL.Query().Get("StartTimeTicks")) / 10000, Manual: true}, &descriptor)
	if err != nil {
		jellyfinFailure(w, err)
		return
	}
	http.Redirect(w, r, j.toLAN(r, descriptor.URL), http.StatusTemporaryRedirect)
}
func (j *jellyfinGateway) localMediaHost(host, requestHost string) bool {
	if strings.EqualFold(host, requestHost) || strings.EqualFold(host, "127.0.0.1:8600") {
		return true
	}
	backend := j.backend
	if backend == "" {
		backend = jellyfinLocalBackend()
	}
	parsed, err := url.Parse(backend)
	return err == nil && strings.EqualFold(host, parsed.Host)
}

func (j *jellyfinGateway) toLAN(r *http.Request, raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if parsed.Host == "" {
		return jellyfinOrigin(r) + raw
	}
	if j.localMediaHost(parsed.Host, r.Host) {
		parsed.Scheme = "http"
		parsed.Host = r.Host
		return parsed.String()
	}
	return raw
}
func (j *jellyfinGateway) image(w http.ResponseWriter, r *http.Request, film row, token string) {
	raw := gconv.String(film["pic"])
	if safePlayerAddress(raw) {
		raw = imageLink(r.Context(), raw)
	}
	if !strings.HasPrefix(raw, "/suxinvideo/") && !strings.HasPrefix(raw, "/resource/static/") {
		w.WriteHeader(404)
		return
	}
	clone := r.Clone(r.Context())
	u, err := url.Parse(raw)
	if err != nil {
		w.WriteHeader(404)
		return
	}
	clone.URL = u
	j.proxyMedia(w, clone, token)
}
func jellyfinProxyPathAllowed(path string) bool {
	if strings.Contains(path, "..") || strings.ContainsAny(path, "\\\x00") {
		return false
	}
	return path == "/suxinvideo/image" || path == "/suxinvideo/image/local" || path == "/suxinvideo/asset" || path == "/suxinvideo/proxy" || path == "/suxinvideo/app/v1/media" || strings.HasPrefix(path, "/suxinvideo/app/v1/hls/") || strings.HasPrefix(path, "/suxinvideo/native/") || strings.HasPrefix(path, "/resource/static/suxinvideo/")
}
func (j *jellyfinGateway) proxyMedia(w http.ResponseWriter, r *http.Request, token string) {
	if !jellyfinProxyPathAllowed(r.URL.Path) {
		jellyfinJSON(w, 404, row{"Message": "媒体路径不存在"})
		return
	}
	request, err := http.NewRequestWithContext(r.Context(), r.Method, j.backend+r.URL.RequestURI(), nil)
	if err != nil {
		jellyfinFailure(w, err)
		return
	}
	request.Host = r.Host
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	for _, name := range []string{"Range", "If-Range", "If-None-Match", "If-Modified-Since"} {
		if v := r.Header.Get(name); v != "" {
			request.Header.Set(name, v)
		}
	}
	response, err := j.client.Do(request)
	if err != nil {
		jellyfinFailure(w, appError(502, "媒体连接失败"))
		return
	}
	defer response.Body.Close()
	for name, values := range response.Header {
		lower := strings.ToLower(name)
		if lower == "connection" || lower == "transfer-encoding" || lower == "set-cookie" {
			continue
		}
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	content := strings.ToLower(response.Header.Get("Content-Type"))
	if strings.Contains(content, "mpegurl") {
		data, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
		if err != nil || len(data) > 4<<20 {
			jellyfinFailure(w, appError(502, "播放列表无效"))
			return
		}
		body := strings.ReplaceAll(string(data), j.backend, jellyfinOrigin(r))
		body = strings.ReplaceAll(body, "https://"+r.Host, jellyfinOrigin(r))
		w.Header().Del("Content-Length")
		w.WriteHeader(response.StatusCode)
		_, _ = io.WriteString(w, body)
		return
	}
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}
func (j *jellyfinGateway) favorite(w http.ResponseWriter, r *http.Request, p AppPrincipal) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	id, err := jellyfinVodID(parts[len(parts)-1])
	if err != nil {
		jellyfinFailure(w, err)
		return
	}
	if _, err = appVisibleFilm(r.Context(), id); err != nil {
		jellyfinFailure(w, err)
		return
	}
	if r.Method == "POST" {
		err = execSQL(r.Context(), "INSERT IGNORE INTO sx_fav(user_id,vod_id,created) VALUES(?,?,?)", p.User["id"], id, time.Now().Unix())
	} else if r.Method == "DELETE" {
		err = execSQL(r.Context(), "DELETE FROM sx_fav WHERE user_id=? AND vod_id=?", p.User["id"], id)
	} else {
		jellyfinFailure(w, appError(405, "操作方法无效"))
		return
	}
	if err != nil {
		jellyfinFailure(w, err)
		return
	}
	jellyfinJSON(w, 200, row{"IsFavorite": r.Method == "POST", "Key": parts[len(parts)-1]})
}
func (j *jellyfinGateway) progress(w http.ResponseWriter, r *http.Request, p AppPrincipal) {
	if r.Method != "POST" {
		jellyfinFailure(w, appError(405, "操作方法无效"))
		return
	}
	var input struct {
		ItemID        string `json:"ItemId"`
		PositionTicks int64
		MediaSourceID string `json:"MediaSourceId"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 128<<10)).Decode(&input) != nil || input.PositionTicks < 0 || input.PositionTicks > 24*3600*10000000 {
		jellyfinFailure(w, appError(400, "播放进度无效"))
		return
	}
	id, err := jellyfinVodID(input.ItemID)
	if err != nil {
		jellyfinFailure(w, err)
		return
	}
	film, err := appVisibleFilm(r.Context(), id)
	if err != nil {
		jellyfinFailure(w, err)
		return
	}
	if _, err = AppAuthorizeFilm(r.Context(), film); err != nil {
		jellyfinFailure(w, err)
		return
	}
	index, src, err := j.resolveEpisode(r.Context(), film, input.ItemID, input.MediaSourceID)
	if err != nil {
		jellyfinFailure(w, err)
		return
	}
	err = execSQL(r.Context(), "INSERT INTO sx_play_record(user_id,vod_id,episode,position,updated,source_code,episode_key,version_key) VALUES(?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE episode=VALUES(episode),position=VALUES(position),updated=VALUES(updated),source_code=VALUES(source_code),episode_key=VALUES(episode_key),version_key=VALUES(version_key)", p.User["id"], id, index+1, input.PositionTicks/10000000, time.Now().Unix(), src.Code, episodeKey(src.Episodes[index]), src.VersionKey)
	if err != nil {
		jellyfinFailure(w, err)
		return
	}
	w.WriteHeader(204)
}
