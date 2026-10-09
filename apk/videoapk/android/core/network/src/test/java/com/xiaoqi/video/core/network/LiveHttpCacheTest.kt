package com.xiaoqi.video.core.network

import okhttp3.Cache
import okhttp3.OkHttpClient
import okhttp3.Request
import org.junit.Assert.*
import org.junit.Test
import java.net.ServerSocket
import java.nio.file.Files
import java.util.concurrent.atomic.AtomicInteger

class LiveHttpCacheTest {
    @Test fun movingPlaylistAtTheSameOpaqueUrlBypassesPreviouslyCachedWindow() {
        val directory=Files.createTempDirectory("xiaoqi-live-cache-test").toFile()
        val cache=Cache(directory,1024*1024)
        val server=ServerSocket(0).apply { soTimeout=5000 }
        val requests=AtomicInteger()
        val worker=Thread {
            try { repeat(2) {
                server.accept().use { socket->
                    socket.soTimeout=5000
                    val input=socket.getInputStream().bufferedReader()
                    while(true) { val line=input.readLine()?:break;if(line.isEmpty())break }
                    val sequence=requests.incrementAndGet()
                    val body="#EXTM3U\n#EXT-X-MEDIA-SEQUENCE:$sequence\n".toByteArray()
                    socket.getOutputStream().apply {
                        write(("HTTP/1.1 200 OK\r\nContent-Type: application/x-mpegURL\r\nCache-Control: public, max-age=3600\r\nContent-Length: ${body.size}\r\nConnection: close\r\n\r\n").toByteArray())
                        write(body);flush()
                    }
                }
            } } catch(_:Exception) { /* Main-thread assertions detect a missing fresh response. */ }
        }.apply { isDaemon=true;start() }
        try {
            val client=OkHttpClient.Builder().cache(cache).build()
            val request=Request.Builder().url("http://127.0.0.1:${server.localPort}/live/media?session=fixed&asset=manifest").build()
            fun read(client:OkHttpClient)=client.newCall(request).execute().use { it.body!!.string() }
            assertTrue(read(client).contains("SEQUENCE:1"))
            assertTrue(read(client).contains("SEQUENCE:1"))
            val live=SiteHttp.withoutLiveCache(client)
            assertNull(live.cache)
            assertTrue(read(live).contains("SEQUENCE:2"))
            assertEquals(2,requests.get())
        } finally { server.close();worker.join(1000);cache.close();directory.deleteRecursively() }
    }
}
