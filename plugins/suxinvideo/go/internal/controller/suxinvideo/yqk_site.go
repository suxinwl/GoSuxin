package suxinvideo

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"github.com/suxinwl/GoSuxin/internal/yqksign"
)

const yqkSiteSnapshotPath = "data/suxinvideo-provider/yqk-site.json"

var yqkDefaultSiteChannels = []yqkSiteChannel{{50, "短剧"}, {65, "奈飞Netflix"}, {2, "电影"}, {3, "电视剧"}, {8, "动漫"}, {10, "综艺"}, {56, "高清韩剧"}, {5, "体育"}}

type yqkHomeView struct {
	Recent, Hot []row
	Hero        []homeHero
}

type yqkSiteSnapshot struct {
	Key     string            `json:"key"`
	Updated int64             `json:"updated"`
	Home    yqkSiteRemoteHome `json:"home"`
}

type yqkSitePageCache struct {
	Ready  chan struct{}
	Until  time.Time
	Cursor string
	Data   yqkSiteRemotePage
	Err    error
}

var yqkSiteState = struct {
	sync.Mutex
	loaded, refreshing bool
	key                string
	nextRefresh        time.Time
	snapshot           yqkSiteSnapshot
	pages              map[string]*yqkSitePageCache
}{pages: make(map[string]*yqkSitePageCache)}

var yqkSiteImportSlots = make(chan struct{}, 2)
var yqkSiteImportLocks = func() [64]chan struct{} {
	var locks [64]chan struct{}
	for i := range locks {
		locks[i] = make(chan struct{}, 1)
	}
	return locks
}()
var fetchYQKHomeSnapshot = fetchYQKSiteHome
var fetchYQKChannelPage = fetchYQKSiteChannel

func yqkSiteSource(ctx context.Context) (int64, error) {
	item, err := one(ctx, "SELECT id FROM sx_collect_api WHERE api_url=? AND status=1 ORDER BY id LIMIT 1", yqkSourceURL)
	if err != nil {
		return 0, err
	}
	if item == nil {
		return 0, errors.New("小柒片源已停用")
	}
	return gconv.Int64(item["id"]), nil
}

func yqkSiteKey(ctx context.Context) string {
	sum := sha256.Sum256([]byte(setting(ctx, "yqk_bootstrap_url", yqkDefaultBootstrapURL)))
	return hex.EncodeToString(sum[:])
}

// Render reads a last-good snapshot and starts one refresh in the background.
// Header, login and playback requests never wait for the upstream catalogue.
func yqkSiteHomeSnapshot(ctx context.Context) yqkSiteSnapshot {
	key := yqkSiteKey(ctx)
	yqkSiteState.Lock()
	if !yqkSiteState.loaded {
		yqkSiteState.loaded = true
		if data, err := os.ReadFile(yqkSiteSnapshotPath); err == nil && len(data) < 4<<20 {
			_ = json.Unmarshal(data, &yqkSiteState.snapshot)
		}
	}
	if yqkSiteState.key != key {
		yqkSiteState.key = key
		yqkSiteState.nextRefresh = time.Time{}
	}
	if !yqkSiteState.refreshing && time.Now().After(yqkSiteState.nextRefresh) {
		yqkSiteState.refreshing = true
		go refreshYQKSiteHome(key)
	}
	snapshot := yqkSiteState.snapshot
	yqkSiteState.Unlock()
	if snapshot.Key != key {
		return yqkSiteSnapshot{}
	}
	return snapshot
}

func refreshYQKSiteHome(key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	home, err := fetchYQKHomeSnapshot(ctx)
	snapshot := yqkSiteSnapshot{Key: key, Updated: time.Now().Unix(), Home: home}
	yqkSiteState.Lock()
	yqkSiteState.refreshing = false
	current := yqkSiteState.key == key
	accepted := false
	if current {
		yqkSiteState.nextRefresh = time.Now().Add(time.Minute)
		if err == nil && (len(home.Recent) > 0 || len(home.Hot) > 0 || len(home.Recommended) > 0) {
			if yqkSiteState.snapshot.Key == key {
				previous := yqkSiteState.snapshot.Home
				if len(snapshot.Home.Channels) == 0 {
					snapshot.Home.Channels = previous.Channels
				}
				if len(snapshot.Home.Recent) == 0 {
					snapshot.Home.Recent = previous.Recent
				}
				if len(snapshot.Home.Hot) == 0 {
					snapshot.Home.Hot = previous.Hot
				}
				if len(snapshot.Home.Recommended) == 0 {
					snapshot.Home.Recommended = previous.Recommended
				}
			}
			yqkSiteState.snapshot = snapshot
			accepted = true
			if len(home.PartialErrors) == 0 {
				yqkSiteState.nextRefresh = time.Now().Add(5 * time.Minute)
			}
		}
	}
	yqkSiteState.Unlock()
	if accepted {
		_ = saveYQKSiteSnapshot(snapshot)
	}
}

func saveYQKSiteSnapshot(snapshot yqkSiteSnapshot) error {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	dir := filepath.Dir(yqkSiteSnapshotPath)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, "yqk-site-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), yqkSiteSnapshotPath)
}

func yqkSiteNavigation(ctx context.Context) ([]row, bool) {
	if setting(ctx, "yqk_navigation_enable", "1") != "1" {
		return nil, false
	}
	items := make([]row, 0, len(yqkDefaultSiteChannels))
	for _, channel := range yqkDefaultSiteChannels {
		// Verified APP names define the navigation, while the destination is the
		// local multisource library. Disabling the APP does not erase the menu.
		items = append(items, row{"id": channel.ID, "name": channel.Name, "url": "/suxinvideo/channel?id=" + strconv.Itoa(channel.ID)})
	}
	return items, true
}

func yqkSiteHome(ctx context.Context) yqkHomeView {
	if _, err := yqkSiteSource(ctx); err != nil {
		return yqkHomeView{}
	}
	home := yqkSiteHomeSnapshot(ctx).Home
	view := yqkHomeView{Recent: yqkSiteRows(ctx, home.Recent), Hot: yqkSiteRows(ctx, home.Hot)}
	for _, item := range yqkSiteRows(ctx, home.Recommended) {
		hero := filmHero(item)
		hero.Link = gconv.String(item["link"]) + "&play=1"
		view.Hero = append(view.Hero, hero)
		if len(view.Hero) >= 6 {
			break
		}
	}
	return view
}

func yqkSiteVodSignInput(ctx context.Context, id string) string {
	return "yqk-site-vod:" + yqkSiteKey(ctx) + ":" + id
}

func yqkSiteVodLink(ctx context.Context, id string) string {
	exp := time.Now().Add(2 * time.Hour).Unix()
	values := url.Values{"id": {id}, "exp": {strconv.FormatInt(exp, 10)}, "sig": {signProxy(ctx, yqkSiteVodSignInput(ctx, id), exp)}}
	return "/suxinvideo/yqk/vod?" + values.Encode()
}

// Fresh maps keep preparePictures from mutating shared metadata or caching an
// expired signed image URL. Remote IDs are never mistaken for local film IDs.
func yqkSiteRows(ctx context.Context, input []map[string]any) []row {
	items := make([]row, 0, len(input))
	seen := make(map[string]bool)
	for _, raw := range input {
		id, name := gconv.String(raw["vodId"]), strings.TrimSpace(gconv.String(raw["vodName"]))
		if !yqkPositiveID(id) || name == "" || seen[id] || !contentRemoteAllowed(ctx, raw) {
			continue
		}
		seen[id] = true
		year, kind, area := yqkListMetadata(raw)
		items = append(items, row{"id": 0, "name": cutRunes(name, 120), "pic": gconv.String(raw["coverImg"]), "remarks": cutRunes(gconv.String(raw["remark"]), 60), "year": year, "area": area, "class": kind, "score": gconv.String(raw["score"]), "score_source": "小柒", "content": gconv.String(raw["intro"]), "vip": 0, "link": yqkSiteVodLink(ctx, id)})
		if len(items) >= 1000 {
			break
		}
	}
	return items
}

type YQKChannelReq struct {
	g.Meta `path:"/channel" method:"get" noValApi:"1"`
	ID     int    `p:"id"`
	Topic  int    `p:"topic"`
	Page   int    `p:"page"`
	Order  string `p:"order"`
	Source string `p:"source"`
	Type   int64  `p:"type"`
	Class  string `p:"class"`
	Year   string `p:"year"`
	Area   string `p:"area"`
}
type YQKChannelRes struct{}

func yqkSitePageKey(key string, channel, topic, page int, order string) string {
	return fmt.Sprintf("%s:%d:%d:%d:%s", key, channel, topic, page, order)
}

func yqkSitePage(ctx context.Context, channel, topic, page int, order string) (yqkSiteRemotePage, error) {
	return yqkSitePageForKey(ctx, yqkSiteKey(ctx), channel, topic, page, order)
}

// Called with the cache mutex held. Every cursor page must still belong to
// the current preceding pages; a refreshed first page invalidates its chain.
func yqkSiteCursorLocked(key string, channel, topic, page int, order string) (string, error) {
	if channel != 0 || page <= 1 {
		return "", nil
	}
	previousCursor := ""
	for n := 1; n < page; n++ {
		previous := yqkSiteState.pages[yqkSitePageKey(key, channel, topic, n, order)]
		if previous == nil || previous.Ready != nil || previous.Err != nil || !time.Now().Before(previous.Until) || previous.Data.NextVal == "" || previous.Cursor != previousCursor {
			return "", errors.New("分页已更新，请从第一页重新浏览")
		}
		previousCursor = previous.Data.NextVal
	}
	return previousCursor, nil
}

func yqkSitePageForKey(ctx context.Context, key string, channel, topic, page int, order string) (yqkSiteRemotePage, error) {
	return yqkSitePageForKeyWithFetch(ctx, key, channel, topic, page, order, fetchYQKChannelPage)
}

func yqkSitePageForKeyWithFetch(ctx context.Context, key string, channel, topic, page int, order string, fetch func(context.Context, int, int, int, string, string) (yqkSiteRemotePage, error)) (yqkSiteRemotePage, error) {
	if err := ctx.Err(); err != nil {
		return yqkSiteRemotePage{}, err
	}
	cacheKey := yqkSitePageKey(key, channel, topic, page, order)
	yqkSiteState.Lock()
	cursor, err := yqkSiteCursorLocked(key, channel, topic, page, order)
	if err != nil {
		yqkSiteState.Unlock()
		return yqkSiteRemotePage{}, err
	}
	entry := yqkSiteState.pages[cacheKey]
	if entry != nil && (entry.Cursor != cursor || (!time.Now().Before(entry.Until) && entry.Ready == nil)) {
		entry = nil
	}
	if entry == nil {
		active := 0
		for _, cached := range yqkSiteState.pages {
			if cached.Ready != nil {
				active++
			}
		}
		if active >= 4 {
			yqkSiteState.Unlock()
			return yqkSiteRemotePage{}, errors.New("频道正在加载，请稍后重试")
		}
		if len(yqkSiteState.pages) >= 128 {
			for oldKey, cached := range yqkSiteState.pages {
				if cached.Ready == nil {
					delete(yqkSiteState.pages, oldKey)
					break
				}
			}
		}
		entry = &yqkSitePageCache{Ready: make(chan struct{}), Cursor: cursor}
		yqkSiteState.pages[cacheKey] = entry
		go func() {
			fetchCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			data, err := fetch(fetchCtx, channel, topic, page, order, cursor)
			yqkSiteState.Lock()
			entry.Data, entry.Err = data, err
			entry.Until = time.Now().Add(5 * time.Minute)
			if err != nil {
				entry.Until = time.Now().Add(5 * time.Second)
			}
			ready := entry.Ready
			entry.Ready = nil
			close(ready)
			yqkSiteState.Unlock()
		}()
	}
	ready := entry.Ready
	yqkSiteState.Unlock()
	if ready != nil {
		select {
		case <-ctx.Done():
			return yqkSiteRemotePage{}, ctx.Err()
		case <-ready:
		}
	}
	yqkSiteState.Lock()
	currentCursor, cursorErr := yqkSiteCursorLocked(key, channel, topic, page, order)
	yqkSiteState.Unlock()
	if cursorErr != nil || currentCursor != cursor {
		return yqkSiteRemotePage{}, errors.New("分页已更新，请从第一页重新浏览")
	}
	return entry.Data, entry.Err
}

func yqkChannelURL(channel, topic, page int, order string) string {
	return "/suxinvideo/channel?" + url.Values{"id": {strconv.Itoa(channel)}, "topic": {strconv.Itoa(topic)}, "page": {strconv.Itoa(page)}, "order": {order}, "source": {"remote"}}.Encode()
}

const yqkChannelBody = `<div class="filter"><div class="frow"><b>分类</b>{{range .Categories}}<a class="{{if .Active}}on{{end}}" href="{{.URL}}">{{.Name}}</a>{{end}}</div><div class="frow"><b>片库</b><a href="{{.LocalURL}}">本站片库</a><a class="on" href="{{.FirstURL}}">小柒精选与专题</a></div>{{if .Orders}}<div class="frow"><b>排序</b>{{range .Orders}}<a class="{{if .Active}}on{{end}}" href="{{.URL}}">{{.Name}}</a>{{end}}</div>{{end}}{{if .Topics}}<div class="frow"><b>专题</b>{{range .Topics}}<a class="{{if .Active}}on{{end}}" href="{{.URL}}">{{.Name}}</a>{{end}}</div>{{end}}</div><p class="sx-count">{{.Title}} · {{.Notice}}</p>{{if .Error}}<p class="sx-empty" role="alert">{{.Error}} <a href="{{.FirstURL}}">返回第一页</a></p>{{else}}{{template "catalogCards" .}}<nav class="pages">{{if .PrevURL}}<a href="{{.PrevURL}}">上一页</a>{{end}}<span>第 {{.Page}} 页{{if .ExactPages}} / {{.Pages}} 页{{end}}</span>{{if .NextURL}}<a href="{{.NextURL}}">下一页</a>{{end}}</nav>{{end}}` + catalogCards

func (*Site) YQKChannel(ctx context.Context, req *YQKChannelReq) (*YQKChannelRes, error) {
	if req.Source == "" && req.Topic > 0 {
		req.Source = "remote" // Preserve bookmarked genuine APP topics.
	}
	if req.Source == "" || req.Source == "local" {
		return renderLocalChannel(ctx, req)
	}
	if req.Source != "remote" || req.Type != 0 || req.Class != "" || req.Year != "" || req.Area != "" {
		badRequest(g.RequestFromCtx(ctx), "片库参数无效")
		return &YQKChannelRes{}, nil
	}
	if (req.ID != 0 && !yqkSiteChannelIDs[req.ID]) || req.Topic < 0 || req.Topic > 1000000000 || req.Page < 0 || req.Page > 10000 || (req.Order != "" && req.Order != "time" && req.Order != "hits") {
		badRequest(g.RequestFromCtx(ctx), "频道参数无效")
		return &YQKChannelRes{}, nil
	}
	if req.Order == "" || req.ID != 0 {
		req.Order = "time"
	}
	page := max(1, req.Page)
	title := "最近更新"
	if req.Order == "hits" {
		title = "热播"
	}
	categories := []catalogLink{{Name: "全部", URL: yqkChannelURL(0, 0, 1, req.Order), Active: req.ID == 0}}
	for _, channel := range yqkDefaultSiteChannels {
		categories = append(categories, catalogLink{Name: channel.Name, URL: yqkChannelURL(channel.ID, 0, 1, "time"), Active: req.ID == channel.ID})
		if req.ID == channel.ID {
			title = channel.Name
		}
	}
	orders := []catalogLink{}
	if req.ID == 0 {
		for _, item := range []struct{ key, name string }{{"time", "最近更新"}, {"hits", "热播"}} {
			orders = append(orders, catalogLink{Name: item.name, URL: yqkChannelURL(0, 0, 1, item.key), Active: req.Order == item.key})
		}
	}
	data, err := yqkSiteRemotePage{}, error(nil)
	if _, err = yqkSiteSource(ctx); err == nil {
		data, err = yqkSitePage(ctx, req.ID, req.Topic, page, req.Order)
	}
	extra := map[string]any{"Title": title, "Categories": categories, "Orders": orders, "Page": page, "Pages": data.TotalPages, "ExactPages": req.ID > 0, "FirstURL": yqkChannelURL(req.ID, 0, 1, req.Order), "LocalURL": localChannelURL(&YQKChannelReq{ID: req.ID}, 1), "Notice": data.Notice, "Vods": []row{}}
	if err != nil {
		g.RequestFromCtx(ctx).Response.WriteHeader(http.StatusServiceUnavailable)
		extra["Error"] = "暂时无法加载该频道，请稍后重试。"
	} else {
		extra["Vods"] = yqkSiteRows(ctx, data.Items)
		if len(data.Topics) > 0 {
			topics := []catalogLink{{Name: "精选", URL: yqkChannelURL(req.ID, 0, 1, req.Order), Active: req.Topic == 0}}
			for _, topic := range data.Topics {
				topics = append(topics, catalogLink{Name: topic.Name, URL: yqkChannelURL(req.ID, topic.ID, 1, req.Order), Active: topic.ID == req.Topic})
			}
			extra["Topics"] = topics
		}
		if page > 1 {
			extra["PrevURL"] = yqkChannelURL(req.ID, req.Topic, page-1, req.Order)
		}
		if data.HasNext {
			extra["NextURL"] = yqkChannelURL(req.ID, req.Topic, page+1, req.Order)
		}
	}
	types, _ := all(ctx, "SELECT id,name FROM sx_type WHERE pid=0 AND status=1 ORDER BY sort,id LIMIT 30")
	render(ctx, title, types, yqkChannelBody, extra)
	return &YQKChannelRes{}, nil
}

type YQKSiteVodReq struct {
	g.Meta `path:"/yqk/vod" method:"get" noValApi:"1"`
	ID     string `p:"id"`
	Exp    int64  `p:"exp"`
	Sig    string `p:"sig"`
	Play   int    `p:"play"`
}
type YQKSiteVodRes struct{}

func validYQKSiteVodLink(ctx context.Context, req *YQKSiteVodReq) bool {
	now := time.Now().Unix()
	return (req.Play == 0 || req.Play == 1) && yqkPositiveID(req.ID) && req.Exp >= now && req.Exp <= now+4*3600 && len(req.Sig) == 64 && hmac.Equal([]byte(req.Sig), []byte(signProxy(ctx, yqkSiteVodSignInput(ctx, req.ID), req.Exp)))
}

func yqkSiteRedirect(ctx context.Context, req *YQKSiteVodReq, localID string) {
	page := "detail"
	if req.Play == 1 {
		page = "play"
	}
	g.RequestFromCtx(ctx).Response.RedirectTo("/suxinvideo/"+page+"?id="+localID, http.StatusSeeOther)
}

func yqkSiteVodError(ctx context.Context, status int, message string) {
	types, _ := all(ctx, "SELECT id,name FROM sx_type WHERE pid=0 AND status=1 ORDER BY sort,id LIMIT 30")
	g.RequestFromCtx(ctx).Response.WriteHeader(status)
	body := `<section class="sec"><h1>片源暂不可用</h1><p>{{.Message}}</p><p><a href="/suxinvideo/">返回首页</a></p></section>`
	render(ctx, "片源暂不可用", types, body, map[string]any{"Message": message})
}

func (*Site) YQKSiteVod(ctx context.Context, req *YQKSiteVodReq) (*YQKSiteVodRes, error) {
	r := g.RequestFromCtx(ctx)
	if !validYQKSiteVodLink(ctx, req) {
		yqkSiteVodError(ctx, http.StatusBadRequest, "影片链接无效或已过期，请刷新页面后重试。")
		return &YQKSiteVodRes{}, nil
	}
	local, err := yqkSiteImport(ctx, req.ID)
	if err != nil {
		var known *AppError
		if errors.As(err, &known) && known.Status == http.StatusNotFound {
			notFound(ctx)
		} else if errors.As(err, &known) && known.Status == http.StatusTooManyRequests {
			r.Response.Header().Set("Retry-After", "3")
			yqkSiteVodError(ctx, known.Status, known.Message)
		} else {
			yqkSiteVodError(ctx, http.StatusServiceUnavailable, "暂时无法载入这部影片，请稍后重试。")
		}
		return &YQKSiteVodRes{}, nil
	}
	yqkSiteRedirect(ctx, req, gconv.String(local["id"]))
	return &YQKSiteVodRes{}, nil
}

// The website and native deferred resolver share import limits and moderation.
func yqkSiteImport(ctx context.Context, id string) (row, error) {
	if !yqkPositiveID(id) {
		return nil, appError(http.StatusBadRequest, "影片编号无效")
	}
	apiID, err := yqkSiteSource(ctx)
	if err != nil {
		return nil, appError(http.StatusNotFound, "小柒片源已停用")
	}
	// A local hidden title remains hidden; recent imports can redirect without
	// competing with slow catalogue requests for a network slot.
	local, err := one(ctx, "SELECT id,status,updatetime,name,class,type_id FROM sx_vod WHERE api_id=? AND api_vid=?", apiID, id)
	if err != nil {
		return nil, err
	}
	if err == nil && local != nil {
		if gconv.Int(local["status"]) != 1 || !contentVodAllowed(ctx, local) {
			return nil, appError(http.StatusNotFound, "影片不存在或已下架")
		}
		if time.Now().Unix()-gconv.Int64(local["updatetime"]) < 300 {
			return local, nil
		}
	}
	fetchCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	select {
	case yqkSiteImportSlots <- struct{}{}:
		defer func() { <-yqkSiteImportSlots }()
	default:
		return nil, appError(http.StatusTooManyRequests, "片源正在准备，请稍后重试。")
	}
	sum := sha256.Sum256([]byte(id))
	lock := yqkSiteImportLocks[int(sum[0])%len(yqkSiteImportLocks)]
	select {
	case lock <- struct{}{}:
		defer func() { <-lock }()
	case <-fetchCtx.Done():
		return nil, fetchCtx.Err()
	}
	// Serialise imports of the same remote film. Repeated clicks within five
	// minutes reuse the existing title and preserve its VIP/points rules.
	local, err = one(fetchCtx, "SELECT id,status,updatetime,name,class,type_id FROM sx_vod WHERE api_id=? AND api_vid=?", apiID, id)
	if err != nil {
		return nil, err
	}
	if err == nil && local != nil {
		if gconv.Int(local["status"]) != 1 || !contentVodAllowed(ctx, local) {
			return nil, appError(http.StatusNotFound, "影片不存在或已下架")
		}
		if time.Now().Unix()-gconv.Int64(local["updatetime"]) < 300 {
			return local, nil
		}
	}
	var item map[string]any
	blocked := false
	err = withYQK(fetchCtx, false, func(ctx context.Context, client *yqksign.Client, address string) error {
		detail, e := yqkDetail(ctx, client, address, id)
		if e != nil {
			return e
		}
		yqkDetailMetadata(ctx, client, address, detail)
		item = yqkVodItem(detail, 0, "")
		if !contentMacAllowed(ctx, item) {
			blocked = true
			return errors.New("影片不公开")
		}
		return nil
	})
	if blocked {
		return nil, appError(http.StatusNotFound, "影片不存在或已下架")
	}
	if err == nil {
		var state int
		state, err = upsertMacVod(fetchCtx, apiID, item)
		if err == nil && state == collectVodBlocked {
			return nil, appError(http.StatusNotFound, "影片不存在或已下架")
		}
	}
	if err == nil {
		local, err = one(fetchCtx, "SELECT id,status,name,class,type_id FROM sx_vod WHERE api_id=? AND api_vid=?", apiID, id)
		if err == nil && local == nil {
			name := cutRunes(gconv.String(item["vod_name"]), 120)
			local, err = one(fetchCtx, "SELECT id,status,name,class,type_id FROM sx_vod WHERE (name=? OR name_norm=?) AND play_url LIKE ? ORDER BY id LIMIT 1", name, normalizeVodName(name), "%yqk://"+id+"/%")
		}
	}
	if err != nil || local == nil {
		return nil, appError(http.StatusServiceUnavailable, "暂时无法载入这部影片，请稍后重试。")
	}
	if gconv.Int(local["status"]) != 1 || !contentVodAllowed(ctx, local) {
		return nil, appError(http.StatusNotFound, "影片不存在或已下架")
	}
	return local, nil
}
