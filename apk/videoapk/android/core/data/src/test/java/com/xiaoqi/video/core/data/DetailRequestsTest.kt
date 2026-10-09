package com.xiaoqi.video.core.data

import kotlinx.coroutines.*
import org.junit.Assert.*
import org.junit.Test
import java.util.concurrent.atomic.AtomicInteger

class DetailRequestsTest {
    @Test fun focusWarmupAndClickShareOneHttpOperation()=runBlocking {
        val scope=CoroutineScope(SupervisorJob()+Dispatchers.Default)
        val calls=AtomicInteger();val started=CompletableDeferred<Unit>();val release=CompletableDeferred<Unit>()
        val cache=DetailRequests<String,String>(scope,load={ calls.incrementAndGet();started.complete(Unit);release.await();"fresh metadata" })
        try {
            val warmup=async(start=CoroutineStart.UNDISPATCHED) { cache.get("user7:film42") }
            started.await()
            val click=async(start=CoroutineStart.UNDISPATCHED) { cache.get("user7:film42") }
            release.complete(Unit)
            assertEquals("fresh metadata",warmup.await());assertEquals("fresh metadata",click.await());assertEquals(1,calls.get())
        } finally { scope.cancel() }
    }

    @Test fun cancelledWarmupDoesNotCancelAnAttachedForegroundReader()=runBlocking {
        val scope=CoroutineScope(SupervisorJob()+Dispatchers.Default)
        val calls=AtomicInteger();val started=CompletableDeferred<Unit>();val release=CompletableDeferred<Unit>()
        val cache=DetailRequests<String,String>(scope,load={ calls.incrementAndGet();started.complete(Unit);release.await();"film" })
        try {
            val warmup=async(start=CoroutineStart.UNDISPATCHED) { cache.get("same") };started.await()
            val foreground=async(start=CoroutineStart.UNDISPATCHED) { cache.get("same") }
            warmup.cancelAndJoin();release.complete(Unit)
            assertEquals("film",foreground.await());assertEquals(1,calls.get())
        } finally { scope.cancel() }
    }

    @Test fun leavingTheOnlyReaderCancelsTheUpstreamRequest()=runBlocking {
        val scope=CoroutineScope(SupervisorJob()+Dispatchers.Default)
        val started=CompletableDeferred<Unit>();val cancelled=CompletableDeferred<Unit>();val calls=AtomicInteger()
        val cache=DetailRequests<String,String>(scope,load={
            if(calls.incrementAndGet()==1) { started.complete(Unit);try { awaitCancellation() } finally { cancelled.complete(Unit) } }
            "reopened"
        })
        try {
            val page=async(start=CoroutineStart.UNDISPATCHED) { cache.get("film") };started.await();page.cancelAndJoin()
            withTimeout(2000) { cancelled.await() }
            assertEquals("reopened",cache.get("film"));assertEquals(2,calls.get())
        } finally { scope.cancel() }
    }

    @Test fun expiredOrExplicitFreshRequestsCannotUseOldSourceMetadata()=runBlocking {
        val scope=CoroutineScope(SupervisorJob()+Dispatchers.Default)
        var clock=100L;val calls=AtomicInteger()
        val cache=DetailRequests<String,Int>(scope,ttlMs=3000,now={ clock },load={ calls.incrementAndGet() })
        try {
            assertEquals(1,cache.get("film"));assertEquals(1,cache.get("film"))
            clock+=3001;assertEquals(2,cache.get("film"))
            assertEquals(3,cache.get("film",fresh=true))
        } finally { scope.cancel() }
    }

    @Test fun memberAndSessionKeysDoNotShareHistoryOrSourceMetadata()=runBlocking {
        val scope=CoroutineScope(SupervisorJob()+Dispatchers.Default);val calls=AtomicInteger()
        val cache=DetailRequests<String,Int>(scope,load={ calls.incrementAndGet() })
        try {
            assertEquals(1,cache.get("member7:tokenA:film42"))
            assertEquals(2,cache.get("member8:tokenB:film42"))
            assertEquals(3,cache.get("member7:tokenC:film42"))
            assertEquals(1,cache.get("member7:tokenA:film42"))
        } finally { scope.cancel() }
    }

    @Test fun boundedWarmupEvictsOldMetadataAndNeverCachesFailures()=runBlocking {
        val scope=CoroutineScope(SupervisorJob()+Dispatchers.Default);val calls=AtomicInteger()
        val cache=DetailRequests<String,Int>(scope,maxEntries=2,load={ key->val n=calls.incrementAndGet();if(key=="failed")throw IllegalStateException("upstream unavailable");n })
        try {
            cache.get("one");cache.get("two");cache.get("three")
            assertEquals(4,cache.get("one"))
            repeat(2) { try { cache.get("failed");fail("Failed detail must not enter warmup cache") } catch(_:IllegalStateException) {} }
            assertEquals(6,calls.get())
        } finally { scope.cancel() }
    }

    @Test fun siteChangeCancelsSharedOldMetadataAndRefetchesTheSameFilmId()=runBlocking {
        val scope=CoroutineScope(SupervisorJob()+Dispatchers.Unconfined)
        val started=CompletableDeferred<Unit>();var calls=0
        val cache=DetailRequests<Long,String>(scope,load={
            if(++calls==1) { started.complete(Unit);awaitCancellation() }
            "new-site-film"
        })
        try {
            val warmup=async(start=CoroutineStart.UNDISPATCHED) { cache.get(42) }
            val foreground=async(start=CoroutineStart.UNDISPATCHED) { cache.get(42) }
            started.await();cache.clear();warmup.join();foreground.join()
            assertTrue(warmup.isCancelled);assertTrue(foreground.isCancelled)
            assertEquals("new-site-film",cache.get(42));assertEquals(2,calls)
        } finally { scope.cancel() }
    }

    @Test fun peekingDoesNotStartOrAwaitAnUnfinishedDetailRequest()=runBlocking {
        val scope=CoroutineScope(SupervisorJob()+Dispatchers.Unconfined)
        val started=CompletableDeferred<Unit>();val release=CompletableDeferred<Unit>();var calls=0
        val cache=DetailRequests<String,String>(scope,load={ calls++;started.complete(Unit);release.await();"ready" })
        try {
            assertNull(cache.peek("film"));assertEquals(0,calls)
            val warmup=async(start=CoroutineStart.UNDISPATCHED) { cache.get("film") }
            started.await()
            assertNull(withTimeout(1000) { cache.peek("film") })
            assertEquals(1,calls)
            release.complete(Unit);warmup.await()
            assertEquals("ready",cache.peek("film"));assertEquals(1,calls)
        } finally { scope.cancel() }
    }

    @Test fun instantHandoffKeepsTheOriginalShortTtlAndFullIdentityKey()=runBlocking {
        val scope=CoroutineScope(SupervisorJob()+Dispatchers.Unconfined)
        var clock=100L;var calls=0
        val cache=DetailRequests<String,Int>(scope,ttlMs=3000,now={ clock },load={ ++calls })
        try {
            val guest="originA:generation0:guest:film42"
            cache.get(guest)
            assertEquals(1,cache.peek(guest))
            assertNull(cache.peek("originA:generation0:member7:film42"))
            assertNull(cache.peek("originB:generation1:guest:film42"))
            clock+=3000
            assertNull(cache.peek(guest))
            assertEquals(2,cache.get(guest))
        } finally { scope.cancel() }
    }

    @Test fun freshOrMutatedFilmDoesNotEvictAnUnrelatedWarmup()=runBlocking {
        val scope=CoroutineScope(SupervisorJob()+Dispatchers.Unconfined);var calls=0
        val cache=DetailRequests<String,Int>(scope,load={ ++calls })
        try {
            cache.get("guest:film42");cache.get("guest:film43");cache.get("member7:film42")
            assertEquals(4,cache.get("guest:film42",fresh=true))
            assertEquals(2,cache.peek("guest:film43"))
            assertEquals(3,cache.peek("member7:film42"))
            cache.invalidate { it.endsWith(":film42") }
            assertNull(cache.peek("guest:film42"));assertNull(cache.peek("member7:film42"))
            assertEquals(2,cache.get("guest:film43"))
            assertEquals(5,cache.get("guest:film42"))
        } finally { scope.cancel() }
    }

    @Test fun localMutationCancelsAnOlderUnfinishedMetadataSnapshot()=runBlocking {
        val scope=CoroutineScope(SupervisorJob()+Dispatchers.Unconfined)
        val started=CompletableDeferred<Unit>();val stopped=CompletableDeferred<Unit>();var calls=0
        val cache=DetailRequests<Long,String>(scope,load={
            if(++calls==1) { started.complete(Unit);try { awaitCancellation() } finally { stopped.complete(Unit) } }
            "updated"
        })
        try {
            val loading=async(start=CoroutineStart.UNDISPATCHED) { cache.get(42) }
            started.await();cache.invalidate { it==42L };loading.join()
            assertTrue(loading.isCancelled);withTimeout(1000) { stopped.await() }
            assertEquals("updated",cache.get(42));assertEquals(2,calls)
        } finally { scope.cancel() }
    }
}
