package com.xiaoqi.video.core.data

import com.xiaoqi.video.core.model.*
import kotlinx.coroutines.*
import org.junit.Assert.*
import org.junit.Test
import java.io.IOException

class LiveChannelsLoaderTest {
    @Test fun firstSuccessfulResponseCompletesWithoutAnyMountedGridOrLayoutSignal()=runBlocking {
        val scope=CoroutineScope(SupervisorJob()+Dispatchers.Unconfined)
        val response=CompletableDeferred<LiveChannelPage>()
        val loader=LiveChannelsLoader(scope) { _,_,_->response.await() }
        try {
            val job=loader.load(2,"新闻",1)
            assertTrue(loader.state.value.loading)
            response.complete(page(1))
            withTimeout(1000) { job.join() }
            assertFalse(loader.state.value.loading)
            assertEquals(1L,loader.state.value.result!!.items.single().id)
        } finally { loader.close();scope.cancel() }
    }

    @Test fun emptyCatalogueCompletesWhileNoGridCanBeComposed()=runBlocking {
        val scope=CoroutineScope(SupervisorJob()+Dispatchers.Unconfined)
        val loader=LiveChannelsLoader(scope) { _,_,_->LiveChannelPage() }
        try {
            withTimeout(1000) { loader.load(9,"",1).join() }
            assertFalse(loader.state.value.loading)
            assertNotNull(loader.state.value.result)
            assertTrue(loader.state.value.result!!.items.isEmpty())
            assertEquals("",loader.state.value.error)
        } finally { loader.close();scope.cancel() }
    }

    @Test fun failedRequestLeavesLoadingAndSuccessfulRetryDoesNotWaitForLayout()=runBlocking {
        val scope=CoroutineScope(SupervisorJob()+Dispatchers.Unconfined)
        var calls=0
        val loader=LiveChannelsLoader(scope) { _,_,_->if(++calls==1)throw IOException("fixture failure") else page(2) }
        try {
            loader.load(0,"",1).join()
            assertFalse(loader.state.value.loading)
            assertTrue(loader.state.value.error.isNotBlank())
            withTimeout(1000) { loader.load(0,"",1).join() }
            assertFalse(loader.state.value.loading)
            assertEquals("",loader.state.value.error)
            assertEquals(2L,loader.state.value.result!!.items.single().id)
        } finally { loader.close();scope.cancel() }
    }

    @Test fun cancelledOldGroupCannotOverwriteTheNewPageOrItsLoadingState()=runBlocking {
        val scope=CoroutineScope(SupervisorJob()+Dispatchers.Unconfined)
        val late=CompletableDeferred<LiveChannelPage>()
        val loader=LiveChannelsLoader(scope) { group,_,_->if(group==1L)withContext(NonCancellable) { late.await() } else page(2) }
        try {
            val old=loader.load(1,"旧分组",1)
            loader.load(2,"新分组",3).join()
            late.complete(page(1))
            withTimeout(1000) { old.join() }
            assertEquals(2L,loader.state.value.groupId)
            assertEquals("新分组",loader.state.value.query)
            assertEquals(3,loader.state.value.page)
            assertEquals(2L,loader.state.value.result!!.items.single().id)
            assertFalse(loader.state.value.loading)
        } finally { loader.close();scope.cancel() }
    }

    @Test fun leavingTheScreenCancelsThePendingNetworkRequest()=runBlocking {
        val scope=CoroutineScope(SupervisorJob()+Dispatchers.Unconfined)
        val cancelled=CompletableDeferred<Unit>()
        val loader=LiveChannelsLoader(scope) { _,_,_->try { awaitCancellation() } finally { cancelled.complete(Unit) } }
        try {
            val job=loader.load(0,"",1)
            loader.close()
            withTimeout(1000) { cancelled.await();job.join() }
            assertTrue(job.isCancelled)
            assertFalse(loader.state.value.loading)
        } finally { loader.close();scope.cancel() }
    }

    private fun page(id:Long)=LiveChannelPage(items=listOf(LiveChannel(id,"频道 $id")),total=1,size=60)
}
