package com.xiaoqi.video.core.cast

import android.content.Context
import android.net.wifi.WifiManager
import com.xiaoqi.video.core.network.SiteHttp
import kotlinx.coroutines.*
import org.jupnp.model.types.UDN
import org.w3c.dom.Element
import java.net.*
import java.io.*
import javax.xml.parsers.DocumentBuilderFactory

data class DlnaDevice(val id:String,val name:String,val control:String,val protocolControl:String,val host:InetAddress)
data class DlnaStatus(val playing:Boolean=false,val position:Long=0,val duration:Long=0)

/** Android transport adapter: multicast + bounded socket HTTP, with jUPnP's UDN model. No Jetty runtime. */
class Dlna(private val context:Context) {
    private var lock:WifiManager.MulticastLock?=null
    private var relay:LanMediaRelay?=null
    private var selected:DlnaDevice?=null
    suspend fun discover():List<DlnaDevice> = withContext(Dispatchers.IO) {
        val wifi=context.applicationContext.getSystemService(Context.WIFI_SERVICE) as WifiManager
        lock=wifi.createMulticastLock("xiaoqi_dlna").apply { setReferenceCounted(false);acquire() }
        try {
            val addresses=linkedMapOf<String,String>()
            DatagramSocket().use { socket ->
                socket.soTimeout=900
                val query="M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 2\r\nST: urn:schemas-upnp-org:device:MediaRenderer:1\r\n\r\n".toByteArray()
                socket.send(DatagramPacket(query,query.size,InetAddress.getByName("239.255.255.250"),1900))
                val until=System.currentTimeMillis()+3500
                while(System.currentTimeMillis()<until)try {
                    val packet=DatagramPacket(ByteArray(8192),8192);socket.receive(packet)
                    if(!packet.address.isSiteLocalAddress)continue
                    val headers=String(packet.data,0,packet.length).lineSequence().mapNotNull { line -> line.indexOf(':').takeIf { it>0 }?.let { line.take(it).lowercase() to line.drop(it+1).trim() } }.toMap()
                    val location=headers["location"]?:continue
                    addresses[headers["usn"]?:location]=location
                } catch(_:SocketTimeoutException) {}
            }
            addresses.mapNotNull { (usn,location) -> runCatching { description(usn,location) }.getOrNull() }
        } finally { lock?.release();lock=null }
    }
    private fun xml(text:String):Element {
        require(text.length<2*1024*1024 && !Regex("<!\\s*(DOCTYPE|ENTITY)",RegexOption.IGNORE_CASE).containsMatchIn(text)) { "设备 XML 包含不允许的实体声明" }
        val factory=DocumentBuilderFactory.newInstance().apply {
            isNamespaceAware=true;isExpandEntityReferences=false
            // Android's DOM provider supports fewer parser features than desktop JAXP.
            // Reject declarations above, and additionally disable features wherever supported.
            runCatching { setFeature("http://apache.org/xml/features/disallow-doctype-decl",true) }
            runCatching { setFeature("http://xml.org/sax/features/external-general-entities",false) }
            runCatching { setFeature("http://xml.org/sax/features/external-parameter-entities",false) }
        }
        return factory.newDocumentBuilder().parse(ByteArrayInputStream(text.toByteArray())).documentElement
    }
    private fun Element.text(tag:String)=getElementsByTagName(tag).item(0)?.textContent.orEmpty()
    private fun description(usn:String,url:String):DlnaDevice {
        val uri=URI(url);val host=InetAddress.getByName(uri.host);require(host.isSiteLocalAddress)
        val root=xml(LanHttp.request(uri,"GET",emptyMap(),""))
        val base=root.text("URLBase").takeIf { it.isNotBlank() }?.let(::URI)?:uri
        var av="";var cm=""
        val nodes=root.getElementsByTagName("service")
        for(i in 0 until nodes.length) {
            val service=nodes.item(i) as Element
            if(service.text("serviceType").contains(":AVTransport:"))av=base.resolve(service.text("controlURL")).toString()
            if(service.text("serviceType").contains(":ConnectionManager:"))cm=base.resolve(service.text("controlURL")).toString()
        }
        require(av.isNotBlank())
        return DlnaDevice(runCatching { UDN.valueOf(usn.substringBefore("::")).toString() }.getOrDefault(usn),root.text("friendlyName").ifBlank { "电视 ${uri.host}" },av,cm,host)
    }
    suspend fun play(device:DlnaDevice,mediaUrl:String,title:String,type:String):String = withContext(Dispatchers.IO) {
        stop();selected=device
        val acceptsHls=if(device.protocolControl.isBlank())false else runCatching { soap(device.protocolControl,"ConnectionManager","GetProtocolInfo",emptyMap()).contains("mpegurl",true) }.getOrDefault(false)
        val hls=type in listOf("hls","m3u8","application/vnd.apple.mpegurl") || mediaUrl.substringBefore('?').endsWith(".m3u8")
        if(hls && !acceptsHls)return@withContext "needs_mp4"
        val server=LanMediaRelay(device.host).apply { start(mediaUrl) };relay=server
        val mime=if(hls)"application/vnd.apple.mpegurl" else "video/mp4"
        val didl="<DIDL-Lite xmlns=\"urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/\" xmlns:dc=\"http://purl.org/dc/elements/1.1/\" xmlns:upnp=\"urn:schemas-upnp-org:metadata-1-0/upnp/\"><item id=\"1\" parentID=\"0\" restricted=\"1\"><dc:title>${escape(title)}</dc:title><upnp:class>object.item.videoItem</upnp:class><res protocolInfo=\"http-get:*:$mime:*\">${escape(server.url)}</res></item></DIDL-Lite>"
        soap(device.control,"AVTransport","SetAVTransportURI",mapOf("InstanceID" to "0","CurrentURI" to server.url,"CurrentURIMetaData" to didl))
        soap(device.control,"AVTransport","Play",mapOf("InstanceID" to "0","Speed" to "1"));"playing"
    }
    suspend fun pause()=withContext(Dispatchers.IO) { selected?.let { soap(it.control,"AVTransport","Pause",mapOf("InstanceID" to "0")) } }
    suspend fun resume()=withContext(Dispatchers.IO) { selected?.let { soap(it.control,"AVTransport","Play",mapOf("InstanceID" to "0","Speed" to "1")) } }
    suspend fun seek(milliseconds:Long)=withContext(Dispatchers.IO) { selected?.let { val seconds=milliseconds/1000;soap(it.control,"AVTransport","Seek",mapOf("InstanceID" to "0","Unit" to "REL_TIME","Target" to "%02d:%02d:%02d".format(seconds/3600,(seconds/60)%60,seconds%60))) } }
    suspend fun state():DlnaStatus=withContext(Dispatchers.IO) {
        val device=selected?:return@withContext DlnaStatus()
        val transport=xml(soap(device.control,"AVTransport","GetTransportInfo",mapOf("InstanceID" to "0")))
        val position=xml(soap(device.control,"AVTransport","GetPositionInfo",mapOf("InstanceID" to "0")))
        fun time(text:String):Long { val p=text.split(':').map { it.toDoubleOrNull()?:0.0 };return if(p.size==3)((p[0]*3600+p[1]*60+p[2])*1000).toLong() else 0 }
        DlnaStatus(transport.text("CurrentTransportState")=="PLAYING",time(position.text("RelTime")),time(position.text("TrackDuration")))
    }
    fun stop() { val d=selected;selected=null;relay?.close();relay=null;if(d!=null)CoroutineScope(Dispatchers.IO).launch { runCatching { soap(d.control,"AVTransport","Stop",mapOf("InstanceID" to "0")) } } }
    private fun soap(url:String,service:String,action:String,args:Map<String,String>):String {
        val body="<?xml version=\"1.0\"?><s:Envelope xmlns:s=\"http://schemas.xmlsoap.org/soap/envelope/\" s:encodingStyle=\"http://schemas.xmlsoap.org/soap/encoding/\"><s:Body><u:$action xmlns:u=\"urn:schemas-upnp-org:service:$service:1\">${args.entries.joinToString("") { "<${it.key}>${escape(it.value)}</${it.key}>" }}</u:$action></s:Body></s:Envelope>"
        return LanHttp.request(URI(url),"POST",mapOf("Content-Type" to "text/xml; charset=utf-8","SOAPAction" to "\"urn:schemas-upnp-org:service:$service:1#$action\""),body)
    }
    private fun escape(s:String)=s.replace("&","&amp;").replace("<","&lt;").replace(">","&gt;").replace("\"","&quot;")
}

private object LanHttp {
    fun request(uri:URI,method:String,headers:Map<String,String>,body:String):String {
        val address=InetAddress.getByName(uri.host);require(address.isSiteLocalAddress && uri.scheme=="http") { "只支持当前局域网设备" }
        val bytes=body.toByteArray(Charsets.UTF_8)
        Socket().use { socket ->
            socket.connect(InetSocketAddress(address,if(uri.port>0)uri.port else 80),4000);socket.soTimeout=6000
            val path=(uri.rawPath.ifBlank { "/" })+(uri.rawQuery?.let { "?$it" }?:"")
            val request="$method $path HTTP/1.1\r\nHost: ${uri.host}\r\nConnection: close\r\nContent-Length: ${bytes.size}\r\n"+headers.entries.joinToString("") { "${it.key}: ${it.value}\r\n" }+"\r\n"
            socket.getOutputStream().apply { write(request.toByteArray());write(bytes);flush() }
            val input=socket.getInputStream().buffered();val status=readLine(input);val result=linkedMapOf<String,String>()
            while(true) { val line=readLine(input);if(line.isBlank())break;val index=line.indexOf(':');if(index>0)result[line.take(index).lowercase()]=line.drop(index+1).trim() }
            val output=ByteArrayOutputStream()
            if(result["transfer-encoding"]?.contains("chunked")==true) {
                while(true) { val size=readLine(input).substringBefore(';').trim().toInt(16);if(size==0)break;require(output.size()+size<2*1024*1024);var remaining=size;while(remaining>0) { val block=ByteArray(minOf(remaining,8192));val n=input.read(block);if(n<0)throw EOFException();output.write(block,0,n);remaining-=n };readLine(input) }
            } else { val block=ByteArray(8192);while(true) { val n=input.read(block);if(n<0)break;output.write(block,0,n);require(output.size()<2*1024*1024) } }
            val text=String(output.toByteArray(),Charsets.UTF_8)
            require(status.split(' ').getOrNull(1)?.toIntOrNull() in 200..299) { "设备拒绝操作：${status.take(60)}" }
            return text
        }
    }
    private fun readLine(input:InputStream):String { val out=ByteArrayOutputStream();while(true) { val b=input.read();if(b<0||b==10)break;if(b!=13)out.write(b);require(out.size()<8192) };return out.toString("UTF-8") }
}
