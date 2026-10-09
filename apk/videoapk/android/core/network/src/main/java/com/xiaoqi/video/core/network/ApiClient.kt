package com.xiaoqi.video.core.network

import android.content.Context
import android.os.Build
import com.google.gson.*
import com.xiaoqi.video.core.model.*
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException
import okhttp3.*
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import java.io.IOException
import java.security.cert.CertificateFactory
import java.security.cert.X509Certificate
import java.util.concurrent.TimeUnit
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicLong

class ApiException(val status: Int, val code: Int, override val message: String,val data:JsonElement?=null) : IOException(message)

object LiveNetworkErrors {
    fun message(error:Throwable):String = when(error) {
        is ApiException->error.message
        is java.net.SocketTimeoutException->"直播连接超时，可重试或选择备用线路"
        is java.net.UnknownHostException->"无法连接直播服务，请检查网络"
        is javax.net.ssl.SSLException->"直播安全连接失败，请确认 APP 与站点证书"
        is IOException->"直播网络连接中断，可重试或选择备用线路"
        else->error.localizedMessage?.takeIf { Regex("[\\u3400-\\u9FFF]").containsMatchIn(it) }?:"直播暂时不可用，请重试或切换频道"
    }
}

object SiteHttp {
    const val HOST = "xq.suxinwl.com"
    const val PORT = 8600
    const val BASE = "https://$HOST:$PORT"
    private val endpointState = MutableStateFlow(BASE)
    val endpoint: StateFlow<String> = endpointState.asStateFlow()
    val currentBase: String get() = endpointState.value
    val API: String get() = "$currentBase/suxinvideo/app/v1"
    private val endpointGeneration = AtomicLong(0)
    val generation: Long get() = endpointGeneration.get()
    private val activeCalls = ConcurrentHashMap.newKeySet<Call>()
    private var preferences: android.content.SharedPreferences? = null
    @Volatile private var secured: OkHttpClient? = null
    @Volatile private var pinnedRoot: X509Certificate? = null
    private val standard = OkHttpClient.Builder()
        .connectTimeout(12, TimeUnit.SECONDS).readTimeout(35, TimeUnit.SECONDS)
        .eventListenerFactory {
            object : EventListener() {
                override fun callStart(call: Call) { activeCalls.add(call) }
                override fun callEnd(call: Call) { activeCalls.remove(call) }
                override fun callFailed(call: Call, ioe: IOException) { activeCalls.remove(call) }
            }
        }
        .addInterceptor { chain ->
            val snapshot = chain.request().tag(ServerRequestContext::class.java)
            if (snapshot != null && snapshot.generation != generation) throw IOException("服务地址已切换")
            val response = chain.proceed(chain.request())
            if (snapshot != null && snapshot.generation != generation) {
                response.close()
                throw IOException("服务地址已切换")
            }
            response
        }
        .addNetworkInterceptor { chain ->
            if (chain.request().header("Authorization") != null && !isSiteHttpsUrl(chain.request().url.toString())) {
                throw IOException("禁止向其它服务发送账号凭据")
            }
            chain.proceed(chain.request())
        }
        .build()
    /** Select TLS for each new media/download request so a long-lived factory follows endpoint changes. */
    val callFactory: Call.Factory = object : Call.Factory {
        override fun newCall(request: Request): Call = client(request.url.toString())
            .newCall(request.newBuilder().tag(ServerRequestContext::class.java, requestContext()).build())
    }
    val liveCallFactory: Call.Factory = object : Call.Factory {
        override fun newCall(request: Request): Call = withoutLiveCache(client(request.url.toString()))
            .newCall(request.newBuilder().tag(ServerRequestContext::class.java, requestContext()).build())
    }
    @Synchronized
    fun initialize(context: Context) {
        secured = null
        pinnedRoot = null
        try {
            val id = context.resources.getIdentifier("site_ca", "raw", context.packageName)
            check(id != 0) { "缺少本站 CA 资源 site_ca" }
            val ca = context.resources.openRawResource(id).use {
                CertificateFactory.getInstance("X.509").generateCertificate(it) as X509Certificate
            }
            val publicId=context.resources.getIdentifier("isrg_root_x1","raw",context.packageName)
            check(publicId!=0) { "缺少公信根证书资源 isrg_root_x1" }
            val publicRoot=context.resources.openRawResource(publicId).use {
                requirePinnedPublicRoot(CertificateFactory.getInstance("X.509").generateCertificate(it) as X509Certificate)
            }
            pinnedRoot=publicRoot
            // Android 7+ must retain the platform's host-aware trust manager and domain configuration.
            secured = if (Build.VERSION.SDK_INT >= 24) standard else legacySiteClient(standard,ca,HOST,publicRoot)
            preferences = context.applicationContext.getSharedPreferences("site-endpoint", Context.MODE_PRIVATE)
            if (generation == 0L) {
                endpointState.value = preferences?.getString("verified_origin", null)?.let(::normalizeOrigin) ?: BASE
            }
        } catch (e: Exception) {
            throw IllegalStateException("本站 HTTPS 初始化失败，请检查证书资源", e)
        }
    }
    fun absolute(value: String): String = when {
        value.startsWith("https://") || value.startsWith("http://") -> value
        value.startsWith("//") -> "https:$value"
        else -> "$currentBase/${value.trimStart('/')}"
    }
    fun normalizeOrigin(value: String): String? = value.toHttpUrlOrNull()?.takeIf {
        it.isHttps && it.username.isEmpty() && it.password.isEmpty() && it.encodedPath == "/" && it.query == null && it.fragment == null
    }?.toString()?.removeSuffix("/")
    fun isSiteHttpsUrl(value: String): Boolean {
        val target = value.toHttpUrlOrNull() ?: return false
        val current = currentBase.toHttpUrl()
        return target.isHttps && target.host == current.host && target.port == current.port && target.username.isEmpty() && target.password.isEmpty()
    }
    @Synchronized
    fun selectEndpoint(origin: String): Boolean {
        val normalized = requireNotNull(normalizeOrigin(origin)) { "服务地址必须是无账号、路径或参数的 HTTPS 地址" }
        checkNotNull(secured) { "本站 HTTPS 尚未成功初始化" }
        if (normalized == currentBase) return false
        endpointGeneration.incrementAndGet()
        activeCalls.toList().forEach(Call::cancel)
        standard.dispatcher.cancelAll()
        standard.connectionPool.evictAll()
        endpointState.value = normalized
        preferences?.edit()?.putString("verified_origin", normalized)?.apply()
        return true
    }
    /** Bootstrap writes only a verified candidate; playback in this process keeps its current origin. */
    @Synchronized
    fun rememberVerifiedEndpoint(origin: String): Boolean {
        val normalized = requireNotNull(normalizeOrigin(origin)) { "服务地址无效" }
        checkNotNull(secured) { "本站 HTTPS 尚未成功初始化" }
        preferences?.edit()?.putString("verified_origin", normalized)
            ?.putString("last_verified_origin",normalized)?.putLong("last_verified_at",System.currentTimeMillis())?.apply()
        return normalized != currentBase
    }
    @Synchronized
    internal fun hasRecentEndpointVerification(origin:String):Boolean {
        val saved=preferences?:return false
        return EndpointVerification.isFresh(origin,saved.getString("last_verified_origin","").orEmpty(),
            saved.getLong("last_verified_at",0L),System.currentTimeMillis())
    }
    @Synchronized
    internal fun requestContext(): ServerRequestContext = ServerRequestContext(currentBase, generation)
    internal fun publicRootAnchor():X509Certificate=checkNotNull(pinnedRoot) { "公信根证书尚未初始化" }
    fun client(url: String = currentBase): OkHttpClient = if (url.toHttpUrl().host == HOST) {
        checkNotNull(secured) { "本站 HTTPS 尚未成功初始化" }
    } else standard
    /** Live manifests move at the same URL. Neither HTTP nor offline caches may freeze that window. */
    fun liveClient():OkHttpClient = withoutLiveCache(client())
    internal fun withoutLiveCache(client:OkHttpClient):OkHttpClient = client.newBuilder().cache(null).build()
}

internal data class ServerRequestContext(val origin: String, val generation: Long)

object JsonWire {
    val gson: Gson = GsonBuilder().setFieldNamingPolicy(FieldNamingPolicy.LOWER_CASE_WITH_UNDERSCORES).create()
    fun JsonObject.string(vararg names: String, fallback: String = ""): String = names.firstNotNullOfOrNull { key -> get(key)?.takeIf { it.isJsonPrimitive }?.asString } ?: fallback
    fun JsonObject.long(vararg names: String, fallback: Long = 0): Long = names.firstNotNullOfOrNull { name ->
        get(name)?.takeIf { it.isJsonPrimitive }?.let { value -> runCatching { value.asBigDecimal.longValueExact() }.getOrNull() }
    } ?: fallback
    fun JsonObject.array(vararg names: String): JsonArray = names.firstNotNullOfOrNull { name -> get(name)?.let { value ->
        when { value.isJsonArray -> value.asJsonArray;value.isJsonObject -> value.asJsonObject.let { nested -> listOf("items","list","films","videos","categories","channels").firstNotNullOfOrNull { nested.get(it)?.takeIf { e -> e.isJsonArray }?.asJsonArray } };else -> null }
    } } ?: JsonArray()
    fun JsonObject.obj(vararg names: String): JsonObject = names.firstNotNullOfOrNull { get(it)?.takeIf { v -> v.isJsonObject }?.asJsonObject } ?: JsonObject()
    fun film(value: JsonElement): Film {
        val o = value.takeIf { it.isJsonObject }?.asJsonObject?:JsonObject()
        return Film(o.long("id", "vod_id"), o.string("name", "vod_name").trim(), o.string("pic", "vod_pic", "poster"), o.string("banner", "backdrop"),
            o.string("score", "vod_score").toDoubleOrNull()?.takeIf { it.isFinite() } ?: 0.0, o.string("remarks", "vod_remarks"), o.string("year", "vod_year"), o.string("area", "vod_area"),
            o.long("type_id"), o.string("type_name", "type"), o.string("actor", "vod_actor"), o.string("director", "vod_director"), o.string("content", "vod_content"),
            o.string("is_short") in listOf("true", "1"), o.string("is_anime") in listOf("true", "1"), o.string("vip", "vod_vip") in listOf("true", "1"), o.long("points", "vod_points").toInt(), o.long("total_episodes").toInt(),
            o.string("provider").trim().lowercase(),o.long("remote_id"),o.string("resolve_token"))
    }
    fun films(value: JsonElement): List<Film> {
        val rows=when { value.isJsonArray -> value.asJsonArray;value.isJsonObject -> value.asJsonObject.array("items","list","films","videos");else -> JsonArray() }
        return rows.filter { it.isJsonObject }.map(::film).filter { it.hasPlayableIdentity && it.name.isNotBlank() }.distinctBy { it.cardKey }
    }
    /** Decode the full page, rather than a preview or one row, using the same path as the catalogue UI. */
    fun filmPage(value:JsonElement,fallbackPage:Int=1,fallbackSize:Int=30):FilmPage {
        val data=if(value.isJsonObject&&value.asJsonObject.has("data"))value.asJsonObject.get("data") else value
        if(data.isJsonArray) {
            val items=films(data)
            return FilmPage(items,items.size,fallbackPage,fallbackPage)
        }
        val o=data.takeIf { it.isJsonObject }?.asJsonObject?:JsonObject()
        val items=films(o)
        val total=o.long("total",fallback=items.size.toLong()).coerceAtLeast(0).toInt()
        val size=o.long("size",fallback=fallbackSize.toLong()).coerceAtLeast(1).toInt()
        val page=o.long("page",fallback=fallbackPage.toLong()).coerceAtLeast(1).toInt()
        val pages=o.long("pages",fallback=((total.toLong()+size-1)/size).coerceAtLeast(1)).coerceAtLeast(1).toInt()
        return FilmPage(items,total,page,pages,categories(o.array("topics")),o.string("notice"),o.string("exact_pages",fallback="true").lowercase() !in setOf("false","0","off"))
    }
    fun categories(value:JsonElement):List<Category> {
        val rows=when { value.isJsonArray -> value.asJsonArray;value.isJsonObject -> value.asJsonObject.array("items","categories","channels","navigation");else -> JsonArray() }
        return rows.filter { it.isJsonObject }.map { e -> val o=e.asJsonObject;Category(o.long("id","type_id"),o.string("name","type_name").trim(),o.long("parent_id","pid")) }.filter { it.id>0 && it.name.isNotBlank() }.distinctBy { it.id }
    }
    fun config(value:JsonElement):SiteConfig {
        val o=value.takeIf { it.isJsonObject }?.asJsonObject?:JsonObject()
        val methods=o.array("payment_methods").filter { it.isJsonObject }.map { val p=it.asJsonObject;PayMethod(p.string("code"),p.string("name")) }.filter { it.code.isNotBlank() }.distinctBy { it.code }
        fun enabled(name:String)=o.string(name,fallback="true").lowercase() !in setOf("false","0","off")
        return SiteConfig(o.string("name","site_name",fallback="小柒影视"),o.string("logo","site_logo"),o.string("description"),categories(o.array("categories","navigation")),methods,enabled("registration_enabled"),enabled("member_enabled"),enabled("comment_enabled"),AppearanceRules.theme(o.string("theme","template","site_template",fallback="suxinlite")),enabled("registration_requires_email_code"),enabled("password_reset_enabled"))
    }
    fun home(value:JsonElement):HomeData {
        val o=value.takeIf { it.isJsonObject }?.asJsonObject?:JsonObject()
        val banners=o.array("banners","slides").filter { it.isJsonObject }.map { e ->
            val b=e.asJsonObject
            Banner(b.string("title","name").trim(),b.string("image","pic","poster"),b.long("film_id","vod_id","id"),b.string("url","link"))
        }.filter { it.title.isNotBlank() && (it.filmId>0 || it.url.isNotBlank()) }.distinctBy { "${it.filmId}:${it.url}:${it.title}" }
        val sections=linkedMapOf<String,HomeSection>()
        fun add(key:String,name:String,id:Long,items:List<Film>) {
            if(items.isEmpty() || name.isBlank())return
            val merged=(sections[key]?.items.orEmpty()+items).distinctBy { it.cardKey }
            sections[key]=HomeSection(name,id,merged,key)
        }
        add("recent","最近更新",0,films(o.array("recent")))
        add("hot","热播推荐",0,films(o.array("hot")))
        o.array("sections","blocks").filter { it.isJsonObject }.forEach { e ->
            val s=e.asJsonObject;val name=s.string("name","title").trim();val id=s.long("channel_id","type_id","id")
            val key=when { name=="最近更新" -> "recent";name in listOf("热播推荐","热播") -> "hot";id>0 -> "channel:$id";else -> "section:$name" }
            add(key,name,id,films(s.array("items","films","videos")))
        }
        return HomeData(banners,sections.values.toList())
    }
    fun source(value:JsonElement):Source? {
        if(!value.isJsonObject)return null
        val o=value.asJsonObject;val code=o.string("code").trim()
        if(code.isBlank())return null
        val episodes=o.array("episodes","ep_list").filter { it.isJsonObject }.map { e -> val ep=e.asJsonObject;Episode(ep.string("key","episode_key"),ep.long("number",fallback=-1).toInt(),ep.string("name")) }.filter { it.key.isNotBlank() }.distinctBy { it.key }
        return Source(code,o.string("base_code"),o.string("version_key"),o.string("name",fallback=code),episodes)
    }
    inline fun <reified T> decode(value: JsonElement): T = gson.fromJson(value, T::class.java)
}

class ApiClient internal constructor(private val originProvider: () -> String, private val transport: Call.Factory) {
    constructor() : this({ SiteHttp.currentBase }, SiteHttp.callFactory)
    private data class ScopedToken(val value: String, val origin: String)
    @Volatile private var scopedToken = ScopedToken("", originProvider())
    var token: String
        get() = scopedToken.takeIf { it.origin == originProvider() }?.value.orEmpty()
        set(value) { setToken(value) }
    fun setToken(value: String, origin: String = originProvider()) {
        if (origin == originProvider()) scopedToken = ScopedToken(value, origin)
    }
    var refresh: (suspend () -> Boolean)? = null
    suspend fun request(path: String, method: String = "GET", body: Any? = null, query: Map<String, String> = emptyMap(), retry: Boolean = true,accessToken:String?=null,expectedOrigin:String?=null): JsonElement {
        val snapshot = SiteHttp.requestContext().copy(origin = originProvider())
        if (expectedOrigin != null && expectedOrigin != snapshot.origin) throw kotlinx.coroutines.CancellationException("服务地址已切换")
        val requestToken=accessToken?:token
        val url = (if (path.startsWith("http")) path else "${snapshot.origin}/suxinvideo/app/v1/${path.trimStart('/')}").toHttpUrl().newBuilder().apply { query.forEach { (k,v) -> addQueryParameter(k,v) } }.build()
        val origin = snapshot.origin.toHttpUrl()
        require(url.scheme == origin.scheme && url.host == origin.host && url.port == origin.port && url.username.isEmpty() && url.password.isEmpty()) { "API 地址与当前服务不匹配" }
        val b = Request.Builder().url(url).tag(ServerRequestContext::class.java, snapshot).header("Accept", "application/json").header("X-Client-Platform", "android")
        if (requestToken.isNotEmpty()) b.header("Authorization", "Bearer $requestToken")
        if (method != "GET") b.method(method, JsonWire.gson.toJson(body ?: emptyMap<String,String>()).toRequestBody("application/json; charset=utf-8".toMediaType()))
        val response = transport.newCall(b.build()).awaitResponse()
        if (snapshot.generation != SiteHttp.generation || snapshot.origin != originProvider()) throw kotlinx.coroutines.CancellationException("服务地址已切换")
        if (response.first == 401 && retry && accessToken==null && requestToken.isNotEmpty() && requestToken==token && path != "auth/refresh" && refresh?.invoke() == true) return request(path, method, body, query, false, expectedOrigin=snapshot.origin)
        val result = withContext(Dispatchers.Default) { runCatching { JsonParser.parseString(response.second).asJsonObject }.getOrElse { throw ApiException(response.first, response.first, "服务暂时不可用（${response.first}）") } }
        val code = result.get("code")?.asInt ?: if (response.first in 200..299) 0 else response.first
        if (response.first !in 200..299 || code != 0) throw ApiException(response.first, code, result.get("message")?.asString ?: "请求失败",result.get("data"))
        return result.get("data") ?: JsonObject()
    }
}

/** Cancelling navigation must also close its HTTP call, rather than occupying a connection until timeout. */
internal suspend fun Call.awaitResponse():Pair<Int,String> = suspendCancellableCoroutine { continuation->
    continuation.invokeOnCancellation { cancel() }
    enqueue(object:Callback {
        override fun onFailure(call:Call,e:IOException) { if(continuation.isActive)continuation.resumeWithException(e) }
        override fun onResponse(call:Call,response:Response) {
            try {
                val value=response.use { it.code to it.body?.string().orEmpty() }
                if(continuation.isActive)continuation.resume(value)
            } catch(e:Exception) { if(continuation.isActive)continuation.resumeWithException(e) }
        }
    })
}
