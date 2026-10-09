package com.xiaoqi.video.core.player

import com.xiaoqi.video.core.model.*
import org.junit.Assert.*
import org.junit.Test

class PlaybackRecoveryRulesTest {
    private val film=Film(19,"恢复测试")
    private val old=Source("broken",versionKey="season:1",episodes=listOf(Episode("old:3",3)))
    private val request=PlaybackIdentity(film.id,old.code,"old:3",positionMs=45000,versionKey="season:1")
    private fun detail(vararg sources:Source)=Detail(film,sources.toList(),"",emptyList(),emptyList())

    @Test fun accessDenialsDownlistedFilmsAndOfflineNeverTriggerSearch() {
        listOf(401,403,404,410,451).forEach { assertFalse(PlaybackRecoveryRules.canDiscover(false,it,film.id)) }
        assertFalse(PlaybackRecoveryRules.canDiscover(true,0,film.id))
        assertFalse(PlaybackRecoveryRules.canDiscover(false,0,0))
        assertTrue(PlaybackRecoveryRules.canDiscover(false,503,film.id))
    }
    @Test fun aDiscoveredLineResumesTheMatchingEpisodeAndPosition() {
        val next=Source("working",versionKey="season:1",episodes=listOf(Episode("new:2",2),Episode("new:3",3)))
        val recovered=PlaybackRecoveryRules.discoveredIdentity(request,listOf(old),detail(old,next),setOf("broken"),false)
        assertEquals("working",recovered?.line)
        assertEquals("new:3",recovered?.episodeKey)
        assertEquals(1,recovered?.episode)
        assertEquals(45000L,recovered?.positionMs)
    }
    @Test fun discoveryCannotCrossAnEditionOrGuessAnotherEpisode() {
        val wrongVersion=Source("season2",versionKey="season:2",episodes=listOf(Episode("new:3",3)))
        val wrongEpisode=Source("season1",versionKey="season:1",episodes=listOf(Episode("new:2",2)))
        assertNull(PlaybackRecoveryRules.discoveredIdentity(request,listOf(old),detail(wrongVersion,wrongEpisode),emptySet(),false))
    }
    @Test fun updatedExistingLinesMayRecoverWithoutIncreasingTheLineCount() {
        assertNull(PlaybackRecoveryRules.discoveredIdentity(request,listOf(old),detail(old),setOf("broken"),false))
        assertEquals("broken",PlaybackRecoveryRules.discoveredIdentity(request,listOf(old),detail(old),setOf("broken"),true)?.line)
    }
    @Test fun aFilmWithNoPreviousLinesMayStartItsPreferredFirstEpisode() {
        val first=Source("preferred",versionKey="movie",episodes=listOf(Episode("movie",-1)))
        assertEquals("movie",PlaybackRecoveryRules.discoveredIdentity(PlaybackIdentity(film.id),emptyList(),detail(first),emptySet(),false)?.episodeKey)
        assertNull(PlaybackRecoveryRules.discoveredIdentity(PlaybackIdentity(film.id,episode=5),emptyList(),detail(first),emptySet(),false))
    }
    @Test fun automaticAttemptIdentityFollowsEpisodeNotTheFailingLine() {
        val second=Source("working",versionKey="season:1",episodes=listOf(Episode("new:3",3)))
        val same=request.copy(line="working",episodeKey="new:3")
        assertEquals(PlaybackRecoveryRules.attemptKey(request,listOf(old,second)),PlaybackRecoveryRules.attemptKey(same,listOf(old,second)))
        assertNotEquals(PlaybackRecoveryRules.attemptKey(request,listOf(old,second)),PlaybackRecoveryRules.attemptKey(request.copy(episodeKey="old:4",episode=1),listOf(old,second)))
    }
    @Test fun onlyActiveTaskStatusesKeepPollingAndReportedCountsAreReal() {
        assertTrue(SourceDiscoveryProgress(status="queued").running)
        assertTrue(SourceDiscoveryProgress(status="running").running)
        listOf("done","partial","error","disabled","busy").forEach { assertFalse(SourceDiscoveryProgress(status=it).running) }
        assertTrue(SourceDiscoveryProgress("running",checked=2,total=18,added=1,message="正在搜索").summary.contains("2/18"))
    }

    @Test fun aFailedNewEpisodeNeverRecoversOrManuallySwitchesBackToTheOldDescriptor() {
        val previous=old.copy(episodes=listOf(Episode("old:3",3),Episode("old:4",4)))
        val descriptor=Playback(vodId=film.id,line=old.code,versionKey=old.versionKey,episodeKey="old:3")
        val requested=request.copy(episodeKey="old:4",episode=1,positionMs=0)
        val pending=PlaybackRecoveryRules.currentIdentity(requested,descriptor,45000,"",pending=true)!!
        assertEquals("old:4",pending.episodeKey)
        assertEquals(0L,pending.positionMs)
        val next=Source("discovered",versionKey="season:1",episodes=listOf(Episode("new:3",3),Episode("new:4",4)))
        val recovered=PlaybackRecoveryRules.discoveredIdentity(pending,listOf(previous),detail(previous,next),setOf(old.code),false)
        assertEquals("new:4",recovered?.episodeKey)
        assertEquals(0L,recovered?.positionMs)
        assertEquals("new:4",PlaybackMediaRules.manualSource(listOf(previous,next),next,pending,pending.positionMs)?.episodeKey)
    }

    @Test fun aResolvedIdentityUsesTheActualLineVersionAndCurrentProgress() {
        val descriptor=Playback(vodId=film.id,line="actual",versionKey="season:1",episodeKey="episode:3",episode=2)
        val normalized=PlaybackIdentity(film.id,descriptor.line,descriptor.episodeKey,descriptor.episode,versionKey=descriptor.versionKey)
        val current=PlaybackRecoveryRules.currentIdentity(normalized,descriptor,89000,"1080",pending=false)
        assertEquals("actual",current?.line)
        assertEquals("season:1",current?.versionKey)
        assertEquals("episode:3",current?.episodeKey)
        assertEquals(89000L,current?.positionMs)
        assertEquals("1080",current?.quality)
    }

    @Test fun aPendingSameEpisodeSeekOrQualityFailureKeepsTheRequestedPosition() {
        val descriptor=Playback(vodId=film.id,line=old.code,versionKey=old.versionKey,episodeKey="old:3")
        val requested=request.copy(positionMs=12000,quality="720")
        val pending=PlaybackRecoveryRules.currentIdentity(requested,descriptor,89000,"720",pending=true)
        assertEquals(12000L,pending?.positionMs)
        assertEquals("720",pending?.quality)
        assertEquals(89000L,PlaybackRecoveryRules.currentIdentity(requested,descriptor,89000,"720",pending=false)?.positionMs)
    }

    @Test fun anInitiallyEmptySearchContinuesUsingTheNormalizedEpisodeAndLatestProgress() {
        val first=Source("found",versionKey="season:1",episodes=listOf(Episode("found:1",1)))
        val discovered=PlaybackRecoveryRules.discoveredIdentity(PlaybackIdentity(film.id),emptyList(),detail(first),emptySet(),false)!!
        val descriptor=Playback(vodId=film.id,line=discovered.line,versionKey=discovered.versionKey,episodeKey=discovered.episodeKey)
        val current=PlaybackRecoveryRules.currentIdentity(discovered,descriptor,17000,"",pending=false)!!
        assertEquals(PlaybackRecoveryRules.attemptKey(discovered,listOf(first)),PlaybackRecoveryRules.attemptKey(current,listOf(first)))
        val later=Source("later",versionKey="season:1",episodes=listOf(Episode("later:1",1)))
        val recovered=PlaybackRecoveryRules.discoveredIdentity(current,listOf(first),detail(first,later),setOf(first.code),false)
        assertEquals("later:1",recovered?.episodeKey)
        assertEquals(17000L,recovered?.positionMs)
    }
}
