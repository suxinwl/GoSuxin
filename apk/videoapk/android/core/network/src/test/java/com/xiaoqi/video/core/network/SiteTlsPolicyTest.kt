package com.xiaoqi.video.core.network

import okhttp3.OkHttpClient
import org.junit.Assert.*
import org.junit.Test
import java.lang.reflect.Proxy
import java.math.BigInteger
import java.security.Principal
import java.security.PublicKey
import java.security.cert.Certificate
import java.security.cert.CertificateException
import java.security.cert.CertificateFactory
import java.security.cert.CertificateExpiredException
import java.security.cert.X509Certificate
import java.util.Date
import javax.net.ssl.SSLSession
import javax.net.ssl.X509TrustManager
import javax.security.auth.x500.X500Principal

class SiteTlsPolicyTest {
    private val strictVerifier = OkHttpClient().hostnameVerifier

    @Test fun bundledPublicRootMatchesTheOfficialPinnedCertificate() {
        val root=javaClass.classLoader!!.getResourceAsStream("isrg_root_x1.pem")!!.use {
            CertificateFactory.getInstance("X.509").generateCertificate(it) as X509Certificate
        }
        assertSame(root,requirePinnedPublicRoot(root))
        assertTrue(root.subjectX500Principal.name.contains("ISRG Root X1"))
        assertThrows(IllegalStateException::class.java) { requirePinnedPublicRoot(TestCertificate()) }
    }

    @Test fun oldAndroidCanUseThePublicRootWithoutConsultingAnExpiredPrivateCa() {
        val system=RecordingTrustManager(false);val public=RecordingTrustManager(true);val site=RecordingTrustManager(true)
        var privateCaChecks=0
        val manager=LegacySiteTrustManager(system,site,publicManager=public,siteCaValidity={
            privateCaChecks++;throw CertificateExpiredException("Expired bundled private CA")
        })
        manager.checkServerTrusted(arrayOf(TestCertificate()),"RSA")
        assertEquals(1,system.serverChecks);assertEquals(1,public.serverChecks)
        assertEquals(0,site.serverChecks);assertEquals(0,privateCaChecks)
    }

    @Test fun systemTrustStillWinsWithoutConsultingEitherBundledRoot() {
        val system=RecordingTrustManager(true);val public=RecordingTrustManager(false);val site=RecordingTrustManager(false)
        val manager=LegacySiteTrustManager(system,site,publicManager=public,
            publicCaValidity={ throw CertificateExpiredException("Expired public CA") },siteCaValidity={ throw CertificateExpiredException("Expired private CA") })
        manager.checkServerTrusted(arrayOf(TestCertificate()),"RSA")
        assertEquals(1,system.serverChecks);assertEquals(0,public.serverChecks);assertEquals(0,site.serverChecks)
    }

    @Test fun expiredPublicRootCannotGrantFallbackTrustAndTheIndependentPrivatePathStillWorks() {
        val system=RecordingTrustManager(false);val public=RecordingTrustManager(true);val site=RecordingTrustManager(true)
        val manager=LegacySiteTrustManager(system,site,publicManager=public,
            publicCaValidity={ throw CertificateExpiredException("Expired public CA") })
        manager.checkServerTrusted(arrayOf(TestCertificate()),"RSA")
        assertEquals(1,system.serverChecks);assertEquals(0,public.serverChecks);assertEquals(1,site.serverChecks)
    }

    @Test fun rejectedPublicAndPrivateRootsRetainAllThreeFailureReasons() {
        val manager=LegacySiteTrustManager(RecordingTrustManager(false),RecordingTrustManager(false),publicManager=RecordingTrustManager(false))
        val failure=assertThrows(CertificateException::class.java) { manager.checkServerTrusted(arrayOf(TestCertificate()),"RSA") }
        assertEquals(2,failure.suppressed.size)
    }

    @Test fun bundledPublicRootCannotGrantAnExternalRedirectHostnameTrust() {
        val system=RecordingTrustManager(false)
        LegacySiteTrustManager(system,RecordingTrustManager(false),publicManager=RecordingTrustManager(true))
            .checkServerTrusted(arrayOf(TestCertificate("other.example")),"RSA")
        val verifier=legacySiteHostnameVerifier(SiteHttp.HOST,strictVerifier,system)
        assertFalse(verifier.verify("other.example",session(TestCertificate("other.example"))))
        assertFalse(verifier.verify(SiteHttp.HOST,session(TestCertificate("other.example"))))
    }

    @Test fun anonymousSourceCanUseThePinnedPublicRootWithoutAnyPrivateManager() {
        val system=RecordingTrustManager(false);val public=RecordingTrustManager(true);var privateChecks=0
        val manager=LegacySiteTrustManager(system,null,publicManager=public,siteCaValidity={ privateChecks++ })
        manager.checkServerTrusted(arrayOf(TestCertificate()),"RSA")
        assertEquals(1,system.serverChecks);assertEquals(1,public.serverChecks);assertEquals(0,privateChecks)
    }

    @Test fun anonymousSourceRejectsExpiredOrUnknownPublicChainsWithoutPrivateFallback() {
        val public=RecordingTrustManager(true);var privateChecks=0
        val expired=LegacySiteTrustManager(RecordingTrustManager(false),null,publicManager=public,
            publicCaValidity={ throw CertificateExpiredException("Expired public anchor") },siteCaValidity={ privateChecks++ })
        assertThrows(CertificateExpiredException::class.java) { expired.checkServerTrusted(arrayOf(TestCertificate()),"RSA") }
        assertEquals(0,public.serverChecks);assertEquals(0,privateChecks)
        val unknown=LegacySiteTrustManager(RecordingTrustManager(false),null,publicManager=RecordingTrustManager(false),siteCaValidity={ privateChecks++ })
        val failure=assertThrows(CertificateException::class.java) { unknown.checkServerTrusted(arrayOf(TestCertificate()),"RSA") }
        assertEquals(1,failure.suppressed.size);assertEquals(0,privateChecks)
    }

    @Test fun sourcePublicTrustKeepsTheExactIpHostnameAndCannotAuthorizeOtherDomains() {
        val sourceHost="59.36.165.33";val system=RecordingTrustManager(false)
        val verifier=legacySiteHostnameVerifier(sourceHost,strictVerifier,system)
        assertTrue(verifier.verify(sourceHost,session(TestCertificate(sourceHost,ip=true))))
        assertFalse(verifier.verify(sourceHost,session(TestCertificate("59.36.165.34",ip=true))))
        assertFalse(verifier.verify(SiteHttp.HOST,session(TestCertificate(SiteHttp.HOST))))
    }

    @Test fun updateAddressRequiresTheFixedHttpsOrigin() {
        assertTrue(SiteHttp.isSiteHttpsUrl("${SiteHttp.BASE}/suxinvideo/app/v1/releases/latest.apk"))
        listOf(
            "http://${SiteHttp.HOST}:${SiteHttp.PORT}/app.apk",
            "https://${SiteHttp.HOST}/app.apk",
            "https://${SiteHttp.HOST}:8601/app.apk",
            "https://other.example:8600/app.apk",
            "https://sub.${SiteHttp.HOST}:8600/app.apk",
            "https://${SiteHttp.HOST}.other.example:8600/app.apk",
            "https://user:password@${SiteHttp.HOST}:8600/app.apk",
            "not a URL"
        ).forEach { assertFalse(it, SiteHttp.isSiteHttpsUrl(it)) }
    }

    @Test fun systemTrustedChainsDoNotNeedTheSiteCa() {
        val system = RecordingTrustManager(true)
        val site = RecordingTrustManager(false)
        LegacySiteTrustManager(system, site).checkServerTrusted(arrayOf(TestCertificate()), "RSA")
        assertEquals(1, system.serverChecks)
        assertEquals(0, site.serverChecks)
    }

    @Test fun siteCaIsUsedWhenSystemTrustRejectsAValidChain() {
        val system = RecordingTrustManager(false)
        val site = RecordingTrustManager(true)
        LegacySiteTrustManager(system, site).checkServerTrusted(arrayOf(TestCertificate()), "RSA")
        assertEquals(1, system.serverChecks)
        assertEquals(1, site.serverChecks)
    }

    @Test fun anExpiredPrivateCaDoesNotBlockAValidSystemTrustedChain() {
        val system = RecordingTrustManager(true)
        val site = RecordingTrustManager(false)
        var privateCaChecks = 0
        val manager = LegacySiteTrustManager(system, site) {
            privateCaChecks++
            throw CertificateExpiredException("Expired bundled CA")
        }
        manager.checkServerTrusted(arrayOf(TestCertificate()), "RSA")
        assertEquals(1, system.serverChecks)
        assertEquals(0, site.serverChecks)
        assertEquals(0, privateCaChecks)
    }

    @Test fun anExpiredPrivateCaCannotAuthorizeAFallbackChain() {
        val system = RecordingTrustManager(false)
        val site = RecordingTrustManager(true)
        val manager = LegacySiteTrustManager(system, site) {
            throw CertificateExpiredException("Expired bundled CA")
        }
        assertThrows(CertificateExpiredException::class.java) {
            manager.checkServerTrusted(arrayOf(TestCertificate()), "RSA")
        }
        assertEquals(1, system.serverChecks)
        assertEquals(0, site.serverChecks)
    }

    @Test fun anUntrustedChainPreservesBothTrustFailures() {
        val manager = LegacySiteTrustManager(RecordingTrustManager(false), RecordingTrustManager(false))
        val failure = assertThrows(CertificateException::class.java) {
            manager.checkServerTrusted(arrayOf(TestCertificate()), "RSA")
        }
        assertEquals(1, failure.suppressed.size)
        assertTrue(failure.suppressed.single() is CertificateException)
    }

    @Test fun emptyChainsAreRejectedBeforeAnyTrustFallback() {
        val system = RecordingTrustManager(true)
        val site = RecordingTrustManager(true)
        assertThrows(CertificateException::class.java) {
            LegacySiteTrustManager(system, site).checkServerTrusted(emptyArray(), "RSA")
        }
        assertEquals(0, system.serverChecks)
        assertEquals(0, site.serverChecks)
    }

    @Test fun expiredLeafAndIssuerAreRejectedEvenWhenTrustManagersAcceptThem() {
        listOf(
            arrayOf<X509Certificate>(TestCertificate(valid = false)),
            arrayOf<X509Certificate>(TestCertificate(), TestCertificate(valid = false))
        ).forEach { chain ->
            val system = RecordingTrustManager(true)
            val site = RecordingTrustManager(true)
            assertThrows(CertificateExpiredException::class.java) {
                LegacySiteTrustManager(system, site).checkServerTrusted(chain, "RSA")
            }
            assertEquals(0, system.serverChecks)
            assertEquals(0, site.serverChecks)
        }
    }

    @Test fun aMatchingSiteCertificateRetainsStrictHostnameVerification() {
        val system = RecordingTrustManager(false)
        val verifier = legacySiteHostnameVerifier(SiteHttp.HOST, strictVerifier, system)
        assertTrue(verifier.verify(SiteHttp.HOST, session(TestCertificate(SiteHttp.HOST))))
        assertFalse(verifier.verify(SiteHttp.HOST, session(TestCertificate("other.example"))))
        assertEquals(0, system.serverChecks)
    }

    @Test fun siteCaTrustCannotAuthorizeAnExternalHostAfterARedirect() {
        val system = RecordingTrustManager(false)
        val verifier = legacySiteHostnameVerifier(SiteHttp.HOST, strictVerifier, system)
        assertFalse(verifier.verify("other.example", session(TestCertificate("other.example"))))
        assertEquals(1, system.serverChecks)
    }

    @Test fun externalHostsNeedBothMatchingHostnamesAndSystemTrust() {
        val system = RecordingTrustManager(true)
        val verifier = legacySiteHostnameVerifier(SiteHttp.HOST, strictVerifier, system)
        assertFalse(verifier.verify("other.example", session(TestCertificate(SiteHttp.HOST))))
        assertEquals(0, system.serverChecks)
        assertTrue(verifier.verify("other.example", session(TestCertificate("other.example"))))
        assertEquals(1, system.serverChecks)
        assertEquals("RSA", system.lastAuthType)
    }

    @Test fun externalExpiredChainsCannotUseTheSharedMediaClient() {
        val system = RecordingTrustManager(true)
        val verifier = legacySiteHostnameVerifier(SiteHttp.HOST, strictVerifier, system)
        assertFalse(verifier.verify("other.example", session(TestCertificate("other.example", valid = false))))
        assertEquals(0, system.serverChecks)
    }

    private fun session(vararg certificates: Certificate): SSLSession = Proxy.newProxyInstance(
        SSLSession::class.java.classLoader, arrayOf(SSLSession::class.java)
    ) { _, method, _ ->
        when (method.name) {
            "getPeerCertificates" -> certificates
            else -> error("Unexpected SSLSession call: ${method.name}")
        }
    } as SSLSession

    private class RecordingTrustManager(private val trusted: Boolean) : X509TrustManager {
        var serverChecks = 0
        var lastAuthType = ""
        override fun checkClientTrusted(chain: Array<X509Certificate>, authType: String) = Unit
        override fun checkServerTrusted(chain: Array<X509Certificate>, authType: String) {
            serverChecks++
            lastAuthType = authType
            if (!trusted) throw CertificateException("Rejected test chain")
        }
        override fun getAcceptedIssuers(): Array<X509Certificate> = emptyArray()
    }

    /** A controlled SAN and validity result let the tests exercise OkHttp's actual hostname verifier. */
    private class TestCertificate(private val hostname: String = SiteHttp.HOST, private val valid: Boolean = true, private val ip: Boolean = false) : X509Certificate() {
        override fun checkValidity() { if (!valid) throw CertificateExpiredException("Expired test certificate") }
        override fun checkValidity(date: Date) = checkValidity()
        override fun getSubjectAlternativeNames(): Collection<List<*>> = listOf(listOf(if(ip)7 else 2, hostname))
        override fun getVersion() = 3
        override fun getSerialNumber(): BigInteger = BigInteger.ONE
        override fun getIssuerDN(): Principal = X500Principal("CN=Test CA")
        override fun getSubjectDN(): Principal = X500Principal("CN=$hostname")
        override fun getNotBefore() = Date(0)
        override fun getNotAfter() = Date(Long.MAX_VALUE)
        override fun getTBSCertificate() = byteArrayOf(1)
        override fun getSignature() = byteArrayOf(1)
        override fun getSigAlgName() = "SHA256withRSA"
        override fun getSigAlgOID() = "1.2.840.113549.1.1.11"
        override fun getSigAlgParams(): ByteArray? = null
        override fun getIssuerUniqueID(): BooleanArray? = null
        override fun getSubjectUniqueID(): BooleanArray? = null
        override fun getKeyUsage(): BooleanArray? = null
        override fun getBasicConstraints() = -1
        override fun getEncoded() = byteArrayOf(1)
        override fun verify(key: PublicKey) = Unit
        override fun verify(key: PublicKey, sigProvider: String) = Unit
        override fun toString() = "TestCertificate($hostname)"
        override fun getPublicKey(): PublicKey = object : PublicKey {
            override fun getAlgorithm() = "RSA"
            override fun getFormat() = "X.509"
            override fun getEncoded() = byteArrayOf(1)
        }
        override fun hasUnsupportedCriticalExtension() = false
        override fun getCriticalExtensionOIDs(): Set<String>? = null
        override fun getNonCriticalExtensionOIDs(): Set<String>? = null
        override fun getExtensionValue(oid: String): ByteArray? = null
    }
}
