package suxinvideo

import (
	"context"
	"sort"
	"time"

	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
)

// Live is guest-accessible and does not interpret movie IDs or member grants.
// Keeping media outside the business API limiter permits playlist refreshes.
func RegisterLiveRoutes(group *ghttp.RouterGroup) {
	RegisterLiveProgrammeRoutes(group)
	RegisterLiveDistributionRoutes(group)
	group.Group("/app/v1/live", func(api *ghttp.RouterGroup) {
		api.Middleware(appAPILimiter.Middleware, func(r *ghttp.Request) {
			if err := liveOptionalAuth(r); err != nil {
				appWrite(r, nil, err)
				return
			}
			r.Middleware.Next()
		})
		api.GET("/groups", func(r *ghttp.Request) { items, err := liveGroups(r.Context()); appWrite(r, row{"items": items}, err) })
		api.GET("/channels", liveChannelsHandler)
		api.POST("/resolve", liveResolveHandler)
		api.POST("/replay/resolve", liveReplayResolveHandler)
		api.POST("/renew", func(r *ghttp.Request) {
			item, err := getLiveSession(r.Get("session_id").String())
			if err == nil {
				_, err = checkLiveSession(r.Context(), item)
			}
			if err != nil {
				appWrite(r, nil, err)
				return
			}
			item.mu.Lock()
			needsRefresh := item.RuntimeStream.SourceKind == "provider" && item.SourceExpire < time.Now().Add(2*time.Minute).Unix()
			item.mu.Unlock()
			if needsRefresh {
				if err = liveRefreshSessionSource(r.Context(), item); err != nil {
					appWrite(r, nil, err)
					return
				}
			}
			item.mu.Lock()
			item.Expires = time.Now().Add(liveSessionLifetime)
			exp := item.Expires.Unix()
			item.mu.Unlock()
			appWrite(r, row{"session_id": r.Get("session_id").String(), "expires_at": exp}, nil)
		})
	})
	group.Group("/live", func(media *ghttp.RouterGroup) {
		media.Middleware(appMediaLimiter.Middleware)
		media.GET("/media", liveMediaHandler)
		media.HEAD("/media", liveMediaHandler)
	})
}
func livePublicStreams(streams []LiveStream) []row {
	out := make([]row, 0, len(streams))
	for _, s := range streams {
		out = append(out, row{"id": s.ID, "name": s.Name, "health": s.Health, "priority": s.Priority, "quality": s.Quality})
	}
	return out
}
func livePublicChannel(c LiveChannel) row {
	return row{"id": c.ID, "tvg_id": c.TVGID, "name": c.Name, "logo": c.Logo, "group_id": c.GroupID, "group_name": c.GroupName, "streams": livePublicStreams(c.Streams)}
}
func liveChannelsHandler(r *ghttp.Request) {
	page, size := r.Get("page", 1).Int(), r.Get("size", 60).Int()
	page = max(1, min(page, 10000))
	size = max(1, min(size, 100))
	if id := r.Get("channel_id").Int64(); id > 0 {
		c, err := liveChannel(r.Context(), id)
		if err != nil || !c.Enabled {
			appWrite(r, nil, appError(404, "直播频道不存在或已停用"))
			return
		}
		c.Streams, err = liveStreams(r.Context(), id)
		appWrite(r, row{"items": []row{livePublicChannel(c)}, "total": 1, "page": 1, "size": 1}, err)
		return
	}
	items, total, err := liveChannels(r.Context(), r.Get("group_id").Int64(), r.Get("q").String(), page, size)
	public := make([]row, 0, len(items))
	for _, item := range items {
		public = append(public, livePublicChannel(item))
	}
	appWrite(r, row{"items": public, "total": total, "page": page, "size": size}, err)
}
func liveOrderedStreams(streams []LiveStream, requested int64, exclude []int64) []LiveStream {
	blocked := map[int64]bool{}
	for _, id := range exclude {
		blocked[id] = true
	}
	result := []LiveStream{}
	for _, s := range streams {
		if s.Enabled && !blocked[s.ID] && (requested == 0 || requested == s.ID) {
			result = append(result, s)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if (a.Health == "healthy") != (b.Health == "healthy") {
			return a.Health == "healthy"
		}
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		return a.ID < b.ID
	})
	return result
}
func liveResolve(ctx context.Context, channelID, streamID int64, exclude []int64) (row, error) {
	if channelID < 1 || streamID < 0 || len(exclude) > 100 {
		return nil, appError(400, "直播频道或线路参数无效")
	}
	channel, err := liveChannel(ctx, channelID)
	if err != nil || !channel.Enabled {
		return nil, appError(404, "直播频道不存在或已停用")
	}
	streams, err := liveStreams(ctx, channelID)
	if err != nil {
		return nil, err
	}
	candidates := liveOrderedStreams(streams, streamID, exclude)
	if len(candidates) == 0 {
		return nil, appError(404, "该频道暂无可用直播线路")
	}
	// Probe only the chosen line. Clients exclude it and ask for the next one on
	// failure; a large subscription never turns a channel click into a full scan.
	selected := candidates[0]
	if err = liveStreamAccess(ctx, selected, liveViewerFromContext(ctx)); err != nil {
		return nil, err
	}
	runtime, err := liveMaterializeStream(ctx, selected, 0, 0)
	var probe LiveProbe
	if err == nil {
		probe, err = livePlaybackProbe(ctx, runtime)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		_ = liveSetHealth(ctx, selected.ID, false, "播放检测失败")
		return row{"stream_id": selected.ID, "streams": livePublicStreams(streams)}, &AppError{Status: 502, Message: "当前直播线路不可用，请尝试备用线路"}
	}
	_ = liveSetHealth(ctx, selected.ID, true, probe.Quality)
	mode := "live"
	programmeID, durationMS := int64(0), int64(0)
	if runtime.SourceMode == "event_replay" {
		mode = "event_replay"
		programmeID = runtime.ProgrammeID
		durationMS = runtime.DurationMS
	}
	return livePlaybackDescription(ctx, channel, selected, runtime, probe, mode, programmeID, durationMS)
}

func livePlaybackDescription(ctx context.Context, channel LiveChannel, selected, runtime LiveStream, probe LiveProbe, mode string, programmeID, durationMS int64) (row, error) {
	token, item, err := newLiveSession(channel.ID, selected.ID)
	if err != nil {
		return nil, err
	}
	qualities := make([]LiveQuality, 0, len(probe.Qualities))
	item.SourceURL = selected.URL
	item.Viewer = liveViewerFromContext(ctx)
	item.RuntimeStream = runtime
	item.SourceRevision = selected.SourceRevision
	item.Mode = mode
	item.ProgrammeID = programmeID
	item.SourceExpire = runtime.ExpiresAt
	for _, q := range probe.Qualities {
		q.URL = item.link(token, q.URL, true)
		qualities = append(qualities, q)
	}
	mime := "application/x-mpegURL"
	if runtime.MediaType == "mp4" {
		mime = "video/mp4"
	}
	streams, err := liveStreams(ctx, channel.ID)
	if err != nil {
		return nil, err
	}
	return row{"channel_id": channel.ID, "name": channel.Name, "logo": channel.Logo, "stream_id": selected.ID, "stream_name": selected.Name, "url": item.link(token, probe.URL, true), "mime_type": mime, "expires_at": item.Expires.Unix(), "session_id": token, "qualities": qualities, "streams": livePublicStreams(streams), "is_live": mode == "live", "mode": mode, "programme_id": programmeID, "event_id": runtime.EventID, "duration_ms": max(int64(0), durationMS)}, nil
}

func liveReplayResolveHandler(r *ghttp.Request) {
	programme, stream, err := liveReplayProgramme(r.Context(), r.Get("programme_id").Int64(), r.Get("stream_id").Int64())
	if err != nil {
		appWrite(r, nil, err)
		return
	}
	runtime, err := liveMaterializeStream(r.Context(), stream, programme.Start, programme.Stop)
	if err != nil {
		appWrite(r, nil, err)
		return
	}
	probe, err := livePlaybackProbe(r.Context(), runtime)
	if err != nil {
		appWrite(r, nil, appError(502, "平台暂未提供此节目回看，请稍后重试"))
		return
	}
	channel, err := liveChannel(r.Context(), programme.ChannelID)
	if err != nil {
		appWrite(r, nil, err)
		return
	}
	durationMS := runtime.DurationMS
	if durationMS == 0 && programme.Stop > programme.Start && programme.Start > 0 {
		durationMS = (programme.Stop - programme.Start) * 1000
	}
	data, err := livePlaybackDescription(r.Context(), channel, stream, runtime, probe, programme.Kind, programme.ID, durationMS)
	if data != nil {
		data["programme_title"] = programme.Title
	}
	appWrite(r, data, err)
}

func liveRefreshSessionSource(ctx context.Context, item *liveMediaSession) error {
	stream, err := liveStream(ctx, item.StreamID)
	if err != nil {
		return err
	}
	if err = liveStreamAccess(ctx, stream, item.Viewer); err != nil {
		return err
	}
	start, end := int64(0), int64(0)
	if item.ProgrammeID > 0 {
		p, _, e := liveReplayProgramme(liveContextWithViewer(ctx, item.Viewer), item.ProgrammeID, item.StreamID)
		if e != nil {
			return e
		}
		start, end = p.Start, p.Stop
	}
	runtime, err := liveMaterializeStream(ctx, stream, start, end)
	if err != nil {
		return err
	}
	item.mu.Lock()
	old := item.RuntimeStream.URL
	for id, asset := range item.Assets {
		if asset.Permanent && asset.URL == old {
			asset.URL = runtime.URL
			item.Assets[id] = asset
		}
	}
	item.RuntimeStream = runtime
	item.SourceExpire = runtime.ExpiresAt
	item.mu.Unlock()
	return nil
}
func liveResolveHandler(r *ghttp.Request) {
	excluded := r.Get("exclude_stream_ids").Int64s()
	data, err := liveResolve(r.Context(), r.Get("channel_id").Int64(), r.Get("stream_id").Int64(), excluded)
	// Error data preserves the failed stream identity for an automatic next line.
	if err != nil && data != nil {
		r.Response.Header().Set("Cache-Control", "private, no-store")
		r.Response.WriteHeader(502)
		r.Response.WriteJson(row{"code": 502, "message": "当前直播线路不可用，请尝试备用线路", "data": data})
		return
	}
	appWrite(r, data, err)
}
