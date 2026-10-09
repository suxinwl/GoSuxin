package com.xiaoqi.video.core.cast

import com.xiaoqi.video.core.network.SiteHttp
import okhttp3.Request
import java.io.*
import java.net.*
import java.security.MessageDigest
import java.util.UUID
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Executors

/** Capability-scoped relay, restricted to the selected renderer address and explicitly registered resources. */
class LanMediaRelay(private val renderer:InetAddress):Closeable {
    private val capability=UUID.randomUUID().toString().replace("-","")
    private val routes=ConcurrentHashMap<String,String>()
    private val workers=Executors.newFixedThreadPool(4) { Thread(it,"xiaoqi-cast-relay").apply { isDaemon=true } }
    private var listener:ServerSocket?=null
    private var origin=""
    var url:String="";private set
    private fun register(upstream:String):String {
        val key=MessageDigest.getInstance("SHA-256").digest(upstream.toByteArray()).take(16).joinToString("") { "%02x".format(it) }
        require(routes.size<100000) { "播放清单过大" };routes[key]=upstream
        return "$origin/$capability/$key"
    }
    fun start(upstream:String) {
        val routed=runCatching { DatagramSocket().use { it.connect(renderer,1900);it.localAddress as? Inet4Address } }.getOrNull()?.takeIf { it.isSiteLocalAddress&&!it.isLoopbackAddress }
        val address=routed?:NetworkInterface.getNetworkInterfaces().toList().filter { it.isUp && !it.isLoopback }.flatMap { it.inetAddresses.toList() }.filterIsInstance<Inet4Address>().firstOrNull { it.isSiteLocalAddress && !it.isLoopbackAddress }?:throw IOException("请连接与电视相同的 Wi-Fi")
        val server=ServerSocket(0,12,address);listener=server;origin="http://${address.hostAddress}:${server.localPort}";url=register(SiteHttp.absolute(upstream))
        Thread({ while(!server.isClosed)try { val socket=server.accept();workers.execute { serve(socket) } } catch(_:IOException) {} },"xiaoqi-relay-listener").apply { isDaemon=true;start() }
    }
    private fun serve(socket:Socket) { socket.use {
        try {
            socket.soTimeout=10000
            if(socket.inetAddress!=renderer)return
            val input=socket.getInputStream().buffered();val line=readLine(input);val parts=line.split(' ');if(parts.size!=3 || parts[0] !in listOf("GET","HEAD"))return
            val headers=linkedMapOf<String,String>();for(index in 0 until 80) { val h=readLine(input);if(h.isBlank())break;val i=h.indexOf(':');if(i>0)headers[h.take(i).lowercase()]=h.drop(i+1).trim() }
            val path=parts[1].substringBefore('?').split('/');if(path.size!=3 || path[1]!=capability)return
            val upstream=routes[path[2]]?:return
            val request=Request.Builder().url(upstream).header("User-Agent","XiaoqiVideo/1.0 AndroidCast").apply {
                if(parts[0]=="HEAD")head()
                headers["range"]?.takeIf { it.matches(Regex("bytes=\\d*-\\d*")) }?.let { header("Range",it) }
            }.build()
            SiteHttp.callFactory.newCall(request).execute().use { response ->
                val body=response.body?:return
                val type=response.header("Content-Type").orEmpty()
                val isManifest=upstream.substringBefore('?').endsWith(".m3u8") || type.contains("mpegurl",true)
                val out=socket.getOutputStream()
                if(isManifest && response.isSuccessful && parts[0]!="HEAD") {
                    val manifest=body.string();require(manifest.length<8*1024*1024)
                    val base=URI(upstream)
                    val rewritten=manifest.lineSequence().joinToString("\n") { entry ->
                        when { entry.isBlank()->entry;entry.startsWith("#")->Regex("URI=\"([^\"]+)\"").replace(entry) { m -> "URI=\"${register(base.resolve(m.groupValues[1]).toString())}\"" };else->register(base.resolve(entry.trim()).toString()) }
                    }.toByteArray(Charsets.UTF_8)
                    writeHeaders(out,200,"application/vnd.apple.mpegurl",rewritten.size.toLong(),null);out.write(rewritten)
                } else {
                    writeHeaders(out,response.code,type.ifBlank { "video/mp4" },body.contentLength(),response.header("Content-Range"))
                    if(parts[0]!="HEAD")body.byteStream().use { it.copyTo(out,64*1024) }
                }
                out.flush()
            }
        } catch(_:Exception) { /* Closing the connection lets the renderer display a normal transport error. */ }
    } }
    private fun writeHeaders(out:OutputStream,code:Int,type:String,size:Long,range:String?) {
        val text="HTTP/1.1 $code ${if(code in 200..299)"OK" else "Upstream Error"}\r\nContent-Type: $type\r\nConnection: close\r\nAccept-Ranges: bytes\r\n"+(if(size>=0)"Content-Length: $size\r\n" else "")+(range?.let { "Content-Range: $it\r\n" }?:"")+"\r\n"
        out.write(text.toByteArray(Charsets.US_ASCII))
    }
    private fun readLine(input:InputStream):String { val out=ByteArrayOutputStream();while(true) { val b=input.read();if(b<0||b==10)break;if(b!=13)out.write(b);require(out.size()<8192) };return out.toString("UTF-8") }
    override fun close() { listener?.close();listener=null;workers.shutdownNow();routes.clear() }
}
