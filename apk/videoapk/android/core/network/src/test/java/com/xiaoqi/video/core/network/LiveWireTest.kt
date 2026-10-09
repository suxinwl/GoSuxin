package com.xiaoqi.video.core.network

import com.google.gson.JsonParser
import com.xiaoqi.video.core.model.*
import com.xiaoqi.video.core.network.JsonWire.long
import org.junit.Assert.*
import org.junit.Test

class LiveWireTest {
    @Test fun channelListWithoutStreamDetailsUsesAnEmptyList() {
        val channel=JsonWire.decode<LiveChannel>(JsonParser.parseString("""{"id":12,"name":"翡翠台","group_id":2,"group_name":"港台","logo":"/logo.png","tvg_id":"jade"}"""))
        assertEquals(12L,channel.id);assertEquals(2L,channel.groupId)
        assertEquals("jade",channel.tvgId);assertTrue(channel.streams.isEmpty())
    }
    @Test fun nativePlaybackKeepsOpaqueSessionAndChannelIdentity() {
        val playback=JsonWire.decode<LivePlayback>(JsonParser.parseString("""{"channel_id":12,"name":"翡翠台","stream_id":21,"stream_name":"主线","url":"/suxinvideo/live/media/session/index.m3u8","mime_type":"application/x-mpegURL","expires_at":2000,"session_id":"opaque","is_live":true,"streams":[{"id":21,"name":"主线","health":"healthy","priority":10,"quality":"高清"}],"qualities":[{"id":"720","label":"720P","height":720,"url":"/suxinvideo/live/media/session/720.m3u8"}]}"""))
        assertEquals(12L,playback.channelId);assertEquals(21L,playback.streamId)
        assertEquals("opaque",playback.sessionId);assertTrue(playback.isLive)
        assertEquals("healthy",playback.streams.single().health)
        assertEquals(720,playback.qualities.single().height)
        assertEquals("720P",playback.qualities.single().label)
    }
    @Test fun liveFailureIdentitySurvivesTheSharedApiException() {
        val failure=ApiException(502,502,"当前直播线路不可用",JsonParser.parseString("""{"stream_id":21,"streams":[{"id":21},{"id":22}]}"""))
        assertEquals(21L,failure.data!!.asJsonObject.long("stream_id"))
        assertEquals(2,failure.data!!.asJsonObject.getAsJsonArray("streams").size())
    }
    @Test fun eventReplayKeepsItsProgrammeIdentityFiniteDurationAndExplicitMime() {
        val playback=JsonWire.decode<LivePlayback>(JsonParser.parseString("""{"channel_id":12,"programme_id":321,"mode":"event_replay","is_live":false,"stream_id":21,"url":"/suxinvideo/live/media?session=opaque&asset=opaque","mime_type":"video/mp4","duration_ms":3600000,"qualities":[{"id":"720","url":"/opaque","mime_type":"application/x-mpegURL"}]}"""))
        assertFalse(playback.isLive);assertEquals(321L,playback.programmeId);assertEquals(3600000L,playback.durationMs)
        assertEquals("video/mp4",playback.mimeType);assertEquals("application/x-mpegURL",playback.qualities.single().mimeType)
        assertTrue(LiveProgrammeRules.matches(12,321,playback))
    }
    @Test fun emptyEpgAndNullableNowNextAreAcceptedWithoutInventingReplayItems() {
        val schedule=JsonWire.decode<LiveEpg>(JsonParser.parseString("""{"channel_id":12,"date":"2026-10-03","timezone":"Asia/Shanghai","items":[],"now":null,"next":null}"""))
        assertEquals(12L,schedule.channelId);assertTrue(schedule.items.isEmpty());assertNull(schedule.now);assertNull(schedule.next)
    }
}
