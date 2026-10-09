package com.xiaoqi.video.core.data

import com.xiaoqi.video.core.model.*
import com.xiaoqi.video.core.network.JsonWire
import com.xiaoqi.video.core.network.JsonWire.array
import com.xiaoqi.video.core.network.JsonWire.long
import com.xiaoqi.video.core.network.JsonWire.string
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/** Guests remain supported; a signed-in member uses the normal revocable/refreshable token. */
suspend fun AppRepository.liveGroups():List<LiveGroup> = api.request("live/groups").asJsonObject
    .array("items").filter { it.isJsonObject }.map { JsonWire.decode<LiveGroup>(it) }.filter { it.id>0 && it.name.isNotBlank() }.distinctBy { it.id }

suspend fun AppRepository.liveChannels(group:Long=0,query:String="",page:Int=1,size:Int=60,channelId:Long=0):LiveChannelPage {
    val params=mutableMapOf("group_id" to group.toString(),"q" to query.trim(),"page" to page.coerceAtLeast(1).toString(),"size" to size.coerceIn(1,100).toString())
    if(channelId>0)params["channel_id"]=channelId.toString()
    val o=api.request("live/channels",query=params).asJsonObject
    return LiveChannelPage(o.array("items").filter { it.isJsonObject }.map { JsonWire.decode<LiveChannel>(it) }
        .filter { it.id>0 && it.name.isNotBlank() }.distinctBy { it.id },o.long("total").toInt().coerceAtLeast(0),o.long("page",fallback=page.toLong()).toInt().coerceAtLeast(1),o.long("size",fallback=size.toLong()).toInt().coerceIn(1,100))
}

suspend fun AppRepository.liveResolve(channelId:Long,streamId:Long=0,excluded:Set<Long> = emptySet()):LivePlayback {
    require(channelId>0) { "直播频道编号无效" }
    val result=JsonWire.decode<LivePlayback>(api.request("live/resolve","POST",LivePlaybackRules.resolveBody(channelId,streamId,excluded)))
    require(LiveProgrammeRules.matches(channelId,0,result) && result.url.isNotBlank()) { "直播源未提供可用播放地址" }
    return result
}

suspend fun AppRepository.liveRenew(sessionId:String):LiveRenewal = JsonWire.decode(api.request("live/renew","POST",mapOf("session_id" to sessionId)))

suspend fun AppRepository.liveEpg(channelId:Long,date:String):LiveEpg {
    require(channelId>0) { "频道编号无效" }
    val o=api.request("live/epg",query=mapOf("channel_id" to channelId.toString(),"date" to date)).asJsonObject
    val items=o.array("items").filter { it.isJsonObject }.map { JsonWire.decode<LiveProgramme>(it) }.filter { it.id>0&&it.title.isNotBlank() }.distinctBy { it.id }
    fun item(key:String)=o.get(key)?.takeIf { it.isJsonObject }?.let { JsonWire.decode<LiveProgramme>(it) }
    return LiveEpg(o.long("channel_id",fallback=channelId),o.string("date",fallback=date),o.string("timezone",fallback="Asia/Shanghai"),items,item("now"),item("next"))
}

suspend fun AppRepository.liveReplay(channelId:Long,programmeId:Long,streamId:Long=0,quality:String=""):LivePlayback {
    require(channelId>0&&programmeId>0) { "回看节目编号无效" }
    val result=JsonWire.decode<LivePlayback>(api.request("live/replay/resolve","POST",mapOf("channel_id" to channelId,"programme_id" to programmeId,"stream_id" to streamId,"quality" to quality)))
    require(LiveProgrammeRules.matches(channelId,programmeId,result)&&result.url.isNotBlank()) { "回看节目身份或播放地址无效" }
    return result
}

suspend fun AppRepository.rememberLiveChannel(channel:LiveChannel)=withContext(Dispatchers.IO) {
    db.records().put(StoredRecord("live_last_channel","last",0,JsonWire.gson.toJson(channel),System.currentTimeMillis()))
}

suspend fun AppRepository.lastLiveChannel():LiveChannel?=withContext(Dispatchers.IO) {
    db.records().get("live_last_channel","last")?.let { runCatching { JsonWire.gson.fromJson(it.json,LiveChannel::class.java) }.getOrNull() }
}

/** This bucket never shares film progress and is written only after media becomes ready. */
suspend fun AppRepository.rememberLiveStream(channelId:Long,streamId:Long,successfulAt:Long)=withContext(Dispatchers.IO) {
    if(channelId<=0||streamId<=0)return@withContext
    db.runInTransaction(Runnable {
        val previous=db.records().get("live_recent_stream",channelId.toString())
        if(previous==null||previous.updated<=successfulAt)db.records().put(StoredRecord("live_recent_stream",channelId.toString(),0,JsonWire.gson.toJson(mapOf("stream_id" to streamId)),successfulAt))
    })
}

suspend fun AppRepository.lastLiveStream(channelId:Long):Long=withContext(Dispatchers.IO) {
    db.records().get("live_recent_stream",channelId.toString())?.let { record->
        runCatching { com.google.gson.JsonParser.parseString(record.json).asJsonObject.long("stream_id") }.getOrDefault(0)
    }?.coerceAtLeast(0)?:0
}
