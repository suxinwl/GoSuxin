package suxinvideo

import (
	"context"
	_ "embed"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

// Live pages deliberately use a separate player from VOD. A live channel has
// no film, episode, download identity or finite playback history.
type LivePageReq struct {
	g.Meta `path:"/live" method:"get" noValApi:"1"`
}
type LivePageRes struct{}
type LiveSiteAssetReq struct {
	g.Meta `path:"/live/asset" method:"get" noValApi:"1"`
	File   string `p:"file"`
}
type LiveSiteAssetRes struct{}

//go:embed live_web.js
var liveWebJS []byte

//go:embed live_web.css
var liveWebCSS []byte

//go:embed live_web_epg.js
var liveWebEpgJS []byte

func (*Site) LivePage(ctx context.Context, _ *LivePageReq) (*LivePageRes, error) {
	types, err := all(ctx, "SELECT id,name FROM sx_type WHERE pid=0 AND status=1 ORDER BY sort,id LIMIT 30")
	if err != nil {
		return nil, err
	}
	render(ctx, "电视直播", types, liveSiteBody, map[string]any{})
	return &LivePageRes{}, nil
}

func (*Site) LiveSiteAsset(ctx context.Context, req *LiveSiteAssetReq) (*LiveSiteAssetRes, error) {
	r := g.RequestFromCtx(ctx)
	r.Response.Header().Set("Cache-Control", "public, max-age=3600")
	r.Response.Header().Set("X-Content-Type-Options", "nosniff")
	switch req.File {
	case "live.js":
		r.Response.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		r.Response.Write(liveWebJS)
	case "live.css":
		r.Response.Header().Set("Content-Type", "text/css; charset=utf-8")
		r.Response.Write(liveWebCSS)
	case "epg.js":
		r.Response.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		r.Response.Write(liveWebEpgJS)
	default:
		notFound(ctx)
	}
	return &LiveSiteAssetRes{}, nil
}

const liveSiteBody = `{{define "body"}}<link rel="stylesheet" href="/suxinvideo/live/asset?file=live.css&v=2.5.0">
<section class="sx-live" id="sxLive"><div class="sx-live-heading"><div><h1>电视直播</h1><p>央视 · 卫视 · 港澳台</p></div><span class="sx-live-badge">● 直播</span></div>
<div class="sx-live-layout"><section class="sx-live-view"><div class="sx-live-stage" id="liveStage" tabindex="0" aria-label="直播播放器">
<video id="liveVideo" playsinline webkit-playsinline preload="metadata" aria-label="直播画面"></video><div class="sx-live-shade" id="liveShade"></div>
<div class="sx-live-message" id="liveMessage" role="status" aria-live="polite"><span class="sx-live-spinner" id="liveSpinner" hidden aria-hidden="true"></span><span id="liveMessageText">请选择直播频道</span><div class="sx-live-message-actions"><button id="liveRetry" type="button" hidden>重新连接</button><button id="liveStart" type="button" hidden>点击播放</button><button id="liveMessageChannels" type="button" hidden>选择频道</button></div></div><div class="sx-live-gesture" id="liveGesture" hidden></div>
<div id="liveReplayProgress" class="sx-live-replay-progress" hidden><input id="liveReplaySeek" type="range" min="0" max="1000" value="0" aria-label="回看进度"><span id="liveReplayTime">0:00 / 0:00</span></div>
<div class="sx-live-controls" id="liveControls"><button id="liveToggle" type="button" aria-label="播放">播放</button><button id="liveMute" type="button" aria-label="静音">声音</button><span class="sx-live-now" id="liveModeLabel">● 直播中</span><button id="liveSettingsToggle" type="button" aria-controls="liveSettings" aria-expanded="false" hidden>线路</button><button id="liveFull" type="button">全屏</button></div>
<div class="sx-live-settings" id="liveSettings" hidden><div class="sx-live-settings-heading"><strong>播放设置</strong><button id="liveSettingsChannels" type="button">频道</button><button id="liveSettingsClose" type="button" aria-label="关闭播放设置">关闭</button></div></div>
</div><div class="sx-live-caption"><div><h2 id="liveName">尚未选择频道</h2><span id="liveLine">自动选择可用线路</span></div><button id="liveChoose" type="button">频道列表</button></div>
<div id="liveOptionsDock"><div class="sx-live-options" id="liveOptions"><label><span class="sx-live-label">播放线路</span><select id="liveStreams" aria-label="选择直播线路" disabled><option>自动选择</option></select></label><label id="liveQualityWrap" hidden><span class="sx-live-label">清晰度</span><select id="liveQuality" aria-label="选择清晰度"></select></label><button id="liveEdge" type="button">返回直播</button></div></div><p id="liveHint" class="sx-live-hint" role="status" hidden></p><p class="sx-live-help">实时直播 · 播放中断时自动换线</p><section class="sx-live-epg" aria-label="频道节目单"><div class="sx-live-epg-head"><h3>节目单</h3><input id="liveEpgDate" type="date" aria-label="节目日期"><button id="liveEpgRefresh" type="button">刷新</button></div><p id="liveEpgNow">暂无节目单</p><p id="liveEpgNext"></p><p id="liveEpgStatus" role="status">选择频道后查看节目单</p><ol id="liveEpgList"></ol></section></section>
<aside class="sx-live-catalog" id="liveCatalog"><div class="sx-live-catalog-heading"><h3>频道列表</h3><span>点击直接播放</span></div><form id="liveSearch" role="search"><input id="liveSearchInput" type="search" placeholder="搜索央视、卫视、凤凰台…" maxlength="100" aria-label="频道名称"><button type="submit">搜索</button></form><nav class="sx-live-groups" id="liveGroups" aria-label="频道分组"></nav><p id="liveListStatus" class="sx-live-list-status" role="status"></p><div class="sx-live-channels" id="liveChannels" aria-label="直播频道列表"></div><nav class="sx-live-pages"><button id="livePrev" type="button" disabled>上一页</button><span id="livePage">1 / 1</span><button id="liveNext" type="button" disabled>下一页</button></nav></aside></div></section>
<script src="/suxinvideo/asset?theme={{.Theme}}&file=hls.js&v=2.3.19"></script><script src="/suxinvideo/live/asset?file=live.js&v=2.5.0" defer></script><script src="/suxinvideo/live/asset?file=epg.js&v=2.5.0" defer></script>{{end}}`
