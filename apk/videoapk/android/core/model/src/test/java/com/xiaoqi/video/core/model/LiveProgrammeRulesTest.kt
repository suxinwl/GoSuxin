package com.xiaoqi.video.core.model

import org.junit.Assert.*
import org.junit.Test

class LiveProgrammeRulesTest {
    @Test fun aScheduleAloneDoesNotAuthorizePastPresentOrFutureReplays() {
        val ended=LiveProgramme(7,2,"新闻",100,200)
        assertFalse(LiveProgrammeRules.replayable(ended,300))
        assertFalse(LiveProgrammeRules.replayable(ended.copy(canReplay=true),150))
        assertFalse(LiveProgrammeRules.replayable(ended.copy(canReplay=true),50))
        assertTrue(LiveProgrammeRules.replayable(ended.copy(canReplay=true),200))
        assertFalse(LiveProgrammeRules.replayable(ended.copy(id=0,canReplay=true),300))
    }
    @Test fun wrongProgrammeChannelOrLiveDescriptorCannotReplaceAnRequestedReplay() {
        val playback=LivePlayback(channelId=2,programmeId=7,isLive=false,mode="catchup",streamId=4)
        assertTrue(LiveProgrammeRules.matches(2,7,playback))
        assertFalse(LiveProgrammeRules.matches(2,8,playback))
        assertFalse(LiveProgrammeRules.matches(3,7,playback))
        assertFalse(LiveProgrammeRules.matches(2,7,playback.copy(isLive=true)))
        assertFalse(LiveProgrammeRules.matches(2,7,playback.copy(mode="live")))
        assertFalse(LiveProgrammeRules.matches(2,0,playback))
    }
    @Test fun replayIdentityDoesNotReuseFilmOrLiveChannelIdentity() {
        val playback=LivePlayback(channelId=2,streamId=4,programmeId=7,isLive=false,mode="event_replay")
        assertEquals("live-replay:2:7:4:event_replay",LiveProgrammeRules.identity(playback))
        assertNotEquals(LiveProgrammeRules.identity(playback),LiveProgrammeRules.identity(playback.copy(isLive=true)))
        assertNotEquals(LiveProgrammeRules.identity(playback),LiveProgrammeRules.identity(playback.copy(programmeId=8)))
    }
    @Test fun aChannelMayResolveAnActualEventButNotAnUnrequestedCatchup() {
        val event=LivePlayback(channelId=2,programmeId=7,isLive=false,mode="event_replay",eventId="match-2026-10-03")
        assertTrue(LiveProgrammeRules.matches(2,0,event))
        assertTrue(LiveProgrammeRules.matches(2,0,event.copy(programmeId=0)))
        assertFalse(LiveProgrammeRules.matches(3,0,event))
        assertFalse(LiveProgrammeRules.matches(2,0,event.copy(mode="catchup")))
        assertFalse(LiveProgrammeRules.matches(2,0,event.copy(programmeId=-1)))
        assertFalse(LiveProgrammeRules.matches(2,8,event))
        assertTrue(LiveProgrammeRules.sameEvent("match-2026-10-03",event))
        assertFalse(LiveProgrammeRules.sameEvent("match-2026-10-02",event))
        assertNotEquals(LiveProgrammeRules.identity(event),LiveProgrammeRules.identity(event.copy(eventId="match-2026-10-02")))
        assertNotEquals(LiveProgrammeRules.identity(event),LiveProgrammeRules.identity(event.copy(isLive=true)))
    }
    @Test fun replayResumeIsClampedButLiveNeverUsesAnOldWindowPosition() {
        val replay=LivePlayback(isLive=false,durationMs=20000)
        assertEquals(12000L,LiveProgrammeRules.restorePosition(replay,12000))
        assertEquals(19999L,LiveProgrammeRules.restorePosition(replay,99000))
        assertEquals(0L,LiveProgrammeRules.restorePosition(replay,-1))
        assertEquals(0L,LiveProgrammeRules.restorePosition(replay.copy(isLive=true),12000))
    }
    @Test fun opaqueMediaIsSelectedByExplicitMimeAndUnknownTypesAreRejected() {
        assertTrue(LiveProgrammeRules.isHls("application/vnd.apple.mpegurl; charset=UTF-8"))
        assertTrue(LiveProgrammeRules.isHls(""))
        assertFalse(LiveProgrammeRules.isHls("video/mp4"))
        assertEquals("video/x-flv",LiveProgrammeRules.mediaMime("video/flv"))
        assertThrows(IllegalArgumentException::class.java) { LiveProgrammeRules.mediaMime("text/html") }
    }
    @Test fun replayRefreshIsBoundedAndNeverBypassesPolicyDenials() {
        listOf(0,408,502,503).forEach { assertTrue(LiveProgrammeRules.canRefreshReplay(it,false));assertFalse(LiveProgrammeRules.canRefreshReplay(it,true)) }
        listOf(400,401,403,404,410,451).forEach { assertFalse(LiveProgrammeRules.canRefreshReplay(it,false)) }
    }
    @Test fun datesUseTheScheduleTimeZoneInsteadOfDeviceTimeZone() {
        val seconds=java.time.Instant.parse("2026-10-03T17:00:00Z").epochSecond
        assertEquals("2026-10-04",LiveProgrammeRules.today(seconds,"Asia/Shanghai"))
        assertEquals("2026-10-03",LiveProgrammeRules.today(seconds,"UTC"))
        assertEquals("01:00",LiveProgrammeRules.clock(seconds,"Asia/Shanghai"))
        assertEquals("2026-10-04",LiveProgrammeRules.today(seconds,"invalid/timezone"))
        assertEquals(9,LiveProgrammeRules.dates(seconds).size)
    }
}
