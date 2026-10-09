@file:androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
package com.xiaoqi.video.feature.catalog

import android.app.Activity
import android.app.PictureInPictureParams
import android.content.Context
import android.content.ContextWrapper
import android.content.pm.ActivityInfo
import android.os.Build
import android.util.Rational
import android.view.View
import androidx.activity.compose.BackHandler
import androidx.activity.ComponentActivity
import androidx.compose.foundation.*
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.*
import androidx.compose.foundation.lazy.grid.*
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.Alignment
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.key.*
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.app.PictureInPictureModeChangedInfo
import androidx.core.util.Consumer
import androidx.media3.ui.PlayerView
import androidx.media3.ui.AspectRatioFrameLayout
import com.xiaoqi.video.core.data.AppRepository
import com.xiaoqi.video.core.design.*
import com.xiaoqi.video.core.cast.*
import com.xiaoqi.video.core.model.*
import com.xiaoqi.video.core.player.*
import com.xiaoqi.video.core.network.JsonWire.string
import com.xiaoqi.video.core.network.JsonWire.long
import com.xiaoqi.video.core.network.SiteHttp
import kotlinx.coroutines.*

@Composable internal fun CastDialog(repo:AppRepository,cast:DeviceCast,dlna:Dlna,state:PlayerState,onDismiss:()->Unit,onConnected:(String,String)->Unit,onStopped:()->Unit) {
    var tvs by remember { mutableStateOf<List<TvDevice>>(emptyList()) };var renderers by remember { mutableStateOf<List<DlnaDevice>>(emptyList()) };var busy by remember { mutableStateOf(true) };var message by remember { mutableStateOf("正在发现局域网设备…") };val scope=rememberCoroutineScope()
    LaunchedEffect(Unit) { runCatching { tvs=cast.devices() }.onFailure { if(repo.session.value==null)message="请登录并在账号页绑定 TV" };runCatching { renderers=dlna.discover() }.onFailure(repo::error);busy=false;message=if(tvs.isEmpty()&&renderers.isEmpty())"未找到电视，请确认手机与电视连接同一 Wi-Fi" else "选择目标电视" }
    val p=state.descriptor
    AlertDialog(onDismissRequest=onDismiss,title={ Text("投屏") },text={ LazyColumn(Modifier.heightIn(max=340.dp),verticalArrangement=Arrangement.spacedBy(8.dp)) {
        item { Text(message);if(busy)CircularProgressIndicator() }
        items(tvs,key={ it.device_id }) { tv->TextButton(enabled=!busy&&p!=null,onClick={ scope.launch { busy=true;try { CastHub.connected(tv.name,"tv");cast.connectMobile(tv);cast.command("play",PlaybackIdentity(p!!.vodId,p.line,p.episodeKey,p.episode,positionMs=state.position,manual=true,versionKey=p.versionKey));onConnected(tv.name,"tv") } catch(e:Throwable) { CastHub.end();repo.error(e) } finally { busy=false } } }) { Text("小柒 TV · ${tv.name}") } }
        items(renderers,key={ it.id }) { device->TextButton(enabled=!busy&&p!=null,onClick={ scope.launch { busy=true;try {
            CastHub.connected(device.name,"dlna")
            val result=dlna.play(device,SiteHttp.absolute(p!!.url),state.detail?.film?.name?:p.name,p.type)
            if(result=="needs_mp4") {
                message="电视不支持 HLS，正在准备兼容视频…"
                val body=repo.playBody(PlaybackIdentity(p.vodId,p.line,p.episodeKey,p.episode,positionMs=state.position,manual=true,versionKey=p.versionKey))
                var job=repo.api.request("cast/prepare","POST",body).asJsonObject
                val id=job.string("job_id")
                CastHub.registerMediaJob(id)
                withTimeout(20*60*1000) { while(job.string("status") !in listOf("completed","failed","cancelled")) { delay(2000);job=repo.api.request("cast/prepare/$id").asJsonObject;message="准备兼容视频 ${job.string("progress",fallback="0")}%" } }
                require(job.string("status")=="completed") { job.string("error",fallback="视频转码失败") }
                dlna.play(device,SiteHttp.absolute(job.string("url")),state.detail?.film?.name?:p.name,"mp4")
            }
            runCatching { dlna.seek(state.position) };onConnected(device.name,"dlna")
        } catch(e:Throwable) { CastHub.end();if(e !is CancellationException)repo.error(e);message=e.localizedMessage.orEmpty() } finally { busy=false } } }) { Text("DLNA · ${device.name}") } }
    } },confirmButton={ TextButton(onClick=onDismiss) { Text("关闭") } },dismissButton={ TextButton(onClick={ onStopped();onDismiss() }) { Text("结束投屏") } })
}
