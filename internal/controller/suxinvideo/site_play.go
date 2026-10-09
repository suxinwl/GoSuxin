package suxinvideo

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/url"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

type playEpisode struct {
	Name   string `json:"name"`
	Key    string `json:"key"`
	Number int    `json:"number"`
	URL    string `json:"url"`
	PSrc   string `json:"psrc"`
	Iframe string `json:"iframe"`
}
type playSource struct {
	Code       string        `json:"code"`
	BaseCode   string        `json:"base_code,omitempty"`
	VersionKey string        `json:"version_key,omitempty"`
	Name       string        `json:"name"`
	Episodes   []playEpisode `json:"episodes"`
	ReportURL  string        `json:"report_url,omitempty"`
}

func safePlayerAddress(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Host != "" && u.User == nil && (u.Scheme == "https" || u.Scheme == "http")
}

func playerViewSources(ctx context.Context, vodID int64, input []source) []playSource {
	globalRule := strings.TrimSpace(setting(ctx, "player_parse", ""))
	result := make([]playSource, 0, len(input))
	for sourceIndex, src := range input {
		owner := vodAliasOwnerSource(src)
		ownerID := vodID
		if src.OwnerVodID > 0 {
			ownerID = src.OwnerVodID
		}
		nativeErciyuan := strings.HasPrefix(owner.Code, "ecy_") && erciyuanAllowedSource(owner)
		rule := strings.TrimSpace(src.Parse)
		if rule == "" {
			rule = globalRule
		}
		view := playSource{Code: src.Code, BaseCode: owner.Code, VersionKey: src.VersionKey, Name: src.Name, Episodes: make([]playEpisode, 0, len(src.Episodes))}
		if rule == "" {
			view.ReportURL = sourceHealthReportURL(ctx, ownerID, owner)
		}
		for episodeIndex, ep := range src.Episodes {
			key, number := playbackEpisodeIdentity(ep.Name)
			item := playEpisode{Name: ep.Name, Key: key, Number: number, URL: ep.URL}
			if (owner.Code == "hongguo" && strings.HasPrefix(ep.URL, "hongguo://")) || (owner.Code == "4kvm" && strings.HasPrefix(ep.URL, "4kvm://")) || (strings.HasPrefix(owner.Code, "yqk_") && strings.HasPrefix(ep.URL, "yqk://")) || nativeErciyuan {
				item.PSrc = fmt.Sprintf("/suxinvideo/native/resolve?vod_id=%d&source=%d&episode=%d&line=%s", ownerID, sourceIndex, episodeIndex, url.QueryEscape(owner.Code))
				view.Episodes = append(view.Episodes, item)
				continue
			}
			if !safePlayerAddress(ep.URL) {
				view.Episodes = append(view.Episodes, item)
				continue
			}
			parsedPage, _ := url.Parse(ep.URL)
			if rule == "" && strings.HasSuffix(src.Code, "yun") && strings.Contains(parsedPage.Path, "/play/") && !strings.HasSuffix(strings.ToLower(parsedPage.Path), ".m3u8") {
				item.Iframe = ep.URL
				view.Episodes = append(view.Episodes, item)
				continue
			}
			direct := ep.URL
			if strings.HasPrefix(strings.ToLower(rule), "m3u8:") {
				direct = strings.ReplaceAll(rule[6:], "{url}", ep.URL)
			} else if rule != "" && strings.Contains(rule, "{url}") {
				candidate := strings.ReplaceAll(rule, "{url}", url.QueryEscape(ep.URL))
				if safePlayerAddress(candidate) {
					item.Iframe = candidate
				}
			}
			if safePlayerAddress(direct) {
				item.PSrc = direct
				parsed, _ := url.Parse(direct)
				if strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8") {
					item.PSrc = proxyLink(ctx, direct)
				}
			}
			view.Episodes = append(view.Episodes, item)
		}
		result = append(result, view)
	}
	return result
}

func renderPlay(ctx context.Context, req *PlayReq) (*PlayRes, error) {
	vod, err := one(ctx, "SELECT * FROM sx_vod WHERE id=? AND "+publicVodCondition(ctx, ""), req.ID)
	if err != nil {
		return nil, err
	}
	if vod == nil {
		notFound(ctx)
		return &PlayRes{}, nil
	}
	if pic := gconv.String(vod["pic"]); strings.HasPrefix(pic, "http://") || strings.HasPrefix(pic, "https://") {
		vod["pic"] = imageLink(ctx, pic)
	}
	types, err := all(ctx, "SELECT id,name FROM sx_type WHERE pid=0 AND status=1 ORDER BY sort,id LIMIT 30")
	if err != nil {
		return nil, err
	}
	originalSources := playlist(vod)
	rawSources, err := hydratePlayers(ctx, vod, originalSources)
	if err != nil {
		return nil, err
	}
	si := playSourceIndex(rawSources, req.Line, req.Source, requestHasSource(ctx))
	ei := req.Episode
	if len(rawSources) > 0 && (ei < 0 || ei >= len(rawSources[si].Episodes)) {
		ei = 0
	}
	user, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	lock := ""
	points := gconv.Int(vod["points"])
	if gconv.Int(vod["vip"]) == 1 {
		if user == nil {
			lock = "login"
		} else if gconv.Int64(user["vip_expire"]) <= time.Now().Unix() {
			lock = "vip"
		}
	} else if points > 0 {
		if user == nil {
			lock = "login"
		} else {
			owned, e := one(ctx, "SELECT id FROM sx_user_vod WHERE user_id=? AND vod_id=?", user["id"], req.ID)
			if e != nil {
				return nil, e
			}
			if owned == nil {
				lock = "points"
				if gconv.Int(user["points"]) >= points {
					lock = "buy"
				}
			}
		}
	}
	startPos := int64(0)
	if user != nil && len(rawSources) > 0 {
		record, e := one(ctx, "SELECT episode,position FROM sx_play_record WHERE user_id=? AND vod_id=?", user["id"], req.ID)
		if e != nil {
			return nil, e
		}
		if record != nil && gconv.Int(record["episode"]) == ei+1 {
			startPos = gconv.Int64(record["position"])
		}
		if lock == "" {
			_ = execSQL(ctx, "INSERT INTO sx_play_record(user_id,vod_id,episode,position,updated) VALUES(?,?,?,0,?) ON DUPLICATE KEY UPDATE episode=VALUES(episode),updated=VALUES(updated)", user["id"], req.ID, ei+1, time.Now().Unix())
		}
	}
	comments, err := all(ctx, "SELECT c.content,c.created,u.name,u.avatar FROM sx_comment c JOIN sx_user u ON u.id=c.user_id WHERE c.vod_id=? AND c.status=1 ORDER BY c.id DESC LIMIT 20", req.ID)
	if err != nil {
		return nil, err
	}
	related, err := all(ctx, "SELECT id,name,pic,remarks,year,score,area,vip FROM sx_vod WHERE "+publicVodListingCondition(ctx, "")+" AND type_id=? AND id<>? ORDER BY total_hits DESC,id DESC LIMIT 12", vod["type_id"], req.ID)
	if err != nil {
		return nil, err
	}
	sources := playerViewSources(ctx, req.ID, rawSources)
	movie, err := moviePlaybackCategory(ctx, gconv.Int64(vod["type_id"]))
	if err != nil {
		return nil, err
	}
	// Distinguish whole-film quality/version labels from real split releases,
	// including evidence carried by hidden or disabled lines.
	movie = moviePlaybackAlignmentAllowed(gconv.String(vod["name"]), movie, originalSources)
	alignMoviePlaybackSources(gconv.String(vod["name"]), movie, sources)
	shortDrama, err := shortDramaPlaybackCategory(ctx, vod, originalSources)
	if err != nil {
		return nil, err
	}
	anime := false
	if !shortDrama {
		anime, err = animePlaybackCategory(ctx, vod)
		if err != nil {
			return nil, err
		}
	}
	preferredProvider, preferredProviderName := playbackPreferredProvider(shortDrama, anime)
	requestedKey := requestedPlaybackKey(req, originalSources, sources, si, movie, gconv.String(vod["name"]))
	requestedKey, compilationFallback := shortDramaCompilationStart(req, originalSources, sources, si, requestedKey, shortDrama, startPos)
	if si < 0 || si >= len(sources) || ei < 0 || ei >= len(sources[si].Episodes) || sources[si].Episodes[ei].Key != requestedKey {
		startPos = 0
	}
	encoded, err := json.Marshal(map[string]any{
		"id": req.ID, "name": vod["name"], "poster": vod["pic"], "remarks": vod["remarks"], "sources": sources,
		"discovery": discoveryURLs(ctx, req.ID), "lock": lock, "requestedKey": requestedKey,
		"preferredProvider": preferredProvider, "preferredProviderName": preferredProviderName,
		"compilationFallback":    compilationFallback,
		"preloadNext":            shortDrama && lock == "",
		"requestedEpisodeNumber": max(0, req.Episode) + 1,
	})
	if err != nil {
		return nil, err
	}
	userPoints := 0
	if user != nil {
		userPoints = gconv.Int(user["points"])
	}
	render(ctx, gconv.String(vod["name"]), types, playThemeBody, map[string]any{
		"Vod": vod, "VodDescription": filmDescription(gconv.String(vod["content"])), "Sources": sources, "Comments": comments, "Related": related,
		"SourceIndex": si, "EpisodeIndex": ei, "Lock": lock, "Points": points,
		"UserPoints": userPoints, "Logged": user != nil, "StartPos": startPos,
		"Autoplay": setting(ctx, "player_autoplay", "1") == "1", "PlayerJSON": template.JS(encoded),
	})
	_ = execSQL(ctx, "UPDATE sx_vod SET total_hits=total_hits+1 WHERE id=?", req.ID)
	return &PlayRes{}, nil
}

func requestHasSource(ctx context.Context) bool {
	return g.RequestFromCtx(ctx).URL.Query().Has("source")
}

// A disabled or hidden line can still identify the requested chapter. Keep
// that identity when a later background lookup supplies another playable line.
func requestedPlaybackKey(req *PlayReq, original []source, visible []playSource, selected int, movie bool, title string) string {
	index := max(0, req.Episode)
	fromOriginal := func(src source) string {
		if index >= len(src.Episodes) {
			return ""
		}
		label := src.Episodes[index].Name
		key, _ := moviePlaybackEpisodeKey(title, label, movie)
		return key
	}
	if req.Line != "" {
		for _, src := range original {
			if src.Code == req.Line {
				if key := fromOriginal(src); key != "" {
					return key
				}
			}
		}
	}
	if selected >= 0 && selected < len(visible) && index < len(visible[selected].Episodes) {
		return visible[selected].Episodes[index].Key
	}
	if req.Source >= 0 && req.Source < len(original) {
		if key := fromOriginal(original[req.Source]); key != "" {
			return key
		}
	}
	return fmt.Sprintf("episode:%d", index+1)
}

const playThemeBody = `<style>
.cms-body #playerBox>#suxinplayer{min-height:0!important}
.cms-body .sx-iframe-player{position:relative;width:100%;height:100%;min-height:0}
.cms-body #playerBox .sx-iframe-player>iframe{display:block;width:100%;height:100%!important;min-height:0!important}
.cms-body .sx-iframe-nextbar{position:absolute;left:0;right:0;bottom:0;display:flex;align-items:center;justify-content:flex-end;height:44px;padding:3px 8px;background:linear-gradient(transparent,rgba(0,0,0,.8));box-sizing:border-box}
.cms-body .sx-iframe-nextbar[hidden]{display:none}
.cms-body .sx-iframe-nextbar button{min-height:36px;padding:6px 10px;border:1px solid #555;border-radius:6px;background:#242424;color:#fff;font-size:13px;cursor:pointer}
/* Controls overlay the picture; no extra row or metadata-driven minimum
   height may shrink a fullscreen frame or widen a mobile grid column. */
.cms-body #playerBox>#suxinplayer{position:absolute;inset:0;height:100%;width:100%;min-width:0}
</style><div class="play-wrap">
 <div><div class="player-box" id="playerBox">
 {{if .Lock}}<div class="plock"><div class="lk">{{if eq .Lock "vip"}}👑{{else if eq .Lock "login"}}🔒{{else}}💎{{end}}</div>
 {{if eq .Lock "login"}}<p>本片需要登录后观看</p><a href="/suxinvideo/login">立即登录</a>
 {{else if eq .Lock "vip"}}<p>VIP 专享影片，开通会员即可观看</p><a href="/suxinvideo/pay">开通 VIP 会员</a>
 {{else if eq .Lock "buy"}}<p>本片需消耗 {{.Points}} 积分解锁，当前余额 {{.UserPoints}} 积分</p><button id="buyVod" type="button">积分解锁本片</button>
 {{else}}<p>积分不足，本片需 {{.Points}} 积分解锁</p><a href="/suxinvideo/pay">前往充值</a>{{end}}</div>
 {{else if not .Sources}}<div class="plock"><p>暂无可用播放线路，片源可能已停用或暂时失效，请稍后重试</p></div>
 {{else}}<div id="suxinplayer" style="width:100%;height:100%"></div>{{end}}
 </div><div class="vdesc" style="margin-top:16px"><b>{{.Vod.name}}</b><span style="color:var(--sub);margin-left:10px">{{.Vod.remarks}} · 播放来源：<span id="srcName"></span></span><span id="lineNotice" style="color:#f5a623;margin-left:10px"></span><div id="discoveryNotice" role="status" aria-live="polite" style="color:var(--sub);font-size:12px;margin-top:8px"></div><p style="margin-top:10px">{{.VodDescription}}</p></div></div>
 <aside class="pinfo"><div class="tabs"><a href="#playerBox" class="on">详情</a><a href="#comments">评论({{len .Comments}})</a></div><h1>{{.Vod.name}}</h1><div class="pmeta">{{.Vod.year}} / {{.Vod.area}} / {{.Vod.class}}</div><div class="pscore">{{if scoreAvailable .Vod}}{{scoreSource .Vod}}：{{end}}<b>{{scoreLabel .Vod}}</b></div><div class="pacts"><button type="button" id="favVod">☆ 收藏影片</button><button type="button" id="shareVod">🔗 影片分享</button></div><div class="ep-h"><b>资源列表</b></div><div id="sourceTabs" class="sx-source-tabs">{{range $i,$src:=.Sources}}<button type="button" data-source="{{$i}}" class="pm">{{$src.Name}}</button>{{end}}</div><div class="ep-grid" id="epGrid"></div></aside>
 </div><section class="sec"><div class="sec-h"><h3>相关视频</h3></div>{{template "cards" .Related}}</section><section class="sec" id="comments"><div class="sec-h"><h3>评论</h3></div><div class="vdesc" style="padding:18px">{{if .Logged}}<form id="commentForm"><input type="text" name="content" maxlength="300" placeholder="发表你的看法…" required><button type="submit">发表</button></form>{{else}}<p><a href="/suxinvideo/login">登录</a> 后参与评论</p>{{end}}<div id="commentList">{{range .Comments}}<div class="cmt"><div class="av"><img src="{{if .avatar}}{{.avatar}}{{else}}{{$.DefaultAvatar}}{{end}}" alt="" style="width:100%;height:100%;object-fit:contain;border-radius:50%"></div><div class="bd"><span class="nm">{{.name}}</span><p>{{.content}}</p></div></div>{{else}}<p>暂无评论，快来抢沙发~</p>{{end}}</div></div></section>
 <script src="/suxinvideo/asset?theme={{.Theme}}&file=hls.js&v=2.3.19"></script><script src="/suxinvideo/asset?theme={{.Theme}}&file=suxinplayer.js&v=2.3.37"></script><script src="/suxinvideo/asset?theme={{.Theme}}&file=source-select.js&v=2.3.37"></script><script src="/suxinvideo/asset?theme={{.Theme}}&file=source-discovery.js&v=2.3.19"></script><script src="/suxinvideo/asset?theme={{.Theme}}&file=episode-next.js&v=2.3.37"></script>
 <script>(function(){const vod={{.PlayerJSON}},autoplay={{.Autoplay}},startPos={{.StartPos}},logged={{.Logged}};let sid={{.SourceIndex}},ep={{.EpisodeIndex}},player=null,buildSeq=0,resolveAbort=null,sourceSelection=null,discoveryTask=null,pendingDiscovery=null,manualSourceCode='',pageGone=false,attempted=new Set(),compilationFallback=vod.compilationFallback||null,pageSuspended=false,pendingStreamFailure=null,selectionSuspended=false,reloadSnapshot=null;const nativeRefreshes=new Map(),$=s=>document.querySelector(s),sources=vod.sources||[];let resumePosition=Math.max(0,Number(startPos)||0);
 function episodeKey(item){const key=item?.key||('label:'+(item?.name||'').trim());return key==='label:'?'':key}
 let resumeKey=vod.requestedKey||episodeKey(sources[sid]?.episodes[ep])||('episode:'+(Number(vod.requestedEpisodeNumber)||1));
 let nextPreloadTimer=0;
 const nextCache=window.SuxinEpisodeNext?.createCache({create:createNextPreload,timeoutMs:20000,ttlMs:300000});
 function getNextEpisode(){const next=window.SuxinEpisodeNext?.findNext(sources,sid,ep);if(next)next.quality=player?.opts?.qualities?.[player.opts.qualityIndex]?.label||'';return next||null}
 function cancelNextPreload(){clearTimeout(nextPreloadTimer);nextPreloadTimer=0;nextCache?.cancel()}
 async function createNextPreload(target,signal){
  const media=await resolvePlaybackItem(target.item,signal);if(signal.aborted||pageGone)throw new DOMException('播放已切换','AbortError');
  const choice=target.quality?media.qualities.find(q=>q.label===target.quality):null;if(choice){media.url=choice.url;media.type=choice.type||media.type;media.duration=Number(choice.duration)||media.duration;media.qualities=media.qualities.map(q=>({...q,selected:q===choice}))}
  const element=document.createElement('div');element.style.cssText='position:fixed;left:-10000px;top:0;width:640px;height:360px;visibility:hidden;pointer-events:none';element.setAttribute('aria-hidden','true');element.inert=true;element.dataset.sxNextPreload='loading';document.body.appendChild(element);
  let instance;try{
   instance=makePlaybackPlayer(element,media,0,target.episode);instance.video.muted=true;instance.video.preload='auto';
   if(instance._keyHandler){document.removeEventListener('keydown',instance._keyHandler);instance._sxSelectionDetached=true}
   const hls=instance.hls,original=hls?{maxBufferLength:hls.config.maxBufferLength,maxMaxBufferLength:hls.config.maxMaxBufferLength,maxBufferSize:hls.config.maxBufferSize}:null;let stopped=false;
   const limit=()=>{if(hls&&!stopped&&instance.video.buffered.length&&instance.video.buffered.end(0)>=8){stopped=true;hls.stopLoad()}};
   if(hls){Object.assign(hls.config,{maxBufferLength:8,maxMaxBufferLength:12,maxBufferSize:12*1024*1024});instance.video.addEventListener('progress',limit);instance.video.addEventListener('canplay',limit)}
   return {player:instance,element:element,resume:()=>{instance.video.removeEventListener('progress',limit);instance.video.removeEventListener('canplay',limit);if(hls&&!instance._destroyed){Object.assign(hls.config,original);hls.startLoad(-1)}}};
  }catch(error){if(instance)instance.destroy();element.remove();throw error}
 }
 function refreshNextControls(){const next=getNextEpisode(),seq=buildSeq,hasNext=Boolean(!vod.lock&&next&&!pageGone);if(player?.setNextEpisode)player.setNextEpisode(hasNext,()=>goToNext(seq));const bar=$('#suxinplayer .sx-iframe-nextbar');if(bar){bar.hidden=!hasNext;bar.parentElement.classList.toggle('sx-player-controls',hasNext)}}
 function scheduleNextPreload(){clearTimeout(nextPreloadTimer);nextPreloadTimer=0;if(!nextCache||!vod.preloadNext||vod.lock||pageGone||document.hidden)return;const next=getNextEpisode();if(!next||!next.item.psrc||next.item.iframe){nextCache.cancel();return}const current=player,seq=buildSeq;if(!current||current._destroyed||current._failed||!current.video||current.video.paused||current.video.readyState<3)return;nextPreloadTimer=setTimeout(()=>{nextPreloadTimer=0;if(seq===buildSeq&&player===current&&!document.hidden&&!current.video.paused)nextCache.start(getNextEpisode())},1000)}
 function bindNextPlayback(instance,seq){refreshNextControls();const kick=()=>{if(seq===buildSeq&&player===instance)scheduleNextPreload()};instance.video?.addEventListener('playing',kick);instance.video?.addEventListener('pause',()=>{if(player===instance){clearTimeout(nextPreloadTimer);nextPreloadTimer=0}});kick()}
 function goToNext(seq){if(seq!==buildSeq||vod.lock||pageGone)return;const next=getNextEpisode();if(!next)return;if(next.source!==sid)manualSourceCode='';selectEpisode(next.source,next.episode,false)}
 function episodeGroups(src){const groups=new Map();src.episodes.forEach((item,index)=>{const key=episodeKey(item)||'#'+index,groupKey=key==='movie:feature'?key+':'+(item.name||'').trim().toLowerCase():key;if(!groups.has(groupKey))groups.set(groupKey,{key,name:item.name,number:Number.isInteger(item.number)?item.number:-1,indexes:[],first:index});groups.get(groupKey).indexes.push(index)});return [...groups.values()].sort((a,b)=>a.number>=0&&b.number>=0?a.number-b.number||a.first-b.first:a.number>=0?1:b.number>=0?-1:a.first-b.first)}
 function preferredEpisode(src,key){let found=-1;if(!key||!src)return found;src.episodes.forEach((item,index)=>{if(episodeKey(item)===key&&(item.psrc||item.iframe))found=index});return found}
 function sourcePreference(src){const code=src?.base_code||src?.code||'',app=code==='yqk_1',xiaoqi=/^yqk_/.test(code);if(vod.preferredProvider==='hongguo')return code==='hongguo'?0:1;if(vod.preferredProvider==='erciyuan')return /^ecy_/.test(code)?0:app?1:xiaoqi?2:3;if(vod.preferredProvider==='yqk')return app?0:xiaoqi?1:2;return 0}
 function sourceVersionCompatible(src){const current=sources[sid];return !current?.version_key||!src?.version_key||current.version_key===src.version_key}
 function directPreferredCandidate(candidates){if(!vod.preferredProvider)return null;const limit=vod.preferredProvider==='erciyuan'?2:vod.preferredProvider==='yqk'?1:0;return candidates.filter(candidate=>sourcePreference(sources[candidate.source])<=limit).sort((a,b)=>sourcePreference(sources[a.source])-sourcePreference(sources[b.source]))[0]||null}
 function canonicalURL(){const src=sources[sid];if(!src)return;const query=new URLSearchParams({id:String(vod.id),source:String(sid),episode:String(ep)});if(src.code)query.set('line',src.code);history.replaceState(null,'','/suxinvideo/play?'+query)}
 function savePlaybackSnapshot(){const src=sources[sid],item=src?.episodes[ep],video=player?.video;if(!src||!item||!video||player._destroyed||pageGone)return;try{sessionStorage.setItem('sx_play_resume_'+vod.id,JSON.stringify({id:vod.id,code:src.code,episode:ep,name:item.name,key:episodeKey(item),version:src.version_key||'',position:Math.max(0,Number(video?.currentTime)||resumePosition||0),paused:Boolean(video?.paused),saved:Date.now()}))}catch(_){}}
 function restorePlaybackSnapshot(){try{const navigation=performance.getEntriesByType('navigation')[0];if(navigation?.type!=='reload'&&!document.wasDiscarded)return null;const saved=JSON.parse(sessionStorage.getItem('sx_play_resume_'+vod.id)||'null'),line=new URL(location.href).searchParams.get('line');if(!saved||saved.id!==vod.id||Date.now()-saved.saved>21600000||Date.now()<saved.saved||!line||saved.code!==line)return null;if(saved.episode!==Number(new URL(location.href).searchParams.get('episode')||0))return null;const index=sources.findIndex(src=>src.code===saved.code&&(!saved.version||!src.version_key||saved.version===src.version_key)),items=sources[index]?.episodes||[],candidate=items[saved.episode],episode=candidate&&episodeKey(candidate)===saved.key&&candidate.name===saved.name?saved.episode:items.findIndex(item=>episodeKey(item)===saved.key&&item.name===saved.name);if(episode<0||!(items[episode].psrc||items[episode].iframe)||!Number.isFinite(saved.position)||saved.position<0)return null;sid=index;ep=episode;resumeKey=saved.key;resumePosition=saved.position;compilationFallback=null;return saved}catch(_){return null}}
 function rememberPosition(){const position=Number(player?.video?.currentTime);if(Number.isFinite(position)&&position>0){resumePosition=position;resumeKey=episodeKey(sources[sid]?.episodes[ep])}savePlaybackSnapshot()}
 function selectEpisode(nextSource,nextEpisode,keepPosition,notice=''){compilationFallback=null;const next=getNextEpisode(),cached=!keepPosition&&next?.source===nextSource&&next.episode===nextEpisode?nextCache?.take(next):null;rememberPosition();cancelNextPreload();const key=episodeKey(sources[nextSource]?.episodes[nextEpisode]);if(!keepPosition||key!==resumeKey||!sourceVersionCompatible(sources[nextSource]))resumePosition=0;resumeKey=key;sid=nextSource;ep=nextEpisode;attempted.clear();nativeRefreshes.clear();$('#lineNotice').textContent=notice;update();canonicalURL();build(true,cached)}
 function update(){const src=sources[sid];if(!src){$('#srcName').textContent='';$('#epGrid').replaceChildren();refreshNextControls();cancelNextPreload();return;}$('#srcName').textContent=src.name;document.querySelectorAll('#sourceTabs button').forEach(b=>b.classList.toggle('on',Number(b.dataset.source)===sid));const grid=$('#epGrid');grid.replaceChildren();episodeGroups(src).forEach(group=>{const a=document.createElement('button');a.type='button';a.className='ep-btn'+(group.indexes.includes(ep)?' on':'');a.textContent=group.name;a.disabled=Boolean(src.activeOnly||group.indexes.every(i=>src.episodes[i].activeOnly||!src.episodes[i].psrc&&!src.episodes[i].iframe));a.onclick=()=>{if(a.disabled)return;manualSourceCode=src.code;const preferred=group.indexes.filter(index=>!src.episodes[index].activeOnly&&(src.episodes[index].psrc||src.episodes[index].iframe)).at(-1);selectEpisode(sid,group.indexes.includes(ep)?ep:preferred??group.indexes[0],group.indexes.includes(ep))};grid.appendChild(a)});refreshNextControls();scheduleNextPreload()}
 function markAttempt(sourceIndex,episodeIndex){const identity=attemptIdentity(sources[sourceIndex],sources[sourceIndex]?.episodes[episodeIndex]);if(!identity)return; sources.forEach((src,si)=>src.episodes.forEach((item,ei)=>{if(attemptIdentity(src,item)===identity)attempted.add(si+':'+ei)}))}
 function nextMatchingEpisode(markCurrent=true){const key=episodeKey(sources[sid]?.episodes[ep])||resumeKey;if(markCurrent)markAttempt(sid,ep);if(!key)return null;for(let offset=0;offset<sources.length;offset++){const nextSource=(sid+offset)%sources.length,src=sources[nextSource];if(src.activeOnly||!sourceVersionCompatible(src))continue;const matches=src.episodes.map((item,index)=>({item,index})).filter(x=>!x.item.activeOnly&&episodeKey(x.item)===key&&(x.item.psrc||x.item.iframe)&&!attempted.has(nextSource+':'+x.index)).sort((a,b)=>Math.abs(a.index-ep)-Math.abs(b.index-ep));if(matches.length)return {source:nextSource,episode:matches[0].index}}return null}
 function fallbackCompilation(auto){const fallback=compilationFallback;compilationFallback=null;if(!fallback||vod.lock||pageGone||resumePosition>0)return false;const index=sources.findIndex(src=>src.code===fallback.code&&!src.activeOnly);if(index<0)return false;const episodeIndex=sources[index].episodes.findIndex(item=>!item.activeOnly&&episodeKey(item)===fallback.key&&(item.psrc||item.iframe));if(episodeIndex<0)return false;sid=index;ep=episodeIndex;resumeKey=fallback.key;resumePosition=0;attempted.clear();nativeRefreshes.clear();update();canonicalURL();$('#lineNotice').textContent='红果分集暂不可用，已回到原合集从头播放';build(auto);return true}
 function cancelSourceSelection(){if(sourceSelection){sourceSelection.cancel();sourceSelection=null}}
 function fail(message){cancelNextPreload();++buildSeq;const notice=$('#lineNotice');if(notice)notice.textContent='';cancelSourceSelection();if(resolveAbort){resolveAbort.abort();resolveAbort=null}if(player){player.destroy();player=null}const box=$('#suxinplayer');if(!box)return;box.replaceChildren();const panel=document.createElement('div');panel.className='sx-play-error';const label=document.createElement('p');label.textContent=message;const retry=document.createElement('button');retry.textContent='重试播放';retry.onclick=()=>{attempted.clear();nativeRefreshes.clear();chooseInitialSource(true)};panel.append(label,retry);const frame=$('#playerBox');if(frame&&(frame._sxPageFullscreen||document.fullscreenElement===frame||document.webkitFullscreenElement===frame)){const close=document.createElement('button');close.type='button';close.textContent=frame._sxPageFullscreen?'退出铺满':'退出全屏';close.onclick=()=>{if(frame._sxPageFullscreen){SuxinPlayer.prototype._pageFullscreen(frame,false);return}try{const exit=document.exitFullscreen||document.webkitExitFullscreen;if(exit){const pending=exit.call(document);if(pending&&pending.then)pending.then(()=>frame.classList.remove('sx-player-expanded'),()=>{})}}catch(_){}};panel.append(close)}box.appendChild(panel);flushDiscoveredSources()}
 function playbackPageVisible(){return !pageGone&&!pageSuspended&&!document.hidden}
 function visibleAbortDeadline(controller,timeout){let remaining=timeout,started=0,timer=0;const pause=()=>{clearTimeout(timer);timer=0;if(started){remaining=Math.max(0,remaining-(Date.now()-started));started=0}},update=()=>{pause();if(playbackPageVisible()){started=Date.now();timer=setTimeout(()=>controller.abort(),remaining)}};document.addEventListener('visibilitychange',update);window.addEventListener('pageshow',update);window.addEventListener('pagehide',pause);update();return ()=>{pause();document.removeEventListener('visibilitychange',update);window.removeEventListener('pageshow',update);window.removeEventListener('pagehide',pause)}}
 function refreshNativePlayback(context){const src=sources[sid],item=src?.episodes[ep];if(!['playback','resume'].includes(context?.phase)||!item?.psrc?.startsWith('/suxinvideo/native/resolve?'))return false;const key=attemptIdentity(src,item);if(nativeRefreshes.has(key))return false;nativeRefreshes.set(key,{quality:player?.opts?.qualities?.[player.opts.qualityIndex]?.label||''});$('#lineNotice').textContent='播放连接中断，正在重新解析当前分集并续播…';build(context?.wasPlaying!==false);return true}
 function streamFailure(seq,message,context){if(seq!==buildSeq)return;if(document.hidden||pageSuspended){pendingStreamFailure={seq,message,context};return}cancelNextPreload();rememberPosition();if(refreshNativePlayback(context))return;const report=context?.url||sources[sid]?.report_url,reportEpisode=context?.episode??ep;if(report)fetch(report+'&episode='+reportEpisode,{method:'POST',credentials:'same-origin',keepalive:true}).catch(()=>{});const next=nextMatchingEpisode();if(!next){if(fallbackCompilation(true))return;fail((message||'播放失败')+'，当前分集没有其他可用线路，请稍后重试');return}sid=next.source;ep=next.episode;manualSourceCode='';$('#lineNotice').textContent=resumePosition>5?'原线路中断，已切换同一集并继续播放':'原线路不可用，已切换同一集';update();canonicalURL();chooseInitialSource(true,true)}
 async function resolvePlaybackItem(item,signal){let media={url:item.psrc,type:/m3u8/i.test(item.psrc)?'m3u8':'mp4',duration:0,qualities:[]};if(item.psrc.startsWith('/suxinvideo/native/resolve?')){const response=await fetch(item.psrc,{credentials:'same-origin',signal:signal}),data=await response.json();if(!response.ok||data.code!==0&&data.code!==1||!data.data?.url)throw Error(data.message||data.msg||'片源解析失败');media={url:data.data.url,type:data.data.type||'mp4',duration:Number(data.data.duration)||0,qualities:Array.isArray(data.data.qualities)?data.data.qualities:[]};const current=sources[sid]?.episodes[ep],hint=current===item?nativeRefreshes.get(attemptIdentity(sources[sid],item))?.quality:'',choice=hint?media.qualities.find(q=>q.label===hint):null;if(choice){media.url=choice.url;media.type=choice.type||media.type;media.duration=Number(choice.duration)||media.duration;media.qualities=media.qualities.map(q=>({...q,selected:q===choice}))}}return media}
 function makePlaybackPlayer(container,media,position,episodeIndex){return new SuxinPlayer({container:container,src:media.url,type:media.type,duration:media.duration,qualities:media.qualities,qualityIndex:media.qualities.findIndex(q=>q.selected),poster:vod.poster,start:position,vodId:vod.id,episode:episodeIndex+1,autoplay:false})}
 function activatePlayer(instance,seq,position,auto){player=instance;instance.opts.autoplay=Boolean(auto);if(instance._sxSelectionDetached&&instance._keyHandler){document.addEventListener('keydown',instance._keyHandler);instance._sxSelectionDetached=false}
  instance.video?.addEventListener('playing',()=>{if(seq===buildSeq&&player===instance){if(compilationFallback&&(sources[sid]?.base_code||sources[sid]?.code)==='hongguo')$('#lineNotice').textContent='已优先 '+sources[sid].name+'，从第1集开始播放';compilationFallback=null}},{once:true});
  instance.opts.onPlayerReplace=p=>{if(seq!==buildSeq){p.destroy();return}player=p;bindNextPlayback(p,seq)};
  bindNextPlayback(instance,seq);
  instance.opts.onProgress=pos=>{if(seq!==buildSeq)return;resumePosition=Number(pos)||0;resumeKey=episodeKey(sources[sid]?.episodes[ep]);savePlaybackSnapshot();if(logged){const form=new FormData();form.set('vod_id',String(vod.id));form.set('episode',String(ep+1));form.set('position',String(pos));fetch('/suxinvideo/progress',{method:'POST',body:form,credentials:'same-origin'}).catch(()=>{})}try{const arr=JSON.parse(localStorage.getItem('sx_watch_hist')||'[]').filter(x=>x.id!==vod.id);arr.unshift({id:vod.id,name:vod.name,pic:vod.poster,ep:ep+1,time:Date.now()});localStorage.setItem('sx_watch_hist',JSON.stringify(arr.slice(0,50)))}catch(e){}};
   const playbackReport={url:sources[sid]?.report_url,episode:ep,phase:'playback'};instance.opts.onStreamError=context=>streamFailure(seq,'播放中断',{...playbackReport,...(context||{})});
  if(instance.video&&nativeRefreshes.has(attemptIdentity(sources[sid],sources[sid]?.episodes[ep]))){instance.video.addEventListener('playing',()=>{if(seq===buildSeq&&player===instance)$('#lineNotice').textContent='已重新连接 '+sources[sid].name+'，继续当前分集';},{once:true})}
  if(position>0&&instance.video&&!instance._nativeMSE){const restore=()=>{if(seq!==buildSeq||player!==instance)return;const video=instance.video;if(Number.isFinite(video.duration)&&position<video.duration-.25){try{video.currentTime=position}catch(e){}}};if(instance.video.readyState>0)restore();else instance.video.addEventListener('loadedmetadata',restore,{once:true})}
  if(auto&&instance.video){const play=instance.video.play();if(play&&play.catch)play.catch(error=>{if(seq!==buildSeq||player!==instance||instance._destroyed||error&&error.name==='AbortError')return;instance.video.muted=true;const silentPlay=instance.video.play();if(silentPlay&&silentPlay.catch)silentPlay.catch(()=>{});if(instance._showUnmute)instance._showUnmute(instance.video,instance.opts.container)})}document.dispatchEvent(new Event('sxpagefullscreenchange'));flushDiscoveredSources();
 }
 async function build(auto,cached=null){if(vod.lock||pageGone){if(cached){cached.player.destroy();cached.element.remove()}return}cancelNextPreload();const preferences=player?.video?{muted:player.video.muted,volume:player.video.volume,playbackRate:player.video.playbackRate}:null;const seq=++buildSeq,box=$('#suxinplayer');cancelSourceSelection();if(resolveAbort){resolveAbort.abort();resolveAbort=null}if(!box||!sources[sid])return;if(player){player.destroy();player=null}box.replaceChildren();const item=sources[sid].episodes[ep];if(!item){fail('暂无可用播放资源');return}const requestedStart=episodeKey(item)===resumeKey?resumePosition:0;markAttempt(sid,ep);if(cached){cached.element.style.cssText='width:100%;height:100%';cached.element.removeAttribute('aria-hidden');cached.element.removeAttribute('data-sx-next-preload');cached.element.inert=false;box.replaceChildren(cached.element);if(preferences){Object.assign(cached.player.video,preferences);Object.assign(cached.player.opts,preferences)}cached.player.opts.episode=ep+1;activatePlayer(cached.player,seq,0,auto);return}if(item.iframe){const frame=document.createElement('iframe');frame.src=item.iframe;frame.allow='autoplay; fullscreen';frame.referrerPolicy='no-referrer';frame.allowFullscreen=true;frame.addEventListener('error',()=>streamFailure(seq,'解析页面加载失败'),{once:true});const shell=document.createElement('div');shell.className='sx-iframe-player';const bar=document.createElement('div');bar.className='sx-iframe-nextbar';const next=document.createElement('button');next.type='button';next.className='sx-next-episode';next.textContent='下一集';next.setAttribute('aria-label','播放下一集');next.onclick=()=>goToNext(seq);bar.appendChild(next);shell.append(frame,bar);box.appendChild(shell);refreshNextControls();flushDiscoveredSources();return}if(!item.psrc){streamFailure(seq,'播放地址无效');return}
  if(item.psrc.startsWith('/suxinvideo/native/resolve?'))box.textContent='正在解析片源…';const controller=new AbortController(),cancelDeadline=visibleAbortDeadline(controller,20000);resolveAbort=controller;let media;
  try{media=await resolvePlaybackItem(item,controller.signal)}catch(error){if(seq===buildSeq)streamFailure(seq,error.name==='AbortError'?'片源解析超时':error.message||'片源解析失败');return}finally{cancelDeadline();if(resolveAbort===controller)resolveAbort=null}
  if(seq!==buildSeq)return;box.replaceChildren();const inner=document.createElement('div');inner.style.cssText='width:100%;height:100%';box.appendChild(inner);activatePlayer(makePlaybackPlayer(inner,media,requestedStart,ep),seq,requestedStart,auto);
 }
 async function chooseInitialSource(auto,recovery=false){if(vod.lock||pageGone)return;if(document.hidden||pageSuspended){selectionSuspended=true;return}cancelNextPreload();const box=$('#suxinplayer');if(!box||!sources[sid])return;const key=episodeKey(sources[sid]?.episodes[ep])||resumeKey,candidates=[],seen=new Set();if(key)for(let offset=0;offset<sources.length;offset++){const sourceIndex=recovery?offset:(sid+offset)%sources.length,src=sources[sourceIndex];if(src.activeOnly||!sourceVersionCompatible(src))continue;src.episodes.forEach((item,episodeIndex)=>{const identity=attemptIdentity(src,item);if(!item.activeOnly&&episodeKey(item)===key&&item.psrc&&!item.iframe&&!attempted.has(sourceIndex+':'+episodeIndex)&&!seen.has(identity)){seen.add(identity);candidates.push({source:sourceIndex,episode:episodeIndex,item:item})}})}
  const preferred=!recovery?directPreferredCandidate(candidates):null;if(preferred){sid=preferred.source;ep=preferred.episode;update();canonicalURL();$('#lineNotice').textContent='默认加载 '+sources[sid].name;build(auto);return}
  if(!window.SuxinSourceSelect||candidates.length<2){const next=candidates.length?candidates[0]:nextMatchingEpisode(false);if(!next){if(fallbackCompilation(auto))return;fail('当前分集的播放线路均已尝试，暂时无法播放，请稍后重试');return}sid=next.source;ep=next.episode;update();canonicalURL();build(auto);return}
  const seq=++buildSeq,position=key===resumeKey?resumePosition:0;cancelSourceSelection();if(resolveAbort){resolveAbort.abort();resolveAbort=null}if(player){player.destroy();player=null}box.textContent='正在优选播放线路…';$('#lineNotice').textContent='正在优选播放线路';
  const preferredName=vod.preferredProviderName||'',hasPreferred=Boolean(preferredName&&candidates.some(candidate=>sourcePreference(sources[candidate.source])===0));
  const selection=window.SuxinSourceSelect.start({candidates:candidates,priority:candidate=>sourcePreference(sources[candidate.source]),concurrency:window.matchMedia?.('(pointer:coarse)').matches?2:4,candidateTimeoutMs:12000,onCandidateFailure:(candidate,reason)=>{if(seq===buildSeq&&(reason==='error'||reason==='timeout'))markAttempt(candidate.source,candidate.episode)},onProgress:state=>{if(seq===buildSeq){const label=hasPreferred?(state.priority===0?'正在优先检测 '+preferredName:preferredName+'暂不可用，正在优选其他播放线路'):'正在优选播放线路';$('#lineNotice').textContent=label+'（已检测 '+state.completed+'/'+state.total+'，正在检测 '+state.active+' 条）'}},create:async(candidate,signal)=>{const media=await resolvePlaybackItem(candidate.item,signal);if(signal.aborted)throw new DOMException('播放已切换','AbortError');const element=document.createElement('div');element.style.cssText='position:fixed;left:-10000px;top:0;width:640px;height:360px;visibility:hidden;pointer-events:none';element.setAttribute('aria-hidden','true');document.body.appendChild(element);let instance;try{instance=makePlaybackPlayer(element,media,position,candidate.episode);if(instance._keyHandler){document.removeEventListener('keydown',instance._keyHandler);instance._sxSelectionDetached=true}return {player:instance,element:element}}catch(error){if(instance)instance.destroy();element.remove();throw error}}});sourceSelection=selection;
  const winner=await selection.promise;if(sourceSelection===selection)sourceSelection=null;if(seq!==buildSeq){if(winner){winner.player.destroy();winner.element.remove()}return}if(!winner){const next=nextMatchingEpisode(false);if(!next){if(fallbackCompilation(auto))return;fail('当前分集的播放线路均已尝试，暂时无法播放，请稍后重试');return}sid=next.source;ep=next.episode;update();canonicalURL();$('#lineNotice').textContent='优选暂未获得可播放画面，继续尝试剩余线路';build(auto);return}
  sid=winner.candidate.source;ep=winner.candidate.episode;markAttempt(sid,ep);update();canonicalURL();winner.element.style.cssText='width:100%;height:100%';winner.element.removeAttribute('aria-hidden');box.replaceChildren(winner.element);$('#lineNotice').textContent=(hasPreferred&&sourcePreference(sources[sid])===0?'已优先 ':'已优选 ')+sources[sid].name+'，首段就绪 '+(winner.elapsedMs/1000).toFixed(1)+' 秒';activatePlayer(winner.player,seq,position,auto);
 }
 function renderSourceTabs(){const tabs=$('#sourceTabs');tabs.replaceChildren();sources.forEach((src,index)=>{if(src.activeOnly)return;const button=document.createElement('button');button.type='button';button.dataset.source=String(index);button.className='pm';button.textContent=src.name;button.onclick=()=>{const key=episodeKey(sources[sid]?.episodes[ep])||resumeKey,match=preferredEpisode(src,key);manualSourceCode=src.code;selectEpisode(index,match>=0?match:0,match>=0,match>=0?'':'该线路没有对应分集，已切换至 '+(src.episodes[0]?.name||'首集'))};tabs.appendChild(button)})}
 function ensurePlaybackBox(){if(vod.lock)return null;let box=$('#suxinplayer');if(!box){box=document.createElement('div');box.id='suxinplayer';box.style.cssText='width:100%;height:100%';$('#playerBox').replaceChildren(box)}return box}
 function flushDiscoveredSources(){if(!pendingDiscovery||pageGone||document.hidden||pageSuspended)return;const pending=pendingDiscovery;pendingDiscovery=null;applyDiscoveredSources(pending.sources,pending.revision)}
 function attemptIdentity(src,item){return src&&item?JSON.stringify([src.code,episodeKey(item),item.url||item.psrc||item.iframe]):''}
 function applyDiscoveredSources(incoming,revision){if(vod.lock||pageGone||!Array.isArray(incoming))return;if(document.hidden||pageSuspended||sourceSelection||resolveAbort){pendingDiscovery={sources:incoming,revision:revision};return}rememberPosition();const current=sources[sid],preserve=Boolean(player&&!player._destroyed&&!player._failed||$('#suxinplayer iframe')),key=episodeKey(current?.episodes[ep])||resumeKey;const plan=SuxinSourceDiscovery.reconcile(sources,incoming,{code:current?.code||'',key:key,preserve:preserve,manualCode:manualSourceCode}),tried=new Set([...attempted].map(value=>{const indexes=value.split(':').map(Number);return attemptIdentity(sources[indexes[0]],sources[indexes[0]]?.episodes[indexes[1]])}));sources.splice(0,sources.length,...plan.sources);attempted=new Set();sources.forEach((src,si)=>src.episodes.forEach((item,ei)=>{if(tried.has(attemptIdentity(src,item)))attempted.add(si+':'+ei)}));sid=plan.source;ep=plan.episode;renderSourceTabs();update();if(plan.matched)canonicalURL();if(preserve||!plan.matched||!plan.changed)return;resumeKey=key;attempted.clear();ensurePlaybackBox();if(manualSourceCode)build(autoplay);else chooseInitialSource(autoplay)}
 async function post(path,form){const response=await fetch(path,{method:'POST',body:form,credentials:'same-origin'});const data=await response.json();if(!response.ok||data.code&&data.code!==0&&data.code!==1)throw Error(data.message||data.msg||'操作失败');return data}
 $('#favVod')?.addEventListener('click',async()=>{const f=new FormData();f.set('vod_id',String(vod.id));try{await post('/suxinvideo/favorite',f);$('#favVod').textContent='已更新收藏'}catch(e){alert(e.message)}});
 $('#shareVod')?.addEventListener('click',()=>navigator.clipboard?.writeText(location.href).then(()=>alert('链接已复制')));
 $('#buyVod')?.addEventListener('click',async()=>{const f=new FormData();f.set('id',String(vod.id));try{await post('/suxinvideo/pay/buyvod',f);location.reload()}catch(e){alert(e.message)}});
 $('#commentForm')?.addEventListener('submit',async e=>{e.preventDefault();const f=new FormData(e.currentTarget);f.set('vod_id',String(vod.id));try{await post('/suxinvideo/comment',f);location.reload()}catch(err){alert(err.message)}});
 document.addEventListener('visibilitychange',()=>{if(document.hidden){rememberPosition();cancelNextPreload();player?.setPageActive?.(false);if(sourceSelection){selectionSuspended=true;++buildSeq;cancelSourceSelection()}}else resumePlaybackPage()});
 window.addEventListener('pagehide',event=>{rememberPosition();if(event.persisted){pageSuspended=true;cancelNextPreload();player?.setPageActive?.(false);if(sourceSelection){selectionSuspended=true;++buildSeq;cancelSourceSelection()}return}pageGone=true;cancelNextPreload();++buildSeq;pendingDiscovery=null;if(discoveryTask)discoveryTask.cancel();cancelSourceSelection();if(resolveAbort)resolveAbort.abort();if(player){player.destroy();player=null}});
 window.addEventListener('pageshow',()=>{pageSuspended=false;resumePlaybackPage()});
 function resumePlaybackPage(){if(!playbackPageVisible())return;player?.setPageActive?.(true);const pending=pendingStreamFailure;pendingStreamFailure=null;if(pending&&pending.seq===buildSeq){streamFailure(pending.seq,pending.message,pending.context);return}if(selectionSuspended){selectionSuspended=false;chooseInitialSource(autoplay,true);return}flushDiscoveredSources();scheduleNextPreload()}
 if(episodeKey(sources[sid]?.episodes[ep])!==resumeKey){let match=preferredEpisode(sources[sid],resumeKey);if(match>=0)ep=match;else{sid=-1;ep=-1;for(let i=0;i<sources.length;i++){match=preferredEpisode(sources[i],resumeKey);if(match>=0){sid=i;ep=match;break}}}}
 reloadSnapshot=restorePlaybackSnapshot();renderSourceTabs();update();canonicalURL();if(!vod.lock&&$('#suxinplayer')){if(sources[sid]){if(reloadSnapshot)build(autoplay&&!reloadSnapshot.paused);else chooseInitialSource(autoplay);}else fail('当前分集暂无可用线路，正在后台查找片源')}
 if(window.SuxinSourceDiscovery&&vod.discovery)discoveryTask=SuxinSourceDiscovery.start({urls:vod.discovery,onState:(_state,text)=>{if(!pageGone)$('#discoveryNotice').textContent=text},onSources:applyDiscoveredSources});
 })();</script>`
