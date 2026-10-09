@file:androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
package com.xiaoqi.video.core.download

import android.content.Context
import androidx.media3.common.*
import androidx.media3.database.StandaloneDatabaseProvider
import androidx.media3.datasource.cache.*
import androidx.media3.datasource.okhttp.OkHttpDataSource
import androidx.media3.exoplayer.DefaultRenderersFactory
import androidx.media3.exoplayer.offline.*
import androidx.media3.exoplayer.scheduler.Requirements
import com.xiaoqi.video.core.data.*
import com.xiaoqi.video.core.model.*
import com.xiaoqi.video.core.network.*
import com.xiaoqi.video.core.player.PlaybackMediaRules
import com.xiaoqi.video.core.player.PlayerHub
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import java.io.File
import java.io.IOException
import java.security.MessageDigest
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Executors

data class DownloadMetadata(val owner:Long,val title:String,val identity:PlaybackIdentity,val license:License,val authorizedAt:Long,val serverOrigin:String?=null)
data class DownloadRow(val download:Download,val metadata:DownloadMetadata) {
    val status:String get()=when(download.state) { Download.STATE_QUEUED->"等待 Wi-Fi / 下载队列";Download.STATE_DOWNLOADING->"正在下载";Download.STATE_COMPLETED->"下载完成";Download.STATE_STOPPED->"已暂停";Download.STATE_FAILED->"下载失败，可重试";Download.STATE_REMOVING->"正在删除";else->"等待中" }
}
object Downloads {
    lateinit var manager:DownloadManager
    lateinit var cache:SimpleCache
    private lateinit var context:Context
    private val scope=CoroutineScope(SupervisorJob()+Dispatchers.Main.immediate)
    val rows=MutableStateFlow<List<DownloadRow>>(emptyList())
    val wifiOnly=MutableStateFlow(true)
    private val renewing=mutableSetOf<String>()
    private val autoRenewed=mutableSetOf<String>()
    private val overrides=ConcurrentHashMap<String,DownloadMetadata>()
    private val removed=ConcurrentHashMap.newKeySet<String>()
    private val validation=Mutex()
    private var refreshing=false
    private var clockFloor=0L
    private var clockSaved=0L
    private val preferences get()=context.getSharedPreferences("download-policy",Context.MODE_PRIVATE)
    private val cacheKeys=CacheKeyFactory { spec ->
        val s=AppGraph.repository.session.value
        // Separate account/device caches so removing one user's download cannot damage another's.
        val namespace=MediaCacheKeys.namespace(SiteHttp.currentBase,SiteHttp.BASE,s?.user?.id?:0,s?.deviceId.orEmpty())
        MediaCacheKeys.build(spec.uri.toString(),namespace,spec.key)
    }
    fun initialize(ctx:Context) {
        if(::manager.isInitialized)return
        context=ctx.applicationContext;AppGraph.initialize(context)
        wifiOnly.value=preferences.getBoolean("wifi_only",true)
        clockFloor=preferences.getLong("clock_floor",0)
        val provider=StandaloneDatabaseProvider(context)
        cache=SimpleCache(File(context.filesDir,"media-cache"),NoOpCacheEvictor(),provider)
        val upstream=OkHttpDataSource.Factory(SiteHttp.callFactory).setUserAgent("XiaoqiVideo/1.0 Android")
        val factory=CacheDataSource.Factory().setCache(cache).setCacheKeyFactory(cacheKeys).setUpstreamDataSourceFactory(upstream)
        val downloadUpstream=OkHttpDataSource.Factory(SiteHttp.callFactory).setUserAgent("XiaoqiVideo/1.0 AndroidDownload").setDefaultRequestProperties(mapOf("X-Xiaoqi-Download" to "1"))
        val workers=Executors.newFixedThreadPool(2)
        val downloaderFactory=DownloaderFactory { request ->
            // Removal also runs after account switches. Bind this factory to the request's owner,
            // rather than whichever account happens to be logged in when the bytes are removed.
            val m=runCatching { JsonWire.gson.fromJson(String(request.data,Charsets.UTF_8),DownloadMetadata::class.java) }.getOrNull()
            val namespace=MediaCacheKeys.namespace(m?.serverOrigin?:SiteHttp.BASE,SiteHttp.BASE,m?.owner?:0,m?.license?.deviceId.orEmpty())
            val scopedKeys=CacheKeyFactory { spec -> MediaCacheKeys.build(spec.uri.toString(),namespace,spec.key) }
            val downloadFactory=CacheDataSource.Factory().setCache(cache).setCacheKeyFactory(scopedKeys).setUpstreamDataSourceFactory(downloadUpstream)
            DefaultDownloaderFactory(downloadFactory,workers).createDownloader(request)
        }
        manager=DownloadManager(context,DefaultDownloadIndex(provider),downloaderFactory).apply {
            maxParallelDownloads=2;requirements=Requirements(if(wifiOnly.value)Requirements.NETWORK_UNMETERED else Requirements.NETWORK);minRetryCount=3
            // Restored requests must wait for the persisted member session to be loaded.
            pauseDownloads()
        }
        PlayerHub.initialize(context)
        PlayerHub.engine.networkFactory=factory
        // An offline cache miss fails immediately. It never falls through to an expired URL.
        PlayerHub.engine.offlineFactory=CacheDataSource.Factory().setCache(cache).setCacheKeyFactory(cacheKeys).setUpstreamDataSourceFactory(null).setCacheWriteDataSinkFactory(null)
        manager.addListener(object:DownloadManager.Listener {
            override fun onDownloadChanged(downloadManager:DownloadManager,download:Download,finalException:Exception?) {
                refreshRows()
                if(download.state==Download.STATE_FAILED && download.request.id !in renewing && download.request.id !in autoRenewed) {
                    val m=metadata(download)?:return
                    if(owned(m)) {
                        renewing.add(download.request.id);autoRenewed.add(download.request.id)
                        scope.launch { try { renew(download,m) } catch(_:Throwable) { /* One automatic refresh; manual retry remains available. */ } finally { renewing.remove(download.request.id) } }
                    }
                }
            }
            override fun onDownloadRemoved(downloadManager:DownloadManager,download:Download) {
                removed.add(download.request.id);overrides.remove(download.request.id)
                PlayerHub.engine.invalidateOfflineDownload(download.request.id,"下载文件已删除")
                scope.launch { withContext(Dispatchers.IO) { AppGraph.repository.db.records().remove("download_licenses",download.request.id) } }
                refreshRows()
            }
        })
        scope.launch {
            val repo=AppGraph.repository
            repo.ready.first { it }
            repo.session.collect { session ->
                manager.pauseDownloads()
                if(session!=null)withContext(Dispatchers.IO) {
                    repo.db.records().list(session.user.id,"download_licenses").forEach { item ->
                        runCatching { JsonWire.gson.fromJson(item.json,DownloadMetadata::class.java) }.getOrNull()?.let { overrides[item.key]=it }
                    }
                }
                val downloads=allDownloads()
                downloads.forEach { d ->
                    val m=metadata(d)
                    if(m==null || !owned(m)) {
                        if(d.stopReason==0 && d.state !in listOf(Download.STATE_COMPLETED,Download.STATE_REMOVING))manager.setStopReason(d.request.id,2)
                    } else if(d.stopReason==2) {
                        if(refusal(m)==null)manager.setStopReason(d.request.id,0)
                    }
                }
                if(session!=null && downloads.any { d -> metadata(d)?.let(::owned)==true && d.stopReason==0 && d.state in listOf(Download.STATE_QUEUED,Download.STATE_DOWNLOADING) }) {
                    runCatching { DownloadService.sendResumeDownloads(context,VideoDownloadService::class.java,false) }
                }
                refreshRows()
            }
        }
        scope.launch { while(isActive) {
            refreshRows()
            val now=System.currentTimeMillis()/1000
            if(now>=clockFloor && now-clockSaved>=60) { clockFloor=now;clockSaved=now;preferences.edit().putLong("clock_floor",clockFloor).apply() }
            delay(1000)
        } }
    }
    fun setWifiOnly(value:Boolean) { wifiOnly.value=value;preferences.edit().putBoolean("wifi_only",value).apply();manager.requirements=Requirements(if(value)Requirements.NETWORK_UNMETERED else Requirements.NETWORK) }
    private fun metadata(d:Download):DownloadMetadata?=overrides[d.request.id]?:runCatching { JsonWire.gson.fromJson(String(d.request.data,Charsets.UTF_8),DownloadMetadata::class.java) }.getOrNull()
    private fun owned(m:DownloadMetadata):Boolean {
        val session=AppGraph.repository.session.value?:return false
        return (m.serverOrigin?:SiteHttp.BASE)==SiteHttp.currentBase && session.user.id==m.owner && session.deviceId==m.license.deviceId
    }
    private fun refusal(m:DownloadMetadata):String? {
        if((m.serverOrigin?:SiteHttp.BASE)!=SiteHttp.currentBase)return "该下载属于其它服务地址，请切回原地址后播放"
        val s=AppGraph.repository.session.value
        return OfflineEntitlement.refusal(m.owner,s?.user?.id?:0,m.license.deviceId,s?.deviceId.orEmpty(),m.license.issuedAt.takeIf { it>0 }?:m.authorizedAt,m.license.expiresAt,System.currentTimeMillis()/1000,clockFloor)
    }
    private suspend fun allDownloads():List<Download> = withContext(Dispatchers.IO) {
        buildList { manager.downloadIndex.getDownloads().use { cursor -> while(cursor.moveToNext())add(cursor.download) } }
    }
    private suspend fun persist(id:String,m:DownloadMetadata) {
        withContext(Dispatchers.IO) { AppGraph.repository.db.records().put(StoredRecord("download_licenses",id,m.owner,JsonWire.gson.toJson(m),System.currentTimeMillis())) }
        overrides[id]=m;removed.remove(id)
    }
    private fun refreshRows() {
        if(refreshing)return
        refreshing=true
        scope.launch { try {
            val account=AppGraph.repository.session.value?.user?.id?:0
            rows.value=allDownloads().mapNotNull { d -> metadata(d)?.takeIf { it.owner==account && (it.serverOrigin?:SiteHttp.BASE)==SiteHttp.currentBase }?.let { DownloadRow(d,it) } }
        } finally { refreshing=false } }
    }
    suspend fun enqueue(identity:PlaybackIdentity,title:String) {
        val repo=AppGraph.repository;val owner=repo.session.value?.user?.id?:throw IllegalStateException("请先登录后下载")
        val origin=SiteHttp.currentBase
        if(File(context.filesDir.absolutePath).usableSpace<200L*1024*1024)throw IOException("存储空间不足，请先清理下载")
        val license=repo.authorize(identity)
        val p=license.descriptor
        val normalized=identity.copy(line=p.line,episodeKey=p.episodeKey,episode=p.episode,quality=p.quality.ifBlank { identity.quality },versionKey=p.versionKey,positionMs=0)
        val identityKey=PlaybackRules.downloadKey(owner,p.vodId,normalized.line,p.versionKey,normalized.episodeKey,normalized.quality)
        val key=if(origin==SiteHttp.BASE)identityKey else "$origin:$identityKey"
        val id=MessageDigest.getInstance("SHA-256").digest(key.toByteArray()).joinToString("") { "%02x".format(it) }
        val m=DownloadMetadata(owner,title,normalized,license,System.currentTimeMillis()/1000,origin)
        require(owned(m)) { "下载授权不属于当前账号和设备" }
        val previous=allDownloads().find { it.request.id==id }?.let(::metadata)
        require(previous==null || previous.license.revision==license.revision) { "片源已更新，请删除旧下载后重新下载" }
        submit(id,m)
    }
    private suspend fun submit(id:String,m:DownloadMetadata) {
        require(owned(m)) { "账号已切换，请重新登录下载账号" }
        val p=m.license.descriptor
        require(p.seekMode!="server-offset") { "该线路尚不支持完整离线下载，请选择其它线路" }
        val item=MediaItem.Builder().setUri(SiteHttp.absolute(p.url)).apply { if(PlaybackMediaRules.isHls(p.type,p.url))setMimeType(MimeTypes.APPLICATION_M3U8) }.build()
        val selected=p.qualities.firstOrNull { it.id==m.identity.quality }
        val parameters=DownloadHelper.DEFAULT_TRACK_SELECTOR_PARAMETERS.buildUpon().apply {
            if(selected!=null && selected.height>0)setMaxVideoSize(selected.width.takeIf { it>0 }?:Int.MAX_VALUE,selected.height)
            if(selected!=null && selected.bitrate in 1..Int.MAX_VALUE.toLong())setMaxVideoBitrate(selected.bitrate.toInt())
        }.build()
        val helper=DownloadHelper.Factory().setDataSourceFactory(OkHttpDataSource.Factory(SiteHttp.callFactory).setDefaultRequestProperties(mapOf("X-Xiaoqi-Download" to "1"))).setRenderersFactory(DefaultRenderersFactory(context)).setTrackSelectionParameters(parameters).create(item)
        val request=suspendCancellableCoroutine<DownloadRequest> { continuation ->
            continuation.invokeOnCancellation { helper.release() }
            helper.prepare(object:DownloadHelper.Callback {
                override fun onPrepared(helper:DownloadHelper,tracksInfoAvailable:Boolean) { try {
                    val request=helper.getDownloadRequest(id,JsonWire.gson.toJson(m).toByteArray(Charsets.UTF_8))
                    if(continuation.isActive)continuation.resumeWith(Result.success(request))
                } catch(e:Throwable) { if(continuation.isActive)continuation.resumeWith(Result.failure(e)) } finally { helper.release() } }
                override fun onPrepareError(helper:DownloadHelper,e:IOException) { helper.release();if(continuation.isActive)continuation.resumeWith(Result.failure(e)) }
            })
        }
        require(owned(m)) { "账号已切换，请重新登录下载账号" }
        persist(id,m)
        DownloadService.sendAddDownload(context,VideoDownloadService::class.java,request,false)
        DownloadService.sendResumeDownloads(context,VideoDownloadService::class.java,false)
        refreshRows()
    }
    private suspend fun renew(d:Download,m:DownloadMetadata) {
        require(owned(m)) { "请登录原下载账号，授权仅限当前设备" }
        val next=AppGraph.repository.renewLicense(m.license.id)
        require(next.revision==m.license.revision) { "片源内容已更新，请删除旧下载后重新下载" }
        val updated=m.copy(license=next,authorizedAt=System.currentTimeMillis()/1000)
        require(owned(updated)) { "账号已切换，请重新登录下载账号" }
        if(d.state==Download.STATE_COMPLETED)persist(d.request.id,updated) else submit(d.request.id,updated)
    }
    fun pause(id:String)=DownloadService.sendSetStopReason(context,VideoDownloadService::class.java,id,1,false)
    fun resume(row:DownloadRow) {
        val id=row.download.request.id
        if(id in renewing)return
        autoRenewed.remove(id);renewing.add(id)
        scope.launch { try {
            renew(row.download,overrides[id]?:row.metadata)
            DownloadService.sendSetStopReason(context,VideoDownloadService::class.java,id,0,false)
            DownloadService.sendResumeDownloads(context,VideoDownloadService::class.java,false)
        } catch(e:Throwable) { AppGraph.repository.error(e) } finally { renewing.remove(id) } }
    }
    fun delete(id:String) {
        removed.add(id);PlayerHub.engine.invalidateOfflineDownload(id,"下载文件已删除")
        DownloadService.sendRemoveDownload(context,VideoDownloadService::class.java,id,false)
    }
    suspend fun play(row:DownloadRow) {
        val id=row.download.request.id
        val m=overrides[id]?:row.metadata
        refusal(m)?.let { throw IllegalStateException(it) }
        require(row.download.state==Download.STATE_COMPLETED && id !in removed) { "尚未下载完成或文件已删除" }
        val p=m.license.descriptor.copy(url=row.download.request.uri.toString())
        val progress=AppGraph.repository.progress(p.vodId)
        val position=progress?.takeIf { it.get("line")?.asString==p.line && it.get("episode_key")?.asString==p.episodeKey && it.get("version_key")?.asString==p.versionKey }?.get("position_ms")?.asLong?:0
        PlayerHub.engine.offline(p,position,row.download.request.streamKeys,id) {
            if(id in removed)"下载文件或离线授权已删除" else refusal(overrides[id]?:m)
        }
    }
    suspend fun validateOnline() = validation.withLock {
        val repo=AppGraph.repository
        val account=repo.session.value?.user?.id?:return@withLock
        val listed=try { repo.api.request("downloads/licenses").asJsonArray.associateBy { it.asJsonObject.get("id").asString } }
            catch(e:CancellationException) { throw e } catch(_:IOException) { return@withLock } catch(e:ApiException) { return@withLock }
        val current=allDownloads().mapNotNull { d -> metadata(d)?.takeIf { it.owner==account && (it.serverOrigin?:SiteHttp.BASE)==SiteHttp.currentBase }?.let { DownloadRow(d,it) } }
        for(r in current) {
            val id=r.download.request.id
            val m=overrides[id]?:r.metadata
            if(!owned(m))continue
            val item=listed[m.license.id]?.asJsonObject
            if(item?.get("revoked")?.asBoolean==true || (item!=null && item.get("revision")?.asString!=m.license.revision)) { delete(id);continue }
            // License listing is bounded; absence alone does not revoke an older valid download.
            if(m.license.expiresAt-System.currentTimeMillis()/1000>24*3600 && item!=null)continue
            try { renew(r.download,m) }
            catch(e:CancellationException) { throw e }
            catch(e:ApiException) { if(e.status in listOf(403,404,409))delete(id) }
            catch(_:IOException) { /* Offline transport errors retain the existing bounded entitlement. */ }
            catch(e:IllegalArgumentException) { delete(id) }
        }
        refreshRows()
    }
}
