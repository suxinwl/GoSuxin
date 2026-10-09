package com.xiaoqi.video.feature.catalog

import com.xiaoqi.video.core.model.Release
import com.xiaoqi.video.core.network.ApiException
import java.io.IOException
import java.io.File
import java.security.MessageDigest
import java.net.SocketTimeoutException
import javax.net.ssl.SSLHandshakeException
import org.junit.Assert.*
import org.junit.Test
import kotlinx.coroutines.runBlocking

class UpdateRulesTest {
    private val release=Release("mobile","1.0.13",113,23,18_000_000,"a".repeat(64),"/suxinvideo/app/releases/file/update.apk","修复更新")
    private fun validate(value:Release=release,platform:String="mobile",installed:Long=111,sdk:Int=23)=UpdateRules.validate(value,platform,installed,sdk)
    private fun rejected(value:Release=release,platform:String="mobile",installed:Long=111,sdk:Int=23) {
        try { validate(value,platform,installed,sdk);fail("Unsafe update metadata was accepted") } catch(_:IllegalArgumentException) {}
    }

    @Test fun aNewSamePlatformVersionAcceptsOldSupportedDevices() { validate();validate(installed=112,sdk=36) }
    @Test fun mobileAndTvUpdatesCannotBeInterchangedOrChooseCachePaths() { rejected(platform="tv");rejected(release.copy(platform="../tv"));rejected(release.copy(platform="")) }
    @Test fun equalAndOlderBuildsCannotStartInstallation() { rejected(installed=113);rejected(release.copy(versionCode=110));rejected(release.copy(versionName="")) }
    @Test fun systemRequirementAndBoundedSizeAreCheckedBeforeDownloading() { rejected(release.copy(minSdk=24));rejected(release.copy(size=0));rejected(release.copy(size=UpdateRules.MAX_APK_BYTES+1)) }
    @Test fun missingOrMalformedHashCannotSelectAnUnverifiedCacheFile() { rejected(release.copy(sha256=""));rejected(release.copy(sha256="a".repeat(63)));rejected(release.copy(sha256="g".repeat(64)));validate(release.copy(sha256="A".repeat(64))) }
    @Test fun aMissingDownloadAddressIsRejected() { rejected(release.copy(url="")) }
    @Test fun downloadProgressDoesNotOverflowTheDialogIndicator() { assertEquals(0,UpdateRules.progress(-1,100));assertEquals(50,UpdateRules.progress(50,100));assertEquals(100,UpdateRules.progress(150,100));assertEquals(0,UpdateRules.progress(50,0)) }
    @Test fun networkFailuresOfferUsefulLocalizedRecoveryInsteadOfRawTlsExceptions() {
        assertTrue(UpdateRules.errorMessage(SocketTimeoutException("timeout")).contains("无需卸载"))
        assertTrue(UpdateRules.errorMessage(SSLHandshakeException("unknown certificate")).contains("安全连接失败"))
        assertTrue(UpdateRules.errorMessage(IOException("Failed to connect")).contains("已完整下载"))
        assertEquals("更新暂不可用",UpdateRules.errorMessage(ApiException(503,503,"更新暂不可用")))
    }
    @Test fun aVerifiedCacheCanBeReusedAfterReturningFromSystemSettings()=runBlocking {
        val bytes=ByteArray(2048) { (it%251).toByte() }
        val cached=File.createTempFile("xiaoqi-update-cache-", ".apk")
        val metadata=release.copy(size=bytes.size.toLong(),sha256=MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it) })
        try { cached.writeBytes(bytes);assertTrue(hasExpectedContent(cached,metadata));assertTrue(hasExpectedContent(cached,metadata)) } finally { cached.delete() }
    }
    @Test fun changedAndTruncatedCacheFilesRequireAnotherDownload()=runBlocking {
        val bytes=ByteArray(2048) { 11 }
        val cached=File.createTempFile("xiaoqi-update-cache-", ".apk")
        val metadata=release.copy(size=bytes.size.toLong(),sha256=MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it) })
        try {
            cached.writeBytes(bytes.copyOf().also { it[0]=12 });assertFalse(hasExpectedContent(cached,metadata))
            cached.writeBytes(bytes.copyOf(1024));assertFalse(hasExpectedContent(cached,metadata))
        } finally { cached.delete() }
    }
}
