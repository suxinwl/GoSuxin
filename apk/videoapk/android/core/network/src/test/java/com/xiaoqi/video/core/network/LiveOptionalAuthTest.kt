package com.xiaoqi.video.core.network

import kotlinx.coroutines.runBlocking
import org.junit.Assert.*
import org.junit.Test
import okhttp3.OkHttpClient
import okhttp3.HttpUrl.Companion.toHttpUrl
import java.net.ServerSocket
import java.util.Collections

class LiveOptionalAuthTest {
    private fun apiFor(url:String):ApiClient {
        val origin=url.toHttpUrl().let { "${it.scheme}://${it.host}:${it.port}" }
        return ApiClient({ origin },OkHttpClient())
    }
    @Test fun aMemberCatalogueRequestRefreshesExpiredIdentityAndKeepsBearerOnTheRetry()=runBlocking {
        serve(listOf(401,200)) { url,headers->
            val api=apiFor(url).apply { token="expired-fixture" };var refreshes=0
            api.refresh={ refreshes++;api.token="renewed-fixture";true }
            api.request(url)
            assertEquals(1,refreshes)
            assertEquals(listOf("Bearer expired-fixture","Bearer renewed-fixture"),headers)
        }
    }
    @Test fun aGuestCannotEnterTheMemberRefreshFlow()=runBlocking {
        serve(listOf(401)) { url,headers->
            val api=apiFor(url);var refreshes=0
            api.refresh={ refreshes++;true }
            try { api.request(url);fail("Guest denial must be propagated") } catch(e:ApiException) { assertEquals(401,e.status) }
            assertEquals(0,refreshes);assertEquals(listOf(""),headers)
        }
    }
    @Test fun memberPolicyDenialDoesNotRefreshOrDowngradeToAnAnonymousRequest()=runBlocking {
        serve(listOf(403)) { url,headers->
            val api=apiFor(url).apply { token="member-fixture" };var refreshes=0
            api.refresh={ refreshes++;true }
            try { api.request(url);fail("Policy denial must be propagated") } catch(e:ApiException) { assertEquals(403,e.status) }
            assertEquals(0,refreshes);assertEquals(listOf("Bearer member-fixture"),headers)
        }
    }
    private suspend fun serve(statuses:List<Int>,block:suspend (String,List<String>)->Unit) {
        val server=ServerSocket(0).apply { soTimeout=5000 }
        val authorizations=Collections.synchronizedList(mutableListOf<String>())
        val worker=Thread {
            try { statuses.forEach { status->server.accept().use { socket->
                socket.soTimeout=5000;val input=socket.getInputStream().bufferedReader();var auth=""
                while(true) { val line=input.readLine()?:break;if(line.isEmpty())break;if(line.startsWith("Authorization:",true))auth=line.substringAfter(':').trim() }
                authorizations+=auth
                val body=(if(status==200)"""{"code":0,"data":{"items":[]}}""" else """{"code":$status,"message":"fixture denial","data":{}}""").toByteArray()
                socket.getOutputStream().apply { write("HTTP/1.1 $status Fixture\r\nContent-Type: application/json\r\nContent-Length: ${body.size}\r\nConnection: close\r\n\r\n".toByteArray());write(body);flush() }
            } } } catch(_:Exception) { /* Missing requests or responses fail the assertions above. */ }
        }.apply { isDaemon=true;start() }
        try { block("http://127.0.0.1:${server.localPort}/live/epg",authorizations) }
        finally { server.close();worker.join(1000) }
    }
}
