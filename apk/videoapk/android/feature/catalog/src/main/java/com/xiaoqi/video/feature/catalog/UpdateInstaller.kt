package com.xiaoqi.video.feature.catalog

import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.provider.Settings
import androidx.core.content.FileProvider
import com.xiaoqi.video.core.data.AppRepository
import com.xiaoqi.video.core.model.Release
import com.xiaoqi.video.core.network.SiteHttp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import okhttp3.Request
import java.io.File
import java.security.MessageDigest

enum class UpdateInstallResult { INSTALLER_OPENED, PERMISSION_REQUIRED }

object UpdateInstaller {
    val progress = MutableStateFlow(-1)
    val pendingPermission = MutableStateFlow<Release?>(null)
    internal val lock = Mutex()
    fun canInstall(context: Context): Boolean = Build.VERSION.SDK_INT < 26 || context.packageManager.canRequestPackageInstalls()
    fun dismissPermission() { pendingPermission.value = null }
}

/** A verified APK survives the unknown-sources settings screen and is reused on return/retry. */
suspend fun installRelease(context: Context, r: Release, repo: AppRepository): UpdateInstallResult = UpdateInstaller.lock.withLock {
    val flags = if (Build.VERSION.SDK_INT >= 28) PackageManager.GET_SIGNING_CERTIFICATES else PackageManager.GET_SIGNATURES
    val installed = context.packageManager.getPackageInfo(context.packageName, flags)
    @Suppress("DEPRECATION") val installedVersion = if (Build.VERSION.SDK_INT >= 28) installed.longVersionCode else installed.versionCode.toLong()
    val platform = if (context.packageName.endsWith(".tv")) "tv" else "mobile"
    UpdateRules.validate(r, platform, installedVersion, Build.VERSION.SDK_INT)
    val file = withContext(Dispatchers.IO) {
        val url = SiteHttp.absolute(r.url)
        val directory = File(context.cacheDir, "updates")
        require(directory.isDirectory || directory.mkdirs()) { "无法创建更新目录，请检查存储空间" }
        val name = "xiaoqi-${r.platform}-${r.versionCode}-${r.sha256.lowercase().take(16)}"
        val tmp = File(directory, "$name.tmp")
        val target = File(directory, "$name.apk")
        UpdateInstaller.progress.value = 0
        try {
            if (!hasExpectedContent(target, r)) {
                require(SiteHttp.isSiteHttpsUrl(url)) { "更新地址与当前服务器不匹配，请重新检查更新" }
                if (target.exists()) require(target.delete()) { "无法移除损坏的更新包" }
                SiteHttp.callFactory.newCall(Request.Builder().url(url).build()).execute().use { response ->
                    require(SiteHttp.isSiteHttpsUrl(response.request.url.toString())) { "更新下载跳转地址无效" }
                    require(response.isSuccessful) { "更新下载失败：${response.code}" }
                    val body = response.body ?: error("更新文件为空")
                    require(body.contentLength() < 0 || body.contentLength() == r.size) { "安装包大小校验失败，请重新检查更新" }
                    var received = 0L
                    body.byteStream().use { input ->
                        tmp.outputStream().use { output ->
                            val buffer = ByteArray(64 * 1024)
                            while (true) {
                                currentCoroutineContext().ensureActive()
                                val n = input.read(buffer)
                                if (n < 0) break
                                received += n
                                require(received <= r.size && received <= UpdateRules.MAX_APK_BYTES) { "更新文件超过公布大小" }
                                output.write(buffer, 0, n)
                                UpdateInstaller.progress.value = UpdateRules.progress(received, r.size)
                            }
                        }
                    }
                }
                require(hasExpectedContent(tmp, r)) { "安装包 SHA256 或大小校验失败，请重试" }
                require(tmp.renameTo(target)) { "保存安装包失败，请检查存储空间" }
            }
            val archive = context.packageManager.getPackageArchiveInfo(target.absolutePath, flags) ?: error("安装包无法读取")
            require(archive.packageName == context.packageName) { "安装包与当前客户端不匹配" }
            @Suppress("DEPRECATION") val version = if (Build.VERSION.SDK_INT >= 28) archive.longVersionCode else archive.versionCode.toLong()
            require(version == r.versionCode.toLong() && archive.versionName == r.versionName) { "安装包版本与更新信息不一致" }
            @Suppress("DEPRECATION") val a = if (Build.VERSION.SDK_INT >= 28) archive.signingInfo?.apkContentsSigners?.toSet() else archive.signatures?.toSet()
            @Suppress("DEPRECATION") val b = if (Build.VERSION.SDK_INT >= 28) installed.signingInfo?.apkContentsSigners?.toSet() else installed.signatures?.toSet()
            require(!a.isNullOrEmpty() && a == b) { "安装包签名与当前客户端不一致" }
            UpdateInstaller.progress.value = 100
            target
        } finally {
            if (tmp.exists()) tmp.delete()
            UpdateInstaller.progress.value = -1
        }
    }
    if (!UpdateInstaller.canInstall(context)) {
        UpdateInstaller.pendingPermission.value = r
        context.startActivity(Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:${context.packageName}")))
        repo.notice.value = "请允许安装应用；返回 APP 后将继续安装，无需重新下载"
        return@withLock UpdateInstallResult.PERMISSION_REQUIRED
    }
    val uri = FileProvider.getUriForFile(context, "${context.packageName}.files", file)
    context.startActivity(Intent(Intent.ACTION_VIEW).setDataAndType(uri, "application/vnd.android.package-archive").addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION))
    UpdateInstaller.pendingPermission.value = null
    UpdateInstallResult.INSTALLER_OPENED
}

internal suspend fun hasExpectedContent(file: File, release: Release): Boolean {
    if (!file.isFile || file.length() != release.size) return false
    val digest = MessageDigest.getInstance("SHA-256")
    file.inputStream().use { input ->
        val buffer = ByteArray(64 * 1024)
        while (true) {
            currentCoroutineContext().ensureActive()
            val n = input.read(buffer)
            if (n < 0) break
            digest.update(buffer, 0, n)
        }
    }
    return digest.digest().joinToString("") { "%02x".format(it) }.equals(release.sha256, true)
}
