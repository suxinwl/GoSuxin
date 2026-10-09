package com.xiaoqi.video.core.player

import kotlinx.coroutines.*
import org.junit.Assert.*
import org.junit.Test

class PlaybackResolutionTest {
    @Test fun accountSwitchCancelsResolutionAndEndsItsLoadingState()=runBlocking {
        var loading=true;var error="";var applied=false;var callbacks=0
        try {
            resolvePlaybackRequest(load={ throw CancellationException("account changed") },onInterrupted={ loading=false;error=it;callbacks++ })
            applied=true
        } catch(_:CancellationException) { }
        assertFalse(loading);assertFalse(applied);assertTrue(error.contains("重试"));assertEquals(1,callbacks);assertTrue(currentCoroutineContext().isActive)
    }

    @Test fun stalledResolutionIsCancelledAndOffersRetry()=runBlocking {
        var loading=true;var error="";val upstreamCancelled=CompletableDeferred<Unit>()
        try {
            resolvePlaybackRequest(load={ try { awaitCancellation() } finally { upstreamCancelled.complete(Unit) } },
                onInterrupted={ loading=false;error=it },timeoutMs=30)
            fail("Resolution must have a deadline")
        } catch(_:TimeoutCancellationException) { }
        assertTrue(upstreamCancelled.isCompleted);assertFalse(loading);assertTrue(error.contains("超时"))
    }

    @Test fun navigationCancellationCannotChangeTheNextScreenOrApplyMedia()=runBlocking {
        val started=CompletableDeferred<Unit>();val upstreamCancelled=CompletableDeferred<Unit>();var error="";var applied=false
        val task=launch {
            resolvePlaybackRequest(load={ started.complete(Unit);try { awaitCancellation() } finally { upstreamCancelled.complete(Unit) } },onInterrupted={ error=it })
            applied=true
        }
        started.await();task.cancelAndJoin()
        assertTrue(upstreamCancelled.isCompleted);assertFalse(applied);assertEquals("",error)
    }

    @Test fun aLateTransportResultCannotStartPlaybackAfterNavigation()=runBlocking {
        val started=CompletableDeferred<Unit>();val release=CompletableDeferred<Unit>();var applied=false;var callbacks=0
        val task=launch {
            resolvePlaybackRequest(load={ withContext(NonCancellable) { started.complete(Unit);release.await();"late media" } },onInterrupted={ callbacks++ })
            applied=true
        }
        started.await();task.cancel();release.complete(Unit);task.join()
        assertFalse(applied);assertEquals(0,callbacks)
    }
}
