package suxinvideo

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

// Channel IDs belong to the APP; category IDs belong to sx_type. They must
// never be compared directly, even when their numeric values happen to match.
type localChannelTaxonomy struct {
	Types    []row
	ByID     map[int64]row
	Channels map[int]map[int64]bool
	Evidence map[int]map[int64]bool
}

func localCategoryChannels(name string) []int {
	name = discoveryNormalizeTitle(name)
	if strings.Contains(name, "netflix") || strings.Contains(name, "奈飞") {
		return []int{65}
	}
	if strings.Contains(name, "韩剧") || name == "韩国剧" {
		return []int{3, 56}
	}
	for _, label := range []string{"体育", "篮球", "足球", "网球", "排球", "羽毛球", "乒乓球", "棒球", "赛车", "高尔夫", "斯诺克"} {
		if name == label || strings.HasSuffix(name, label+"赛事") || strings.HasSuffix(name, label+"直播") {
			return []int{5}
		}
	}
	if strings.Contains(name, "短剧") || name == "古装仙侠" || name == "反转爽剧" || name == "现代言情" || name == "女频恋爱" || name == "脑洞悬疑" || name == "年代穿越" {
		return []int{50}
	}
	if name == "动漫电影" || name == "动画电影" || name == "动画片" {
		return []int{2, 8}
	}
	if strings.Contains(name, "动漫") || name == "动画" || name == "漫剧" {
		return []int{8}
	}
	if strings.Contains(name, "综艺") {
		return []int{10}
	}
	if strings.Contains(name, "纪录") || strings.Contains(name, "预告") {
		return nil
	}
	if name == "电影" || moviePlaybackCategories[name] {
		return []int{2}
	}
	if name == "剧集" || strings.HasSuffix(name, "剧") {
		return []int{3}
	}
	return nil
}

func buildLocalChannelTaxonomy(input []row) localChannelTaxonomy {
	tax := localChannelTaxonomy{ByID: make(map[int64]row), Channels: make(map[int]map[int64]bool), Evidence: make(map[int]map[int64]bool)}
	allTypes := make(map[int64]row, len(input))
	for _, item := range input {
		id := gconv.Int64(item["id"])
		if id > 0 {
			allTypes[id] = item
		}
	}
	for _, item := range input {
		id := gconv.Int64(item["id"])
		if id <= 0 || gconv.Int(item["status"]) != 1 {
			continue
		}
		channels := map[int]bool{}
		visited := map[int64]bool{}
		valid := true
		for current := id; current > 0; {
			parent, exists := allTypes[current]
			if !exists || visited[current] || gconv.Int(parent["status"]) != 1 {
				valid = false
				break
			}
			visited[current] = true
			for _, channel := range localCategoryChannels(gconv.String(parent["name"])) {
				channels[channel] = true
			}
			current = gconv.Int64(parent["pid"])
		}
		if !valid {
			continue
		}
		tax.Types = append(tax.Types, item)
		tax.ByID[id] = item
		for channel := range channels {
			if tax.Channels[channel] == nil {
				tax.Channels[channel] = make(map[int64]bool)
			}
			tax.Channels[channel][id] = true
		}
	}
	return tax
}

func loadLocalChannelTaxonomy(ctx context.Context) (localChannelTaxonomy, error) {
	types, err := all(ctx, "SELECT id,name,pid,status,sort FROM sx_type ORDER BY sort,id LIMIT 5000")
	if err != nil {
		return localChannelTaxonomy{}, err
	}
	allowed := make([]row, 0, len(types))
	for _, item := range types {
		if contentVodAllowed(ctx, row{"name": "", "class": item["name"], "type_id": item["id"]}) {
			allowed = append(allowed, item)
		}
	}
	tax := buildLocalChannelTaxonomy(allowed)
	// Hongguo's source is exclusively short drama. Existing provider ownership
	// supplies evidence for orphan genre categories without rewriting sx_type.
	evidence, err := all(ctx, "SELECT DISTINCT v.type_id FROM sx_vod v JOIN sx_collect_api c ON c.id=v.api_id WHERE c.status=1 AND c.api_url=?", hongguoSourceURL)
	if err != nil {
		return localChannelTaxonomy{}, err
	}
	tax.Evidence[50] = make(map[int64]bool)
	for _, item := range evidence {
		id := gconv.Int64(item["type_id"])
		if tax.ByID[id] != nil {
			tax.Evidence[50][id] = true
		}
	}
	return tax, nil
}

func (tax localChannelTaxonomy) channelContains(channel int, id int64) bool {
	return tax.ByID[id] != nil && (channel == 0 || tax.Channels[channel][id] || tax.Evidence[channel][id])
}

func (tax localChannelTaxonomy) descendants(id int64) []int64 {
	result := []int64{}
	for _, item := range tax.Types {
		candidate := gconv.Int64(item["id"])
		for current := candidate; current > 0; current = gconv.Int64(tax.ByID[current]["pid"]) {
			if current == id {
				result = append(result, candidate)
				break
			}
		}
	}
	return result
}

func localChannelIDs(tax localChannelTaxonomy, channel int) []int64 {
	ids := []int64{}
	for _, item := range tax.Types {
		id := gconv.Int64(item["id"])
		if channel == 0 || tax.Channels[channel][id] {
			ids = append(ids, id)
		}
	}
	return ids
}

func localChannelTypeSQL(ids []int64, args *[]any) string {
	if len(ids) == 0 {
		return "0=1"
	}
	for _, id := range ids {
		*args = append(*args, id)
	}
	return "v.type_id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")"
}

type localChannelQuery struct {
	Where, Order string
	Args         []any
}

func buildLocalChannelQuery(req *YQKChannelReq, tax localChannelTaxonomy, publicCondition string) (localChannelQuery, error) {
	orders := map[string]string{"time": "v.updatetime DESC,v.id DESC", "hits": "v.total_hits DESC,v.id DESC", "score": "v.score DESC,v.id DESC", "new": "v.addtime DESC,v.id DESC"}
	order := req.Order
	if order == "" {
		order = "time"
	}
	orderSQL, valid := orders[order]
	if !valid || req.ID != 0 && !yqkSiteChannelIDs[req.ID] || req.Page < 0 || req.Page > 10000 || req.Type < 0 || req.Topic != 0 {
		return localChannelQuery{}, errors.New("频道参数无效")
	}
	args := []any{}
	category := localChannelTypeSQL(localChannelIDs(tax, req.ID), &args)
	switch req.ID {
	case 0:
		category = "(v.type_id=0 OR " + category + ")"
	case 2:
		category = "(" + category + " OR v.class REGEXP '(^|[,，、/|;； ])(电影|movie|动漫电影|动画电影)([,，、/|;； ]|$)')"
	case 3:
		category = "(" + category + " OR v.class REGEXP '(^|[,，、/|;； ])(电视剧|剧集|韩剧|韩国剧|美剧|日剧|series)([,，、/|;； ]|$)')"
	case 8:
		category = "(" + category + " OR v.class REGEXP '(^|[,，、/|;； ])(动漫|动画|动漫电影|动画电影|anime)([,，、/|;； ]|$)')"
	case 10:
		category = "(" + category + " OR v.class REGEXP '(^|[,，、/|;； ])(综艺|variety)([,，、/|;； ]|$)')"
	case 50:
		category = "(" + category + " OR v.class IN ('短剧','短剧大全','short') OR EXISTS (SELECT 1 FROM sx_collect_api hc WHERE hc.id=v.api_id AND hc.status=1 AND hc.api_url='hongguo://app'))"
	case 65:
		category = "(" + category + " OR v.class REGEXP '(^|[,，、/|;； ])(Netflix|netflix|奈飞)([,，、/|;； ]|$)')"
	case 56:
		series := localChannelTypeSQL(localChannelIDs(tax, 3), &args)
		category = "(" + category + " OR v.class REGEXP '(^|[,，、/|;； ])(韩剧|韩国剧)([,，、/|;； ]|$)' OR ((" + series + " OR v.class REGEXP '(^|[,，、/|;； ])(电视剧|剧集|series)([,，、/|;； ]|$)') AND v.area IN ('韩国','Korea','South Korea','kr')))"
	case 5:
		category = "(" + category + " OR v.class IN ('体育','篮球','足球','sports'))"
	}
	conditions := []string{publicCondition, category}
	if req.ID != 0 {
		// Metadata can place a title in multiple channels; it cannot make an
		// administratively disabled or detached category visible again.
		conditions = append(conditions, "(v.type_id=0 OR "+localChannelTypeSQL(localChannelIDs(tax, 0), &args)+")")
	}
	if req.Type > 0 {
		if !tax.channelContains(req.ID, req.Type) {
			return localChannelQuery{}, errors.New("分类不属于当前频道")
		}
		conditions = append(conditions, localChannelTypeSQL(tax.descendants(req.Type), &args))
	}
	for _, filter := range []struct{ column, value string }{{"class", req.Class}, {"year", req.Year}, {"area", req.Area}} {
		value := strings.TrimSpace(filter.value)
		if len([]rune(value)) > 30 {
			return localChannelQuery{}, errors.New("筛选条件过长")
		}
		if value != "" {
			conditions = append(conditions, "v."+filter.column+" LIKE ?")
			args = append(args, "%"+value+"%")
		}
	}
	return localChannelQuery{Where: strings.Join(conditions, " AND "), Order: orderSQL, Args: args}, nil
}

func localChannelURL(req *YQKChannelReq, page int) string {
	values := url.Values{"id": {strconv.Itoa(req.ID)}, "page": {strconv.Itoa(page)}, "source": {"local"}}
	for key, value := range map[string]string{"order": req.Order, "type": strconv.FormatInt(req.Type, 10), "class": req.Class, "year": req.Year, "area": req.Area} {
		if value != "" && !(key == "type" && req.Type == 0) {
			values.Set(key, value)
		}
	}
	return catalogURL("/suxinvideo/channel", values)
}

func localChannelChildren(tax localChannelTaxonomy) map[int64][]row {
	children := make(map[int64][]row)
	for _, channel := range yqkDefaultSiteChannels {
		for _, item := range tax.Types {
			id := gconv.Int64(item["id"])
			if tax.channelContains(channel.ID, id) {
				children[int64(channel.ID)] = append(children[int64(channel.ID)], row{"id": id, "name": item["name"], "url": localChannelURL(&YQKChannelReq{ID: channel.ID, Type: id}, 1)})
			}
		}
		children[int64(channel.ID)] = append(children[int64(channel.ID)], row{"id": 0, "name": "小柒精选与专题", "url": yqkChannelURL(channel.ID, 0, 1, "time")})
	}
	return children
}

func localChannelPagination(total, requested int) (page, pages, offset int) {
	pages = max(1, (total+23)/24)
	page = min(max(1, requested), pages)
	return page, pages, (page - 1) * 24
}

const localChannelBody = `<style>.sx-channel-page #wfGrid>*{min-width:0}</style><div class="sx-channel-page"><div class="filter"><div class="frow"><b>分类</b>{{range .Categories}}<a class="{{if .Active}}on{{end}}" href="{{.URL}}">{{.Name}}</a>{{end}}</div><div class="frow"><b>片库</b>{{range .Sources}}<a class="{{if .Active}}on{{end}}" href="{{.URL}}">{{.Name}}</a>{{end}}</div><div class="frow"><b>子分类</b>{{range .Subtypes}}<a class="{{if .Active}}on{{end}}" href="{{.URL}}">{{.Name}}</a>{{end}}</div><div class="frow"><b>排序</b>{{range .Orders}}<a class="{{if .Active}}on{{end}}" href="{{.URL}}">{{.Name}}</a>{{end}}</div><form class="frow sx-extra-filter" method="get" action="/suxinvideo/channel"><input type="hidden" name="id" value="{{.ID}}"><input type="hidden" name="source" value="local"><input type="hidden" name="type" value="{{.TypeID}}"><input type="hidden" name="order" value="{{.Order}}"><b>筛选</b><input name="class" value="{{.Class}}" placeholder="类型"><input name="year" value="{{.Year}}" placeholder="年份"><input name="area" value="{{.Area}}" placeholder="地区"><button class="btn-main">应用</button></form></div><p class="sx-count">{{.Title}} · 本站片库共 {{.Total}} 部影片 · <a href="/suxinvideo/channel?id=0">全部本地分类</a></p>{{template "catalogCards" .}}<nav class="pages">{{if .PrevURL}}<a href="{{.PrevURL}}">上一页</a>{{end}}<span>第 {{.Page}} / {{.Pages}} 页</span>{{if .NextURL}}<a href="{{.NextURL}}">下一页</a>{{end}}</nav></div>` + catalogCards

func renderLocalChannel(ctx context.Context, req *YQKChannelReq) (*YQKChannelRes, error) {
	tax, err := loadLocalChannelTaxonomy(ctx)
	if err != nil {
		return nil, err
	}
	query, err := buildLocalChannelQuery(req, tax, publicVodListingCondition(ctx, "v"))
	if err != nil {
		badRequest(g.RequestFromCtx(ctx), err.Error())
		return &YQKChannelRes{}, nil
	}
	if req.Order == "" {
		req.Order = "time"
	}
	count, err := one(ctx, "SELECT COUNT(*) n FROM sx_vod v WHERE "+query.Where, query.Args...)
	if err != nil {
		return nil, err
	}
	total := gconv.Int(count["n"])
	page, pages, offset := localChannelPagination(total, req.Page)
	args := append(append([]any{}, query.Args...), offset)
	vods, err := all(ctx, "SELECT v.id,v.name,v.pic,v.remarks,v.year,v.score,v.area,v.vip FROM sx_vod v WHERE "+query.Where+" ORDER BY "+query.Order+" LIMIT 24 OFFSET ?", args...)
	if err != nil {
		return nil, err
	}
	title := "影片库"
	categories := []catalogLink{{Name: "全部", URL: localChannelURL(&YQKChannelReq{Order: req.Order}, 1), Active: req.ID == 0}}
	for _, channel := range yqkDefaultSiteChannels {
		categories = append(categories, catalogLink{Name: channel.Name, URL: localChannelURL(&YQKChannelReq{ID: channel.ID, Order: req.Order}, 1), Active: req.ID == channel.ID})
		if channel.ID == req.ID {
			title = channel.Name
		}
	}
	base := *req
	base.Type = 0
	subtypes := []catalogLink{{Name: "全部", URL: localChannelURL(&base, 1), Active: req.Type == 0}}
	for _, item := range tax.Types {
		id := gconv.Int64(item["id"])
		if tax.channelContains(req.ID, id) {
			copyReq := *req
			copyReq.Type = id
			subtypes = append(subtypes, catalogLink{Name: gconv.String(item["name"]), URL: localChannelURL(&copyReq, 1), Active: req.Type == id})
		}
	}
	orders := []catalogLink{}
	for _, item := range []struct{ key, name string }{{"time", "最近更新"}, {"hits", "热播"}, {"score", "评分"}, {"new", "最新上架"}} {
		copyReq := *req
		copyReq.Order = item.key
		orders = append(orders, catalogLink{Name: item.name, URL: localChannelURL(&copyReq, 1), Active: req.Order == item.key})
	}
	prev, next := "", ""
	if page > 1 {
		prev = localChannelURL(req, page-1)
	}
	if page < pages {
		next = localChannelURL(req, page+1)
	}
	sources := []catalogLink{{Name: "本站片库", URL: localChannelURL(req, 1), Active: true}, {Name: "小柒精选与专题", URL: yqkChannelURL(req.ID, 0, 1, "time")}}
	render(ctx, title, tax.Types, localChannelBody, map[string]any{"Title": title, "Vods": vods, "Categories": categories, "Sources": sources, "Subtypes": subtypes, "Orders": orders, "ID": req.ID, "TypeID": req.Type, "Order": req.Order, "Class": req.Class, "Year": req.Year, "Area": req.Area, "Total": total, "Page": page, "Pages": pages, "PrevURL": prev, "NextURL": next})
	return &YQKChannelRes{}, nil
}
