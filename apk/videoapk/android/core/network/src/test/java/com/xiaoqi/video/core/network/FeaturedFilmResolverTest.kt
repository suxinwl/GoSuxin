package com.xiaoqi.video.core.network

import com.google.gson.JsonParser
import com.xiaoqi.video.core.model.Film
import kotlinx.coroutines.runBlocking
import okhttp3.Call
import okhttp3.OkHttpClient
import java.net.ServerSocket
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.*
import org.junit.Test

class FeaturedFilmResolverTest {
    private val remote=Film(name="远端精选",provider="yqk",remoteId=2164,resolveToken="signed-catalogue-fixture")

    @Test fun aFullFeaturedPageRetainsRemoteCardsAndSeparatesProviderIdsFromLocalIds() {
        val items=(1..30).joinToString(",") { i->if(i<=7)"""{"id":$i,"name":"本地$i"}"""
            else """{"id":0,"provider":"yqk","remote_id":${i-7},"resolve_token":"signed-$i","name":"精选$i"}""" }
        val page=JsonWire.filmPage(JsonParser.parseString("""{"items":[$items],"page":1,"size":30,"pages":2,"exact_pages":false}"""))
        assertEquals(30,page.items.size);assertEquals(7,page.items.count { it.id>0 });assertEquals(23,page.items.count { it.id==0L })
        assertEquals(30,page.items.map { it.cardKey }.toSet().size)
        assertEquals("local:1",page.items.first().cardKey);assertEquals("yqk:1",page.items[7].cardKey)
        assertEquals(0L,page.items[7].id);assertFalse(page.exactPages)
    }

    @Test fun unsignedOrUnsupportedRemoteCardsCannotBecomePlaybackIds() {
        assertFalse(remote.copy(resolveToken="").hasPlayableIdentity)
        assertFalse(remote.copy(provider="other").hasPlayableIdentity)
        assertFalse(remote.copy(id=-1).hasPlayableIdentity)
        assertFalse(remote.copy(remoteId=0).hasPlayableIdentity)
        val cards=JsonWire.films(JsonParser.parseString("""[{"id":0,"name":"missing"},{"id":0,"provider":"yqk","remote_id":5,"name":"unsigned"},{"id":0,"provider":"yqk","remote_id":5,"resolve_token":"fixture","name":"allowed"},{"id":0,"provider":"yqk","remote_id":5,"resolve_token":"new-token","name":"duplicate"}]"""))
        assertEquals(1,cards.size);assertEquals("yqk:5",cards.single().cardKey)
    }

    @Test fun resolvingUsesTheSignedAnonymousPostAndOnlyTheReturnedCmsIdForPlayback()=runBlocking {
        serve("""{"code":0,"data":{"vod_id":781}}""") { api,capture->
            api.token="must-not-leave-fixture"
            assertEquals(781L,api.resolveFeaturedFilm(remote))
            assertEquals("POST /suxinvideo/app/v1/catalog/featured/resolve HTTP/1.1",capture.get().first)
            assertEquals("",capture.get().second["authorization"].orEmpty())
            val body=JsonParser.parseString(capture.get().third).asJsonObject
            assertEquals(2164L,body.get("remote_id").asLong)
            assertEquals(remote.resolveToken,body.get("resolve_token").asString)
            assertFalse(body.has("vod_id"));assertFalse(body.has("id"))
        }
    }

    @Test fun localCardsDoNotImportAndInvalidResolveResultsNeverFallBackToProviderIds()=runBlocking {
        val offline=ApiClient({ "https://fixture.invalid" },Call.Factory { error("A local card must not issue a request") })
        assertEquals(7L,offline.resolveFeaturedFilm(remote.copy(id=7)))
        serve("""{"code":0,"data":{"remote_id":2164,"vod_id":0}}""") { api,_->
            try { api.resolveFeaturedFilm(remote);fail("A provider ID must not be used as a CMS film ID") } catch(_:IllegalArgumentException) {}
        }
    }

    private suspend fun serve(body:String,block:suspend (ApiClient,AtomicReference<Triple<String,Map<String,String>,String>>)->Unit) {
        val server=ServerSocket(0).apply { soTimeout=5000 }
        val capture=AtomicReference<Triple<String,Map<String,String>,String>>()
        val worker=Thread {
            try { server.accept().use { socket->
                socket.soTimeout=5000
                val input=socket.getInputStream().bufferedReader(Charsets.UTF_8)
                val first=input.readLine();val headers=mutableMapOf<String,String>()
                while(true) { val line=input.readLine()?:break;if(line.isEmpty())break;headers[line.substringBefore(':').lowercase()]=line.substringAfter(':').trim() }
                val data=CharArray(headers["content-length"]?.toIntOrNull()?:0)
                var read=0;while(read<data.size) { val n=input.read(data,read,data.size-read);if(n<0)break;read+=n }
                capture.set(Triple(first,headers,String(data,0,read)))
                val bytes=body.toByteArray(Charsets.UTF_8)
                socket.getOutputStream().apply { write("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: ${bytes.size}\r\nConnection: close\r\n\r\n".toByteArray());write(bytes);flush() }
            } } catch(_:Exception) { /* Client errors and captured-request assertions fail the test. */ }
        }.apply { isDaemon=true;start() }
        try { block(ApiClient({ "http://127.0.0.1:${server.localPort}" },OkHttpClient()),capture) }
        finally { server.close();worker.join(1000) }
    }
}
