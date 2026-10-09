package suxinvideo

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

type catalogLink struct {
	Name   string
	URL    string
	Active bool
}

const catalogCards = `{{define "catalogCards"}}<div class="{{if eq .Theme "guoguo"}}replica-card-grid{{else if eq .Theme "suxinlite"}}mgrid{{else if eq .SiteMode "movie"}}mgrid{{else if eq .Theme "iqiyi"}}iqgrid{{else}}grid g6{{end}}" id="wfGrid">{{range .Vods}}
{{if eq $.Theme "guoguo"}}{{template "guoguoCard" .}}{{else if or (eq $.Theme "suxinlite") (eq $.SiteMode "movie")}}<a class="mcard" href="{{vodURL .}}"><div class="pic"><img src="{{pic .pic}}" loading="lazy" alt="{{.name}}" onerror="this.onerror=null;this.src='/suxinvideo/asset?theme=suxinlite&file=nopic.svg'"><span class="bd" title="{{scoreSource .}}">{{scoreLabel .}}</span><span class="rm">{{.remarks}}</span>{{if .vip}}<span class="vip">vip</span>{{end}}</div><div class="nm">{{.name}}</div><div class="ds">{{.area}} · {{.year}}</div></a>
{{else if eq $.Theme "iqiyi"}}<a class="iqc" href="{{vodURL .}}"><div class="pic"><img src="{{pic .pic}}" loading="lazy" alt="{{.name}}" onerror="this.onerror=null;this.src='/suxinvideo/asset?theme=iqiyi&file=nopic.svg'"><span class="tag">{{.year}}</span>{{if .vip}}<span class="vip">vip专享</span>{{end}}</div><div class="nm">{{.name}}</div><div class="st">{{.remarks}}</div></a>
{{else}}<a class="vcard land" href="{{vodURL .}}"><div class="pic"><img src="{{pic .pic}}" loading="lazy" alt="{{.name}}" onerror="this.onerror=null;this.src='/suxinvideo/asset?theme=suxinpro&file=nopic.svg'"><span class="rm">{{.remarks}}</span></div><div class="nm">{{.name}}</div><div class="ds">{{.year}}</div></a>{{end}}
{{else}}<p class="sx-empty">暂无影片</p>{{end}}</div>{{end}}` + guoguoCard

const catalogTypeBody = `{{define "body"}}<div class="filter"><div class="frow"><b>分类</b>{{range .Categories}}<a class="{{if .Active}}on{{end}}" href="{{.URL}}">{{.Name}}</a>{{end}}</div><div class="frow"><b>排序</b>{{range .Orders}}<a class="{{if .Active}}on{{end}}" href="{{.URL}}">{{.Name}}</a>{{end}}</div><form class="frow sx-extra-filter" method="get" action="/suxinvideo/type"><input type="hidden" name="id" value="{{.ID}}"><input type="hidden" name="order" value="{{.Order}}"><b>筛选</b><input name="class" value="{{.Class}}" placeholder="类型"><input name="year" value="{{.Year}}" placeholder="年份"><input name="area" value="{{.Area}}" placeholder="地区"><button class="btn-main">应用</button></form></div><p class="sx-count">{{.Title}} · 共 {{.Total}} 部影片</p>{{template "catalogCards" .}}<nav class="pages">{{if .PrevURL}}<a href="{{.PrevURL}}">上一页</a>{{end}}<span>第 {{.Page}} / {{.Pages}} 页</span>{{if .NextURL}}<a href="{{.NextURL}}">下一页</a>{{end}}</nav>{{end}}` + catalogCards
const catalogSearchBody = `{{define "body"}}<form class="filter" action="/suxinvideo/search" method="get"><div class="frow"><input class="sx-input" type="search" name="wd" value="{{.Word}}" placeholder="输入影片名称" required><button class="btn-main">搜索</button></div></form>{{if .Word}}<p class="sx-count">找到 {{.Total}} 部与“{{.Word}}”相关的影片</p>{{else}}<div class="sx-hot"><h3>热门搜索</h3>{{range .Hot}}<a href="/suxinvideo/search?wd={{urlquery .name}}">{{.name}}</a>{{end}}</div>{{end}}{{template "catalogCards" .}}<nav class="pages">{{if .PrevURL}}<a href="{{.PrevURL}}">上一页</a>{{end}}<span>第 {{.Page}} / {{.Pages}} 页</span>{{if .NextURL}}<a href="{{.NextURL}}">下一页</a>{{end}}</nav>{{end}}` + catalogCards

func catalogURL(path string, values url.Values) string { return path + "?" + values.Encode() }
func catalogQuery(req *TypeReq, page int) url.Values {
	values := url.Values{"id": {strconv.FormatInt(req.ID, 10)}, "order": {req.Order}, "page": {strconv.Itoa(page)}}
	if req.Class != "" {
		values.Set("class", req.Class)
	}
	if req.Year != "" {
		values.Set("year", req.Year)
	}
	if req.Area != "" {
		values.Set("area", req.Area)
	}
	return values
}

func renderType(ctx context.Context, req *TypeReq) (*TypeRes, error) {
	page := clampPage(req.Page)
	allTypes, err := all(ctx, "SELECT id,name FROM sx_type WHERE pid=0 AND status=1 ORDER BY sort,id LIMIT 30")
	if err != nil {
		return nil, err
	}
	allTypes = visibleCategories(ctx, allTypes)
	orderMap := map[string]string{"time": "updatetime DESC,id DESC", "hits": "total_hits DESC,id DESC", "score": "score DESC,id DESC", "new": "addtime DESC,id DESC"}
	if _, ok := orderMap[req.Order]; !ok {
		req.Order = "time"
	}
	conditions := []string{publicVodListingCondition(ctx, "v")}
	args := []any{}
	title := "影片库"
	if req.ID > 0 {
		category, e := one(ctx, "SELECT id,name FROM sx_type WHERE id=? AND status=1", req.ID)
		if e != nil {
			return nil, e
		}
		if category == nil || !contentCategoryAllowed(ctx, category) {
			notFound(ctx)
			return &TypeRes{}, nil
		}
		title = gconv.String(category["name"])
		conditions = append(conditions, "(v.type_id=? OR v.type_id IN (SELECT id FROM sx_type WHERE pid=? AND status=1))")
		args = append(args, req.ID, req.ID)
	}
	for _, filter := range []struct{ name, value string }{{"class", req.Class}, {"year", req.Year}, {"area", req.Area}} {
		if len([]rune(filter.value)) > 30 {
			badRequest(g.RequestFromCtx(ctx), "筛选条件过长")
			return &TypeRes{}, nil
		}
		if strings.TrimSpace(filter.value) != "" {
			conditions = append(conditions, "v."+filter.name+" LIKE ?")
			args = append(args, "%"+strings.TrimSpace(filter.value)+"%")
		}
	}
	where := strings.Join(conditions, " AND ")
	count, err := one(ctx, "SELECT COUNT(*) AS n FROM sx_vod v WHERE "+where, args...)
	if err != nil {
		return nil, err
	}
	total := gconv.Int(count["n"])
	queryArgs := append(append([]any{}, args...), (page-1)*24)
	vods, err := all(ctx, "SELECT v.id,v.name,v.pic,v.remarks,v.year,v.score,v.area,v.vip FROM sx_vod v WHERE "+where+" ORDER BY v."+orderMap[req.Order]+" LIMIT 24 OFFSET ?", queryArgs...)
	if err != nil {
		return nil, err
	}
	categories := []catalogLink{{Name: "全部", URL: catalogURL("/suxinvideo/type", func() url.Values { q := catalogQuery(req, 1); q.Set("id", "0"); return q }()), Active: req.ID == 0}}
	for _, t := range allTypes {
		q := catalogQuery(req, 1)
		q.Set("id", gconv.String(t["id"]))
		categories = append(categories, catalogLink{Name: gconv.String(t["name"]), URL: catalogURL("/suxinvideo/type", q), Active: gconv.Int64(t["id"]) == req.ID})
	}
	orders := []catalogLink{}
	for _, item := range []struct{ key, label string }{{"time", "最近更新"}, {"hits", "最热"}, {"score", "评分"}, {"new", "最新上架"}} {
		q := catalogQuery(req, 1)
		q.Set("order", item.key)
		orders = append(orders, catalogLink{Name: item.label, URL: catalogURL("/suxinvideo/type", q), Active: req.Order == item.key})
	}
	pages := max(1, (total+23)/24)
	prev, next := "", ""
	if page > 1 {
		prev = catalogURL("/suxinvideo/type", catalogQuery(req, page-1))
	}
	if page < pages {
		next = catalogURL("/suxinvideo/type", catalogQuery(req, page+1))
	}
	render(ctx, title, allTypes, catalogTypeBody, map[string]any{"Title": title, "Vods": vods, "Categories": categories, "Orders": orders, "ID": req.ID, "Order": req.Order, "Class": req.Class, "Year": req.Year, "Area": req.Area, "Total": total, "Page": page, "Pages": pages, "PrevURL": prev, "NextURL": next})
	return &TypeRes{}, nil
}

func renderSearch(ctx context.Context, req *SearchReq) (*SearchRes, error) {
	word := strings.TrimSpace(req.WD)
	if len([]rune(word)) > 60 {
		badRequest(g.RequestFromCtx(ctx), "搜索词过长")
		return &SearchRes{}, nil
	}
	if word != "" && !searchAllowed(ctx, cmsClientIP(ctx, g.RequestFromCtx(ctx))) {
		badRequest(g.RequestFromCtx(ctx), "搜索过于频繁，请稍后再试")
		return &SearchRes{}, nil
	}
	types, err := all(ctx, "SELECT id,name FROM sx_type WHERE pid=0 AND status=1 ORDER BY sort,id LIMIT 30")
	if err != nil {
		return nil, err
	}
	page, total := clampPage(req.Page), 0
	vods := []row{}
	hot := []row{}
	if word != "" {
		count, e := one(ctx, "SELECT COUNT(*) n FROM sx_vod WHERE "+publicVodListingCondition(ctx, "")+" AND name LIKE ?", "%"+word+"%")
		if e != nil {
			return nil, e
		}
		total = gconv.Int(count["n"])
		vods, err = all(ctx, "SELECT id,name,pic,remarks,year,score,area,vip FROM sx_vod WHERE "+publicVodListingCondition(ctx, "")+" AND name LIKE ? ORDER BY total_hits DESC,id DESC LIMIT 24 OFFSET ?", "%"+word+"%", (page-1)*24)
	} else {
		hot, err = all(ctx, "SELECT id,name FROM sx_vod WHERE "+publicVodListingCondition(ctx, "")+" ORDER BY total_hits DESC,id DESC LIMIT 10")
		if err == nil {
			vods, err = all(ctx, "SELECT id,name,pic,remarks,year,score,area,vip FROM sx_vod WHERE "+publicVodListingCondition(ctx, "")+" ORDER BY total_hits DESC,id DESC LIMIT 18")
		}
	}
	if err != nil {
		return nil, err
	}
	pages := max(1, (total+23)/24)
	prev, next := "", ""
	if page > 1 {
		prev = catalogURL("/suxinvideo/search", url.Values{"wd": {word}, "page": {strconv.Itoa(page - 1)}})
	}
	if word != "" && page < pages {
		next = catalogURL("/suxinvideo/search", url.Values{"wd": {word}, "page": {strconv.Itoa(page + 1)}})
	}
	render(ctx, "搜索", types, catalogSearchBody, map[string]any{"Word": word, "Vods": vods, "Hot": hot, "Total": total, "Page": page, "Pages": pages, "PrevURL": prev, "NextURL": next})
	return &SearchRes{}, nil
}

var topicIDs = regexp.MustCompile(`\d+`)

func renderTopic(ctx context.Context, req *TopicReq) (*TopicRes, error) {
	types, err := all(ctx, "SELECT id,name FROM sx_type WHERE pid=0 AND status=1 ORDER BY sort,id LIMIT 30")
	if err != nil {
		return nil, err
	}
	topics, err := all(ctx, "SELECT id,name,pic,description FROM sx_topic WHERE status=1 ORDER BY id DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	var selected row
	vods := []row{}
	if req.ID > 0 {
		selected, err = one(ctx, "SELECT id,name,description,content FROM sx_topic WHERE id=? AND status=1", req.ID)
		if err != nil {
			return nil, err
		}
		if selected == nil {
			notFound(ctx)
			return &TopicRes{}, nil
		}
		ids := topicIDs.FindAllString(gconv.String(selected["content"]), 60)
		if len(ids) > 0 {
			query := "SELECT id,name,pic,remarks,year,score,area,vip FROM sx_vod WHERE " + publicVodListingCondition(ctx, "") + " AND id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")"
			args := make([]any, len(ids))
			for i, id := range ids {
				args[i] = id
			}
			vods, err = all(ctx, query, args...)
			if err != nil {
				return nil, err
			}
		}
	}
	const body = `<div class="sec-h"><h3>专题列表</h3></div><div class="tgrid">{{range .Topics}}<a class="tcard" href="/suxinvideo/topic?id={{.id}}"><img src="{{pic .pic}}" alt="{{.name}}" loading="lazy"><b>{{.name}}</b></a>{{else}}<p>暂无专题</p>{{end}}</div>{{if .Selected}}<div class="sec-h"><h3>{{.Selected.name}}</h3></div><p>{{.Selected.description}}</p>{{template "catalogCards" .}}{{end}}` + catalogCards
	title := "专题"
	if selected != nil {
		title = fmt.Sprint(selected["name"])
	}
	render(ctx, title, types, body, map[string]any{"Topics": topics, "Selected": selected, "Vods": vods})
	return &TopicRes{}, nil
}
