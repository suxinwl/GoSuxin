package com.xiaoqi.video.core.model

import org.junit.Assert.*
import org.junit.Test

class LivePlaybackRulesTest {
    @Test fun liveRequestUsesChannelIdentityAndStableFailureExclusions() {
        val request=LivePlaybackRules.resolveBody(12,3,setOf(8,2,0,-5))
        assertEquals(12L,request["channel_id"])
        assertEquals(3L,request["stream_id"])
        assertEquals(listOf(2L,8L),request["exclude_stream_ids"])
        assertFalse(request.containsKey("vod_id"))
        assertFalse(request.containsKey("position_ms"))
        assertFalse(request.containsKey("episode_key"))
    }
    @Test fun obsoleteChannelResponseCannotMatchTheNewSelection() {
        assertTrue(LivePlaybackRules.channelMatches(3,3))
        assertFalse(LivePlaybackRules.channelMatches(3,4))
        assertFalse(LivePlaybackRules.channelMatches(0,0))
    }
    @Test fun authenticationAndPolicyDenialsDoNotCycleThroughOtherStreams() {
        listOf(401,403,404,410,451).forEach { assertFalse(LivePlaybackRules.canRetry(it)) }
        listOf(0,408,500,502,503,504).forEach { assertTrue(LivePlaybackRules.canRetry(it)) }
    }
    @Test fun sessionRenewalRunsBeforeExpiryWithoutHotLooping() {
        assertEquals(60000L,LivePlaybackRules.renewalDelayMs(2000,1000))
        assertEquals(40000L,LivePlaybackRules.renewalDelayMs(1100,1000))
        assertEquals(5000L,LivePlaybackRules.renewalDelayMs(990,1000))
    }
    @Test fun channelPaginationDoesNotDropPartialRows() {
        assertEquals(3,LiveChannelPage(total=121,size=60).pages)
        assertEquals(1,LiveChannelPage(total=0,size=60).pages)
    }
    @Test fun failuresWalkEveryCandidateOnceAndThenStop() {
        val first=LivePlaybackRules.failedStream(emptySet(),10,listOf(10,20,30))
        assertTrue(first.retry);assertEquals(setOf(10L),first.excluded)
        val second=LivePlaybackRules.failedStream(first.excluded,20,listOf(10,20,30))
        assertTrue(second.retry);assertEquals(setOf(10L,20L),second.excluded)
        val last=LivePlaybackRules.failedStream(second.excluded,30,listOf(10,20,30))
        assertFalse(last.retry);assertEquals(setOf(10L,20L,30L),last.excluded)
    }
    @Test fun aServerReturningAnExcludedOrMissingIdentityCannotLoop() {
        assertFalse(LivePlaybackRules.failedStream(setOf(10),10,listOf(10,20)).retry)
        assertFalse(LivePlaybackRules.failedStream(emptySet(),0,listOf(10,20)).retry)
        assertTrue(LivePlaybackRules.failedStream(emptySet(),10,emptyList()).retry)
    }
    @Test fun aLineIsRefreshedExactlyOnceBeforeItIsExcluded() {
        val first=LivePlaybackRules.recoverStream(emptySet(),emptySet(),10,listOf(10,20),502)
        assertTrue(first.retry);assertEquals(10L,first.streamId)
        assertTrue(first.excluded.isEmpty());assertEquals(setOf(10L),first.refreshed)
        val fallback=LivePlaybackRules.recoverStream(first.excluded,first.refreshed,10,listOf(10,20),502)
        assertTrue(fallback.retry);assertEquals(0L,fallback.streamId)
        assertEquals(setOf(10L),fallback.excluded)
        val rejected=LivePlaybackRules.recoverStream(fallback.excluded,fallback.refreshed,10,listOf(10,20),502)
        assertFalse(rejected.retry)
    }
    @Test fun finalLineGetsItsOwnRefreshBeforeFallbackIsExhausted() {
        val refresh=LivePlaybackRules.recoverStream(setOf(10),setOf(10),20,listOf(10,20))
        assertTrue(refresh.retry);assertEquals(20L,refresh.streamId)
        assertEquals(setOf(10L),refresh.excluded)
        val exhausted=LivePlaybackRules.recoverStream(refresh.excluded,refresh.refreshed,20,listOf(10,20))
        assertFalse(exhausted.retry);assertEquals(setOf(10L,20L),exhausted.excluded)
    }
    @Test fun deniedOrUnidentifiedFailuresCannotTriggerARefresh() {
        listOf(401,403,404,410,451).forEach { status->
            val denied=LivePlaybackRules.recoverStream(emptySet(),emptySet(),10,listOf(10,20),status)
            assertFalse(denied.retry);assertTrue(denied.excluded.isEmpty());assertTrue(denied.refreshed.isEmpty())
        }
        assertFalse(LivePlaybackRules.recoverStream(emptySet(),emptySet(),0,listOf(10,20),502).retry)
    }
}
