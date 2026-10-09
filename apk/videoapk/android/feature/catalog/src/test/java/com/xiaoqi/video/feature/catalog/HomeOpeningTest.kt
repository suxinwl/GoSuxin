package com.xiaoqi.video.feature.catalog

import com.xiaoqi.video.core.model.HomeData
import com.xiaoqi.video.core.model.HomeSection
import com.xiaoqi.video.core.network.ApiException
import kotlinx.coroutines.*
import org.junit.Assert.*
import org.junit.Test
import java.io.IOException

class HomeOpeningTest {
    private val old=HomeData(emptyList(),listOf(HomeSection("cached",0,emptyList())))
    private val current=HomeData(emptyList(),listOf(HomeSection("fresh",0,emptyList())))

    @Test fun slowCacheValidationDoesNotDelayFreshHomeAndCannotReplaceItsResult()=runBlocking {
        val releaseCache=CompletableDeferred<Unit>();val freshStarted=CompletableDeferred<Unit>()
        val shown=mutableListOf<HomeData>()
        val task=launch { loadHomeOpening(cached={ releaseCache.await();old },fresh={ freshStarted.complete(Unit);current },
            onHome=shown::add,onError={ throw it },onRejectCached={ fail("Fresh home is valid") }) }
        withTimeout(1000) { freshStarted.await() };yield()
        assertEquals(listOf(current),shown)
        releaseCache.complete(Unit);task.join();assertEquals(listOf(current),shown)
    }

    @Test fun verifiedCacheCanShowWhileNetworkHomeIsStillPending()=runBlocking {
        val releaseFresh=CompletableDeferred<Unit>();val shown=mutableListOf<HomeData>()
        val task=launch { loadHomeOpening(cached={ old },fresh={ releaseFresh.await();current },
            onHome=shown::add,onError={ throw it },onRejectCached={}) }
        yield();yield();assertEquals(listOf(old),shown)
        releaseFresh.complete(Unit);task.join();assertEquals(listOf(old,current),shown)
    }

    @Test fun explicitServerRejectionPreventsLateCacheFallback()=runBlocking {
        val releaseCache=CompletableDeferred<Unit>();var displayed:HomeData?=null;var error:Throwable?=null
        val rejected=CompletableDeferred<Unit>()
        val task=launch { loadHomeOpening(cached={ releaseCache.await();old },fresh={ throw ApiException(451,451,"影片已屏蔽") },
            onHome={ displayed=it },onError={ error=it },onRejectCached={ displayed=null;rejected.complete(Unit) }) }
        rejected.await();releaseCache.complete(Unit);task.join()
        assertNull(displayed);assertTrue(error is ApiException)
    }

    @Test fun transportFailureMayRetainIndependentlyVerifiedCache()=runBlocking {
        val releaseCache=CompletableDeferred<Unit>();val errorSeen=CompletableDeferred<Unit>();var displayed:HomeData?=null
        val task=launch { loadHomeOpening(cached={ releaseCache.await();old },fresh={ throw IOException("network") },
            onHome={ displayed=it },onError={ errorSeen.complete(Unit) },onRejectCached={ fail("Transport failure does not revoke verified cache") }) }
        errorSeen.await();releaseCache.complete(Unit);task.join();assertEquals(old,displayed)
    }

    @Test fun leavingHomeCancelsBothRequestsAndPreventsLateDisplay()=runBlocking {
        val cacheStarted=CompletableDeferred<Unit>();val freshStarted=CompletableDeferred<Unit>();var shown=false
        val task=launch { loadHomeOpening(cached={ cacheStarted.complete(Unit);awaitCancellation() },fresh={ freshStarted.complete(Unit);awaitCancellation() },
            onHome={ shown=true },onError={ fail("Navigation cancellation stays silent") },onRejectCached={}) }
        cacheStarted.await();freshStarted.await();task.cancelAndJoin();assertFalse(shown)
    }
}
