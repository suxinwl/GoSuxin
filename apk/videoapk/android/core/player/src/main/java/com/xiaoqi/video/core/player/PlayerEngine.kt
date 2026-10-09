@file:androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
package com.xiaoqi.video.core.player

import android.content.Context
import android.content.Intent
import androidx.media3.common.*
import androidx.media3.datasource.DataSource
import androidx.media3.datasource.okhttp.OkHttpDataSource
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.DefaultLoadControl
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import com.xiaoqi.video.core.data.*
import com.xiaoqi.video.core.model.*
import com.xiaoqi.video.core.network.ApiException
import com.xiaoqi.video.core.network.SiteHttp
import com.xiaoqi.video.core.network.JsonWire.string
import com.xiaoqi.video.core.network.JsonWire.long
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*

data class PlayerState(val descriptor:Playback?=null,val detail:Detail?=null,val loading:Boolean=false,val playing:Boolean=false,val position:Long=0,val duration:Long=0,val buffered:Long=0,val error:String="",val quality:String="自动",val trackQualities:List<Pair<String,String>> = emptyList(),val subtitles:List<Pair<String,String>> = emptyList(),val offline:Boolean=false,val loadingMessage:String="正在连接播放源",val errorStatus:Int=0,val discovery:SourceDiscoveryProgress=SourceDiscoveryProgress(),val requested:PlaybackIdentity?=null) {
    val episodeLabel:String get()=PlaybackLabelRules.episodeName(detail,descriptor,requested)
}

class PlayerEngine(private val context:Context,private val repo:AppRepository) {
    private val scope=CoroutineScope(SupervisorJob()+Dispatchers.Main.immediate)
    var networkFactory:DataSource.Factory = OkHttpDataSource.Factory(SiteHttp.callFactory).setUserAgent("XiaoqiVideo/1.0 Android")
    var offlineFactory:DataSource.Factory?=null
    val player=ExoPlayer.Builder(context).setMediaSourceFactory(DefaultMediaSourceFactory(context).setDataSourceFactory(networkFactory)).setLoadControl(DefaultLoadControl.Builder().setBufferDurationsMs(12000,45000,1000,2000).build()).build()
    val state=MutableStateFlow(PlayerState())
    val live=LivePlaybackController(context,repo,player) { stop();state.value=PlayerState() }
    private var loadJob:Job?=null
    private var discoveryJob:Job?=null
    private val discoveryAttempted=mutableSetOf<String>()
    private var requestedIdentity:PlaybackIdentity?=null
    private var resolvingIdentity=false
    private var metadataRefreshed=false
    private var openingRetry:(()->Unit)?=null
    private var preloadJob:Job?=null
    private var preloadPlayer:ExoPlayer?=null
    private var preloaded:Playback?=null
    private val attempted=linkedSetOf<String>()
    private var refreshed=false
    private var savedAt=0L
    private var selectedQuality=""
    private var owner=0L
    private var backgroundPaused=false
    private var desiredPlay=false
    private var wasActive=false
    private var offlineKeys:List<StreamKey> = emptyList()
    private var offlineDownloadId=""
    private var offlineGuard:(()->String?)?=null

    init {
        player.addListener(object:Player.Listener {
            override fun onIsPlayingChanged(isPlaying:Boolean) { if(!live.state.value.active)state.value=state.value.copy(playing=isPlaying) }
            override fun onPlaybackStateChanged(playbackState:Int) {
                if(live.state.value.active)return
                state.value=state.value.copy(loading=playbackState==Player.STATE_BUFFERING,loadingMessage=if(playbackState==Player.STATE_BUFFERING)"正在缓冲视频" else state.value.loadingMessage)
                if(playbackState==Player.STATE_ENDED && !state.value.offline)next()
                if(playbackState==Player.STATE_READY) { updateTime();schedulePreload() }
            }
            override fun onPlayerError(error:PlaybackException) {
                if(live.state.value.active)return
                if(state.value.offline)state.value=state.value.copy(error="离线文件不完整或已失效，请重新下载",loading=false)
                else recover(error)
            }
            override fun onTracksChanged(tracks:Tracks) {
                if(live.state.value.active)return
                val video=mutableListOf<Pair<String,String>>();val text=mutableListOf<Pair<String,String>>()
                tracks.groups.forEachIndexed { gi,g -> (0 until g.length).forEach { ti ->
                    val f=g.getTrackFormat(ti)
                    if(g.isTrackSupported(ti)) when(g.type) {
                        C.TRACK_TYPE_VIDEO -> if(f.height>0)video.add("track:$gi:$ti" to "${f.height}P")
                        C.TRACK_TYPE_TEXT -> text.add("track:$gi:$ti" to (f.label?:f.language?:"字幕 ${ti+1}"))
                    }
                } }
                state.value=state.value.copy(trackQualities=video.distinctBy { it.second }.sortedByDescending { it.second.removeSuffix("P").toIntOrNull()?:0 },subtitles=text)
            }
        })
        scope.launch { while(isActive) {
            delay(500)
            if(!checkOfflineAccess())continue
            updateTime()
            val now=System.currentTimeMillis()
            if(now-savedAt>10000) { savedAt=now;saveProgress() }
        } }
        scope.launch {
            repo.ready.first { it }
            repo.session.collect { s ->
                val next:Long=s?.user?.id?:0L
                if(owner!=0L && owner!=next && !live.state.value.active) {
                    // The session has already changed. Do not attribute the previous film to it.
                    stop(save=false)
                    state.value=PlayerState(error="账号已切换，请重新打开影片")
                }
                owner=next
            }
        }
    }
    private fun updateTime() {
        if(live.state.value.active)return
        val p=state.value.descriptor?:return
        val offset=if(p.seekMode=="server-offset")p.resumeOffsetMs else 0
        val duration=when { p.durationMs>0 -> p.durationMs;player.duration!=C.TIME_UNSET -> player.duration+offset;else -> 0 }
        state.value=state.value.copy(position=player.currentPosition+offset,duration=duration,buffered=player.bufferedPosition+offset)
    }
    fun open(detail:Detail,identity:PlaybackIdentity) {
        live.stop()
        openingRetry=null
        val current=state.value.descriptor
        if(current?.vodId==identity.vodId && state.value.detail!=null && current.episodeKey==identity.episodeKey && identity.line==current.line && (identity.versionKey.isBlank() || current.versionKey==identity.versionKey) && state.value.error.isBlank() && player.playbackState!=Player.STATE_IDLE && (current.expiresAt==0L || current.expiresAt>System.currentTimeMillis()/1000+15)) {
            desiredPlay=true;if(!backgroundPaused)player.play();return
        }
        saveProgress();clearPreload();offlineGuard=null;offlineDownloadId=""
        player.stop();player.clearMediaItems()
        discoveryJob?.cancel();discoveryJob=null;discoveryAttempted.clear()
        attempted.clear();refreshed=false;metadataRefreshed=false;selectedQuality=identity.quality
        state.value=PlayerState(detail=detail,loading=true,loadingMessage="正在解析播放源")
        load(identity)
    }
    /** Show a fresh native player immediately; no old line or media survives navigation. */
    fun beginOpening(summary:Film,onRetry:(()->Unit)?=null) {
        stop()
        openingRetry=onRetry
        desiredPlay=true
        state.value=PlayerState(detail=Detail(summary,emptyList(),"",emptyList(),emptyList()),loading=true,loadingMessage="正在加载影片信息")
        metadataRefreshed=false
    }
    fun updateOpeningMessage(id:Long,message:String) {
        if(state.value.detail?.film?.id==id&&state.value.descriptor==null&&state.value.loading)
            state.value=state.value.copy(loadingMessage=message)
    }
    fun failOpening(id:Long,error:Throwable) {
        if(state.value.detail?.film?.id==id&&state.value.descriptor==null)
            state.value=state.value.copy(loading=false,error=error.localizedMessage?:"影片信息加载失败",errorStatus=(error as? ApiException)?.status?:0)
    }
    private fun load(identity:PlaybackIdentity,play:Boolean=true,fromDiscovery:Boolean=false) {
        val previous=currentIdentity()
        val sources=state.value.detail?.sources.orEmpty()
        if(previous!=null && PlaybackRecoveryRules.attemptKey(previous,sources)!=PlaybackRecoveryRules.attemptKey(identity,sources)) {
            // The film-wide server search may continue, but an old episode's
            // client continuation must not take control of the newly chosen one.
            if(!fromDiscovery) { discoveryJob?.cancel();discoveryJob=null }
            player.stop();player.clearMediaItems()
            state.value=state.value.copy(descriptor=null,position=identity.positionMs,duration=0,discovery=if(fromDiscovery)state.value.discovery else SourceDiscoveryProgress())
        }
        requestedIdentity=identity;resolvingIdentity=true
        state.value=state.value.copy(requested=identity)
        loadJob?.cancel();desiredPlay=play;player.pause()
        loadJob=scope.launch {
            state.value=state.value.copy(loading=true,error="",errorStatus=0,loadingMessage="正在解析播放源")
            try {
                val cached=preloaded?.takeIf {
                    it.vodId==identity.vodId && it.line==identity.line && it.episodeKey==identity.episodeKey &&
                    it.versionKey==identity.versionKey && (identity.quality.isBlank() || it.quality==identity.quality) &&
                    it.expiresAt>System.currentTimeMillis()/1000+30
                }
                val p=cached?:resolvePlaybackRequest(load={ repo.resolve(identity) },onInterrupted={ message->
                    if(requestedIdentity==identity)state.value=state.value.copy(error=message,loading=false,errorStatus=0)
                })
                ensureActive()
                attempted.add(p.line)
                apply(p,identity.positionMs,desiredPlay,false)
            } catch(e:CancellationException) { throw e } catch(e:Throwable) {
                state.value=state.value.copy(error=e.localizedMessage?:"线路暂时不可用",loading=false,errorStatus=(e as? ApiException)?.status?:0)
                if(e is ApiException && e.status in listOf(401,403,410,451)) {
                    discoveryJob?.cancel();discoveryJob=null
                    state.value=state.value.copy(discovery=SourceDiscoveryProgress())
                    return@launch
                }
                if(e is ApiException&&e.status in listOf(404,409)&&!metadataRefreshed) {
                    metadataRefreshed=true
                    try { updateDetail(repo.detail(identity.vodId,fresh=true)) }
                    catch(cancel:CancellationException) { throw cancel }
                    catch(failure:Throwable) {
                        if(failure is ApiException && failure.status in setOf(401,403,404,410,451)) {
                            discoveryJob?.cancel();discoveryJob=null
                            state.value=state.value.copy(error=failure.message,errorStatus=failure.status,discovery=SourceDiscoveryProgress());return@launch
                        }
                    }
                }
                if(identity.line.isNotBlank())attempted.add(identity.line)
                fallback(identity)
            }
        }
    }
    private fun apply(p:Playback,position:Long,play:Boolean,offline:Boolean) {
        require(p.url.isNotBlank()) { "资源未提供原生播放地址" }
        val factory=if(offline)offlineFactory?:throw IllegalStateException("离线缓存不可用") else networkFactory
        val item=MediaItem.Builder().setUri(SiteHttp.absolute(p.url)).setMediaId("${p.vodId}:${p.versionKey}:${p.episodeKey}").setMediaMetadata(MediaMetadata.Builder().setTitle(p.name).build()).apply {
            if(PlaybackMediaRules.isHls(p.type,p.url))setMimeType(MimeTypes.APPLICATION_M3U8)
            if(offline)setStreamKeys(offlineKeys)
        }.build()
        val source=DefaultMediaSourceFactory(context).setDataSourceFactory(factory).createMediaSource(item)
        // Resolve may fill an initially empty line/key/version or pick a faster
        // line. After success the requested identity must describe that media.
        requestedIdentity=if(offline)null else identity(p,position)
        resolvingIdentity=false
        state.value=state.value.copy(descriptor=p,requested=requestedIdentity,position=position,duration=p.durationMs,error="",errorStatus=0,loading=true,offline=offline)
        player.setMediaSource(source,if(p.seekMode=="server-offset")0 else position.coerceAtLeast(0))
        player.prepare();player.playWhenReady=play && !backgroundPaused
        if(!offline)runCatching { context.startService(Intent(context,PlaybackService::class.java)) }
    }
    fun offline(p:Playback,position:Long=0,keys:List<StreamKey> = emptyList(),downloadId:String,authorize:()->String?) {
        authorize()?.let { throw IllegalStateException(it) }
        stop();offlineKeys=keys;offlineDownloadId=downloadId;offlineGuard=authorize;desiredPlay=true
        state.value=PlayerState(descriptor=p,offline=true)
        apply(p,position,true,true)
    }
    private fun checkOfflineAccess():Boolean {
        if(!state.value.offline || state.value.descriptor==null)return true
        val failure=offlineGuard?.invoke()?:if(offlineGuard==null)"离线授权不可用" else null
        if(failure==null)return true
        if(state.value.error==failure && player.playbackState==Player.STATE_IDLE)return false
        saveProgress();player.stop();player.clearMediaItems();desiredPlay=false;wasActive=false
        state.value=state.value.copy(error=failure,loading=false,playing=false)
        return false
    }
    fun invalidateOfflineDownload(id:String,message:String="离线授权已撤销，请重新联网下载") {
        if(offlineDownloadId==id && state.value.offline) {
            offlineGuard={ message };checkOfflineAccess()
        }
    }
    private fun identity(p:Playback,position:Long=state.value.position)=PlaybackIdentity(p.vodId,p.line,p.episodeKey,p.episode,selectedQuality,position,true,p.versionKey)
    private fun currentIdentity():PlaybackIdentity?=PlaybackRecoveryRules.currentIdentity(requestedIdentity,state.value.descriptor,state.value.position,selectedQuality,resolvingIdentity)
    private fun recover(error:Throwable) {
        val request=currentIdentity()?:return
        if(!refreshed) { refreshed=true;load(request,desiredPlay) }
        else { state.value=state.value.copy(error="当前线路中断，正在切换可用线路");fallback(request) }
    }
    private fun fallback(identity:PlaybackIdentity) {
        val detail=state.value.detail?:return
        val oldSource=detail.sources.find { it.code==identity.line }
            ?:detail.sources.find { it.code==detail.preferredLine }
            ?:PlaybackRules.defaultSource(detail.film,detail.sources)
        val old=oldSource?.episodes?.find { it.key==identity.episodeKey }
            ?:oldSource?.episodes?.getOrNull(identity.episode)
        val normalized=identity.copy(episodeKey=old?.key?:identity.episodeKey)
        val version=identity.versionKey.ifBlank { oldSource?.versionKey.orEmpty() }
        val next=PlaybackMediaRules.alternate(detail.sources,normalized,version,old?.number,attempted)
        if(next==null) {
            state.value=state.value.copy(error="当前分集暂无可用线路",errorStatus=0,loading=false)
            discoverMoreSources(manual=false)
            return
        }
        val (source,ep)=next
        attempted.add(source.code);refreshed=false
        load(normalized.copy(line=source.code,episodeKey=ep.key,episode=source.episodes.indexOf(ep),manual=true,versionKey=source.versionKey),desiredPlay)
    }
    fun selectSource(s:Source) {
        val detail=state.value.detail?:return
        val pending=currentIdentity()?:PlaybackIdentity(detail.film.id,detail.preferredLine)
        val request=PlaybackMediaRules.manualSource(detail.sources,s,pending,pending.positionMs)
        if(request==null) { repo.notice.value="此线路没有当前分集，请手动选择剧集";return }
        saveProgress();clearPreload()
        attempted.clear();refreshed=false
        load(request.copy(quality=selectedQuality),desiredPlay)
    }
    /** Refresh metadata and newly discovered lines without interrupting playback. */
    fun updateDetail(detail:Detail) {
        val id=state.value.detail?.film?.id?:state.value.descriptor?.vodId?:return
        if(detail.film.id==id)state.value=state.value.copy(detail=detail)
    }
    /** One automatic search per episode; manual searches join the current task. */
    fun discoverMoreSources(manual:Boolean=true):Boolean {
        val snapshot=state.value
        val detail=snapshot.detail?:return false
        if(!PlaybackRecoveryRules.canDiscover(snapshot.offline,snapshot.errorStatus,detail.film.id)) {
            if(manual)repo.notice.value=if(snapshot.offline)"离线播放不能查找片源" else "请先处理登录、观看权限或影片下架提示"
            return false
        }
        if(discoveryJob?.isActive==true)return true
        val request=currentIdentity()?:PlaybackIdentity(detail.film.id)
        val key=PlaybackRecoveryRules.attemptKey(request,detail.sources)
        if(!manual && !discoveryAttempted.add(key))return false
        discoveryAttempted.add(key)
        val owner:Long=repo.session.value?.user?.id?:0L
        val previous=detail.sources
        var recoverySources=previous
        var recoveryKey=key
        val recoveryAttempted=mutableSetOf<String>()
        state.value=state.value.copy(discovery=SourceDiscoveryProgress(status="starting",message="正在查找更多片源"))
        discoveryJob=scope.launch {
            fun current():Boolean=(state.value.detail?.film?.id==request.vodId && (repo.session.value?.user?.id?:0L)==owner && !state.value.offline)
            fun progress(value:com.google.gson.JsonObject)=SourceDiscoveryProgress(value.string("status"),value.long("checked").toInt(),value.long("total").toInt(),value.long("added").toInt(),value.long("updated").toInt(),value.long("failed").toInt(),value.string("message"))
            suspend fun refreshAndResume(status:SourceDiscoveryProgress) {
                val latest=repo.detail(request.vodId,fresh=true)
                if(!current())throw CancellationException("影片或账号已切换")
                updateDetail(latest)
                // A user may have selected another episode or recovered playback
                // while discovery was running. Metadata may refresh, playback may not.
                val pending=currentIdentity()?:PlaybackIdentity(request.vodId)
                if(state.value.error.isNotBlank() && !state.value.loading &&
                    PlaybackRecoveryRules.canDiscover(state.value.offline,state.value.errorStatus,request.vodId) &&
                    PlaybackRecoveryRules.attemptKey(pending,recoverySources)==recoveryKey) {
                    val eligible=latest.copy(sources=latest.sources.filter { it.code !in recoveryAttempted })
                    val candidate=PlaybackRecoveryRules.discoveredIdentity(pending,recoverySources,eligible,attempted,status.updated>0)
                    if(candidate!=null) {
                        recoveryAttempted.add(candidate.line)
                        discoveryAttempted.add(PlaybackRecoveryRules.attemptKey(candidate,latest.sources))
                        recoveryKey=PlaybackRecoveryRules.attemptKey(candidate,latest.sources)
                        recoverySources=latest.sources
                        clearPreload();refreshed=false
                        if(status.updated>0)attempted.clear()
                        load(candidate,desiredPlay,fromDiscovery=true)
                    }
                }
            }
            try {
                withTimeout(220000) {
                    var status=progress(repo.api.request("films/${request.vodId}/discover","POST").asJsonObject)
                    if(!current())return@withTimeout
                    state.value=state.value.copy(discovery=status)
                    var refreshedCount=-1
                    while(status.running) {
                        delay(2000)
                        status=progress(repo.api.request("films/${request.vodId}/discovery").asJsonObject)
                        if(!current())return@withTimeout
                        state.value=state.value.copy(discovery=status)
                        val changed=status.added+status.updated
                        if(changed>0 && changed!=refreshedCount) {
                            refreshAndResume(status);refreshedCount=changed
                        }
                    }
                    if(current() && status.status !in setOf("disabled","busy"))refreshAndResume(status)
                }
            } catch(e:TimeoutCancellationException) {
                if(current())state.value=state.value.copy(discovery=state.value.discovery.copy(status="timeout",message="搜索仍在后台执行，可再次查看进度"))
            } catch(e:CancellationException) { throw e }
            catch(e:Throwable) {
                val denied=(e as? ApiException)?.status?.takeIf { it in setOf(401,403,404,410,451) }
                if(current())state.value=state.value.copy(discovery=state.value.discovery.copy(status="error",message=e.localizedMessage?:"片源搜索失败，请重试"),errorStatus=denied?:state.value.errorStatus)
            }
        }
        return true
    }
    fun retryCurrent():Boolean {
        if(state.value.offline)return false
        val request=currentIdentity()
        if(request==null) { val retry=openingRetry?:return false;retry();return true }
        clearPreload();attempted.clear();refreshed=false
        load(request,true)
        return true
    }
    fun selectEpisode(s:Source,e:Episode) {
        saveProgress();attempted.clear();refreshed=false
        val vodId=state.value.detail?.film?.id?:state.value.descriptor?.vodId?:return
        load(PlaybackIdentity(vodId,s.code,e.key,s.episodes.indexOf(e),selectedQuality,manual=true,versionKey=s.versionKey))
    }
    fun nextAvailable():Boolean { val p=state.value.descriptor?:return false;val s=state.value.detail?.sources?.find { it.code==p.line }?:return false;return PlaybackRules.nextEpisode(s,p.episodeKey)!=null }
    fun next() { val p=state.value.descriptor?:return;val s=state.value.detail?.sources?.find { it.code==p.line }?:return;PlaybackRules.nextEpisode(s,p.episodeKey)?.let { selectEpisode(s,it) } }
    fun seek(position:Long) {
        if(!checkOfflineAccess())return
        val p=state.value.descriptor?:return
        if(p.seekMode=="server-offset" && !state.value.offline)load(identity(p,position),desiredPlay)
        else player.seekTo(position.coerceIn(0,state.value.duration.coerceAtLeast(0)))
    }
    fun quality(id:String,label:String) {
        if(id.startsWith("track:")) {
            val parts=id.split(':');val g=player.currentTracks.groups.getOrNull(parts[1].toInt())?:return
            player.trackSelectionParameters=player.trackSelectionParameters.buildUpon().setOverrideForType(TrackSelectionOverride(g.mediaTrackGroup,listOf(parts[2].toInt()))).build()
        } else if(id=="auto") {
            player.trackSelectionParameters=player.trackSelectionParameters.buildUpon().clearOverridesOfType(C.TRACK_TYPE_VIDEO).build()
        } else {
            if(state.value.offline)return
            val p=state.value.descriptor?:return;selectedQuality=id;clearPreload()
            load(identity(p),desiredPlay)
        }
        state.value=state.value.copy(quality=label)
    }
    fun subtitle(id:String) {
        val params=player.trackSelectionParameters.buildUpon().setTrackTypeDisabled(C.TRACK_TYPE_TEXT,id=="off")
        if(id.startsWith("track:")) { val parts=id.split(':');player.currentTracks.groups.getOrNull(parts[1].toInt())?.let { params.setOverrideForType(TrackSelectionOverride(it.mediaTrackGroup,listOf(parts[2].toInt()))) } }
        player.trackSelectionParameters=params.build()
    }
    fun pauseForBackground(inPip:Boolean) {
        if(live.state.value.active) { if(!inPip)live.pauseForBackground();return }
        if(!inPip && !backgroundPaused) {
            backgroundPaused=true;wasActive=desiredPlay || player.playWhenReady;player.pause();saveProgress()
        }
    }
    fun restoreForeground() {
        if(live.state.value.active) { live.restoreForeground();return }
        backgroundPaused=false
        if(wasActive && checkOfflineAccess()) { wasActive=false;desiredPlay=true;if(state.value.descriptor!=null)player.play() }
    }
    fun toggle() {
        if(live.state.value.active) { live.toggle();return }
        if(!checkOfflineAccess())return
        desiredPlay=if(loadJob?.isActive==true) !desiredPlay else !player.playWhenReady
        if(desiredPlay && !backgroundPaused)player.play() else player.pause()
    }
    fun saveProgress() {
        if(live.state.value.active)return
        val snapshot=state.value;val p=snapshot.descriptor?:return
        val account=repo.session.value?.user?.id?:return
        if(snapshot.position>0)scope.launch {
            if(repo.session.value?.user?.id==account)repo.record(p,snapshot.position,snapshot.duration)
        }
    }
    fun stop(save:Boolean=true) {
        if(save)saveProgress()
        live.stop()
        loadJob?.cancel();loadJob=null;discoveryJob?.cancel();discoveryJob=null;discoveryAttempted.clear();requestedIdentity=null;resolvingIdentity=false;openingRetry=null;clearPreload();offlineGuard=null;offlineDownloadId=""
        desiredPlay=false;wasActive=false;player.stop();player.clearMediaItems()
    }
    private fun clearPreload() {
        preloadJob?.cancel();preloadJob=null;preloadPlayer?.release();preloadPlayer=null;preloaded=null
    }
    private fun schedulePreload() {
        if(state.value.offline)return
        val d=state.value.detail?:return
        if(!(d.film.isShort || d.film.typeName.contains("短剧")))return
        val p=state.value.descriptor?:return
        val s=d.sources.find { it.code==p.line }?:return
        val ep=PlaybackRules.nextEpisode(s,p.episodeKey)?:return
        if(preloaded?.let { it.vodId==p.vodId && it.line==p.line && it.versionKey==p.versionKey && it.episodeKey==ep.key && it.quality==p.quality }==true)return
        clearPreload()
        preloadJob=scope.launch {
            var pre:ExoPlayer?=null
            try {
                val desc=repo.resolve(PlaybackIdentity(p.vodId,s.code,ep.key,s.episodes.indexOf(ep),selectedQuality,manual=true,versionKey=s.versionKey))
                preloaded=desc
                pre=ExoPlayer.Builder(context).setMediaSourceFactory(DefaultMediaSourceFactory(context).setDataSourceFactory(networkFactory)).setLoadControl(DefaultLoadControl.Builder().setBufferDurationsMs(1000,4000,500,1000).build()).build()
                preloadPlayer=pre;pre.volume=0f
                pre.setMediaItem(MediaItem.Builder().setUri(SiteHttp.absolute(desc.url)).apply { if(PlaybackMediaRules.isHls(desc.type,desc.url))setMimeType(MimeTypes.APPLICATION_M3U8) }.build())
                pre.prepare();pre.playWhenReady=false;delay(8000)
            } catch(e:CancellationException) { throw e } catch(_:Throwable) { preloaded=null }
            finally { pre?.release();if(preloadPlayer===pre)preloadPlayer=null }
        }
    }
}
object PlayerHub {
    lateinit var engine:PlayerEngine
    fun initialize(context:Context) { AppGraph.initialize(context);if(!::engine.isInitialized)engine=PlayerEngine(context.applicationContext,AppGraph.repository) }
}
