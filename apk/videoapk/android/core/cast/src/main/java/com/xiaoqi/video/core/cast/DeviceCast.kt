package com.xiaoqi.video.core.cast

import com.google.gson.JsonObject
import com.xiaoqi.video.core.data.AppRepository
import com.xiaoqi.video.core.model.PlaybackIdentity
import com.xiaoqi.video.core.model.Session
import com.xiaoqi.video.core.network.JsonWire
import com.xiaoqi.video.core.network.JsonWire.array
import com.xiaoqi.video.core.network.JsonWire.obj
import com.xiaoqi.video.core.network.JsonWire.string
import com.xiaoqi.video.core.network.SiteHttp
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import okhttp3.*
import java.util.UUID

data class TvDevice(val device_id:String="",val name:String="",val platform:String="tv",val last_seen:Long=0)
data class PairChallenge(val pair_id:String="",val code:String="",val expires_at:Long=0,val qr_url:String="",val poll_token:String="")
class DeviceCast(private val repo:AppRepository) {
    private val scope=CoroutineScope(SupervisorJob()+Dispatchers.Main.immediate)
    val status=MutableStateFlow("")
    val messages=MutableSharedFlow<JsonObject>(extraBufferCapacity=32)
    val remoteStatus=MutableStateFlow(JsonObject())
    var socket:WebSocket?=null;private set
    private var watchJob:Job?=null
    suspend fun devices():List<TvDevice> = repo.api.request("devices").asJsonObject.array("devices").map { JsonWire.decode<TvDevice>(it) }
    suspend fun challenge():PairChallenge = JsonWire.decode(repo.api.request("devices/pair/create","POST",mapOf("device_id" to repo.store.deviceId(),"name" to android.os.Build.MODEL,"platform" to "tv")))
    suspend fun poll(c:PairChallenge):String {
        val o=repo.api.request("devices/pair/status",query=mapOf("pair_id" to c.pair_id,"poll_token" to c.poll_token)).asJsonObject
        if(o.string("status")=="approved")repo.adoptSession(JsonWire.decode<Session>(o.obj("tokens")))
        return o.string("status")
    }
    suspend fun approve(code:String)=repo.api.request("devices/pair/approve","POST",mapOf("code" to code.trim()))
    suspend fun revoke(id:String)=repo.api.request("devices/pair/revoke","POST",mapOf("device_id" to id))
    suspend fun connectMobile(device:TvDevice) { val o=repo.api.request("devices/cast/create","POST",mapOf("device_id" to device.device_id)).asJsonObject;connect(o.string("ws_url")) }
    fun watchTv():Job { watchJob?.takeIf { it.isActive }?.let { return it };return scope.launch {
        while(isActive) {
            if(repo.session.value!=null && socket==null)runCatching {
                val o=repo.api.request("devices/cast/current").asJsonObject
                if(o.string("active")=="true")connect(o.string("ws_url"))
            }
            delay(3000)
        }
    }.also { watchJob=it } }
    private suspend fun connect(url:String) {
        socket?.close(1000,"切换投屏连接")
        val opened=CompletableDeferred<Unit>()
        val target=SiteHttp.absolute(url).replaceFirst("https:","wss:").replaceFirst("http:","ws:")
        val request=Request.Builder().url(target).header("Authorization","Bearer ${repo.api.token}").build()
        require(SiteHttp.isSiteHttpsUrl(request.url.toString())) { "投屏地址与当前服务不匹配" }
        socket=SiteHttp.client(request.url.toString()).newWebSocket(request,object:WebSocketListener() {
            override fun onOpen(webSocket:WebSocket,response:Response) { status.value="已连接";opened.complete(Unit) }
            override fun onMessage(webSocket:WebSocket,text:String) { runCatching { JsonWire.gson.fromJson(text,JsonObject::class.java) }.onSuccess { if(it.string("type")=="status")remoteStatus.value=it.obj("payload");messages.tryEmit(it) } }
            override fun onClosed(webSocket:WebSocket,code:Int,reason:String) { if(socket===webSocket)socket=null;status.value="投屏已断开" }
            override fun onFailure(webSocket:WebSocket,t:Throwable,response:Response?) { if(socket===webSocket)socket=null;status.value="连接中断，正在等待重连";opened.completeExceptionally(t) }
        })
        withTimeout(12000) { opened.await() }
    }
    fun command(action:String,identity:PlaybackIdentity?=null,position:Long?=null) {
        val payload=mutableMapOf<String,Any>()
        identity?.let { payload.putAll(repo.playBody(it)) };position?.let { payload["position_ms"]=it }
        require(socket?.send(JsonWire.gson.toJson(mapOf("type" to "command","id" to UUID.randomUUID().toString(),"action" to action,"payload" to payload)))==true) { "投屏连接已断开，请重新选择电视" }
    }
    fun publishStatus(payload:Map<String,Any>) { socket?.send(JsonWire.gson.toJson(mapOf("type" to "status","payload" to payload))) }
    fun close() { socket?.close(1000,"结束投屏");socket=null }
}
