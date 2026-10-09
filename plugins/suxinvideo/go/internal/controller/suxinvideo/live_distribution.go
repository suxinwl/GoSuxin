package suxinvideo

import (
	"context"
	"encoding/xml"
	"fmt"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"net/url"
	"strings"
	"time"
)

func liveDistributionViewer(ctx context.Context, token string) (LiveViewer, error) {
	if len(token) != 48 {
		return LiveViewer{}, appError(403, "直播订阅令牌无效")
	}
	r, err := one(ctx, "SELECT id,member_id,profile_id,token_hash FROM sx_live_distribution WHERE token_hash=? AND enabled=1 AND (expires_at=0 OR expires_at>?)", appHash(token), time.Now().Unix())
	if err != nil {
		return LiveViewer{}, err
	}
	if r == nil {
		return LiveViewer{}, appError(403, "直播订阅已撤销或过期")
	}
	v := LiveViewer{MemberID: gconv.Int64(r["member_id"]), ProfileID: gconv.Int64(r["profile_id"]), SubscriptionID: gconv.Int64(r["id"]), SubscriptionHash: gconv.String(r["token_hash"])}
	return v, liveValidateViewer(ctx, v)
}
func liveDistributionURLs(token string) row {
	base := "/suxinvideo/live/subscription/" + url.PathEscape(token) + "/"
	return row{"m3u": base + "index.m3u", "txt": base + "index.txt", "xmltv": base + "epg.xml"}
}
func liveRegisterDistributionAdmin(bind func(string, string, func(*ghttp.Request) (any, error))) {
	bind("GET", "distribution", func(r *ghttp.Request) (any, error) {
		rows, err := all(r.Context(), "SELECT d.id,d.member_id,d.profile_id,d.name,d.enabled,d.expires_at,d.created,d.updated,p.name profile_name,u.name member_name FROM sx_live_distribution d JOIN sx_live_profile p ON p.id=d.profile_id LEFT JOIN sx_user u ON u.id=d.member_id ORDER BY d.id DESC LIMIT 500")
		return row{"list": rows}, err
	})
	bind("POST", "distribution/save", func(r *ghttp.Request) (any, error) {
		ctx := r.Context()
		id := r.Get("id").Int64()
		member := r.Get("member_id").Int64()
		profile := r.Get("profile_id").Int64()
		expires := r.Get("expires_at").Int64()
		p, err := one(ctx, "SELECT id FROM sx_live_profile WHERE id=? AND enabled=1", profile)
		if err != nil {
			return nil, err
		}
		if p == nil {
			return nil, appError(400, "请选择启用的分发配置")
		}
		if member > 0 {
			u, e := one(ctx, "SELECT id FROM sx_user WHERE id=? AND status=1", member)
			if e != nil {
				return nil, e
			}
			if u == nil {
				return nil, appError(400, "会员不存在或已停用")
			}
		}
		if member < 1 || (expires > 0 && expires <= time.Now().Unix()) {
			return nil, appError(400, "订阅会员或过期时间无效")
		}
		data := row{"name": cutRunes(r.Get("name", "直播订阅").String(), 120), "member_id": member, "profile_id": profile, "expires_at": expires, "enabled": r.Get("enabled", 1).Int()}
		var token string
		if id == 0 || r.Get("reset").Bool() {
			token, err = appToken()
			if err != nil {
				return nil, err
			}
			data["token_hash"] = appHash(token)
		}
		id, err = liveWriteRecord(ctx, "sx_live_distribution", id, data)
		out := row{"id": id}
		if token != "" {
			out["token"] = token
			out["urls"] = liveDistributionURLs(token)
		}
		return out, err
	})
	bind("POST", "distribution/revoke", func(r *ghttp.Request) (any, error) {
		return true, execSQL(r.Context(), "UPDATE sx_live_distribution SET enabled=0,updated=? WHERE id=?", time.Now().Unix(), r.Get("id").Int64())
	})
}

func liveDistributionChannels(ctx context.Context) ([]LiveChannel, error) {
	items := []LiveChannel{}
	for page := 1; page <= 100; page++ {
		batch, total, err := liveChannels(ctx, 0, "", page, 100)
		if err != nil {
			return nil, err
		}
		items = append(items, batch...)
		if len(batch) == 0 || len(items) >= total {
			break
		}
	}
	return items, nil
}
func liveDistributionOrigin(r *ghttp.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if strings.ContainsAny(host, "\r\n") {
		return ""
	}
	return scheme + "://" + host
}
func liveDistributionTextValue(raw string) string {
	return strings.NewReplacer("\r", " ", "\n", " ", "\"", "'").Replace(raw)
}

type liveExportXML struct {
	XMLName    xml.Name              `xml:"tv"`
	Generator  string                `xml:"generator-info-name,attr"`
	Channels   []liveExportChannel   `xml:"channel"`
	Programmes []liveExportProgramme `xml:"programme"`
}
type liveExportChannel struct {
	ID   string `xml:"id,attr"`
	Name string `xml:"display-name"`
}
type liveExportProgramme struct {
	Channel     string `xml:"channel,attr"`
	Start       string `xml:"start,attr"`
	Stop        string `xml:"stop,attr"`
	Title       string `xml:"title"`
	Description string `xml:"desc,omitempty"`
}

func liveExportChannelID(c LiveChannel) string {
	if c.TVGID != "" {
		return c.TVGID
	}
	return "xiaoqi." + gconv.String(c.ID)
}
func liveDistributionXML(ctx context.Context, channels []LiveChannel) ([]byte, error) {
	doc := liveExportXML{Generator: "Xiaoqi live", Channels: []liveExportChannel{}, Programmes: []liveExportProgramme{}}
	ids := map[int64]string{}
	for _, c := range channels {
		id := liveExportChannelID(c)
		ids[c.ID] = id
		doc.Channels = append(doc.Channels, liveExportChannel{ID: id, Name: c.Name})
	}
	clause, args, err := liveStreamAccessWhere(ctx)
	if err != nil {
		return nil, err
	}
	args = append([]any{time.Now().Add(-7 * 24 * time.Hour).Unix(), time.Now().Add(14 * 24 * time.Hour).Unix()}, args...)
	programmes, err := all(ctx, `SELECT DISTINCT p.channel_id,p.start_at,p.end_at,p.title,p.description FROM sx_live_programme p JOIN sx_live_stream s ON s.channel_id=p.channel_id AND s.provider_key=p.provider_key AND s.module_key=p.module_key AND s.provider_ref=p.provider_ref WHERE p.end_at>? AND p.start_at<? AND s.enabled=1 AND `+clause+` ORDER BY p.channel_id,p.start_at LIMIT 20000`, args...)
	if err != nil {
		return nil, err
	}
	for _, p := range programmes {
		id := ids[gconv.Int64(p["channel_id"])]
		if id == "" {
			continue
		}
		doc.Programmes = append(doc.Programmes, liveExportProgramme{Channel: id, Start: time.Unix(gconv.Int64(p["start_at"]), 0).In(liveShanghai).Format("20060102150405 -0700"), Stop: time.Unix(gconv.Int64(p["end_at"]), 0).In(liveShanghai).Format("20060102150405 -0700"), Title: gconv.String(p["title"]), Description: gconv.String(p["description"])})
	}
	data, err := xml.MarshalIndent(doc, "", "  ")
	return append([]byte(xml.Header), data...), err
}
func RegisterLiveDistributionRoutes(group *ghttp.RouterGroup) {
	group.Group("/live/subscription", func(sub *ghttp.RouterGroup) {
		sub.Middleware(appAPILimiter.Middleware)
		handler := func(format string) func(*ghttp.Request) {
			return func(r *ghttp.Request) {
				r.Response.Header().Set("Cache-Control", "private, no-store")
				r.Response.Header().Set("Referrer-Policy", "no-referrer")
				token := r.GetRouter("token").String()
				viewer, err := liveDistributionViewer(r.Context(), token)
				if err != nil {
					appWrite(r, nil, err)
					return
				}
				r.SetCtx(liveContextWithViewer(r.Context(), viewer))
				channels, err := liveDistributionChannels(r.Context())
				if err != nil {
					appWrite(r, nil, err)
					return
				}
				origin := liveDistributionOrigin(r)
				if format == "xml" {
					data, e := liveDistributionXML(r.Context(), channels)
					if e != nil {
						appWrite(r, nil, e)
						return
					}
					r.Response.Header().Set("Content-Type", "application/xml; charset=utf-8")
					r.Response.Write(data)
					return
				}
				r.Response.Header().Set("Content-Type", "text/plain; charset=utf-8")
				var out strings.Builder
				if format == "m3u" {
					r.Response.Header().Set("Content-Type", "audio/x-mpegurl; charset=utf-8")
					fmt.Fprintf(&out, "#EXTM3U x-tvg-url=\"%s/suxinvideo/live/subscription/%s/epg.xml\"\n", origin, token)
				}
				for _, c := range channels {
					address := fmt.Sprintf("%s/suxinvideo/live/subscription/%s/channel/%d.m3u8", origin, token, c.ID)
					if format == "m3u" {
						fmt.Fprintf(&out, "#EXTINF:-1 tvg-id=\"%s\" tvg-name=\"%s\" tvg-logo=\"%s\" group-title=\"%s\",%s\n%s\n", liveDistributionTextValue(liveExportChannelID(c)), liveDistributionTextValue(c.Name), liveDistributionTextValue(c.Logo), liveDistributionTextValue(c.GroupName), liveDistributionTextValue(c.Name), address)
					} else {
						fmt.Fprintf(&out, "%s,%s\n", liveDistributionTextValue(c.Name), address)
					}
				}
				r.Response.Write(out.String())
			}
		}
		sub.GET("/{token}/index.m3u", handler("m3u"))
		sub.GET("/{token}/index.txt", handler("txt"))
		sub.GET("/{token}/epg.xml", handler("xml"))
		sub.GET("/{token}/channel/{channel}.m3u8", func(r *ghttp.Request) {
			r.Response.Header().Set("Cache-Control", "private, no-store")
			viewer, err := liveDistributionViewer(r.Context(), r.GetRouter("token").String())
			if err != nil {
				appWrite(r, nil, err)
				return
			}
			r.SetCtx(liveContextWithViewer(r.Context(), viewer))
			data, err := liveResolve(r.Context(), r.GetRouter("channel").Int64(), 0, nil)
			if err != nil {
				appWrite(r, nil, err)
				return
			}
			address := gconv.String(data["url"])
			if !strings.HasPrefix(address, "/suxinvideo/live/") {
				appWrite(r, nil, appError(502, "直播播放会话无效"))
				return
			}
			r.Response.Header().Set("Location", address)
			r.Response.WriteHeader(302)
		})
	})
}
