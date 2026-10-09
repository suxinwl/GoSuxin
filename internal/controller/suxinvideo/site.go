package suxinvideo

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

type Site struct{}
type HomeReq struct {
	g.Meta `path:"/" method:"get" noValApi:"1"`
}
type HomeRes struct{}
type TypeReq struct {
	g.Meta `path:"/type" method:"get" noValApi:"1"`
	ID     int64  `p:"id"`
	Page   int    `p:"page"`
	Class  string `p:"class"`
	Year   string `p:"year"`
	Area   string `p:"area"`
	Order  string `p:"order"`
}
type TypeRes struct{}
type TypePrettyReq struct {
	g.Meta `path:"/type/:id" method:"get" noValApi:"1"`
	ID     int64 `p:"id"`
	Page   int   `p:"page"`
}
type TypePrettyRes struct{}
type SearchReq struct {
	g.Meta `path:"/search" method:"get" noValApi:"1"`
	WD     string `p:"wd"`
	Page   int    `p:"page"`
}
type SearchRes struct{}

var searchLimit = struct {
	sync.Mutex
	entries map[string]struct {
		start int64
		count int
	}
}{entries: map[string]struct {
	start int64
	count int
}{}}

func searchAllowed(ctx context.Context, ip string) bool {
	if setting(ctx, "search_limit_enable", "0") != "1" {
		return true
	}
	window := gconv.Int64(setting(ctx, "search_limit_window", "60"))
	times := gconv.Int(setting(ctx, "search_limit_times", "10"))
	if window < 1 || window > 3600 {
		window = 60
	}
	if times < 1 || times > 1000 {
		times = 10
	}
	now := time.Now().Unix()
	searchLimit.Lock()
	defer searchLimit.Unlock()
	if len(searchLimit.entries) > 10000 {
		for key, item := range searchLimit.entries {
			if now-item.start >= window {
				delete(searchLimit.entries, key)
			}
		}
	}
	entry := searchLimit.entries[ip]
	if now-entry.start >= window {
		entry.start, entry.count = now, 0
	}
	entry.count++
	searchLimit.entries[ip] = entry
	return entry.count <= times
}

type DetailReq struct {
	g.Meta `path:"/detail" method:"get" noValApi:"1"`
	ID     int64 `p:"id"`
}
type DetailRes struct{}
type DetailPrettyReq struct {
	g.Meta `path:"/detail/:id" method:"get" noValApi:"1"`
	ID     int64 `p:"id"`
}
type DetailPrettyRes struct{}
type PlayReq struct {
	g.Meta  `path:"/play" method:"get" noValApi:"1"`
	ID      int64  `p:"id"`
	Source  int    `p:"source"`
	Line    string `p:"line"`
	Episode int    `p:"episode"`
}
type PlayRes struct{}
type ArticleReq struct {
	g.Meta `path:"/article" method:"get" noValApi:"1"`
	ID     int64 `p:"id"`
}
type ArticleRes struct{}
type ArticlePrettyReq struct {
	g.Meta `path:"/article/:id" method:"get" noValApi:"1"`
	ID     int64 `p:"id"`
}
type ArticlePrettyRes struct{}
type ArticlesReq struct {
	g.Meta `path:"/articles" method:"get" noValApi:"1"`
	Page   int `p:"page"`
}
type ArticlesRes struct{}
type TopicReq struct {
	g.Meta `path:"/topic" method:"get" noValApi:"1"`
	ID     int64 `p:"id"`
}
type TopicRes struct{}
type TopicPrettyReq struct {
	g.Meta `path:"/topic/:id" method:"get" noValApi:"1"`
	ID     int64 `p:"id"`
}
type TopicPrettyRes struct{}
type SitemapReq struct {
	g.Meta `path:"/sitemap" method:"get" noValApi:"1"`
}
type SitemapRes struct{}
type RSSReq struct {
	g.Meta `path:"/rss" method:"get" noValApi:"1"`
}
type RSSRes struct{}

func (*Site) Home(ctx context.Context, _ *HomeReq) (*HomeRes, error) {
	return renderHome(ctx)
}
func (*Site) Type(ctx context.Context, req *TypeReq) (*TypeRes, error) {
	return renderType(ctx, req)
}
func (*Site) Search(ctx context.Context, req *SearchReq) (*SearchRes, error) {
	return renderSearch(ctx, req)
}
func (*Site) Detail(ctx context.Context, req *DetailReq) (*DetailRes, error) {
	_, err := renderPlay(ctx, &PlayReq{ID: req.ID})
	return &DetailRes{}, err
}
func (*Site) Play(ctx context.Context, req *PlayReq) (*PlayRes, error) {
	return renderPlay(ctx, req)
}
func (*Site) Article(ctx context.Context, req *ArticleReq) (*ArticleRes, error) {
	article, err := one(ctx, "SELECT * FROM sx_article WHERE id=? AND status=1", req.ID)
	if err != nil {
		return nil, err
	}
	if article == nil {
		notFound(ctx)
		return &ArticleRes{}, nil
	}
	types, _ := all(ctx, "SELECT id,name FROM sx_type WHERE pid=0 AND status=1 ORDER BY sort,id LIMIT 30")
	render(ctx, gconv.String(article["title"]), types, articleThemeBody, map[string]any{"Article": article})
	return &ArticleRes{}, nil
}
func (*Site) Articles(ctx context.Context, req *ArticlesReq) (*ArticlesRes, error) {
	articles, err := all(ctx, "SELECT id,title,addtime FROM sx_article WHERE status=1 ORDER BY id DESC LIMIT 30 OFFSET ?", (clampPage(req.Page)-1)*30)
	if err != nil {
		return nil, err
	}
	types, _ := all(ctx, "SELECT id,name FROM sx_type WHERE pid=0 AND status=1 ORDER BY sort,id LIMIT 30")
	render(ctx, "????", types, articlesThemeBody, map[string]any{"Articles": articles})
	return &ArticlesRes{}, nil
}
func (*Site) Topic(ctx context.Context, req *TopicReq) (*TopicRes, error) {
	return renderTopic(ctx, req)
}
func (*Site) Sitemap(ctx context.Context, _ *SitemapReq) (*SitemapRes, error) {
	vods, err := all(ctx, "SELECT id FROM sx_vod WHERE "+publicVodListingCondition(ctx, "")+" ORDER BY updatetime DESC LIMIT 5000")
	if err != nil {
		return nil, err
	}
	r := g.RequestFromCtx(ctx)
	r.Response.Header().Set("Content-Type", "application/xml; charset=utf-8")
	base := requestBase(r)
	r.Response.Write("<?xml version=\"1.0\" encoding=\"UTF-8\"?><urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">")
	detailPrefix := "/suxinvideo/detail?id="
	if setting(ctx, "rewrite_enable", "0") == "1" {
		detailPrefix = "/suxinvideo/detail/"
	}
	for _, v := range vods {
		r.Response.Write("<url><loc>" + xmlEscape(base+detailPrefix+gconv.String(v["id"])) + "</loc></url>")
	}
	r.Response.Write("</urlset>")
	return &SitemapRes{}, nil
}
func (*Site) RSS(ctx context.Context, _ *RSSReq) (*RSSRes, error) {
	vods, err := all(ctx, "SELECT id,name,content,updatetime FROM sx_vod WHERE "+publicVodListingCondition(ctx, "")+" ORDER BY updatetime DESC LIMIT 50")
	if err != nil {
		return nil, err
	}
	r := g.RequestFromCtx(ctx)
	r.Response.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	r.Response.Write("<?xml version=\"1.0\" encoding=\"UTF-8\"?><rss version=\"2.0\"><channel><title>速信影视CMS</title><link>" + xmlEscape(requestBase(r)) + "</link>")
	detailPrefix := "/suxinvideo/detail?id="
	if setting(ctx, "rewrite_enable", "0") == "1" {
		detailPrefix = "/suxinvideo/detail/"
	}
	for _, v := range vods {
		r.Response.Write("<item><title>" + xmlEscape(gconv.String(v["name"])) + "</title><link>" + xmlEscape(requestBase(r)+detailPrefix+gconv.String(v["id"])) + "</link><description>" + xmlEscape(gconv.String(v["content"])) + "</description></item>")
	}
	r.Response.Write("</channel></rss>")
	return &RSSRes{}, nil
}

func (s *Site) DetailPretty(ctx context.Context, req *DetailPrettyReq) (*DetailPrettyRes, error) {
	_, err := s.Detail(ctx, &DetailReq{ID: req.ID})
	return &DetailPrettyRes{}, err
}
func (s *Site) TypePretty(ctx context.Context, req *TypePrettyReq) (*TypePrettyRes, error) {
	_, err := s.Type(ctx, &TypeReq{ID: req.ID, Page: req.Page})
	return &TypePrettyRes{}, err
}
func (s *Site) ArticlePretty(ctx context.Context, req *ArticlePrettyReq) (*ArticlePrettyRes, error) {
	_, err := s.Article(ctx, &ArticleReq{ID: req.ID})
	return &ArticlePrettyRes{}, err
}
func (s *Site) TopicPretty(ctx context.Context, req *TopicPrettyReq) (*TopicPrettyRes, error) {
	_, err := s.Topic(ctx, &TopicReq{ID: req.ID})
	return &TopicPrettyRes{}, err
}

type episode struct{ Name, URL string }
type source struct {
	Code         string
	Name         string
	Parse        string
	Episodes     []episode
	OwnerVodID   int64
	BaseCode     string
	VersionLabel string
	VersionKey   string
}

func hydratePlayers(ctx context.Context, vod row, sources []source) ([]source, error) {
	rows, err := all(ctx, "SELECT code,name,`parse`,status FROM sx_player")
	if err != nil {
		return nil, err
	}
	players := map[string]row{}
	for _, item := range rows {
		players[gconv.String(item["code"])] = item
	}
	collectors, err := all(ctx, "SELECT id,name,api_url,status FROM sx_collect_api")
	if err != nil {
		return nil, err
	}
	return vodAliasAccessibleSources(ctx, vod, sources, players, collectors)
}

func preferredSource(sources []source) int {
	for i, item := range sources {
		if item.Parse != "" {
			continue
		}
		if len(item.Episodes) == 0 {
			continue
		}
		parsed, err := url.Parse(item.Episodes[0].URL)
		if err != nil {
			continue
		}
		path := strings.ToLower(parsed.Path)
		if (parsed.Scheme == "http" || parsed.Scheme == "https") && (strings.HasSuffix(path, ".m3u8") || strings.HasSuffix(path, ".mp4")) {
			return i
		}
	}
	return 0
}

func playlist(v row) []source {
	names := strings.Split(gconv.String(v["play_from"]), "$$$")
	parts := strings.Split(gconv.String(v["play_url"]), "$$$")
	out := []source{}
	for i, p := range parts {
		if strings.TrimSpace(p) == "" {
			continue
		}
		name := "线路" + strconv.Itoa(i+1)
		if i < len(names) && names[i] != "" {
			name = names[i]
		}
		s := source{Code: name, Name: name}
		for _, item := range strings.Split(p, "#") {
			pair := strings.SplitN(item, "$", 2)
			if len(pair) != 2 || strings.TrimSpace(pair[1]) == "" {
				continue
			}
			s.Episodes = append(s.Episodes, episode{pair[0], pair[1]})
		}
		if len(s.Episodes) > 0 {
			out = append(out, s)
		}
	}
	return out
}
func clampPage(n int) int {
	if n < 1 {
		return 1
	}
	if n > 10000 {
		return 10000
	}
	return n
}
func notFound(ctx context.Context) {
	g.RequestFromCtx(ctx).Response.WriteStatus(http.StatusNotFound, "内容不存在")
}
func requestBase(r *ghttp.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
func cmsClientIP(ctx context.Context, r *ghttp.Request) string {
	if setting(ctx, "cdn_mode", "0") == "1" {
		for _, header := range []string{"CF-Connecting-IP", "X-Real-IP"} {
			candidate := strings.TrimSpace(r.Header.Get(header))
			if net.ParseIP(candidate) != nil {
				return candidate
			}
		}
		for _, candidate := range strings.Split(r.Header.Get("X-Forwarded-For"), ",") {
			candidate = strings.TrimSpace(candidate)
			if ip := net.ParseIP(candidate); ip != nil && !ip.IsPrivate() && !ip.IsLoopback() {
				return candidate
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && net.ParseIP(host) != nil {
		return host
	}
	return r.GetClientIp()
}
func xmlEscape(v string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(v))
	return b.String()
}

var pageTemplate = template.Must(template.New("page").Funcs(template.FuncMap{"scoreLabel": scoreLabel, "scoreSource": scoreSource, "scoreAvailable": scoreAvailable, "vodURL": siteVodURL, "navURL": siteNavURL, "str": func(v any) string { return gconv.String(v) }, "pic": func(v any) string {
	s := gconv.String(v)
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "/") {
		return s
	}
	if s == "" {
		return "/suxinvideo/asset?theme=suxinlite&file=nopic.svg"
	}
	return "/" + s
}}).Parse(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Title}} - {{.SiteName}}</title><meta name="description" content="{{.SiteDescription}}"><meta name="keywords" content="{{.SiteKeywords}}"><link rel="icon" href="{{.SiteFavicon}}"><link rel="apple-touch-icon" href="{{.SiteLogo}}"><link rel="stylesheet" href="/suxinvideo/asset?theme={{.Theme}}&file=main.css&v=2.3.29"><style>body{margin:0}.sx-shell{max-width:1240px;margin:auto;padding:24px}.sx-nav{display:flex;gap:18px;align-items:center;flex-wrap:wrap;padding:18px 24px}.sx-nav a{text-decoration:none;color:inherit}.sx-nav form{margin-left:auto}.sx-nav input{padding:8px 12px;border-radius:8px}.sx-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(160px,1fr));gap:20px}.sx-card{text-decoration:none;color:inherit}.sx-card img{width:100%;aspect-ratio:2/3;object-fit:cover;border-radius:12px}.sx-card strong{display:block;margin-top:8px}.hero{padding:48px 24px;border-radius:18px;background:linear-gradient(120deg,#151727,#a12534);color:white}.detail{display:flex;gap:28px}.poster{width:230px;object-fit:cover}.episodes{display:flex;gap:10px;flex-wrap:wrap}.episodes a,.button{padding:9px 14px;border-radius:7px;background:#d22d38;color:#fff;text-decoration:none}.player video{width:100%;max-height:70vh;background:#000}.pages{display:flex;justify-content:center;gap:20px;margin:32px}@media(max-width:600px){.detail{display:block}.poster{max-width:50%}}</style></head><body class="sx-theme-{{.Theme}} sx-mode-{{.SiteMode}}"><header class="sx-nav"><a href="/suxinvideo"><b>{{if .SiteLogo}}<img src="{{.SiteLogo}}" alt="" style="width:30px;height:30px;object-fit:contain;vertical-align:middle;margin-right:8px">{{end}}{{.SiteName}}</b></a><a href="/suxinvideo/live">直播</a><a href="/suxinvideo">首页</a>{{range .NavTypes}}<a href="{{navURL .}}">{{.name}}</a>{{end}}<form action="/suxinvideo/search"><input name="wd" placeholder="搜索影片"><button>搜索</button></form><a href="/suxinvideo/app-download">APP下载</a><a href="/suxinvideo/login">登录</a></header><main class="sx-shell">{{.Body}}</main><footer class="sx-shell"><a href="/suxinvideo/sitemap">站点地图</a> · <a href="/suxinvideo/rss">RSS</a> · {{.SiteName}}</footer></body></html>{{define "cards"}}<div class="sx-grid">{{range .}}<a class="sx-card" href="{{vodURL .}}"><img loading="lazy" src="{{pic .pic}}" alt="{{.name}}"><strong>{{.name}}</strong><small>{{.year}} {{.remarks}}</small></a>{{else}}<p>暂无影片</p>{{end}}</div>{{end}}`))

func render(ctx context.Context, title string, types []row, body string, extra map[string]any) {
	types = visibleCategories(ctx, types)
	r := g.RequestFromCtx(ctx)
	theme := cleanTheme(setting(ctx, "site_template", "suxinlite"))
	mode := setting(ctx, "site_mode", "cms")
	if mode != "cms" && mode != "waterfall" && mode != "movie" {
		mode = "cms"
	}
	navTypes := types
	remoteNavigation := false
	if setting(ctx, "yqk_navigation_enable", "1") == "1" {
		if remoteTypes, available := yqkSiteNavigation(ctx); available && len(remoteTypes) > 0 {
			navTypes, remoteNavigation = remoteTypes, true
		}
	}
	if len(navTypes) > 8 {
		navTypes = navTypes[:8]
	}
	navChildren := make(map[int64][]row)
	if remoteNavigation {
		if taxonomy, err := loadLocalChannelTaxonomy(ctx); err == nil {
			navChildren = localChannelChildren(taxonomy)
		}
	} else {
		if children, err := all(ctx, "SELECT id,name,pid FROM sx_type WHERE pid>0 AND status=1 ORDER BY sort,id LIMIT 300"); err == nil {
			for _, child := range visibleCategories(ctx, children) {
				parentID := gconv.Int64(child["pid"])
				navChildren[parentID] = append(navChildren[parentID], child)
			}
		}
	}
	navItems := make([]map[string]any, 0, len(navTypes))
	for _, navType := range navTypes {
		navItems = append(navItems, map[string]any{"ID": navType["id"], "Name": navType["name"], "URL": siteNavURL(navType), "Children": navChildren[gconv.Int64(navType["id"])]})
	}
	favicon := brandSetting(ctx, "site_favicon")
	if favicon == "" {
		favicon = "/suxinvideo/asset?theme=" + url.QueryEscape(theme) + "&file=favicon.svg"
	}
	data := map[string]any{"Title": title, "SiteName": setting(ctx, "site_name", "速信影视CMS"), "SiteLogo": brandSetting(ctx, "site_logo"), "DefaultAvatar": brandSetting(ctx, "user_default_avatar"), "SiteFavicon": favicon, "SiteDescription": setting(ctx, "site_description", "速信影视CMS"), "SiteKeywords": setting(ctx, "site_keywords", ""), "SiteICP": setting(ctx, "site_icp", ""), "SiteMode": mode, "Theme": theme, "Types": types, "NavTypes": navTypes, "NavItems": navItems}
	for k, v := range extra {
		data[k] = v
	}
	if theme == "guoguo" {
		body = guoguoBody(body)
		data["PageKind"] = "library"
		if body != guoguoHomeBody {
			data["PageKind"] = "content"
		}
		if strings.Contains(body, `id="playerPanel"`) {
			data["PageKind"] = "play"
		}
		data["Hot"] = guoguoRanked(gconv.Maps(data["Hot"]), 12)
		if _, supplied := data["Logged"]; !supplied {
			user, err := currentUser(ctx)
			data["Logged"] = err == nil && user != nil
		}
	}
	prepareVodScores(ctx, data)
	preparePictures(ctx, data)
	if theme == "guoguo" {
		// Copy after signing artwork so all card and ranking images use the
		// same CMS image proxy instead of expiring provider URLs.
		data["GuoguoRecent"] = guoguoRanked(gconv.Maps(data["Recent"]), 12)
		ranking := gconv.Maps(data["GuoguoRankingVods"])
		if len(ranking) == 0 {
			ranking = gconv.Maps(data["Hot"])
		}
		data["GuoguoRanking"] = guoguoRanked(ranking, 6)
	}
	base := pageTemplate
	if themed, ok := themePages[theme]; ok {
		base = themed
	}
	fragment, err := base.Clone()
	if err != nil {
		writeCMSError(ctx, err)
		return
	}
	fragment, err = fragment.New("body").Parse(body)
	if err != nil {
		writeCMSError(ctx, err)
		return
	}
	var buf strings.Builder
	if err = fragment.ExecuteTemplate(&buf, "body", data); err != nil {
		writeCMSError(ctx, err)
		return
	}
	bodyHTML := buf.String()
	path := strings.TrimSuffix(r.URL.Path, "/")
	if path == "/suxinvideo" && setting(ctx, "ad_home_enable", "0") == "1" {
		bodyHTML = setting(ctx, "ad_home_code", "") + bodyHTML
	}
	if path == "/suxinvideo/play" {
		if setting(ctx, "ad_playtop_enable", "0") == "1" {
			bodyHTML = setting(ctx, "ad_playtop_code", "") + bodyHTML
		}
		if setting(ctx, "ad_playbottom_enable", "0") == "1" {
			bodyHTML += setting(ctx, "ad_playbottom_code", "")
		}
	}
	if setting(ctx, "ad_footer_enable", "0") == "1" {
		bodyHTML += setting(ctx, "ad_footer_code", "")
	}
	data["Body"] = template.HTML(bodyHTML)
	var page strings.Builder
	if err = fragment.ExecuteTemplate(&page, "page", data); err != nil {
		writeCMSError(ctx, err)
		return
	}
	r.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
	r.Response.Header().Set("Cache-Control", "no-store")
	htmlPage := page.String()
	if theme == "suxinpro" {
		if setting(ctx, "kp_icon_search", "1") != "1" {
			htmlPage = strings.Replace(htmlPage, `<form class="hsearch"`, `<form class="hsearch" style="display:none"`, 1)
		}
		if setting(ctx, "kp_icon_history", "1") == "1" {
			htmlPage = strings.Replace(htmlPage, `<a href="/suxinvideo/login">登录</a></header>`, `<a href="/suxinvideo/history">历史</a><a href="/suxinvideo/login">登录</a></header>`, 1)
		}
		if setting(ctx, "kp_icon_user", "1") != "1" {
			htmlPage = strings.Replace(htmlPage, `<a href="/suxinvideo/login">登录</a></header>`, `</header>`, 1)
		}
	}
	if theme != "suxinlite" && setting(ctx, "browser_check_enable", "0") == "1" {
		agent := strings.ToLower(r.UserAgent())
		if strings.Contains(agent, "micromessenger") || strings.Contains(agent, "qq/") {
			overlay := `<div id="sx-brmask" style="position:fixed;inset:0;z-index:99999;background:rgba(8,9,11,.97);display:flex;align-items:center;justify-content:center;padding:24px"><div style="max-width:420px;color:#fff;text-align:center"><h2>请在浏览器中打开本站</h2><p>请复制网址到其他浏览器打开</p><button onclick="navigator.clipboard.writeText(location.href)" style="padding:12px;background:#e5322d;color:#fff;border:0;border-radius:8px">一键复制网址</button></div></div>`
			htmlPage = strings.Replace(htmlPage, "</body>", overlay+"</body>", 1)
		}
	}
	if setting(ctx, "rewrite_enable", "0") == "1" {
		for _, name := range []string{"detail", "type", "article", "topic"} {
			htmlPage = strings.ReplaceAll(htmlPage, `href="/suxinvideo/`+name+`?id=`, `href="/suxinvideo/`+name+`/`)
		}
	}
	r.Response.Write(htmlPage)
}
func writeCMSError(ctx context.Context, err error) {
	g.Log().Error(ctx, "CMS rendering:", err)
	r := g.RequestFromCtx(ctx)
	if setting(ctx, "debug", "0") == "1" {
		r.Response.WriteStatus(http.StatusInternalServerError, html.EscapeString(err.Error()))
	} else {
		r.Response.WriteStatus(http.StatusInternalServerError, "页面暂时不可用")
	}
}

var _ = fmt.Sprintf
var _ = url.QueryEscape
var _ = time.Now
