package com.xiaoqi.video.core.data

import kotlinx.coroutines.*
import org.junit.Assert.*
import org.junit.Test

class PlaybackAccessTest {
    @Test fun theRepositoryGuestSessionGuardAllowsAnUnchangedAbsentAccount() {
        // This is the exact guard used before/after readDetail and after detail/progress reads.
        // An untyped Elvis fallback at its former call site boxed zero as Integer instead of Long.
        val guard=PlaybackAccess(0L)
        guard.requireCurrent(null)
        guard.requireCurrent(0L)
        try { guard.requireCurrent(7L);fail("Login must invalidate the previous guest request") }
        catch(_:CancellationException) {}
    }

    @Test fun guestMetadataReachesTheNetworkAndCompletesTheSharedForegroundRequest()=runBlocking {
        val scope=CoroutineScope(SupervisorJob()+Dispatchers.Unconfined)
        var calls=0
        val access=PlaybackAccess(0L)
        val requests=DetailRequests<Long,String>(scope,load={
            access.requireCurrent(null)
            calls++
            val response=access.request({ null }) { "anonymous-metadata" }
            access.requireCurrent(null)
            response
        })
        try {
            val foreground=async(start=CoroutineStart.UNDISPATCHED) { requests.get(2164).also { access.requireCurrent(null) } }
            assertEquals("anonymous-metadata",withTimeout(1000) { foreground.await() })
            assertFalse(foreground.isCancelled);assertEquals(1,calls)
        } finally { scope.cancel() }
    }

    @Test fun guestDetailAndResolveNeverBorrowAStaleMemberCredential()=runBlocking {
        val access=PlaybackAccess(0)
        val sent=mutableListOf<String>()
        for(path in listOf("films/2164","playback/resolve")) {
            val result=access.request({ null }) { override->
                sent+=override?:"stale-member-token"
                "anonymous:$path"
            }
            assertEquals("anonymous:$path",result)
        }
        assertEquals(listOf("",""),sent)
    }

    @Test fun guestPlaybackDoesNotOpenOrWaitForTheMemberResumeDatabase()=runBlocking {
        val result=withTimeout(1000) {
            PlaybackAccess(0).progress<String>({ null }) { error("Guest playback must not read member history") }
        }
        assertNull(result)
    }

    @Test fun aMemberRequestRetainsTheNormalTokenRefreshPath()=runBlocking {
        var token="expired-member-token"
        var refreshed=false
        val result=PlaybackAccess(7).request({ 7L }) { override->
            // ApiClient owns this null-override refresh flow; guest overrides must not enter it.
            val first=override?:token
            if(first=="expired-member-token"&&override==null) {
                refreshed=true;token="renewed-member-token"
            }
            override?:token
        }
        assertTrue(refreshed);assertEquals("renewed-member-token",result)
    }

    @Test fun accountChangesCannotPublishAResolvedUrlForThePreviousMember()=runBlocking {
        var owner:Long?=7L
        val response=CompletableDeferred<String>()
        val request=async(start=CoroutineStart.UNDISPATCHED) {
            PlaybackAccess(7).request({ owner }) { response.await() }
        }
        owner=null;response.complete("previous-member-media")
        try { request.await();fail("A previous account's media authorization must not escape") }
        catch(_:CancellationException) {}
        assertTrue(request.isCancelled)
    }

    @Test fun loggingInDuringGuestResolutionRequiresARequestForTheNewIdentity()=runBlocking {
        var owner:Long?=null
        val response=CompletableDeferred<String>()
        val request=async(start=CoroutineStart.UNDISPATCHED) {
            PlaybackAccess(0).request({ owner }) { response.await() }
        }
        owner=7L;response.complete("guest-descriptor")
        try { request.await();fail("A stale guest result must not enter the new account's playback") }
        catch(_:CancellationException) {}
        assertEquals("member-descriptor",PlaybackAccess(7).request({ owner }) { "member-descriptor" })
    }

    @Test fun aLateHistoryReadCannotApplyAnotherAccountsResumePosition()=runBlocking {
        var owner:Long?=7L
        val response=CompletableDeferred<Long>()
        val request=async(start=CoroutineStart.UNDISPATCHED) {
            PlaybackAccess(7).progress({ owner }) { account->assertEquals(7L,account);response.await() }
        }
        owner=8L;response.complete(120_000L)
        try { request.await();fail("A previous account's resume position must not escape") }
        catch(_:CancellationException) {}
    }
}
