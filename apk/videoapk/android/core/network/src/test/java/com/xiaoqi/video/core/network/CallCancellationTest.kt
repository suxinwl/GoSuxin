package com.xiaoqi.video.core.network

import kotlinx.coroutines.*
import okhttp3.*
import okhttp3.ResponseBody.Companion.toResponseBody
import okio.Timeout
import org.junit.Assert.*
import org.junit.Test

class CallCancellationTest {
    private class ControlledCall:Call {
        private val input=Request.Builder().url("https://xq.suxinwl.com:8600/suxinvideo/app/v1/films").build()
        var callback:Callback?=null
        var cancelled=false
        override fun request()=input
        override fun execute():Response=error("HTTP must not block a coroutine dispatcher")
        override fun enqueue(responseCallback:Callback) { callback=responseCallback }
        override fun cancel() { cancelled=true }
        override fun isExecuted()=callback!=null
        override fun isCanceled()=cancelled
        override fun timeout()=Timeout()
        override fun clone():Call=ControlledCall()
        fun respond() { callback!!.onResponse(this,Response.Builder().request(input).protocol(Protocol.HTTP_1_1).code(200).message("OK").body("{\"code\":0}".toResponseBody()).build()) }
    }

    @Test fun cancelledNavigationCancelsItsSocketCallImmediately()=runBlocking {
        val call=ControlledCall()
        val pending=async(start=CoroutineStart.UNDISPATCHED) { call.awaitResponse() }
        assertTrue(call.isExecuted());assertFalse(call.cancelled)
        pending.cancelAndJoin();assertTrue(call.cancelled)
        // A late network callback must not restore a cancelled screen or leak its response body.
        call.respond();assertTrue(pending.isCancelled)
    }

    @Test fun successfulCallbackReturnsCodeAndBodyWithoutBlocking()=runBlocking {
        val call=ControlledCall()
        val pending=async(start=CoroutineStart.UNDISPATCHED) { call.awaitResponse() }
        call.respond();assertEquals(200 to "{\"code\":0}",pending.await());assertFalse(call.cancelled)
    }
}
