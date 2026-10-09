package suxinvideo

import (
	"context"
	"encoding/xml"
	"errors"
	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"sort"
	"strings"
	"time"
)

type LiveProgramme struct {
	ID          int64  `json:"id"`
	ChannelID   int64  `json:"channel_id"`
	StreamID    int64  `json:"stream_id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Start       int64  `json:"start"`
	Stop        int64  `json:"stop"`
	CanReplay   bool   `json:"can_replay"`
	Kind        string `json:"kind"`
	AccessLevel string `json:"access_level"`
}
type liveXMLTV struct {
	Programmes []liveXMLProgramme `xml:"programme"`
}
type liveXMLProgramme struct {
	Channel     string `xml:"channel,attr"`
	Start       string `xml:"start,attr"`
	Stop        string `xml:"stop,attr"`
	Title       string `xml:"title"`
	Description string `xml:"desc"`
}

var liveShanghai = time.FixedZone("Asia/Shanghai", 8*60*60)

func liveXMLTime(raw string) (time.Time, error) {
	for _, layout := range []string{"20060102150405 -0700", "200601021504 -0700", "20060102150405Z0700", time.RFC3339} {
		if t, e := time.Parse(layout, strings.TrimSpace(raw)); e == nil {
			return t, nil
		}
	}
	return time.ParseInLocation("20060102150405", strings.TrimSpace(raw), liveShanghai)
}

func liveImportProviderEPG(ctx context.Context, key string) error {
	data, err := liveProviderRead(ctx, key, "GET", "/internal/epg", nil, 16<<20)
	if err != nil {
		return err
	}
	var doc liveXMLTV
	if xml.Unmarshal(data, &doc) != nil {
		return errors.New("直播节目单格式无效")
	}
	streams, err := all(ctx, "SELECT s.id,s.channel_id,s.provider_key,s.module_key,s.provider_ref,s.epg_id,s.catchup_enabled FROM sx_live_stream s JOIN sx_live_module m ON m.provider_key=s.provider_key AND m.module_key=s.module_key WHERE s.source_kind='provider' AND s.provider_key=? AND s.enabled=1 AND m.enabled=1", key)
	if err != nil {
		return err
	}
	mapped := map[string][]row{}
	for _, s := range streams {
		id := gconv.String(s["epg_id"])
		if id != "" {
			mapped[id] = append(mapped[id], s)
		}
	}
	now := time.Now()
	for _, p := range doc.Programmes {
		begin, e := liveXMLTime(p.Start)
		if e != nil {
			continue
		}
		end, e := liveXMLTime(p.Stop)
		if e != nil || !end.After(begin) || end.Sub(begin) > 24*time.Hour || begin.Before(now.Add(-7*24*time.Hour)) || begin.After(now.Add(14*24*time.Hour)) || strings.TrimSpace(p.Title) == "" {
			continue
		}
		for _, s := range mapped[p.Channel] {
			ref := gconv.String(s["provider_ref"])
			module := gconv.String(s["module_key"])
			identity := liveIdentity(key + ":" + module + ":" + ref + ":" + p.Start + ":" + p.Stop)
			kind := ""
			if gconv.Bool(s["catchup_enabled"]) {
				kind = "catchup"
			}
			if err = execSQL(ctx, `INSERT INTO sx_live_programme(channel_id,provider_key,module_key,provider_ref,epg_id,identity_key,title,description,start_at,end_at,replay_kind,created,updated)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE title=VALUES(title),description=VALUES(description),replay_kind=VALUES(replay_kind),updated=VALUES(updated)`, s["channel_id"], key, module, ref, p.Channel, identity, cutRunes(p.Title, 300), cutRunes(p.Description, 5000), begin.Unix(), end.Unix(), kind, now.Unix(), now.Unix()); err != nil {
				return err
			}
		}
	}
	return execSQL(ctx, "DELETE FROM sx_live_programme WHERE end_at<? AND (replay_kind<>'event_replay' OR end_at>0)", now.Add(-7*24*time.Hour).Unix())
}

func liveUpsertEventProgramme(ctx context.Context, key string, c liveProviderChannel, streamID int64) error {
	s, err := liveStream(ctx, streamID)
	if err != nil {
		return err
	}
	identity := liveIdentity("event:" + key + ":" + c.ModuleKey + ":" + c.Ref)
	start, stop := int64(0), int64(0)
	if t, e := time.Parse(time.RFC3339, c.Start); e == nil {
		start = t.Unix()
	}
	if t, e := time.Parse(time.RFC3339, c.End); e == nil {
		stop = t.Unix()
	}
	title := c.Title
	if title == "" {
		title = c.Name
	}
	now := time.Now().Unix()
	err = execSQL(ctx, `INSERT INTO sx_live_programme(channel_id,provider_key,module_key,provider_ref,epg_id,identity_key,title,description,start_at,end_at,replay_kind,created,updated) VALUES(?,?,?,?,?,?,?,'',?,?,'event_replay',?,?) ON DUPLICATE KEY UPDATE channel_id=VALUES(channel_id),title=VALUES(title),start_at=VALUES(start_at),end_at=VALUES(end_at),updated=VALUES(updated)`, s.ChannelID, key, c.ModuleKey, c.Ref, c.EpgID, identity, cutRunes(title, 300), start, stop, now, now)
	if err != nil {
		return err
	}
	p, err := one(ctx, "SELECT id FROM sx_live_programme WHERE identity_key=?", identity)
	if err != nil {
		return err
	}
	return execSQL(ctx, "UPDATE sx_live_stream SET programme_id=? WHERE id=?", p["id"], streamID)
}

func liveProgrammeFromRow(r row) LiveProgramme {
	p := LiveProgramme{ID: gconv.Int64(r["id"]), ChannelID: gconv.Int64(r["channel_id"]), StreamID: gconv.Int64(r["stream_id"]), Title: gconv.String(r["title"]), Description: gconv.String(r["description"]), Start: gconv.Int64(r["start_at"]), Stop: gconv.Int64(r["end_at"]), Kind: gconv.String(r["replay_kind"]), AccessLevel: gconv.String(r["access_level"])}
	p.CanReplay = p.Kind != "" && p.Stop <= time.Now().Unix() && (gconv.Bool(r["catchup_enabled"]) || (p.Kind == "event_replay" && gconv.String(r["source_mode"]) == "event_replay"))
	return p
}
func liveProgrammes(ctx context.Context, channelID int64, date string) (row, error) {
	if channelID < 1 {
		return nil, appError(400, "频道参数无效")
	}
	if date == "" {
		date = time.Now().In(liveShanghai).Format("2006-01-02")
	}
	begin, err := time.ParseInLocation("2006-01-02", date, liveShanghai)
	if err != nil {
		return nil, appError(400, "节目日期无效")
	}
	if begin.Before(time.Now().Add(-8*24*time.Hour)) || begin.After(time.Now().Add(15*24*time.Hour)) {
		return nil, appError(400, "节目日期超出范围")
	}
	channel, err := liveChannel(ctx, channelID)
	if err != nil || !channel.Enabled {
		return nil, appError(404, "频道不存在或已停用")
	}
	if len(channel.Streams) == 0 {
		return nil, appError(403, "此频道未获直播授权")
	}
	clause, args, err := liveStreamAccessWhere(ctx)
	if err != nil {
		return nil, err
	}
	args = append([]any{channelID, begin.Add(24 * time.Hour).Unix(), begin.Unix()}, args...)
	records, err := all(ctx, `SELECT p.*,s.id stream_id,s.access_level,s.catchup_enabled,s.source_mode FROM sx_live_programme p JOIN sx_live_stream s ON s.channel_id=p.channel_id AND s.provider_key=p.provider_key AND s.module_key=p.module_key AND s.provider_ref=p.provider_ref
 WHERE p.channel_id=? AND p.start_at<? AND p.end_at>? AND s.enabled=1 AND `+clause+` ORDER BY p.start_at,s.priority DESC,s.id LIMIT 1000`, args...)
	if err != nil {
		return nil, err
	}
	items := []LiveProgramme{}
	seen := map[string]int{}
	for _, r := range records {
		p := liveProgrammeFromRow(r)
		key := gconv.String(p.Start) + ":" + gconv.String(p.Stop) + ":" + p.Title
		if index, exists := seen[key]; exists {
			if p.CanReplay && !items[index].CanReplay {
				items[index] = p
			}
			continue
		}
		seen[key] = len(items)
		items = append(items, p)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Start < items[j].Start })
	var current, next *LiveProgramme
	now := time.Now().Unix()
	for i := range items {
		if items[i].Start <= now && items[i].Stop > now {
			current = &items[i]
		}
		if next == nil && items[i].Start > now {
			next = &items[i]
		}
	}
	return row{"channel_id": channelID, "date": date, "timezone": "Asia/Shanghai", "items": items, "now": current, "next": next}, nil
}

func liveReplayProgramme(ctx context.Context, id, requestedStream int64) (LiveProgramme, LiveStream, error) {
	if id < 1 {
		return LiveProgramme{}, LiveStream{}, appError(400, "回看节目参数无效")
	}
	r, err := one(ctx, "SELECT * FROM sx_live_programme WHERE id=?", id)
	if err != nil {
		return LiveProgramme{}, LiveStream{}, err
	}
	if r == nil {
		return LiveProgramme{}, LiveStream{}, appError(404, "回看节目不存在或已过期")
	}
	channel, err := liveChannel(ctx, gconv.Int64(r["channel_id"]))
	if err != nil || !channel.Enabled {
		return LiveProgramme{}, LiveStream{}, appError(404, "回看频道已停用")
	}
	for _, s := range liveOrderedStreams(channel.Streams, requestedStream, nil) {
		if (!s.Catchup && s.SourceMode != "event_replay") || s.ProviderKey != gconv.String(r["provider_key"]) || s.ModuleKey != gconv.String(r["module_key"]) || s.ProviderRef != gconv.String(r["provider_ref"]) {
			continue
		}
		if err = liveStreamAccess(ctx, s, liveViewerFromContext(ctx)); err != nil {
			return LiveProgramme{}, LiveStream{}, err
		}
		r["stream_id"] = s.ID
		r["catchup_enabled"] = s.Catchup
		r["source_mode"] = s.SourceMode
		r["access_level"] = s.AccessLevel
		p := liveProgrammeFromRow(r)
		if !p.CanReplay || (p.Kind != "event_replay" && p.Start < time.Now().Add(-7*24*time.Hour).Unix()) {
			return p, s, appError(404, "此节目尚未结束或没有可用回看")
		}
		return p, s, nil
	}
	return LiveProgramme{}, LiveStream{}, appError(403, "此节目没有获授权的回看线路")
}

func RegisterLiveProgrammeRoutes(group *ghttp.RouterGroup) {
	group.Group("/app/v1/live", func(api *ghttp.RouterGroup) {
		api.Middleware(appAPILimiter.Middleware)
		api.GET("/epg", func(r *ghttp.Request) {
			if err := liveOptionalAuth(r); err != nil {
				appWrite(r, nil, err)
				return
			}
			data, err := liveProgrammes(r.Context(), r.Get("channel_id").Int64(), r.Get("date").String())
			appWrite(r, data, err)
		})
	})
}
