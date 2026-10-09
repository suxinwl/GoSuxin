package com.xiaoqi.video.core.network

import okhttp3.OkHttpClient
import java.security.KeyStore
import java.security.MessageDigest
import java.security.cert.CertificateException
import java.security.cert.X509Certificate
import javax.net.ssl.HostnameVerifier
import javax.net.ssl.SSLContext
import javax.net.ssl.SSLPeerUnverifiedException
import javax.net.ssl.TrustManagerFactory
import javax.net.ssl.X509TrustManager

internal const val ISRG_ROOT_X1_SHA256 = "96bcec06264976f37460779acf28c5a7cfe8a3c0aae11a8ffcee05c0bddf08c6"

/** This anchor is bundled from the CA's official PEM, never supplied by a remote configuration. */
internal fun requirePinnedPublicRoot(ca: X509Certificate): X509Certificate {
    val fingerprint=MessageDigest.getInstance("SHA-256").digest(ca.encoded).joinToString("") { "%02x".format(it.toInt() and 0xff) }
    check(fingerprint==ISRG_ROOT_X1_SHA256) { "本站公信根证书校验失败" }
    return ca
}

private fun certificateTrustManager(alias:String,ca:X509Certificate):X509TrustManager {
    val store=KeyStore.getInstance(KeyStore.getDefaultType()).apply {
        load(null)
        setCertificateEntry(alias,ca)
    }
    return TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm()).apply {
        init(store)
    }.trustManagers.filterIsInstance<X509TrustManager>().single()
}

/** Android 6 has no Network Security Config; later versions use the platform client unchanged. */
internal fun legacySiteClient(standard: OkHttpClient, ca: X509Certificate, host: String, publicRoot: X509Certificate?=null): OkHttpClient {
    val siteManager=certificateTrustManager("site_ca",ca)
    val publicManager=publicRoot?.let { certificateTrustManager("isrg_root_x1",requirePinnedPublicRoot(it)) }
    val systemManager = TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm()).apply {
        init(null as KeyStore?)
    }.trustManagers.filterIsInstance<X509TrustManager>().single()
    val manager = LegacySiteTrustManager(systemManager,siteManager,publicManager=publicManager,publicCaValidity={ publicRoot?.checkValidity() },siteCaValidity=ca::checkValidity)
    val ssl = SSLContext.getInstance("TLS").apply { init(null, arrayOf(manager), null) }
    return standard.newBuilder()
        .sslSocketFactory(ssl.socketFactory, manager)
        .hostnameVerifier(legacySiteHostnameVerifier(host, standard.hostnameVerifier, systemManager))
        .build()
}

/** The fixed anonymous configuration service gets a public anchor, with no private-site trust. */
internal fun legacyPublicRootClient(standard:OkHttpClient,publicRoot:X509Certificate,host:String):OkHttpClient {
    val root=requirePinnedPublicRoot(publicRoot)
    val publicManager=certificateTrustManager("isrg_root_x1",root)
    val systemManager=TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm()).apply {
        init(null as KeyStore?)
    }.trustManagers.filterIsInstance<X509TrustManager>().single()
    val manager=LegacySiteTrustManager(systemManager,null,publicManager=publicManager,publicCaValidity=root::checkValidity)
    val ssl=SSLContext.getInstance("TLS").apply { init(null,arrayOf(manager),null) }
    return standard.newBuilder().sslSocketFactory(ssl.socketFactory,manager)
        .hostnameVerifier(legacySiteHostnameVerifier(host,standard.hostnameVerifier,systemManager)).build()
}

internal class LegacySiteTrustManager(
    private val systemManager: X509TrustManager,
    private val siteManager: X509TrustManager?,
    private val publicManager: X509TrustManager?=null,
    private val publicCaValidity: () -> Unit = {},
    private val siteCaValidity: () -> Unit = {}
) : X509TrustManager {
    override fun checkClientTrusted(chain: Array<X509Certificate>, authType: String) =
        systemManager.checkClientTrusted(chain, authType)

    override fun checkServerTrusted(chain: Array<X509Certificate>, authType: String) {
        if (chain.isEmpty()) throw CertificateException("Empty TLS certificate chain")
        chain.forEach { it.checkValidity() }
        try {
            systemManager.checkServerTrusted(chain, authType)
        } catch (systemFailure: CertificateException) {
            val failures=mutableListOf(systemFailure)
            if(publicManager!=null) {
                try {
                    publicCaValidity()
                    publicManager.checkServerTrusted(chain,authType)
                    return
                } catch(publicFailure:CertificateException) { failures+=publicFailure }
            }
            if(siteManager==null) {
                val failure=failures.last()
                failures.dropLast(1).forEach(failure::addSuppressed)
                throw failure
            }
            try {
                // An expired bundled CA cannot grant private trust, but does not block public PKI.
                siteCaValidity()
                siteManager.checkServerTrusted(chain, authType)
            } catch (siteFailure: CertificateException) {
                failures.forEach(siteFailure::addSuppressed)
                throw siteFailure
            }
        }
    }

    override fun getAcceptedIssuers(): Array<X509Certificate> =
        systemManager.acceptedIssuers + (publicManager?.acceptedIssuers?:emptyArray()) + (siteManager?.acceptedIssuers?:emptyArray())
}

/** Default hostname checks apply first; redirected/media hosts may only use system trust. */
internal fun legacySiteHostnameVerifier(
    siteHost: String,
    strictVerifier: HostnameVerifier,
    systemManager: X509TrustManager
): HostnameVerifier = HostnameVerifier { host, session ->
    if (!strictVerifier.verify(host, session)) return@HostnameVerifier false
    if (host == siteHost) return@HostnameVerifier true
    try {
        val peers = session.peerCertificates
        val chain = peers.filterIsInstance<X509Certificate>().toTypedArray()
        if (chain.isEmpty() || chain.size != peers.size) return@HostnameVerifier false
        chain.forEach { it.checkValidity() }
        systemManager.checkServerTrusted(chain, chain.first().publicKey.algorithm)
        true
    } catch (_: CertificateException) {
        false
    } catch (_: SSLPeerUnverifiedException) {
        false
    }
}
