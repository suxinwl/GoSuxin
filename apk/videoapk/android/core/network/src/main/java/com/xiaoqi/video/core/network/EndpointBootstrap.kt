package com.xiaoqi.video.core.network

import android.os.Build
import com.google.gson.JsonParser
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import okhttp3.FormBody
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.OkHttpClient
import okhttp3.Request
import java.io.ByteArrayOutputStream
import java.io.IOException
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean

/** Anonymous startup discovery; changing the active address requires an explicit UI action. */
object EndpointBootstrap {
    val SOURCE: String = BuildConfig.BOOTSTRAP_URL
    private const val API_PATH = "/suxinvideo/app/v1"
    private const val MAX_BYTES = 64 * 1024
    private val source = SOURCE.toHttpUrlOrNull()?.takeIf { it.isHttps && it.username.isEmpty() && it.password.isEmpty() }
    private val started = AtomicBoolean(false)
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    // Only the fixed share authority may add the pinned public root on old Android.
    // This remains an anonymous client: no private-site CA, cookies or member headers.
    private val discoveryClient by lazy {
        val builder=OkHttpClient.Builder()
        if(Build.VERSION.SDK_INT<24)shortClient(legacyPublicRootClient(builder.build(),SiteHttp.publicRootAnchor(),requireNotNull(source).host).newBuilder())
        else shortClient(builder)
    }
    private val discovery=EndpointDiscovery(scope,
        fetch={ deadline->if(source==null)null else parseManifest(fetchManifest(BuildConfig.BOOTSTRAP_PASSWORD,deadline)) },
        verify={ candidate,deadline->verifyService(candidate,deadline) },remember=SiteHttp::rememberVerifiedEndpoint,
        recentlyVerified=SiteHttp::hasRecentEndpointVerification,currentOrigin={ SiteHttp.currentBase })
    val state=discovery.state

    /** Reusable after an outage; this only validates/persists a candidate, never swaps a playing session. */
    fun check(onAddressUpdated:()->Unit={}):Boolean=discovery.check(onAddressUpdated)

    fun start(onAddressUpdated: () -> Unit = {}) {
        if (!started.compareAndSet(false, true)) return
        discovery.refresh(onAddressUpdated)
    }

    internal fun parseManifest(content: String): String? = runCatching {
        val value = JsonParser.parseString(content)
        if (!value.isJsonObject) return null
        val item = value.asJsonObject
        val schema = item.get("schema")?.takeIf { it.isJsonPrimitive && it.asJsonPrimitive.isNumber }
            ?.asBigDecimal?.intValueExact() ?: return null
        if (schema != 1) return null
        fun string(name: String): String? = item.get(name)?.takeIf { it.isJsonPrimitive && it.asJsonPrimitive.isString }?.asString
        if (string("project") != "xiaoqi-video" || string("api_path") != API_PATH) return null
        SiteHttp.normalizeOrigin(string("base_url") ?: return null)
    }.getOrNull()

    internal fun allowedShareUrl(url: String): Boolean {
        val entry = source ?: return false
        return url.toHttpUrlOrNull()?.let {
            it.isHttps && it.host == entry.host && it.port == entry.port && it.username.isEmpty() && it.password.isEmpty()
        } == true
    }

    private fun fetchManifest(password: String, deadline: Long): String {
        var request = Request.Builder().url(requireNotNull(source)).header("Accept", "application/json").apply {
            if (password.isNotEmpty()) post(FormBody.Builder().add("file_password", password).build())
        }.build()
        repeat(4) { attempt ->
            if (!allowedShareUrl(request.url.toString())) throw IOException("配置入口跳转地址无效")
            discoveryClient.newCall(request).apply { timeout().deadlineNanoTime(deadline) }.execute().use { response ->
                if (response.code in setOf(301, 302, 303, 307, 308)) {
                    if (attempt == 3) throw IOException("配置入口跳转过多")
                    val next = response.header("Location")?.let(request.url::resolve)
                        ?: throw IOException("配置入口跳转缺少地址")
                    if (!allowedShareUrl(next.toString())) throw IOException("配置入口跳转地址无效")
                    request = request.newBuilder().url(next).apply {
                        if (response.code == 303 || (request.method == "POST" && response.code in setOf(301, 302))) get()
                    }.build()
                } else {
                    if (!response.isSuccessful) throw IOException("配置入口暂时不可用")
                    return smallBody(response)
                }
            }
        }
        throw IOException("配置入口暂时不可用")
    }

    private fun verifyService(origin: String, deadline: Long): Boolean = runCatching {
        val client = shortClient(SiteHttp.client(origin).newBuilder())
        val request = Request.Builder().url("$origin$API_PATH/config").header("Accept", "application/json").build()
        client.newCall(request).apply { timeout().deadlineNanoTime(deadline) }.execute().use { response ->
            if (!response.isSuccessful) return false
            val value = JsonParser.parseString(smallBody(response))
            if (!value.isJsonObject) return false
            val item = value.asJsonObject
            val code = item.get("code")?.takeIf { it.isJsonPrimitive && it.asJsonPrimitive.isNumber }?.asBigDecimal?.intValueExact()
            code == 0 && item.get("data")?.isJsonObject == true
        }
    }.getOrDefault(false)

    private fun shortClient(builder: OkHttpClient.Builder): OkHttpClient = builder
        .connectTimeout(1, TimeUnit.SECONDS).readTimeout(1, TimeUnit.SECONDS).writeTimeout(1, TimeUnit.SECONDS)
        .callTimeout(2, TimeUnit.SECONDS).followRedirects(false).followSslRedirects(false).build()

    private fun smallBody(response: okhttp3.Response): String {
        val body = response.body ?: throw IOException("配置响应为空")
        if (body.contentLength() > MAX_BYTES) throw IOException("配置响应过大")
        val output = ByteArrayOutputStream()
        body.byteStream().use { input ->
            val buffer = ByteArray(4096)
            while (true) {
                val read = input.read(buffer)
                if (read < 0) break
                if (output.size() + read > MAX_BYTES) throw IOException("配置响应过大")
                output.write(buffer, 0, read)
            }
        }
        return output.toString("UTF-8")
    }
}
