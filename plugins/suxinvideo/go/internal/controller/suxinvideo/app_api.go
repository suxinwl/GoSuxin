package suxinvideo

import (
	"context"
	"crypto/rand"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/database/gdb"
	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/gomail.v2"
)

// RegisterAppRoutes expects the public /suxinvideo group. Every handler has its
// own JSON response and never depends on HTML templates or an admin identity.
func RegisterAppRoutes(group *ghttp.RouterGroup) {
	group.Group("/app/v1", func(api *ghttp.RouterGroup) {
		api.Middleware(appAPILimiter.Middleware)
		api.Middleware(func(r *ghttp.Request) {
			if err := EnsureAppSchema(r.Context()); err != nil {
				appWrite(r, nil, err)
				r.ExitAll()
				return
			}
			if !strings.Contains(r.URL.Path, "/auth/") {
				if err := AppAuthenticateRequest(r); err != nil {
					appWrite(r, nil, err)
					r.ExitAll()
					return
				}
			} else if strings.HasSuffix(r.URL.Path, "/auth/logout") {
				if err := AppAuthenticateRequest(r); err != nil {
					appWrite(r, nil, err)
					r.ExitAll()
					return
				}
			}
			r.Middleware.Next()
		})
		api.GET("/config", func(r *ghttp.Request) { appWrite(r, appConfig(r.Context()), nil) })
		api.GET("/captcha", func(r *ghttp.Request) { data, err := appCaptcha(r.Context()); appWrite(r, data, err) })
		api.GET("/home", func(r *ghttp.Request) { data, err := appHome(r.Context()); appWrite(r, data, err) })
		api.GET("/categories", func(r *ghttp.Request) { data, err := appCategories(r.Context()); appWrite(r, data, err) })
		api.GET("/channels", func(r *ghttp.Request) { appWrite(r, appChannels(), nil) })
		api.GET("/catalog/revision", func(r *ghttp.Request) {
			revision, err := appCatalogRevision(r.Context())
			appWrite(r, row{"revision": revision}, err)
		})
		api.GET("/catalog/featured", func(r *ghttp.Request) {
			data, err := appFeatured(r.Context(), r.Get("channel_id", 0).Int(), r.Get("topic_id", 0).Int(), r.Get("page", 1).Int(), r.Get("size", 30).Int(), r.Get("order", "time").String())
			appWrite(r, data, err)
		})
		api.POST("/catalog/featured/resolve", func(r *ghttp.Request) {
			data, err := appResolveFeatured(r.Context(), r.Get("remote_id").String(), r.Get("resolve_token").String())
			appWrite(r, data, err)
		})
		api.GET("/films", func(r *ghttp.Request) {
			revision, revisionErr := appCatalogRevision(r.Context())
			if revisionErr != nil {
				appWrite(r, nil, revisionErr)
				return
			}
			var data row
			var err error
			if r.Get("channel_id").String() != "" {
				data, err = appListChannelFilms(r.Context(), r.Get("channel_id").Int(), r.Get("q").String(), r.Get("type_id").Int64(), r.Get("page", 1).Int(), r.Get("size", 24).Int(), r.Get("order").String(), r.Get("year").String(), r.Get("area").String())
			} else {
				data, err = AppListFilms(r.Context(), r.Get("q").String(), r.Get("type_id").Int64(), r.Get("page", 1).Int(), r.Get("size", 24).Int(), r.Get("order").String(), r.Get("year").String(), r.Get("area").String())
			}
			if err == nil {
				data["catalog_revision"] = revision
			}
			appWrite(r, data, err)
		})
		api.GET("/films/{id}", func(r *ghttp.Request) {
			data, err := AppFilmDetail(r.Context(), r.GetRouter("id").Int64())
			appWrite(r, data, err)
		})
		api.POST("/films/{id}/discover", func(r *ghttp.Request) {
			id := r.GetRouter("id").Int64()
			_, err := appVisibleFilm(r.Context(), id)
			var data sourceDiscoveryState
			if err == nil {
				err = prepareSourceDiscovery(r.Context())
			}
			if err == nil {
				data, err = startSourceDiscovery(r.Context(), id)
			}
			appWrite(r, data, err)
		})
		api.GET("/films/{id}/discovery", func(r *ghttp.Request) {
			id := r.GetRouter("id").Int64()
			_, err := appVisibleFilm(r.Context(), id)
			var data row
			if err == nil {
				item, e := one(r.Context(), "SELECT * FROM sx_source_discovery WHERE vod_id=?", id)
				err = e
				state := discoveryState(item)
				data = row{"status": state.Status, "checked": state.Checked, "total": state.Total, "added": state.Added, "updated": state.Updated, "failed": state.Failed, "message": state.Message, "next_check": state.NextCheck}
				if e == nil && (state.Status == "done" || state.Status == "partial") {
					film, e := appVisibleFilm(r.Context(), id)
					if e == nil {
						sources, e := hydratePlayers(r.Context(), film, playlist(film))
						if e == nil {
							data["sources"] = appSourceViews(r.Context(), film, sources)
						}
					}
				}
			}
			appWrite(r, data, err)
		})
		api.GET("/topics", func(r *ghttp.Request) {
			data, err := appRows(r.Context(), "SELECT id,name,pic,description FROM sx_topic WHERE status=1 ORDER BY id DESC LIMIT 100")
			appPrepareFilms(r.Context(), data)
			appWrite(r, data, err)
		})
		api.GET("/topics/{id}", func(r *ghttp.Request) {
			data, err := appTopic(r.Context(), r.GetRouter("id").Int64())
			appWrite(r, data, err)
		})
		api.GET("/articles", func(r *ghttp.Request) {
			data, err := appRows(r.Context(), "SELECT id,title,addtime FROM sx_article WHERE status=1 ORDER BY id DESC LIMIT 100")
			appWrite(r, data, err)
		})
		api.GET("/articles/{id}", func(r *ghttp.Request) {
			data, err := one(r.Context(), "SELECT id,title,content,addtime FROM sx_article WHERE id=? AND status=1", r.GetRouter("id").Int64())
			if err == nil && data == nil {
				err = appError(404, "文章不存在")
			}
			appWrite(r, data, err)
		})
		api.POST("/auth/login", func(r *ghttp.Request) {
			ctx := r.Context()
			err := appConsumeCaptcha(ctx, r.Get("challenge_id").String(), r.Get("captcha").String())
			var data AppTokens
			if err == nil {
				data, err = AppAuthenticatePassword(ctx, r.Get("email").String(), r.Get("password").String(), r.Get("device_id").String(), r.Get("device_name").String(), cmsClientIP(ctx, r))
			}
			appWrite(r, data, err)
		})
		api.POST("/auth/refresh", func(r *ghttp.Request) {
			data, err := appRefresh(r.Context(), r.Get("refresh_token").String(), r.Get("device_id").String())
			appWrite(r, data, err)
		})
		api.POST("/auth/register", func(r *ghttp.Request) { err := appRegister(r); appWrite(r, row{"registered": err == nil}, err) })
		api.POST("/auth/send-code", func(r *ghttp.Request) { err := appSendCode(r); appWrite(r, row{"sent": err == nil}, err) })
		api.POST("/auth/reset", func(r *ghttp.Request) { err := appResetPassword(r); appWrite(r, row{"reset": err == nil}, err) })
		api.POST("/auth/logout", func(r *ghttp.Request) {
			p, err := appRequirePrincipal(r.Context())
			if err == nil {
				err = execSQL(r.Context(), "UPDATE sx_app_session SET revoked=1,updated=? WHERE id=? AND user_id=?", time.Now().Unix(), p.SessionID, p.User["id"])
			}
			appWrite(r, true, err)
		})
		api.GET("/me", func(r *ghttp.Request) { user, err := appRequireUser(r.Context()); appWrite(r, user, err) })
		api.POST("/me", func(r *ghttp.Request) {
			err := appProfile(r)
			user, _ := AppMember(r.Context())
			if err == nil && user != nil {
				user, err = one(r.Context(), "SELECT id,email,name,points,vip_expire,avatar FROM sx_user WHERE id=? AND status=1", user["id"])
			}
			appWrite(r, applyDefaultUserAvatar(r.Context(), user), err)
		})
		api.GET("/sessions", func(r *ghttp.Request) {
			u, err := appRequireUser(r.Context())
			var data []row
			if err == nil {
				data, err = appRows(r.Context(), "SELECT id,device_id,device_name,created,updated FROM sx_app_session WHERE user_id=? AND revoked=0 AND refresh_expire>? ORDER BY updated DESC", u["id"], time.Now().Unix())
			}
			appWrite(r, data, err)
		})
		api.POST("/sessions/revoke", func(r *ghttp.Request) {
			u, err := appRequireUser(r.Context())
			if err == nil {
				err = execSQL(r.Context(), "UPDATE sx_app_session SET revoked=1,updated=? WHERE user_id=? AND device_id=?", time.Now().Unix(), u["id"], r.Get("device_id").String())
				if err == nil {
					err = execSQL(r.Context(), "UPDATE sx_app_license SET revoked=1 WHERE user_id=? AND device_id=?", u["id"], r.Get("device_id").String())
				}
			}
			appWrite(r, true, err)
		})
		api.GET("/favorites", func(r *ghttp.Request) { data, err := appLibrary(r.Context(), false); appWrite(r, data, err) })
		api.POST("/favorites", func(r *ghttp.Request) { data, err := appFavorite(r); appWrite(r, data, err) })
		api.GET("/history", func(r *ghttp.Request) { data, err := appLibrary(r.Context(), true); appWrite(r, data, err) })
		api.POST("/history", func(r *ghttp.Request) { err := appHistory(r); appWrite(r, true, err) })
		api.GET("/comments", func(r *ghttp.Request) {
			data, err := appComments(r.Context(), r.Get("vod_id").Int64())
			appWrite(r, data, err)
		})
		api.POST("/comments", func(r *ghttp.Request) { pending, err := appComment(r); appWrite(r, row{"pending": pending}, err) })
		api.POST("/film-request", func(r *ghttp.Request) {
			u, err := appRequireUser(r.Context())
			title := strings.TrimSpace(r.Get("title").String())
			if err == nil && (title == "" || len([]rune(title)) > 100) {
				err = appError(400, "片名长度不正确")
			}
			if err == nil {
				err = execSQL(r.Context(), "INSERT INTO sx_film_request(user_id,title,note,status,created) VALUES(?,?,?,0,?)", u["id"], title, strings.TrimSpace(r.Get("note").String()), time.Now().Unix())
			}
			appWrite(r, true, err)
		})
		api.GET("/packages", func(r *ghttp.Request) {
			data, err := appRows(r.Context(), "SELECT id,name,price,points,days FROM sx_goods WHERE status=1 ORDER BY sort,id")
			appWrite(r, data, err)
		})
		api.GET("/orders", func(r *ghttp.Request) {
			u, err := appRequireUser(r.Context())
			var data []row
			if err == nil {
				data, err = appRows(r.Context(), "SELECT order_no,title,amount,status,pay_type,created,paid_time FROM sx_order WHERE user_id=? ORDER BY id DESC LIMIT 100", u["id"])
			}
			appWrite(r, data, err)
		})
		api.POST("/payment/create", func(r *ghttp.Request) { data, err := appPaymentCreate(r); appWrite(r, data, err) })
		api.GET("/payment/checkout", appPaymentCheckout)
		api.GET("/payment/status", appPaymentStatus)
		api.POST("/unlock", func(r *ghttp.Request) { err := appUnlock(r.Context(), r.Get("vod_id").Int64()); appWrite(r, true, err) })
		api.POST("/sign", func(r *ghttp.Request) { points, err := appSign(r.Context()); appWrite(r, row{"points": points}, err) })
		api.POST("/playback/resolve", func(r *ghttp.Request) {
			var input AppPlaybackRequest
			err := r.Parse(&input)
			var data *AppPlaybackDescriptor
			if err == nil {
				data, err = AppResolvePlayback(r.Context(), &input)
			}
			appWrite(r, data, err)
		})
		api.POST("/downloads/authorize", func(r *ghttp.Request) {
			var input AppPlaybackRequest
			err := r.Parse(&input)
			var data row
			if err == nil {
				data, err = appAuthorizeDownload(r.Context(), &input)
			}
			appWrite(r, data, err)
		})
		api.GET("/downloads/licenses", func(r *ghttp.Request) { data, err := appLicenses(r.Context()); appWrite(r, data, err) })
		api.POST("/downloads/renew", func(r *ghttp.Request) {
			data, err := appRenewLicense(r.Context(), r.Get("id").String())
			appWrite(r, data, err)
		})
		api.GET("/media", appMediaProxy)
		api.HEAD("/media", appMediaProxy)
	})
}

func appRequirePrincipal(ctx context.Context) (AppPrincipal, error) {
	p, ok := AppPrincipalFromContext(ctx)
	if !ok || p.User == nil || p.GrantVodID > 0 {
		return AppPrincipal{}, appError(401, "请先登录")
	}
	return p, nil
}
func appRequireUser(ctx context.Context) (row, error) {
	p, err := appRequirePrincipal(ctx)
	return p.User, err
}
func appRows(ctx context.Context, query string, args ...any) ([]row, error) {
	items, err := all(ctx, query, args...)
	if items == nil {
		items = []row{}
	}
	return items, err
}
func appConfig(ctx context.Context) row {
	methods := make([]row, 0)
	for _, method := range enabledPayMethods(ctx) {
		methods = append(methods, row{"code": method.Code, "name": method.Name})
	}
	theme := cleanTheme(setting(ctx, "site_template", "suxinlite"))
	return row{"name": setting(ctx, "site_name", "小柒影视"), "logo": brandSetting(ctx, "site_logo"), "favicon": brandSetting(ctx, "site_favicon"), "default_avatar": brandSetting(ctx, "user_default_avatar"), "theme": theme, "template": theme, "description": setting(ctx, "site_description", ""), "registration_enabled": setting(ctx, "register_enable", "1") == "1", "registration_requires_email_code": registrationRequiresEmailCode(ctx), "password_reset_enabled": passwordResetEnabled(ctx), "member_enabled": setting(ctx, "member_enable", "1") == "1", "comment_enabled": setting(ctx, "comment_enable", "1") == "1", "download_max_parallel": 2, "download_wifi_only": true, "offline_days": 7, "captcha_required": true, "payment_url": "/suxinvideo/pay", "payment_methods": methods, "api_version": 1}
}

func appCategories(ctx context.Context) ([]row, error) {
	items, err := appRows(ctx, "SELECT id,pid,name,icon,image,show_home,status FROM sx_type WHERE status=1 ORDER BY sort,id")
	if err != nil {
		return nil, err
	}
	items = buildLocalChannelTaxonomy(items).Types
	allowed := make([]row, 0, len(items))
	policy := loadContentPolicy(ctx)
	for _, item := range items {
		if policy.blockedTypes[gconv.Int64(item["id"])] {
			continue
		}
		item["id"] = gconv.Int64(item["id"])
		item["pid"] = gconv.Int64(item["pid"])
		allowed = append(allowed, item)
	}
	return allowed, nil
}

func appPrepareFilms(ctx context.Context, items []row) {
	_ = hydrateVodSourceScores(ctx, items)
	types, _ := appRows(ctx, "SELECT id,pid,name FROM sx_type")
	byID := map[int64]row{}
	for _, kind := range types {
		byID[gconv.Int64(kind["id"])] = kind
	}
	for _, item := range items {
		for _, key := range []string{"id", "type_id", "vip", "points", "total_hits", "updatetime", "episode", "position", "duration_ms"} {
			if v, ok := item[key]; ok {
				item[key] = gconv.Int64(v)
			}
		}
		item["score"] = gconv.Float64(item["score"])
		item["score_label"] = scoreLabel(item)
		item["score_source"] = scoreSource(item)
		item["type_name"] = gconv.String(byID[gconv.Int64(item["type_id"])]["name"])
		short, anime := shortDramaMetadata(item, playlist(item)), animePlaybackMetadata(item)
		visited := map[int64]bool{}
		for id := gconv.Int64(item["type_id"]); id > 0 && !visited[id]; id = gconv.Int64(byID[id]["pid"]) {
			visited[id] = true
			name := gconv.String(byID[id]["name"])
			short = short || playbackCategoryHasChannel(name, 50)
			anime = anime || playbackCategoryHasChannel(name, 8)
		}
		item["is_short"], item["is_anime"] = short, anime && !short
		pic := gconv.String(item["pic"])
		if safePlayerAddress(pic) {
			item["pic"] = imageLink(ctx, pic)
		}
	}
}

func AppListFilms(ctx context.Context, query string, typeID int64, page, size int, order, year, area string) (row, error) {
	page = clampPage(page)
	if size < 1 || size > 100 {
		size = 24
	}
	where := publicVodListingCondition(ctx, "")
	args := []any{}
	query = strings.TrimSpace(query)
	if len([]rune(query)) > 100 {
		return nil, appError(400, "搜索词过长")
	}
	if query != "" {
		where += " AND (name LIKE ? OR sub LIKE ?)"
		args = append(args, "%"+query+"%", "%"+query+"%")
	}
	if typeID > 0 {
		tax, e := loadLocalChannelTaxonomy(ctx)
		if e != nil {
			return nil, e
		}
		if tax.ByID[typeID] == nil {
			return nil, appError(400, "分类不可用")
		}
		ids := tax.descendants(typeID)
		where += " AND type_id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")"
		for _, id := range ids {
			args = append(args, id)
		}
	}
	if year != "" && len(year) <= 20 {
		where += " AND year=?"
		args = append(args, year)
	}
	if area != "" && len(area) <= 40 {
		where += " AND area=?"
		args = append(args, area)
	}
	sortSQL := "updatetime DESC,id DESC"
	if order == "hot" || order == "hits" {
		sortSQL = "total_hits DESC,id DESC"
	}
	if order == "score" {
		sortSQL = "score DESC,id DESC"
	}
	count, err := one(ctx, "SELECT COUNT(*) total FROM sx_vod WHERE "+where, args...)
	if err != nil {
		return nil, err
	}
	listArgs := append(append([]any{}, args...), size, (page-1)*size)
	items, err := appRows(ctx, "SELECT id,type_id,name,pic,remarks,year,score,area,vip,points,updatetime FROM sx_vod WHERE "+where+" ORDER BY "+sortSQL+" LIMIT ? OFFSET ?", listArgs...)
	if err != nil {
		return nil, err
	}
	appPrepareFilms(ctx, items)
	return row{"items": items, "page": page, "size": size, "total": gconv.Int64(count["total"]), "pages": max(1, (gconv.Int64(count["total"])+int64(size)-1)/int64(size))}, nil
}

func appHome(ctx context.Context) (row, error) {
	revision, err := appCatalogRevision(ctx)
	if err != nil {
		return nil, err
	}
	categories, err := appCategories(ctx)
	if err != nil {
		return nil, err
	}
	condition := publicVodListingCondition(ctx, "v")
	recent, err := appHomeListRows(ctx, condition, "v.updatetime DESC,v.id DESC", nil, 24)
	if err != nil {
		return nil, err
	}
	hot, err := appHomeListRows(ctx, condition, "v.total_hits DESC,v.id DESC", nil, 12)
	if err != nil {
		return nil, err
	}
	remote := yqkSiteHome(ctx)
	if setting(ctx, "home_recommend_source", "local") == "yqk" {
		if items := appLocalizeRecommendations(ctx, appLimitRows(remote.Recent, 24)); len(items) > 0 {
			recent = items
		}
		if items := appLocalizeRecommendations(ctx, appLimitRows(remote.Hot, 12)); len(items) > 0 {
			hot = items
		}
	}
	heroes := make([]homeHero, 0)
	if setting(ctx, "home_hero_source", "yqk") == "yqk" {
		heroes = append(heroes, appLocalizeHeroes(ctx, remote.Hero)...)
	}
	if cleanTheme(setting(ctx, "site_template", "suxinlite")) == "guoguo" {
		fourHeroes, fourRecent, fourHot := appFourKVMRecommendations(ctx)
		heroes = fourHeroes
		if len(fourRecent) > 0 {
			recent = fourRecent
		}
		if len(fourHot) > 0 {
			hot = fourHot
		}
	}
	if len(heroes) == 0 {
		slides, e := appRows(ctx, "SELECT name,pic,url FROM sx_slide WHERE status=1 AND (pos='top' OR pos='') ORDER BY sort,id DESC LIMIT 8")
		if e != nil {
			return nil, e
		}
		for _, slide := range slides {
			heroes = append(heroes, managedHero(ctx, slide))
		}
	}
	if len(heroes) == 0 {
		for i, item := range hot {
			if i >= 6 {
				break
			}
			heroes = append(heroes, filmHero(item))
		}
	}
	if setting(ctx, "home_slide_enable", "1") != "1" {
		heroes = nil
	}
	for i := range heroes {
		if safePlayerAddress(heroes[i].Pic) {
			heroes[i].Pic = imageLink(ctx, heroes[i].Pic)
		}
		if safePlayerAddress(heroes[i].Poster) {
			heroes[i].Poster = imageLink(ctx, heroes[i].Poster)
		}
	}
	blocks := make([]row, 0)
	tax, err := loadLocalChannelTaxonomy(ctx)
	if err != nil {
		return nil, err
	}
	prepared := append(append([]row{}, recent...), hot...)
	for _, category := range appChannels() {
		query, e := buildLocalChannelQuery(&YQKChannelReq{ID: gconv.Int(category["id"]), Page: 1, Order: "time"}, tax, condition)
		if e != nil {
			return nil, e
		}
		films, e := appHomeListRows(ctx, query.Where, query.Order, query.Args, 12)
		if e != nil {
			return nil, e
		}
		if len(films) > 0 {
			blocks = append(blocks, row{"id": category["id"], "name": category["name"], "items": films})
			prepared = append(prepared, films...)
		}
		if len(blocks) >= 12 {
			break
		}
	}
	// The homepage has no pagination totals. A single metadata pass replaces
	// ten full-library COUNT queries and repeated score/taxonomy scans.
	appPrepareFilms(ctx, prepared)
	banners := make([]row, 0, len(heroes))
	for _, hero := range heroes {
		link, _ := url.Parse(hero.Link)
		id := int64(0)
		if link != nil && (link.Path == "/suxinvideo/detail" || link.Path == "/suxinvideo/play") {
			id = gconv.Int64(link.Query().Get("id"))
		}
		banners = append(banners, row{"vod_id": id, "name": hero.Name, "pic": hero.Pic, "poster": hero.Poster, "link": hero.Link, "button": hero.Button, "film": hero.Film, "description": hero.Description, "year": hero.Year, "remarks": hero.Remarks})
	}
	return row{"banners": banners, "recent": recent, "hot": hot, "sections": blocks, "categories": categories, "channels": appChannels(), "catalog_revision": revision}, nil
}

func appVisibleFilm(ctx context.Context, id int64) (row, error) {
	item, err := one(ctx, "SELECT * FROM sx_vod WHERE id=? AND "+publicVodCondition(ctx, ""), id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, appError(404, "影片不存在或已下架")
	}
	return item, nil
}
func AppFilmDetail(ctx context.Context, id int64) (row, error) {
	film, err := appVisibleFilm(ctx, id)
	if err != nil {
		return nil, err
	}
	sources, err := hydratePlayers(ctx, film, playlist(film))
	if err != nil {
		return nil, err
	}
	sourceViews := appSourceViews(ctx, film, sources)
	preferred := appPreferredLine(ctx, film, sources)
	comments, err := appVisibleFilmComments(ctx, id)
	if err != nil {
		return nil, err
	}
	related, err := appRows(ctx, "SELECT id,name,pic,remarks,year,score,area,vip,type_id FROM sx_vod WHERE "+publicVodListingCondition(ctx, "")+" AND type_id=? AND id<>? ORDER BY total_hits DESC,id DESC LIMIT 12", film["type_id"], id)
	if err != nil {
		return nil, err
	}
	locked, lockErr := AppAuthorizeFilm(ctx, film)
	lockMessage := ""
	if lockErr != nil {
		lockMessage = lockErr.Error()
	}
	appPrepareFilms(ctx, []row{film})
	appPrepareFilms(ctx, related)
	delete(film, "play_from")
	delete(film, "play_url")
	delete(film, "api_vid")
	film["content"] = filmDescription(gconv.String(film["content"]))
	favorite := false
	var history row
	if p, ok := AppPrincipalFromContext(ctx); ok {
		found, _ := one(ctx, "SELECT id FROM sx_fav WHERE user_id=? AND vod_id=?", p.User["id"], id)
		favorite = found != nil
		record, _ := one(ctx, "SELECT source_code,episode_key,version_key,episode,position,duration_ms,updated FROM sx_play_record WHERE user_id=? AND vod_id=?", p.User["id"], id)
		if record != nil {
			history = row{"line": gconv.String(record["source_code"]), "episode_key": gconv.String(record["episode_key"]), "version_key": gconv.String(record["version_key"]), "episode": max(0, gconv.Int(record["episode"])-1), "position_ms": gconv.Int64(record["position"]) * 1000, "duration_ms": gconv.Int64(record["duration_ms"]), "updated": gconv.Int64(record["updated"])}
		}
	}
	return row{"film": film, "sources": sourceViews, "preferred_line": preferred, "comments": comments, "related": related, "lock": locked, "lock_message": lockMessage, "favorite": favorite, "history": history}, nil
}
func appTopic(ctx context.Context, id int64) (row, error) {
	item, err := one(ctx, "SELECT id,name,pic,description,content FROM sx_topic WHERE id=? AND status=1", id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, appError(404, "片单不存在")
	}
	ids := topicIDs.FindAllString(gconv.String(item["content"]), 100)
	films := make([]row, 0)
	for _, raw := range ids {
		film, e := appVisibleFilm(ctx, gconv.Int64(raw))
		if e == nil {
			delete(film, "play_url")
			delete(film, "play_from")
			films = append(films, film)
		}
		if len(films) >= 100 {
			break
		}
	}
	appPrepareFilms(ctx, films)
	appPrepareFilms(ctx, []row{item})
	item["items"] = films
	delete(item, "content")
	return item, nil
}

// AppAuthorizeFilm returns a machine readable lock kind while enforcing both
// VIP and points gates consistently across app, Jellyfin and every media hop.
func AppAuthorizeFilm(ctx context.Context, film row) (string, error) {
	if film == nil || gconv.Int(film["status"]) != 1 || !contentVodAllowed(ctx, film) {
		return "unavailable", appError(404, "影片不存在或已下架")
	}
	if err := AppAuthorizeGrantVod(ctx, gconv.Int64(film["id"])); err != nil {
		return "forbidden", err
	}
	u, err := AppMember(ctx)
	if err != nil {
		return "login", err
	}
	if gconv.Int(film["vip"]) == 1 {
		if u == nil {
			return "login", appError(401, "请先登录")
		}
		if gconv.Int64(u["vip_expire"]) <= time.Now().Unix() {
			return "vip", appError(403, "需要有效会员资格")
		}
	}
	if gconv.Int(film["points"]) > 0 {
		if u == nil {
			return "login", appError(401, "请先登录并解锁影片")
		}
		owned, e := one(ctx, "SELECT id FROM sx_user_vod WHERE user_id=? AND vod_id=?", u["id"], film["id"])
		if e != nil {
			return "", e
		}
		if owned == nil {
			return "points", appError(403, "请先使用积分解锁影片")
		}
	}
	return "", nil
}

func appLibrary(ctx context.Context, history bool) ([]row, error) {
	u, err := appRequireUser(ctx)
	if err != nil {
		return nil, err
	}
	query := "SELECT v.id,v.type_id,v.name,v.pic,v.year,v.remarks,v.score,v.area,v.vip FROM sx_fav f JOIN sx_vod v ON v.id=f.vod_id WHERE f.user_id=? AND " + publicVodCondition(ctx, "v") + " ORDER BY f.id DESC LIMIT 100"
	if history {
		query = "SELECT v.id,v.type_id,v.name,v.pic,v.year,v.remarks,v.score,v.area,v.vip,p.episode,p.position,p.updated,p.source_code,p.episode_key,p.version_key,p.duration_ms FROM sx_play_record p JOIN sx_vod v ON v.id=p.vod_id WHERE p.user_id=? AND " + publicVodCondition(ctx, "v") + " ORDER BY p.updated DESC LIMIT 100"
	}
	items, err := appRows(ctx, query, u["id"])
	appPrepareFilms(ctx, items)
	for _, item := range items {
		if history {
			item["position_ms"] = gconv.Int64(item["position"]) * 1000
			item["line"] = item["source_code"]
		}
	}
	return items, err
}
func appFavorite(r *ghttp.Request) (row, error) {
	u, err := appRequireUser(r.Context())
	if err != nil {
		return nil, err
	}
	id := r.Get("vod_id").Int64()
	if _, err = appVisibleFilm(r.Context(), id); err != nil {
		return nil, err
	}
	favorite := r.Get("favorite").Bool()
	if favorite {
		err = execSQL(r.Context(), "INSERT IGNORE INTO sx_fav(user_id,vod_id,created) VALUES(?,?,?)", u["id"], id, time.Now().Unix())
	} else {
		err = execSQL(r.Context(), "DELETE FROM sx_fav WHERE user_id=? AND vod_id=?", u["id"], id)
	}
	return row{"vod_id": id, "favorite": favorite}, err
}
func appHistory(r *ghttp.Request) error {
	u, err := appRequireUser(r.Context())
	if err != nil {
		return err
	}
	id := r.Get("vod_id").Int64()
	film, err := appVisibleFilm(r.Context(), id)
	if err != nil {
		return err
	}
	if _, err = AppAuthorizeFilm(r.Context(), film); err != nil {
		return err
	}
	sources, err := hydratePlayers(r.Context(), film, playlist(film))
	if err != nil {
		return err
	}
	request := AppPlaybackRequest{VodID: id, Line: r.Get("line").String(), EpisodeKey: r.Get("episode_key").String(), VersionKey: r.Get("version_key").String(), Episode: r.Get("episode").Int(), Manual: true}
	si, ei, err := appChooseEpisode(r.Context(), film, sources, &request)
	if err != nil {
		return err
	}
	position, duration := r.Get("position_ms").Int64(), r.Get("duration_ms").Int64()
	if position < 0 || position > 24*3600*1000 || duration < 0 || duration > 24*3600*1000 {
		return appError(400, "播放进度无效")
	}
	if duration > 0 && position > duration {
		position = duration
	}
	key := appEpisodeIdentity(r.Context(), film, sources[si].Episodes[ei].Name)
	return execSQL(r.Context(), "INSERT INTO sx_play_record(user_id,vod_id,episode,position,updated,source_code,episode_key,version_key,duration_ms) VALUES(?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE episode=VALUES(episode),position=VALUES(position),updated=VALUES(updated),source_code=VALUES(source_code),episode_key=VALUES(episode_key),version_key=VALUES(version_key),duration_ms=VALUES(duration_ms)", u["id"], id, ei+1, position/1000, time.Now().Unix(), sources[si].Code, key, sources[si].VersionKey, duration)
}
func appComments(ctx context.Context, id int64) ([]row, error) {
	if _, err := appVisibleFilm(ctx, id); err != nil {
		return nil, err
	}
	return appVisibleFilmComments(ctx, id)
}

// AppFilmDetail has already checked visibility, so its panel need not fetch the
// entire playlist-bearing film again. The public comments endpoint still checks.
func appVisibleFilmComments(ctx context.Context, id int64) ([]row, error) {
	return appRows(ctx, "SELECT c.id,c.content,c.created,u.name FROM sx_comment c JOIN sx_user u ON u.id=c.user_id WHERE c.vod_id=? AND c.status=1 ORDER BY c.id DESC LIMIT 100", id)
}
func appComment(r *ghttp.Request) (bool, error) {
	if setting(r.Context(), "comment_enable", "1") != "1" {
		return false, appError(403, "评论功能已关闭")
	}
	u, err := appRequireUser(r.Context())
	if err != nil {
		return false, err
	}
	id := r.Get("vod_id").Int64()
	if _, err = appVisibleFilm(r.Context(), id); err != nil {
		return false, err
	}
	content := strings.TrimSpace(r.Get("content").String())
	if len([]rune(content)) < 1 || len([]rune(content)) > 500 {
		return false, appError(400, "评论长度须在1至500字之间")
	}
	status := 1
	if setting(r.Context(), "comment_audit", "0") == "1" {
		status = 0
	}
	err = execSQL(r.Context(), "INSERT INTO sx_comment(user_id,vod_id,content,status,created) VALUES(?,?,?,?,?)", u["id"], id, content, status, time.Now().Unix())
	return status == 0, err
}

func appUnlock(ctx context.Context, id int64) error {
	u, err := appRequireUser(ctx)
	if err != nil {
		return err
	}
	film, err := appVisibleFilm(ctx, id)
	if err != nil {
		return err
	}
	points := gconv.Int64(film["points"])
	if points <= 0 {
		return nil
	}
	return g.DB().Transaction(ctx, func(_ context.Context, tx gdb.TX) error {
		user, e := tx.GetOne("SELECT points FROM sx_user WHERE id=? AND status=1 FOR UPDATE", u["id"])
		if e != nil {
			return e
		}
		if user == nil {
			return appError(401, "账号不可用")
		}
		owned, e := tx.GetOne("SELECT id FROM sx_user_vod WHERE user_id=? AND vod_id=?", u["id"], id)
		if e != nil {
			return e
		}
		if owned != nil {
			return nil
		}
		if gconv.Int64(user["points"]) < points {
			return appError(403, "积分不足")
		}
		if _, e = tx.Exec("UPDATE sx_user SET points=points-? WHERE id=?", points, u["id"]); e != nil {
			return e
		}
		_, e = tx.Exec("INSERT INTO sx_user_vod(user_id,vod_id,points,created) VALUES(?,?,?,?)", u["id"], id, points, time.Now().Unix())
		return e
	})
}
func appSign(ctx context.Context) (int, error) {
	u, err := appRequireUser(ctx)
	if err != nil {
		return 0, err
	}
	points := gconv.Int(setting(ctx, "points_sign", "5"))
	if points < 0 || points > 1000 {
		points = 5
	}
	day := gconv.Int(time.Now().Format("20060102"))
	err = g.DB().Transaction(ctx, func(_ context.Context, tx gdb.TX) error {
		result, e := tx.Exec("INSERT IGNORE INTO sx_sign(user_id,day,points) VALUES(?,?,?)", u["id"], day, points)
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if n == 0 {
			return appError(409, "今天已经签到")
		}
		_, e = tx.Exec("UPDATE sx_user SET points=points+?,sign_day=? WHERE id=?", points, day, u["id"])
		return e
	})
	return points, err
}

func appRegister(r *ghttp.Request) error {
	ctx := r.Context()
	if setting(ctx, "member_enable", "1") != "1" || setting(ctx, "register_enable", "1") != "1" {
		return appError(403, "注册功能已关闭")
	}
	if err := appConsumeCaptcha(ctx, r.Get("challenge_id").String(), r.Get("captcha").String()); err != nil {
		return err
	}
	email := strings.ToLower(strings.TrimSpace(r.Get("email").String()))
	name := strings.TrimSpace(r.Get("name").String())
	password := r.Get("password").String()
	if _, err := mail.ParseAddress(email); err != nil {
		return appError(400, "邮箱格式错误")
	}
	if len(password) < 6 || len(password) > 72 || len([]rune(name)) < 1 || len([]rune(name)) > 20 {
		return appError(400, "昵称或密码长度不正确")
	}
	return registerMemberAccount(ctx, email, name, password, r.Get("email_code").String(), cmsClientIP(ctx, r))
}
func appSendCode(r *ghttp.Request) error {
	ctx := r.Context()
	if setting(ctx, "member_enable", "1") != "1" {
		return appError(403, "会员系统已关闭")
	}
	smtp, ready := memberSMTP(ctx)
	if !ready {
		return appError(503, "邮件服务尚未配置，暂不可发送邮箱验证码")
	}
	if err := appConsumeCaptcha(ctx, r.Get("challenge_id").String(), r.Get("captcha").String()); err != nil {
		return err
	}
	email := strings.ToLower(strings.TrimSpace(r.Get("email").String()))
	if _, err := mail.ParseAddress(email); err != nil {
		return appError(400, "邮箱格式错误")
	}
	purpose := r.Get("purpose", "register").String()
	if purpose != "register" && purpose != "reset" {
		return appError(400, "验证码用途无效")
	}
	if purpose == "register" && setting(ctx, "register_enable", "1") != "1" {
		return appError(403, "注册功能已关闭")
	}
	user, err := one(ctx, "SELECT id FROM sx_user WHERE email=?", email)
	if err != nil {
		return err
	}
	if purpose == "register" && user != nil {
		return appError(409, "邮箱已注册")
	}
	if purpose == "reset" && user == nil {
		return appError(404, "邮箱尚未注册")
	}
	last, err := one(ctx, "SELECT created FROM sx_email_code WHERE email=? ORDER BY id DESC LIMIT 1", email)
	if err != nil {
		return err
	}
	if last != nil && time.Now().Unix()-gconv.Int64(last["created"]) < 60 {
		return appError(429, "发送过于频繁")
	}
	random := make([]byte, 6)
	if _, err = rand.Read(random); err != nil {
		return err
	}
	var code strings.Builder
	for _, b := range random {
		code.WriteString(strconv.Itoa(int(b) % 10))
	}
	message := gomail.NewMessage()
	message.SetHeader("From", smtp.User)
	message.SetHeader("To", email)
	message.SetHeader("Subject", "小柒影视邮箱验证码")
	message.SetBody("text/plain", "验证码："+code.String()+"，10分钟内有效。")
	dialer := gomail.NewDialer(smtp.Host, smtp.Port, smtp.User, smtp.Password)
	if err = dialer.DialAndSend(message); err != nil {
		return appError(502, "邮件发送失败，请稍后重试")
	}
	return execSQL(ctx, "INSERT INTO sx_email_code(email,code,type,expire,used,created) VALUES(?,?,?,?,0,?)", email, code.String(), purpose, time.Now().Add(10*time.Minute).Unix(), time.Now().Unix())
}
func appResetPassword(r *ghttp.Request) error {
	ctx := r.Context()
	if setting(ctx, "member_enable", "1") != "1" {
		return appError(403, "会员系统已关闭")
	}
	if !passwordResetEnabled(ctx) {
		return appError(503, "邮件服务尚未配置，暂不可通过邮箱找回密码")
	}
	if err := appConsumeCaptcha(ctx, r.Get("challenge_id").String(), r.Get("captcha").String()); err != nil {
		return err
	}
	password := r.Get("password").String()
	if len(password) < 6 || len(password) > 72 {
		return appError(400, "密码长度须为6至72位")
	}
	email := strings.ToLower(strings.TrimSpace(r.Get("email").String()))
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return g.DB().Transaction(ctx, func(_ context.Context, tx gdb.TX) error {
		code, e := tx.GetOne("SELECT id FROM sx_email_code WHERE email=? AND code=? AND type='reset' AND used=0 AND expire>? ORDER BY id DESC LIMIT 1 FOR UPDATE", email, r.Get("email_code").String(), time.Now().Unix())
		if e != nil {
			return e
		}
		if code == nil {
			return appError(400, "邮箱验证码错误或已过期")
		}
		if _, e = tx.Exec("UPDATE sx_user SET pwd=? WHERE email=? AND status=1", string(hash), email); e != nil {
			return e
		}
		if _, e = tx.Exec("UPDATE sx_app_session SET revoked=1 WHERE user_id IN (SELECT id FROM sx_user WHERE email=?)", email); e != nil {
			return e
		}
		_, e = tx.Exec("UPDATE sx_email_code SET used=1 WHERE id=?", code["id"])
		return e
	})
}
func appProfile(r *ghttp.Request) error {
	u, err := appRequireUser(r.Context())
	if err != nil {
		return err
	}
	name := strings.TrimSpace(r.Get("name").String())
	if len([]rune(name)) < 1 || len([]rune(name)) > 20 {
		return appError(400, "昵称长度不正确")
	}
	return execSQL(r.Context(), "UPDATE sx_user SET name=? WHERE id=?", name, u["id"])
}
