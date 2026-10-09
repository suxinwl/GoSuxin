package com.xiaoqi.video.core.data

import com.xiaoqi.video.core.model.*
import kotlinx.coroutines.*
import org.junit.Assert.*
import org.junit.Test
import java.io.IOException

class LiveEpgLoaderTest {
    @Test fun emptyProgrammeDayCompletesWithoutAListLayout()=runBlocking {
        val scope=CoroutineScope(SupervisorJob()+Dispatchers.Unconfined)
        val loader=LiveEpgLoader(scope) { id,date->LiveEpg(id,date) }
        try { withTimeout(1000) { loader.load(2,"2026-10-03").join() };assertFalse(loader.state.value.loading);assertTrue(loader.state.value.result!!.items.isEmpty()) }
        finally { loader.close();scope.cancel() }
    }
    @Test fun aLateOldChannelOrDayCannotReplaceTheLatestSchedule()=runBlocking {
        val scope=CoroutineScope(SupervisorJob()+Dispatchers.Unconfined);val late=CompletableDeferred<LiveEpg>()
        val loader=LiveEpgLoader(scope) { id,date->if(id==1L)withContext(NonCancellable) { late.await() } else LiveEpg(id,date) }
        try {
            val old=loader.load(1,"2026-10-02");loader.load(2,"2026-10-03").join()
            late.complete(LiveEpg(1,"2026-10-02"));withTimeout(1000) { old.join() }
            assertEquals(2L,loader.state.value.result!!.channelId);assertEquals("2026-10-03",loader.state.value.date);assertFalse(loader.state.value.loading)
        } finally { loader.close();scope.cancel() }
    }
    @Test fun failureRetryAndClosingCancelWithoutDependingOnProgrammeLayout()=runBlocking {
        val scope=CoroutineScope(SupervisorJob()+Dispatchers.Unconfined);var calls=0
        val loader=LiveEpgLoader(scope) { id,date->if(++calls==1)throw IOException("offline") else if(calls==2)LiveEpg(id,date) else awaitCancellation() }
        try {
            loader.load(1,"2026-10-03").join();assertFalse(loader.state.value.loading);assertTrue(loader.state.value.error.isNotBlank())
            loader.load(1,"2026-10-03").join();assertEquals("",loader.state.value.error)
            val pending=loader.load(1,"2026-10-04");loader.close();withTimeout(1000) { pending.join() };assertTrue(pending.isCancelled)
        } finally { loader.close();scope.cancel() }
    }
}
