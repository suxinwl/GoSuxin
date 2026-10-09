@file:androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
package com.xiaoqi.video.core.player

import android.content.Context
import android.content.Intent
import androidx.media3.common.*
import androidx.media3.datasource.okhttp.OkHttpDataSource
import androidx.media3.datasource.HttpDataSource
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.hls.HlsMediaSource
import androidx.media3.exoplayer.source.ProgressiveMediaSource
import com.xiaoqi.video.core.data.*
import com.xiaoqi.video.core.model.*
import com.xiaoqi.video.core.network.ApiException
import com.xiaoqi.video.core.network.LiveNetworkErrors
import com.xiaoqi.video.core.network.SiteHttp
import com.xiaoqi.video.core.network.JsonWire
import com.xiaoqi.video.core.network.JsonWire.long
import com.xiaoqi.video.core.network.JsonWire.array
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first

data class LivePlayerState(
    val active:Boolean=false,val channel:LiveChannel?=null,val descriptor:LivePlayback?=null,
    val loading:Boolean=false,val playing:Boolean=false,val error:String="",val errorStatus:Int=0,
    val message:String="正在连接直播频道",val quality:String="自动",val trackQualities:List<Pair<String,String>> = emptyList(),
    val subtitles:List<Pair<String,String>> = emptyList(),val failedStreamIds:Set<Long> = emptySet(),
    val programme:LiveProgramme?=null,val position:Long=0,val duration:Long=0,val ended:Boolean=false,
    val eventReplay:Boolean=false,val eventId:String=""
) { val finite:Boolean get()=programme!=null||eventReplay }

/** Live owns rules and identity; the shared ExoPlayer/MediaSession remains singular. */
class LivePlaybackController(
    private val context:Context,private val repo:AppRepository,val player:ExoPlayer,
    private val beforeOpen:()->Unit
) {
    val state=MutableStateFlow(LivePlayerState())
    private val scope=CoroutineScope(SupervisorJob()+Dispatchers.Main.immediate)
    private var loadJob:Job?=null
    private var renewJob:Job?=null
    private var startupJob:Job?=null
    private var detailsJob:Job?=null
    private var channelRequest=0L
    private var generation=0L
    private var backgroundPaused=false
    private var desiredPlay=true
    private var failed=linkedSetOf<Long>()
    private var refreshed=linkedSetOf<Long>()
    private var windowRecovered=linkedSetOf<Long>()
    private var rememberedSuccess=""
    private var replayRefreshed=false
    private var selectedQuality=""

    init {
        player.addListener(object:Player.Listener {
            override fun onIsPlayingChanged(isPlaying:Boolean) {
                if(state.value.active) {
                    state.value=state.value.copy(playing=isPlaying)
                    if(isPlaying)rememberSuccess()
                }
            }
            override fun onPlayWhenReadyChanged(playWhenReady:Boolean,reason:Int) {
                if(state.value.active&&!backgroundPaused&&reason==Player.PLAY_WHEN_READY_CHANGE_REASON_USER_REQUEST)desiredPlay=playWhenReady
            }
            override fun onPlaybackStateChanged(value:Int) {
                if(!state.value.active)return
                if(value==Player.STATE_READY&&state.value.descriptor!=null) {
                    startupJob?.cancel();startupJob=null
                    state.value=state.value.copy(loading=false,error="",errorStatus=0,message=if(state.value.finite)"节目回看中" else "直播中")
                    if(!backgroundPaused&&desiredPlay)rememberSuccess()
                } else if(value==Player.STATE_BUFFERING) {
                    state.value=state.value.copy(loading=true,message=if(state.value.finite)"正在缓冲回看节目" else "正在缓冲直播")
                    if(state.value.descriptor!=null&&desiredPlay&&!backgroundPaused&&startupJob?.isActive!=true)watchStartup(generation)
                }
                else if(value==Player.STATE_ENDED && state.value.descriptor!=null) {
                    if(state.value.finite) {
                        startupJob?.cancel();desiredPlay=false
                        state.value=state.value.copy(loading=false,playing=false,ended=true,message="回看已结束")
                    } else failCurrent("当前线路已结束，正在尝试备用线路")
                }
            }
            override fun onPlayerError(error:PlaybackException) {
                if(!state.value.active)return
                val status=generateSequence<Throwable>(error) { it.cause }.filterIsInstance<HttpDataSource.InvalidResponseCodeException>().firstOrNull()?.responseCode?:0
                if(!LivePlaybackRules.canRetry(status)) {
                    startupJob?.cancel();renewJob?.cancel();player.pause()
                    val hint=when(status) {
                        401->"播放会话已失效，请登录后重试"
                        403->"没有该频道或回看的观看权限，请查看账号／会员"
                        404,410->"该节目或线路已不可用，请返回直播或选择频道"
                        451->"该频道暂不支持当前地区观看"
                        else->"当前播放请求被拒绝"
                    }
                    state.value=state.value.copy(loading=false,playing=false,error=hint,errorStatus=status)
                    return
                }
                val streamId=state.value.descriptor?.streamId?:0
                if(!state.value.finite&&error.errorCode==PlaybackException.ERROR_CODE_BEHIND_LIVE_WINDOW&&streamId>0&&windowRecovered.add(streamId)) {
                    state.value=state.value.copy(loading=true,error="",message="正在返回实时直播")
                    player.seekToDefaultPosition();player.prepare();player.playWhenReady=desiredPlay&&!backgroundPaused
                    watchStartup(generation)
                } else failCurrent("当前线路中断，正在尝试备用线路")
            }
            override fun onTracksChanged(tracks:Tracks) {
                if(!state.value.active)return
                val video=mutableListOf<Pair<String,String>>();val text=mutableListOf<Pair<String,String>>()
                tracks.groups.forEachIndexed { gi,g->(0 until g.length).forEach { ti->
                    if(g.isTrackSupported(ti)) {
                        val format=g.getTrackFormat(ti)
                        if(g.type==C.TRACK_TYPE_VIDEO&&format.height>0)video.add("track:$gi:$ti" to "${format.height}P")
                        if(g.type==C.TRACK_TYPE_TEXT)text.add("track:$gi:$ti" to (format.label?:format.language?:"字幕 ${ti+1}"))
                    }
                } }
                state.value=state.value.copy(trackQualities=video.distinctBy { it.second }.sortedByDescending { it.second.removeSuffix("P").toIntOrNull()?:0 },subtitles=text)
            }
        })
        scope.launch { while(isActive) {
            delay(500)
            val snapshot=state.value;val descriptor=snapshot.descriptor
            if(snapshot.active&&snapshot.finite&&descriptor!=null) {
                val duration=descriptor.durationMs.takeIf { it>0 }?:player.duration.takeIf { it!=C.TIME_UNSET&&it>0 }?:0
                state.value=state.value.copy(position=player.currentPosition.coerceAtLeast(0),duration=duration)
            }
        } }
        scope.launch {
            repo.ready.first { it }
            var owner:Long=repo.session.value?.user?.id?:0L
            repo.session.collect { session->
                val next:Long=session?.user?.id?:0L
                if(next!=owner&&state.value.active) {
                    failed.clear();refreshed.clear();windowRecovered.clear();replayRefreshed=false
                    load(state.value.descriptor?.streamId?:0,message="账号已变化，正在重新授权")
                }
                owner=next
            }
        }
    }

    fun open(channel:LiveChannel) {
        if(channel.id<=0)return
        beforeOpen()
        backgroundPaused=false
        generation++;failed.clear();refreshed.clear();windowRecovered.clear();rememberedSuccess="";desiredPlay=true;replayRefreshed=false;selectedQuality=""
        state.value=LivePlayerState(active=true,channel=channel,loading=true)
        scope.launch { runCatching { repo.rememberLiveChannel(channel) } }
        val detailRequest=++channelRequest
        detailsJob=scope.launch {
            try {
                val detail=repo.liveChannels(channelId=channel.id,size=1).items.firstOrNull()?:return@launch
                if(detailRequest==channelRequest&&state.value.active&&state.value.channel?.id==detail.id&&state.value.descriptor?.streams.isNullOrEmpty())
                    state.value=state.value.copy(channel=detail)
            } catch(e:CancellationException) { throw e } catch(_:Throwable) { /* Resolve remains independent of optional line details. */ }
        }
        load(0,preferRecent=true)
    }
    fun selectStream(id:Long) {
        if(!state.value.active)return
        failed.clear();refreshed.clear();windowRecovered.clear();desiredPlay=true;replayRefreshed=false
        load(id)
    }
    fun retry() {
        if(!state.value.active)return
        failed.clear();refreshed.clear();windowRecovered.clear();desiredPlay=true;replayRefreshed=false
        load(state.value.descriptor?.streamId?:0)
    }
    fun openReplay(programme:LiveProgramme,positionMs:Long=0) {
        val channel=state.value.channel?:return
        if(!state.value.active||!LiveProgrammeRules.replayable(programme,System.currentTimeMillis()/1000)||programme.channelId>0&&programme.channelId!=channel.id)return
        failed.clear();refreshed.clear();windowRecovered.clear();replayRefreshed=false;selectedQuality="";desiredPlay=true
        state.value=state.value.copy(programme=programme,position=positionMs.coerceAtLeast(0),duration=((programme.stop-programme.start)*1000).coerceAtLeast(0),ended=false,eventReplay=false,eventId="")
        load(programme.streamId,message="正在连接回看节目",positionMs=positionMs)
    }
    private fun load(streamId:Long,preferRecent:Boolean=false,message:String="",positionMs:Long?=null) {
        val channel=state.value.channel?:return
        val programme=state.value.programme
        val finite=state.value.finite;val eventId=state.value.eventId
        val resume=if(!finite)0 else (positionMs?:player.currentPosition.takeIf { it>0 }?:state.value.position).coerceAtLeast(0)
        loadJob?.cancel();renewJob?.cancel();startupJob?.cancel()
        generation++;val request=generation
        player.stop();player.clearMediaItems()
        state.value=state.value.copy(descriptor=null,playing=false,loading=true,error="",errorStatus=0,message=message.ifBlank { if(finite)"正在连接回看节目" else if(failed.isEmpty())"正在连接直播频道" else "正在切换备用线路" },quality="自动",trackQualities=emptyList(),subtitles=emptyList(),failedStreamIds=failed.toSet(),position=resume,ended=false)
        loadJob=scope.launch {
            var recent=0L
            try {
                if(preferRecent)recent=try { repo.lastLiveStream(channel.id) } catch(e:CancellationException) { throw e } catch(_:Throwable) { 0 }
                ensureActive()
                if(request!=generation||!state.value.active)return@launch
                val descriptor=if(programme!=null)repo.liveReplay(channel.id,programme.id,streamId,selectedQuality)
                    else repo.liveResolve(channel.id,if(recent>0)recent else streamId,failed.toSet())
                ensureActive()
                if(request!=generation||!state.value.active)return@launch
                require(LiveProgrammeRules.matches(channel.id,programme?.id?:0,descriptor)&&LiveProgrammeRules.sameEvent(eventId,descriptor)) { "播放信息与当前频道或节目不一致，请重新选择" }
                require(descriptor.streamId>0 && descriptor.streamId !in failed) { "频道没有其他可用线路，请稍后重试" }
                state.value=state.value.copy(descriptor=descriptor,eventReplay=programme==null&&!descriptor.isLive&&descriptor.mode=="event_replay",eventId=descriptor.eventId,channel=state.value.channel?.let { current->if(descriptor.streams.isNotEmpty())current.copy(streams=descriptor.streams) else current })
                apply(descriptor.url,descriptor.name,descriptor.mimeType,LiveProgrammeRules.restorePosition(descriptor,resume))
                renew(descriptor,request)
                watchStartup(request)
            } catch(e:CancellationException) { throw e }
            catch(e:Throwable) {
                if(request==generation&&state.value.active) {
                    val details=(e as? ApiException)?.data?.takeIf { it.isJsonObject }?.asJsonObject
                    val failedId=details?.long("stream_id")?:0
                    val candidates=details?.array("streams")?.filter { it.isJsonObject }?.map { JsonWire.decode<LiveStream>(it) }.orEmpty()
                    if(candidates.isNotEmpty())state.value=state.value.copy(channel=state.value.channel?.copy(streams=candidates))
                    if(preferRecent&&recent>0&&e is ApiException&&e.status==404) { load(0);return@launch }
                    if(finite&&e is java.io.IOException&&LiveProgrammeRules.canRefreshReplay((e as? ApiException)?.status?:0,replayRefreshed)) {
                        replayRefreshed=true;load(failedId.takeIf { it>0 }?:streamId.takeIf { it>0 }?:programme?.streamId?:0,message="正在重新连接同一回看节目",positionMs=resume);return@launch
                    }
                    if(!finite&&e is ApiException&&e.status==502) {
                        val decision=LivePlaybackRules.recoverStream(failed,refreshed,failedId,candidates.map { it.id },e.status)
                        failed.addAll(decision.excluded)
                        refreshed.addAll(decision.refreshed)
                        state.value=state.value.copy(failedStreamIds=failed.toSet())
                        if(decision.retry) { load(decision.streamId,message=if(decision.streamId>0)"正在重新解析当前直播线路" else "正在切换备用线路");return@launch }
                    }
                    state.value=state.value.copy(loading=false,playing=false,error=LiveNetworkErrors.message(e),errorStatus=(e as? ApiException)?.status?:0,failedStreamIds=failed.toSet())
                }
            }
        }
    }
    private fun apply(url:String,name:String,mimeType:String="",positionMs:Long=0) {
        val absolute=SiteHttp.absolute(url)
        val descriptor=state.value.descriptor?:return
        val mime=LiveProgrammeRules.mediaMime(mimeType)
        val item=MediaItem.Builder().setUri(absolute).setMediaId(LiveProgrammeRules.identity(descriptor))
            .setMediaMetadata(MediaMetadata.Builder().setTitle(name).build())
            .setMimeType(mime).build()
        player.trackSelectionParameters=player.trackSelectionParameters.buildUpon().clearOverridesOfType(C.TRACK_TYPE_VIDEO).clearOverridesOfType(C.TRACK_TYPE_TEXT).setTrackTypeDisabled(C.TRACK_TYPE_TEXT,false).build()
        player.setPlaybackSpeed(1f)
        // Downloads replaces the VOD factory with a CacheDataSource. A sliding live playlist
        // must bypass that factory entirely, including opaque quality/session URLs.
        val liveNetwork=OkHttpDataSource.Factory(SiteHttp.liveCallFactory).setUserAgent("XiaoqiVideo/1.0 Android Live")
            .setDefaultRequestProperties(mapOf("Cache-Control" to "no-cache", "Accept" to "*/*"))
        val source=if(LiveProgrammeRules.isHls(mime))HlsMediaSource.Factory(liveNetwork).createMediaSource(item)
            else ProgressiveMediaSource.Factory(liveNetwork).createMediaSource(item)
        if(descriptor.isLive)player.setMediaSource(source) else player.setMediaSource(source,positionMs)
        player.prepare();player.playWhenReady=desiredPlay&&!backgroundPaused
        runCatching { context.startService(Intent(context,PlaybackService::class.java)) }
    }
    private fun watchStartup(request:Long) {
        startupJob?.cancel()
        startupJob=scope.launch {
            delay(35000)
            if(request==generation&&state.value.active&&state.value.loading&&desiredPlay&&!backgroundPaused)failCurrent("此线路连接超时，正在尝试备用线路")
        }
    }
    private fun failCurrent(message:String) {
        val snapshot=state.value
        if(!snapshot.active||!LivePlaybackRules.canRetry(snapshot.errorStatus))return
        val stream=snapshot.descriptor?.streamId?:return
        if(snapshot.finite) {
            if(LiveProgrammeRules.canRefreshReplay(snapshot.errorStatus,replayRefreshed)) { replayRefreshed=true;load(stream,message="正在重新连接同一回看节目");return }
            startupJob?.cancel();renewJob?.cancel();player.pause()
            state.value=snapshot.copy(loading=false,playing=false,error="回看连接中断，可重试或返回直播")
            return
        }
        val decision=LivePlaybackRules.recoverStream(failed,refreshed,stream,(snapshot.descriptor?.streams?.takeIf { it.isNotEmpty() }?:snapshot.channel?.streams.orEmpty()).map { it.id },snapshot.errorStatus)
        failed.addAll(decision.excluded)
        refreshed.addAll(decision.refreshed)
        if(!decision.retry) {
            startupJob?.cancel();renewJob?.cancel();player.pause()
            state.value=snapshot.copy(loading=false,playing=false,error="频道没有其他可用线路，请稍后重试",failedStreamIds=failed.toSet())
            return
        }
        state.value=snapshot.copy(message=message,failedStreamIds=failed.toSet())
        load(decision.streamId,message=if(decision.streamId>0)"正在重新解析当前直播线路" else message)
    }
    private fun rememberSuccess() {
        val snapshot=state.value;val descriptor=snapshot.descriptor?:return
        if(!snapshot.active||snapshot.finite||snapshot.channel?.id!=descriptor.channelId||descriptor.streamId<=0||snapshot.error.isNotBlank()||player.playbackState!=Player.STATE_READY||player.currentMediaItem?.mediaId!=LiveProgrammeRules.identity(descriptor))return
        val key="${descriptor.channelId}:${descriptor.streamId}"
        if(key==rememberedSuccess)return
        rememberedSuccess=key
        val successfulAt=System.currentTimeMillis()
        scope.launch { runCatching { repo.rememberLiveStream(descriptor.channelId,descriptor.streamId,successfulAt) } }
    }
    private fun renew(descriptor:LivePlayback,request:Long) {
        renewJob?.cancel()
        if(descriptor.sessionId.isBlank()||descriptor.expiresAt<=0||backgroundPaused)return
        renewJob=scope.launch {
            var expires=descriptor.expiresAt
            while(isActive&&request==generation&&state.value.active) {
                delay(LivePlaybackRules.renewalDelayMs(expires,System.currentTimeMillis()/1000))
                if(request!=generation||backgroundPaused)return@launch
                try {
                    val renewal=repo.liveRenew(descriptor.sessionId)
                    if(request!=generation)return@launch
                    require(renewal.sessionId==descriptor.sessionId&&renewal.expiresAt>System.currentTimeMillis()/1000) { "直播会话已过期" }
                    expires=renewal.expiresAt
                    state.value=state.value.copy(descriptor=state.value.descriptor?.copy(expiresAt=expires))
                } catch(e:CancellationException) { throw e }
                catch(e:Throwable) {
                    if(request==generation&&state.value.active) {
                        if(e is ApiException&&!LivePlaybackRules.canRetry(e.status)) {
                            startupJob?.cancel();player.pause();state.value=state.value.copy(loading=false,error=LiveNetworkErrors.message(e),errorStatus=e.status)
                        } else if(state.value.finite)failCurrent("回看会话续期失败") else load(descriptor.streamId)
                    }
                    return@launch
                }
            }
        }
    }
    fun toggle() {
        if(!state.value.active)return
        desiredPlay=!desiredPlay
        if(desiredPlay&&!backgroundPaused) {
            if(state.value.finite) { if(state.value.ended) { player.seekTo(0);state.value=state.value.copy(ended=false) } }
            else if(state.value.descriptor?.mimeType?.let { LiveProgrammeRules.isHls(it) }!=false)player.seekToDefaultPosition()
            else { load(state.value.descriptor?.streamId?:0);return }
            player.play();if(state.value.loading)watchStartup(generation)
        }
        else { startupJob?.cancel();player.pause() }
    }
    fun returnToLive() {
        if(!state.value.active)return
        if(state.value.programme!=null) {
            val stream=state.value.descriptor?.streamId?:0
            state.value=state.value.copy(programme=null,position=0,duration=0,ended=false,eventReplay=false,eventId="")
            failed.clear();refreshed.clear();windowRecovered.clear();replayRefreshed=false;selectedQuality="";desiredPlay=true
            load(stream,message="正在返回实时直播",positionMs=0);return
        }
        if(state.value.error.isNotBlank()||state.value.descriptor==null||player.playbackState==Player.STATE_IDLE) { retry();return }
        desiredPlay=true
        if(state.value.descriptor?.mimeType?.let { LiveProgrammeRules.isHls(it) }==false) { load(state.value.descriptor?.streamId?:0);return }
        player.seekToDefaultPosition();player.playWhenReady=!backgroundPaused
        if(state.value.loading)watchStartup(generation)
    }
    fun quality(id:String,label:String) {
        if(!state.value.active)return
        if(id=="auto")player.trackSelectionParameters=player.trackSelectionParameters.buildUpon().clearOverridesOfType(C.TRACK_TYPE_VIDEO).build()
        else if(id.startsWith("track:")) {
            val parts=id.split(':');val group=player.currentTracks.groups.getOrNull(parts.getOrNull(1)?.toIntOrNull()?:-1)?:return
            val track=parts.getOrNull(2)?.toIntOrNull()?:return
            player.trackSelectionParameters=player.trackSelectionParameters.buildUpon().setOverrideForType(TrackSelectionOverride(group.mediaTrackGroup,listOf(track))).build()
        } else {
            val descriptor=state.value.descriptor?:return
            val variant=descriptor.qualities.find { it.id==id&&it.url.isNotBlank() }?:return
            val position=LiveProgrammeRules.restorePosition(descriptor,player.currentPosition)
            selectedQuality=id
            apply(variant.url,descriptor.name,variant.mimeType.ifBlank { descriptor.mimeType },position);watchStartup(generation)
        }
        state.value=state.value.copy(quality=label)
    }
    fun seek(positionMs:Long) {
        val descriptor=state.value.descriptor?:return
        if(descriptor.isLive||!player.isCurrentMediaItemSeekable)return
        val target=LiveProgrammeRules.restorePosition(descriptor.copy(durationMs=state.value.duration),positionMs)
        player.seekTo(target);state.value=state.value.copy(position=target,ended=false)
    }
    fun pauseForBackground() {
        if(!state.value.active||backgroundPaused)return
        backgroundPaused=true;renewJob?.cancel();startupJob?.cancel();player.pause()
    }
    fun restoreForeground() {
        if(!state.value.active||!backgroundPaused)return
        backgroundPaused=false
        val descriptor=state.value.descriptor?:return
        if(descriptor.expiresAt>0&&descriptor.expiresAt<=System.currentTimeMillis()/1000+15||descriptor.isLive&&!LiveProgrammeRules.isHls(descriptor.mimeType))load(descriptor.streamId)
        else {
            if(descriptor.isLive)player.seekToDefaultPosition()
            player.playWhenReady=desiredPlay
            renew(descriptor,generation)
            if(state.value.loading)watchStartup(generation)
        }
    }
    fun stop() {
        generation++;channelRequest++;loadJob?.cancel();renewJob?.cancel();startupJob?.cancel();detailsJob?.cancel()
        loadJob=null;renewJob=null;startupJob=null;detailsJob=null;backgroundPaused=false;failed.clear();refreshed.clear();windowRecovered.clear();rememberedSuccess="";replayRefreshed=false;selectedQuality=""
        state.value=LivePlayerState()
    }
}
