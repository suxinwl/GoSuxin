package com.xiaoqi.video.core.model

/** Television identities are independent of films and episodes. */
data class LiveGroup(val id:Long=0,val name:String="")
data class LiveStream(val id:Long=0,val name:String="",val health:String="",val priority:Int=0,val quality:String="",val accessLevel:String="public")
data class LiveChannel(val id:Long=0,val name:String="",val logo:String="",val groupId:Long=0,val groupName:String="",val tvgId:String="",val streams:List<LiveStream> = emptyList(),val accessLevel:String="public")
data class LiveChannelPage(val items:List<LiveChannel> = emptyList(),val total:Int=0,val page:Int=1,val size:Int=60) {
    val pages:Int get()=((total.toLong()+size.coerceAtLeast(1)-1)/size.coerceAtLeast(1)).coerceAtLeast(1).toInt()
}
data class LiveQuality(val id:String="",val label:String="",val width:Int=0,val height:Int=0,val url:String="",val mimeType:String="")
data class LivePlayback(val channelId:Long=0,val name:String="",val logo:String="",val streamId:Long=0,val streamName:String="",val url:String="",val mimeType:String="",val expiresAt:Long=0,val sessionId:String="",val qualities:List<LiveQuality> = emptyList(),val isLive:Boolean=true,val streams:List<LiveStream> = emptyList(),val programmeId:Long=0,val mode:String="live",val durationMs:Long=0,val eventId:String="",val programmeTitle:String="")
data class LiveRenewal(val sessionId:String="",val expiresAt:Long=0)
data class LiveRetryDecision(val excluded:Set<Long>,val retry:Boolean)
data class LiveRecoveryDecision(val excluded:Set<Long>,val refreshed:Set<Long>,val retry:Boolean,val streamId:Long=0)

/** Translate technical fallback labels; dimensions only come from real media metadata. */
object LivePresentationRules {
    private val chinese=Regex("[\\u3400-\\u9FFF]")
    fun streamName(stream:LiveStream,index:Int):String = stream.name.trim().takeIf { chinese.containsMatchIn(it) }
        ?:if(index==0)"主线路" else "备用线路 $index"
    fun qualityLabel(label:String,height:Int=0,width:Int=0):String {
        if(height>0)return if(width>0)"${width}×${height}" else "${height}P"
        val value=label.trim()
        if(chinese.containsMatchIn(value))return value
        if(value.matches(Regex("\\d{3,4}[pP]")))return value.uppercase()
        if(value.matches(Regex("\\d{3,4}[xX×]\\d{3,4}")))return value.replace('x','×').replace('X','×')
        return when(value.lowercase()) { "hd"->"高清";"fhd"->"全高清";"sd"->"标清";"auto"->"自动";else->"原始清晰度" }
    }
    fun streamStatus(stream:LiveStream,failed:Boolean=false):String = if(failed)"本轮连接失败" else when(stream.health.lowercase()) {
        "healthy"->"已检测可用";"unhealthy","failed","offline"->"待重试";else->"未检测"
    }
}

object LivePlaybackRules {
    fun canRetry(status:Int)=status !in setOf(401,403,404,410,451)
    fun resolveBody(channelId:Long,streamId:Long,excluded:Set<Long>):Map<String,Any> = mapOf(
        "channel_id" to channelId,"stream_id" to streamId,"exclude_stream_ids" to excluded.filter { it>0 }.sorted())
    fun renewalDelayMs(expiresAt:Long,nowSeconds:Long):Long=((expiresAt-nowSeconds-60).coerceAtLeast(5)*1000).coerceAtMost(60000)
    fun channelMatches(requested:Long,received:Long)=requested>0 && requested==received
    fun failedStream(excluded:Set<Long>,failedId:Long,candidates:List<Long>):LiveRetryDecision {
        val updated=if(failedId>0)excluded+failedId else excluded
        return LiveRetryDecision(updated,failedId>0&&failedId !in excluded&&(candidates.isEmpty()||candidates.any { it>0&&it !in updated }))
    }
    /** Refresh a failed line once before excluding it; a repeated failure cannot loop. */
    fun recoverStream(excluded:Set<Long>,refreshed:Set<Long>,failedId:Long,candidates:List<Long>,status:Int=0):LiveRecoveryDecision {
        if(!canRetry(status)||failedId<=0||failedId in excluded)return LiveRecoveryDecision(excluded,refreshed,false)
        if(failedId !in refreshed)return LiveRecoveryDecision(excluded,refreshed+failedId,true,failedId)
        val fallback=failedStream(excluded,failedId,candidates)
        return LiveRecoveryDecision(fallback.excluded,refreshed,fallback.retry)
    }
}
