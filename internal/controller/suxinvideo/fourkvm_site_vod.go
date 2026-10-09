package suxinvideo

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

var fourKVMSiteSlugPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
var fourKVMSiteSectionPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,39}$`)
var fourKVMSiteImportSlots = make(chan struct{}, 2)
var fourKVMSiteImportLocks = func() [64]chan struct{} {
	var locks [64]chan struct{}
	for i := range locks {
		locks[i] = make(chan struct{}, 1)
	}
	return locks
}()

type FourKVMSiteVodReq struct {
	g.Meta `path:"/4kvm/vod" method:"get" noValApi:"1"`
	ID     string `p:"id"`
	Exp    int64  `p:"exp"`
	Sig    string `p:"sig"`
	Play   int    `p:"play"`
}
type FourKVMSiteVodRes struct{}

func fourKVMSiteVodSignInput(slug string) string { return "4kvm-site-vod:" + slug }

// Links contain a bounded provider identifier, never a foreign redirect or a
// temporary media address. The optional play flag only chooses a CMS page.
func fourKVMSiteVodLink(ctx context.Context, slug string) string {
	if !fourKVMSiteSlugPattern.MatchString(slug) {
		return "/suxinvideo/"
	}
	exp := time.Now().Add(2 * time.Hour).Unix()
	values := url.Values{"id": {slug}, "exp": {strconv.FormatInt(exp, 10)}, "sig": {signProxy(ctx, fourKVMSiteVodSignInput(slug), exp)}}
	return "/suxinvideo/4kvm/vod?" + values.Encode()
}

func validFourKVMSiteVodLinkAt(req *FourKVMSiteVodReq, now int64, sign func(string, int64) string) bool {
	return req != nil && (req.Play == 0 || req.Play == 1) && fourKVMSiteSlugPattern.MatchString(req.ID) &&
		req.Exp >= now && req.Exp <= now+4*3600 && len(req.Sig) == 64 &&
		hmac.Equal([]byte(req.Sig), []byte(sign(fourKVMSiteVodSignInput(req.ID), req.Exp)))
}

func validFourKVMSiteVodLink(ctx context.Context, req *FourKVMSiteVodReq) bool {
	return validFourKVMSiteVodLinkAt(req, time.Now().Unix(), func(value string, exp int64) string { return signProxy(ctx, value, exp) })
}

func fourKVMSiteHasSlug(vod row, slug string) bool {
	for _, src := range playlist(vod) {
		if src.Code != "4kvm" {
			continue
		}
		for _, ep := range src.Episodes {
			if film, valid := fourKVMDiscoveryMarker(ep.URL); valid && film == slug {
				return true
			}
		}
	}
	return false
}

const fourKVMSiteVodColumns = "id,status,name,class,type_id,api_id,api_vid,play_from,play_url"

// The primary collector ID changes neither when a second provider is merged
// nor when title aliases are approved. Read source bindings and verify the
// exact native film marker instead of confusing a remote slug with a local ID.
func findFourKVMSiteVod(ctx context.Context, apiID int64, slug string) (row, error) {
	local, err := one(ctx, "SELECT "+fourKVMSiteVodColumns+" FROM sx_vod WHERE api_id=? AND api_vid=? ORDER BY id LIMIT 1", apiID, slug)
	if err != nil || local != nil {
		return local, err
	}
	candidates, err := all(ctx, "SELECT v.* FROM sx_vod v JOIN sx_vod_source_score s ON s.vod_id=v.id WHERE s.api_id=? AND s.api_vid=? ORDER BY v.id LIMIT 32", apiID, slug)
	if err != nil {
		return nil, err
	}
	for _, film := range candidates {
		if fourKVMSiteHasSlug(film, slug) {
			return film, nil
		}
	}
	// LOCATE has literal semantics even for underscores in permitted slugs.
	// Parse the returned playlists to reject substrings inside labels or a
	// marker from another provider. This is only used on a clicked film.
	candidates, err = all(ctx, "SELECT "+fourKVMSiteVodColumns+" FROM sx_vod WHERE FIND_IN_SET('4kvm',REPLACE(play_from,'$$$',','))>0 AND LOCATE(?,play_url)>0 ORDER BY id LIMIT 64", "4kvm://"+slug+"?")
	if err != nil {
		return nil, err
	}
	for _, film := range candidates {
		if fourKVMSiteHasSlug(film, slug) {
			return film, nil
		}
	}
	return nil, nil
}

func fourKVMSiteVodPublic(ctx context.Context, film row) bool {
	return film != nil && gconv.Int(film["status"]) == 1 && contentVodAllowed(ctx, film)
}

func fourKVMSiteVodRedirect(ctx context.Context, req *FourKVMSiteVodReq, film row) {
	page := "detail"
	if req.Play == 1 {
		page = "play"
	}
	r := g.RequestFromCtx(ctx)
	r.Response.Header().Set("Cache-Control", "no-store")
	r.Response.RedirectTo("/suxinvideo/"+page+"?id="+gconv.String(film["id"]), http.StatusSeeOther)
}

func (*Site) FourKVMSiteVod(ctx context.Context, req *FourKVMSiteVodReq) (*FourKVMSiteVodRes, error) {
	r := g.RequestFromCtx(ctx)
	if !validFourKVMSiteVodLink(ctx, req) {
		yqkSiteVodError(ctx, http.StatusBadRequest, "影片链接无效或已过期，请刷新页面后重试。")
		return &FourKVMSiteVodRes{}, nil
	}
	collector, err := fourKVMSiteSource(ctx)
	if err != nil {
		notFound(ctx)
		return &FourKVMSiteVodRes{}, nil
	}
	apiID := gconv.Int64(collector["id"])
	local, err := findFourKVMSiteVod(ctx, apiID, req.ID)
	if err != nil {
		yqkSiteVodError(ctx, http.StatusServiceUnavailable, "暂时无法载入这部影片，请稍后重试。")
		return &FourKVMSiteVodRes{}, nil
	}
	if local != nil {
		if !fourKVMSiteVodPublic(ctx, local) {
			notFound(ctx)
			return &FourKVMSiteVodRes{}, nil
		}
		if fourKVMSiteHasSlug(local, req.ID) {
			fourKVMSiteVodRedirect(ctx, req, local)
			return &FourKVMSiteVodRes{}, nil
		}
	}
	fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	select {
	case fourKVMSiteImportSlots <- struct{}{}:
		defer func() { <-fourKVMSiteImportSlots }()
	default:
		r.Response.Header().Set("Retry-After", "3")
		yqkSiteVodError(ctx, http.StatusTooManyRequests, "片源正在准备，请稍后重试。")
		return &FourKVMSiteVodRes{}, nil
	}
	sum := sha256.Sum256([]byte(req.ID))
	lock := fourKVMSiteImportLocks[int(sum[0])%len(fourKVMSiteImportLocks)]
	select {
	case lock <- struct{}{}:
		defer func() { <-lock }()
	case <-fetchCtx.Done():
		yqkSiteVodError(ctx, http.StatusServiceUnavailable, "影片准备超时，请稍后重试。")
		return &FourKVMSiteVodRes{}, nil
	}
	// Concurrent clicks of the same slug reuse the first completed import.
	local, err = findFourKVMSiteVod(fetchCtx, apiID, req.ID)
	if err == nil && local != nil {
		if !fourKVMSiteVodPublic(ctx, local) {
			notFound(ctx)
			return &FourKVMSiteVodRes{}, nil
		}
		if fourKVMSiteHasSlug(local, req.ID) {
			fourKVMSiteVodRedirect(ctx, req, local)
			return &FourKVMSiteVodRes{}, nil
		}
	}
	if err == nil {
		current, sourceErr := fourKVMSiteSource(fetchCtx)
		if sourceErr != nil || gconv.Int64(current["id"]) != apiID {
			notFound(ctx)
			return &FourKVMSiteVodRes{}, nil
		}
	}
	var data macPayload
	if err == nil {
		data, err = fetchNativeCollectSourceWith(fetchCtx, "4kvm", url.Values{"ac": {"detail"}, "ids": {req.ID}}, cmsProviders)
	}
	if err == nil && (len(data.List) != 1 || gconv.String(data.List[0]["vod_id"]) != req.ID) {
		err = errors.New("4KVM 影片详情与编号不符")
	}
	if err == nil {
		var state int
		state, err = upsertMacVod(fetchCtx, apiID, data.List[0])
		if err == nil && state == collectVodBlocked {
			notFound(ctx)
			return &FourKVMSiteVodRes{}, nil
		}
	}
	if err == nil {
		local, err = findFourKVMSiteVod(fetchCtx, apiID, req.ID)
	}
	if err != nil || local == nil || !fourKVMSiteHasSlug(local, req.ID) {
		yqkSiteVodError(ctx, http.StatusServiceUnavailable, "暂时无法载入这部影片，请稍后重试。")
		return &FourKVMSiteVodRes{}, nil
	}
	if !fourKVMSiteVodPublic(ctx, local) {
		notFound(ctx)
		return &FourKVMSiteVodRes{}, nil
	}
	fourKVMSiteVodRedirect(ctx, req, local)
	return &FourKVMSiteVodRes{}, nil
}

type FourKVMSiteCatalogReq struct {
	g.Meta  `path:"/4kvm/catalog" method:"get" noValApi:"1"`
	Section string `p:"section"`
}
type FourKVMSiteCatalogRes struct{}

func fourKVMSiteCatalogURL(section string) string {
	return "/suxinvideo/4kvm/catalog?" + url.Values{"section": {section}}.Encode()
}

func (*Site) FourKVMSiteCatalog(ctx context.Context, req *FourKVMSiteCatalogReq) (*FourKVMSiteCatalogRes, error) {
	if req.Section == "" {
		req.Section = "recent"
	}
	if !fourKVMSiteSectionPattern.MatchString(req.Section) {
		badRequest(g.RequestFromCtx(ctx), "片库参数无效")
		return &FourKVMSiteCatalogRes{}, nil
	}
	if _, err := fourKVMSiteSource(ctx); err != nil {
		notFound(ctx)
		return &FourKVMSiteCatalogRes{}, nil
	}
	snapshot := fourKVMSiteSnapshot(ctx)
	var items []row
	title, notice, found := "4KVM 最近更新", "4KVM 首页推荐", false
	categories := []catalogLink{}
	for _, section := range snapshot.Home.Sections {
		categories = append(categories, catalogLink{Name: section.Title, URL: fourKVMSiteCatalogURL(section.Key), Active: section.Key == req.Section})
		if section.Key == req.Section {
			found = true
			title = "4KVM " + section.Title
			notice = section.Subtitle
			items = fourKVMSiteRows(ctx, section.Dramas)
		}
	}
	if !found {
		if req.Section != "recent" && req.Section != "hot" {
			notFound(ctx)
			return &FourKVMSiteCatalogRes{}, nil
		}
		if req.Section == "hot" {
			title = "4KVM 热播"
		}
		items = fourKVMSiteLocalRows(ctx, req.Section, 100)
		if len(categories) == 0 {
			categories = []catalogLink{{Name: "最近更新", URL: fourKVMSiteCatalogURL("recent"), Active: req.Section == "recent"}, {Name: "热播", URL: fourKVMSiteCatalogURL("hot"), Active: req.Section == "hot"}}
		}
		notice = "首页推荐正在更新，先展示已采集的 4KVM 影片"
	}
	types, _ := all(ctx, "SELECT id,name FROM sx_type WHERE pid=0 AND status=1 ORDER BY sort,id LIMIT 30")
	body := `<div class="filter"><div class="frow"><b>4KVM 推荐</b>{{range .Categories}}<a class="{{if .Active}}on{{end}}" href="{{.URL}}">{{.Name}}</a>{{end}}</div><div class="frow"><b>片库</b><a href="/suxinvideo/channel?id=0">本站全部片源</a><a class="on" href="{{.FirstURL}}">4KVM 推荐</a></div></div><p class="sx-count">{{.Title}} · {{.Notice}}</p>{{template "catalogCards" .}}` + catalogCards
	render(ctx, title, types, body, map[string]any{"Title": title, "Categories": categories, "FirstURL": fourKVMSiteCatalogURL(req.Section), "Notice": notice, "Vods": items})
	return &FourKVMSiteCatalogRes{}, nil
}
