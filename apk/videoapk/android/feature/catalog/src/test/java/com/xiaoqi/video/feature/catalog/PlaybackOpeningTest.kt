package com.xiaoqi.video.feature.catalog

import com.google.gson.JsonObject
import com.google.gson.JsonParser
import com.xiaoqi.video.core.model.*
import kotlinx.coroutines.*
import org.junit.Assert.*
import org.junit.Test
import java.io.IOException

class PlaybackOpeningTest {
    private val metadata=Detail(Film(42,"兰香如故"),listOf(Source("yqk_1","yqk","tv-cut","小柒APP",listOf(Episode("first",1,"第1集"),Episode("second",2,"第2集")))),"yqk_1",emptyList(),emptyList())

    @Test fun guestOpensWithoutAccountStorageOrMemberHistory()=runBlocking {
        val phases=mutableListOf<String>()
        val result=preparePlaybackOpening(42,hasAccount={ false },detail={ metadata },
            progress={ error("A guest must not open the member database") },phase=phases::add)
        assertEquals(42L,result.identity.vodId);assertEquals("first",result.identity.episodeKey)
        assertEquals(listOf("正在加载影片信息"),phases);assertFalse(result.identity.manual)
    }

    @Test fun slowResumeStorageCannotPreventMemberPlayback()=runBlocking {
        val cancelled=CompletableDeferred<Unit>()
        val result=preparePlaybackOpening(42,hasAccount={ true },detail={ metadata },progress={
            try { awaitCancellation() } finally { cancelled.complete(Unit) }
        },phase={},historyTimeoutMs=30)
        assertTrue(cancelled.isCompleted);assertEquals("first",result.identity.episodeKey);assertEquals(0L,result.identity.positionMs)
    }

    @Test fun damagedResumeStorageDoesNotHidePublicMetadata()=runBlocking {
        val result=preparePlaybackOpening(42,hasAccount={ true },detail={ metadata },progress={ throw IOException("storage unavailable") },phase={})
        assertEquals(metadata,result.detail);assertFalse(result.identity.manual)
    }

    @Test fun validMemberResumeKeepsChosenEpisodeAndPosition()=runBlocking {
        val history=JsonParser.parseString("""{"line":"yqk_1","version_key":"tv-cut","episode_key":"second","position_ms":9500,"quality":"720"}""").asJsonObject
        val result=preparePlaybackOpening(42,hasAccount={ true },detail={ metadata },progress={ history },phase={})
        assertTrue(result.identity.manual);assertEquals("second",result.identity.episodeKey);assertEquals(9500L,result.identity.positionMs);assertEquals("720",result.identity.quality)
    }

    @Test fun explicitPlaybackIntentDoesNotReadResumeStorage()=runBlocking {
        val requested=PlaybackIdentity(42,"yqk_1","second",1,positionMs=16000,manual=true,versionKey="tv-cut")
        val result=preparePlaybackOpening(42,requested,hasAccount={ true },detail={ metadata },progress={ error("Explicit intent is authoritative") },phase={})
        assertEquals(requested,result.identity)
    }

    @Test fun stalledMetadataEndsAtTheOpeningDeadline()=runBlocking {
        val cancelled=CompletableDeferred<Unit>()
        try {
            preparePlaybackOpening(42,hasAccount={ false },detail={ try { awaitCancellation() } finally { cancelled.complete(Unit) } },progress={ JsonObject() },phase={},timeoutMs=30)
            fail("A metadata request must not leave an infinite spinner")
        } catch(_:TimeoutCancellationException) { assertTrue(cancelled.isCompleted) }
    }

    @Test fun leavingThePageCancelsMetadataAndCannotStartPlayback()=runBlocking {
        val started=CompletableDeferred<Unit>();val cancelled=CompletableDeferred<Unit>();var opened=false
        val task=launch {
            preparePlaybackOpening(42,hasAccount={ false },detail={ started.complete(Unit);try { awaitCancellation() } finally { cancelled.complete(Unit) } },progress={ null },phase={})
            opened=true
        }
        started.await();task.cancelAndJoin();assertTrue(cancelled.isCompleted);assertFalse(opened)
    }

    @Test fun warmMetadataStartsWithoutAnotherRequestOrMetadataLoadingPhase()=runBlocking {
        val phases=mutableListOf<String>()
        val result=preparePlaybackOpening(42,hasAccount={ false },detail={ error("Warm metadata must not be downloaded again") },
            progress={ error("Guest storage must not be consulted") },phase=phases::add,cachedDetail={ metadata })
        assertEquals(metadata,result.detail);assertTrue(phases.isEmpty());assertEquals("first",result.identity.episodeKey)
    }

    @Test fun remotePosterResolvesOnceInsideTheSameOpening()=runBlocking {
        val remote=Film(name="兰香如故",provider="yqk",remoteId=120,resolveToken="catalogue-token")
        var imports=0;var reads=0;val phases=mutableListOf<String>()
        val result=prepareFilmPlaybackOpening(remote,hasAccount={ false },resolveFilm={
            assertEquals(remote,it);imports++;42L
        },detail={ assertEquals(42L,it);reads++;metadata },progress={ null },phase=phases::add)
        assertEquals(1,imports);assertEquals(1,reads);assertEquals(42L,result.identity.vodId)
        assertEquals(listOf("正在连接片源","正在加载影片信息"),phases)
    }

    @Test fun localPosterDoesNotImportRemoteIdentity()=runBlocking {
        val result=prepareFilmPlaybackOpening(metadata.film,hasAccount={ false },resolveFilm={ error("Local film already has its identity") },
            detail={ metadata },progress={ null },phase={})
        assertEquals(42L,result.identity.vodId)
    }

    @Test fun remoteIdentityAndMetadataShareOneDeadline()=runBlocking {
        var detailStarted=false
        val remote=Film(provider="yqk",remoteId=120,resolveToken="catalogue-token")
        try {
            prepareFilmPlaybackOpening(remote,hasAccount={ false },resolveFilm={ awaitCancellation() },
                detail={ detailStarted=true;metadata },progress={ null },phase={},timeoutMs=30)
            fail("Remote import must obey the player opening deadline")
        } catch(_:TimeoutCancellationException) { assertFalse(detailStarted) }
    }

    @Test fun leavingRemotePosterCannotOpenItsLateResolvedFilm()=runBlocking {
        val remote=Film(provider="yqk",remoteId=120,resolveToken="catalogue-token")
        val started=CompletableDeferred<Unit>();var detailStarted=false;var opened=false
        val task=launch {
            prepareFilmPlaybackOpening(remote,hasAccount={ false },resolveFilm={ started.complete(Unit);awaitCancellation() },
                detail={ detailStarted=true;metadata },progress={ null },phase={})
            opened=true
        }
        started.await();task.cancelAndJoin();assertFalse(detailStarted);assertFalse(opened)
    }
}
