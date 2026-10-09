package com.xiaoqi.video.core.data

import com.xiaoqi.video.core.network.ApiException
import kotlinx.coroutines.*
import org.junit.Assert.*
import org.junit.Test
import java.io.IOException

class CatalogPagesTest {
    @Test fun actualQueryBuilderUsesRequestCapacityRatherThanTheMapsEntryCount() {
        listOf(12,30,48,72).forEach { size->
            listOf(1,2).forEach { page->
                val request=CatalogRequest(channel=65,type=0,page=page,size=size).normalized()
                val query=request.queryParameters()
                assertEquals(size.toString(),query["size"])
                assertEquals(page.toString(),query["page"])
                assertEquals("65",query["channel_id"])
                assertEquals("0",query["type_id"])
                assertEquals("time",query["order"])
                assertNotEquals("3",query["size"])
            }
        }
        assertEquals("30",CatalogRequest().queryParameters()["size"])
        assertEquals("12",CatalogRequest(query="仙逆",size=12).queryParameters()["size"])
    }
    private fun runCase(test:suspend CoroutineScope.(CoroutineScope)->Unit)=runBlocking {
        val workers=CoroutineScope(SupervisorJob()+Dispatchers.Unconfined)
        try { withTimeout(5000) { test(workers) } } finally { workers.cancel() }
    }

    @Test fun completeFilterKeyKeepsEveryPageAndFilterSeparate()=runCase { workers->
        var loads=0
        val pages=CatalogPages<CatalogRequest,String>(workers,revision={ "r1" },load={ CatalogResult("page-${++loads}","r1") })
        val base=CatalogRequest(query="仙逆",channel=8,type=24,page=2,sort="hits",area="中国",year="2026")
        val keys=listOf(base,base.copy(query="仙 逆"),base.copy(channel=3),base.copy(type=25),base.copy(page=3),base.copy(sort="score"),base.copy(area="日本"),base.copy(year="2025"),base.copy(size=15))
        val values=keys.map { pages.get(it.normalized()) }
        assertEquals(9,loads);assertEquals(9,values.distinct().size)
        assertEquals(values.first(),pages.get(base));assertEquals(9,loads)
        assertEquals("hits",base.copy(sort="hot").normalized().sort)
        assertFalse(base.copy(channel=0).queryParameters().containsKey("channel_id"))
        assertEquals("8",base.queryParameters()["channel_id"])
    }

    @Test fun initialPageStartsWithoutWaitingForRevisionAndConcurrentReadersShareNetwork()=runCase { workers->
        val gate=CompletableDeferred<Unit>();var loads=0;var policies=0
        val pages=CatalogPages<Int,String>(workers,revision={ policies++;error("initial pages must not wait") },load={ loads++;gate.await();CatalogResult("one","r1") })
        val first=async(start=CoroutineStart.UNDISPATCHED) { pages.get(1) }
        val second=async(start=CoroutineStart.UNDISPATCHED) { pages.get(1) }
        assertEquals(1,loads);assertEquals(0,policies)
        gate.complete(Unit);assertEquals("one",first.await());assertEquals("one",second.await())
    }

    @Test fun nextPagePrefetchIsUsedOnlyAfterPolicyCheck()=runCase { workers->
        var loads=0;var policies=0
        val pages=CatalogPages<Int,String>(workers,revision={ policies++;"r1" },load={ CatalogResult("page-$it-${++loads}","r1") })
        assertTrue(pages.prefetch(2));assertEquals(1,loads)
        assertEquals("page-2-1",pages.get(2));assertEquals(1,loads);assertEquals(1,policies)
    }

    @Test fun blockRevisionChangeReplacesCachedCardsAndUnavailablePolicyNeverUsesThem()=runCase { workers->
        var revision="r1";var loads=0;var unavailable=false
        val pages=CatalogPages<Int,String>(workers,revision={ if(unavailable)throw IOException("offline");revision },load={ CatalogResult(if(++loads==1)"old-title" else "filtered-list",revision) })
        assertEquals("old-title",pages.get(1))
        revision="r2";assertEquals("filtered-list",pages.get(1));assertEquals(2,loads)
        unavailable=true;assertEquals("filtered-list",pages.get(1));assertEquals(3,loads)
    }

    @Test fun authorizationFailureOnRevisionDoesNotDisplayCacheOrFetchAroundPolicy()=runCase { workers->
        var deny=false;var loads=0
        val pages=CatalogPages<Int,String>(workers,revision={ if(deny)throw ApiException(403,403,"hidden");"r1" },load={ loads++;CatalogResult("cached","r1") })
        pages.get(1);deny=true
        try { pages.get(1);fail("cached cards must be rejected") }catch(e:ApiException) { assertEquals(403,e.status) }
        assertEquals(1,loads)
    }

    @Test fun prefetchFailureDoesNotPoisonCurrentPageAndUnknownRevisionsAreNotCached()=runCase { workers->
        var loads=0
        val pages=CatalogPages<Int,String>(workers,revision={ "r1" },load={ loads++;if(it==2)throw IOException("source failed");CatalogResult("current","r1") })
        pages.get(1);assertFalse(pages.prefetch(2));assertEquals("current",pages.get(1));assertEquals(2,loads)
        var legacyLoads=0
        val legacy=CatalogPages<Int,String>(workers,revision={ "" },load={ CatalogResult("legacy-${++legacyLoads}","") })
        assertEquals("legacy-1",legacy.get(1));assertEquals("legacy-2",legacy.get(1))
    }

    @Test fun cancelledReaderKeepsOtherReaderButLastCancellationStopsLoader()=runCase { workers->
        val gate=CompletableDeferred<Unit>();val stopped=CompletableDeferred<Unit>();var loads=0
        val pages=CatalogPages<Int,String>(workers,revision={ "r1" },load={ loads++;try { gate.await();CatalogResult("ok","r1") } finally { stopped.complete(Unit) } })
        val first=async(start=CoroutineStart.UNDISPATCHED) { pages.get(1) }
        val second=async(start=CoroutineStart.UNDISPATCHED) { pages.get(1) }
        first.cancelAndJoin();assertFalse(stopped.isCompleted);assertEquals(1,loads)
        second.cancelAndJoin();stopped.await();assertTrue(stopped.isCompleted)
    }

    @Test fun backgroundLimitDoesNotQueueAndPlaybackCancelsOnlyWarmupReader()=runCase { workers->
        val gate=CompletableDeferred<Unit>();var loads=0
        val pages=CatalogPages<Int,String>(workers,revision={ "r1" },load={ loads++;gate.await();CatalogResult("page-$it","r1") })
        val warm1=async(start=CoroutineStart.UNDISPATCHED) { pages.prefetch(1) }
        val warm2=async(start=CoroutineStart.UNDISPATCHED) { pages.prefetch(2) }
        assertFalse(pages.prefetch(3));assertEquals(2,loads)
        val foreground=async(start=CoroutineStart.UNDISPATCHED) { pages.get(1) }
        pages.pausePrefetch();warm1.join();warm2.join()
        assertTrue(warm1.isCancelled);assertTrue(warm2.isCancelled);assertFalse(foreground.isCancelled)
        assertFalse(pages.prefetch(4));gate.complete(Unit);assertEquals("page-1",foreground.await())
    }

    @Test fun expiryAndLruEvictionRefetchOnlyNeededPages()=runCase { workers->
        var at=100L;var loads=0
        val pages=CatalogPages<Int,Int>(workers,revision={ "r1" },ttlMs=10,maxEntries=2,now={ at },load={ CatalogResult(++loads,"r1") })
        assertEquals(1,pages.get(1));assertEquals(2,pages.get(2));assertEquals(1,pages.get(1));assertEquals(3,pages.get(3))
        assertEquals(4,pages.get(2));at+=11;assertEquals(5,pages.get(2))
    }

    @Test fun siteChangeCannotCacheAResponseWhoseLoaderFinishesAfterCancellation()=runCase { workers->
        val started=CompletableDeferred<Unit>();val release=CompletableDeferred<Unit>();var loads=0
        val pages=CatalogPages<Int,String>(workers,revision={ "same-revision" },load={
            if(++loads==1) {
                started.complete(Unit)
                withContext(NonCancellable) { release.await() }
                CatalogResult("old-site","same-revision")
            } else CatalogResult("new-site","same-revision")
        })
        val old=async(start=CoroutineStart.UNDISPATCHED) { pages.get(1) }
        started.await();pages.clear()
        assertEquals("new-site",pages.get(1))
        release.complete(Unit);old.join();assertTrue(old.isCancelled)
        assertEquals("new-site",pages.get(1));assertEquals(2,loads)
    }

    @Test fun oldPolicyCheckCannotReturnOldCardsOrEraseTheNewSitesCache()=runCase { workers->
        val started=CompletableDeferred<Unit>();val release=CompletableDeferred<Unit>();var loads=0;var policies=0
        val pages=CatalogPages<Int,String>(workers,revision={
            if(++policies==1) { started.complete(Unit);release.await() }
            "same-revision"
        },load={ CatalogResult(if(++loads==1)"old-site" else "new-site","same-revision") })
        assertEquals("old-site",pages.get(1))
        val old=async(start=CoroutineStart.UNDISPATCHED) { pages.get(1) }
        started.await();pages.clear()
        assertEquals("new-site",pages.get(1))
        release.complete(Unit);old.join();assertTrue(old.isCancelled)
        assertEquals("new-site",pages.get(1));assertEquals(2,loads)
    }
}
