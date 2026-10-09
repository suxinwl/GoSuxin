package com.xiaoqi.video.core.model

import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter

/** Programmes are not films: they never have a vod ID, downloads or episode navigation. */
data class LiveProgramme(val id:Long=0,val channelId:Long=0,val title:String="",val start:Long=0,val stop:Long=0,
    val canReplay:Boolean=false,val kind:String="",val streamId:Long=0,val accessLevel:String="public")
data class LiveEpg(val channelId:Long=0,val date:String="",val timezone:String="Asia/Shanghai",
    val items:List<LiveProgramme> = emptyList(),val now:LiveProgramme?=null,val next:LiveProgramme?=null)

object LiveProgrammeRules {
    fun canRefreshReplay(status:Int,alreadyRefreshed:Boolean)=!alreadyRefreshed&&(status==0||status==408||status==429||status>=500)
    fun replayable(programme:LiveProgramme,nowSeconds:Long)=programme.id>0&&programme.canReplay&&programme.start>0&&programme.stop>programme.start&&programme.stop<=nowSeconds
    fun phase(programme:LiveProgramme,nowSeconds:Long)=when {
        nowSeconds<programme.start->"即将播出"
        nowSeconds<programme.stop->"正在播出"
        replayable(programme,nowSeconds)->if(programme.kind=="event_replay")"赛事回放" else "可回看"
        else->"无回看"
    }
    fun mediaMime(value:String):String=when(value.lowercase().substringBefore(';').trim()) {
        "","application/x-mpegurl","application/vnd.apple.mpegurl"->"application/x-mpegURL"
        "video/flv","video/x-flv"->"video/x-flv"
        "video/mp4","application/mp4"->"video/mp4"
        else->throw IllegalArgumentException("不支持的直播媒体格式")
    }
    fun isHls(value:String)=mediaMime(value)=="application/x-mpegURL"
    fun identity(descriptor:LivePlayback):String=if(descriptor.isLive)"live:${descriptor.channelId}:${descriptor.streamId}"
        else if(descriptor.eventId.isNotBlank())"live-event:${descriptor.channelId}:${descriptor.eventId}:${descriptor.streamId}"
        else "live-replay:${descriptor.channelId}:${descriptor.programmeId}:${descriptor.streamId}:${descriptor.mode}"
    fun matches(channelId:Long,programmeId:Long,descriptor:LivePlayback)=channelId>0&&channelId==descriptor.channelId&&
        if(programmeId>0)!descriptor.isLive&&descriptor.programmeId==programmeId&&descriptor.mode in setOf("catchup","event_replay")
        else descriptor.isLive&&descriptor.programmeId==0L || !descriptor.isLive&&descriptor.mode=="event_replay"&&descriptor.programmeId>=0
    fun sameEvent(expected:String,descriptor:LivePlayback)=expected.isBlank()||!descriptor.isLive&&descriptor.mode=="event_replay"&&descriptor.eventId==expected
    fun restorePosition(descriptor:LivePlayback,position:Long)=if(descriptor.isLive)0L else position.coerceAtLeast(0).let { value->
        if(descriptor.durationMs>0)value.coerceAtMost((descriptor.durationMs-1).coerceAtLeast(0)) else value
    }
    fun zone(value:String):ZoneId=runCatching { ZoneId.of(value) }.getOrElse { ZoneId.of("Asia/Shanghai") }
    fun today(nowSeconds:Long=System.currentTimeMillis()/1000,timezone:String="Asia/Shanghai")=Instant.ofEpochSecond(nowSeconds).atZone(zone(timezone)).toLocalDate().toString()
    fun dates(nowSeconds:Long=System.currentTimeMillis()/1000,timezone:String="Asia/Shanghai"):List<String> {
        val today=LocalDate.parse(today(nowSeconds,timezone))
        return (-7L..1L).map { today.plusDays(it).toString() }
    }
    fun clock(seconds:Long,timezone:String="Asia/Shanghai"):String=if(seconds<=0)"--:--" else
        DateTimeFormatter.ofPattern("HH:mm").withZone(zone(timezone)).format(Instant.ofEpochSecond(seconds))
    fun range(programme:LiveProgramme,timezone:String="Asia/Shanghai")="${clock(programme.start,timezone)}–${clock(programme.stop,timezone)}"
}
