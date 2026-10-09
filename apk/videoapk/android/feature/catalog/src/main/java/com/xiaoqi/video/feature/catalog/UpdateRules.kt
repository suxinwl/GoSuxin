package com.xiaoqi.video.feature.catalog

import com.xiaoqi.video.core.model.Release
import com.xiaoqi.video.core.network.ApiException
import java.io.IOException
import java.net.SocketTimeoutException
import java.net.UnknownHostException
import javax.net.ssl.SSLException

/** Validate metadata before a response can select a cache filename or launch Android's installer. */
object UpdateRules {
    const val MAX_APK_BYTES = 512L * 1024 * 1024

    fun validate(release: Release, platform: String, installedVersion: Long, sdk: Int) {
        require(release.platform == platform && platform in setOf("mobile", "tv")) { "更新包与当前客户端不匹配" }
        require(release.versionCode.toLong() > installedVersion && release.versionName.isNotBlank()) { "更新版本无效，请重新检查更新" }
        require(release.minSdk >= 23 && sdk >= release.minSdk) { "该版本要求 Android API ${release.minSdk}" }
        require(release.size in 1024..MAX_APK_BYTES) { "更新文件大小无效" }
        require(Regex("[0-9a-fA-F]{64}").matches(release.sha256)) { "更新文件校验信息无效" }
        require(release.url.isNotBlank()) { "更新地址为空" }
    }

    fun progress(received: Long, expected: Long): Int = if (expected <= 0) 0
        else ((received.coerceAtLeast(0).coerceAtMost(expected) * 100) / expected).toInt().coerceIn(0, 100)

    fun errorMessage(error: Throwable): String = when (error) {
        is ApiException -> error.message
        is SocketTimeoutException -> "更新连接超时，请重试；也可打开下载页覆盖安装，无需卸载 APP"
        is UnknownHostException -> "无法连接更新服务器，请检查网络后重试"
        is SSLException -> "更新安全连接失败，请打开下载页获取同签名新版并覆盖安装，无需卸载 APP"
        is IOException -> "更新连接中断，请重试；已完整下载的安装包会保留"
        else -> error.localizedMessage?.takeIf { Regex("[\\u3400-\\u9FFF]").containsMatchIn(it) }
            ?: "更新失败，请重试或打开下载页覆盖安装"
    }
}
