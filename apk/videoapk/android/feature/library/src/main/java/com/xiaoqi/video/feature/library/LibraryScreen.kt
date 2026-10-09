@file:androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
package com.xiaoqi.video.feature.library

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.*
import androidx.compose.foundation.lazy.grid.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.Alignment
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp
import com.xiaoqi.video.core.data.AppRepository
import com.xiaoqi.video.core.design.*
import com.xiaoqi.video.core.download.*
import com.xiaoqi.video.core.model.*
import kotlinx.coroutines.launch
import kotlinx.coroutines.CancellationException

@Composable fun LibraryScreen(repo:AppRepository,tv:Boolean,onFilm:(Long)->Unit,onOffline:()->Unit,onLogin:()->Unit) {
    val session by repo.session.collectAsState();val changed by repo.changed.collectAsState()
    var tab by remember { mutableStateOf(LibrarySection.History) };var loading by remember { mutableStateOf(false) }
    var films by remember { mutableStateOf<List<Film>>(emptyList()) };var histories by remember { mutableStateOf<List<History>>(emptyList()) }
    val rows by Downloads.rows.collectAsState();val wifiOnly by Downloads.wifiOnly.collectAsState();val scope=rememberCoroutineScope()
    var deletion by remember { mutableStateOf<DownloadRow?>(null) }
    LaunchedEffect(tab,session?.user?.id,changed) {
        if(session==null)return@LaunchedEffect
        loading=true
        try { when(tab) {
            LibrarySection.History->histories=repo.histories()
            LibrarySection.Favorites->films=repo.favorites()
            LibrarySection.Downloads->Downloads.validateOnline()
        } } catch(e:CancellationException) { throw e } catch(e:Throwable) { repo.error(e) } finally { loading=false }
    }
    if(session==null) { EmptyState("登录后查看收藏、历史和离线下载","登录",onLogin);return }
    Column(Modifier.fillMaxSize().padding(horizontal=if(tv)28.dp else 12.dp)) {
        Row(Modifier.fillMaxWidth().padding(vertical=12.dp),horizontalArrangement=Arrangement.spacedBy(8.dp)) { LibrarySection.available(tv).forEach { section->FilterChip(tab==section,{ tab=section },modifier=Modifier.testTag("library-tab-${section.key}"),label={ Text(section.title) }) } }
        if(loading)Loading()
        when(tab) {
            LibrarySection.Favorites -> if(films.isEmpty()&&!loading)EmptyState("暂无收藏") else LazyVerticalGrid(GridCells.Adaptive(if(tv)170.dp else 138.dp),horizontalArrangement=Arrangement.spacedBy(12.dp),verticalArrangement=Arrangement.spacedBy(12.dp)) { items(films,key={ it.id }) { Poster(it,tv,{ onFilm(it.id) }) } }
            LibrarySection.History -> if(histories.isEmpty()&&!loading)EmptyState("暂无观看历史") else LazyColumn(verticalArrangement=Arrangement.spacedBy(10.dp)) { items(histories,key={ it.film.id }) { h -> Card(onClick={ onFilm(h.film.id) },modifier=Modifier.fillMaxWidth()) { Row(Modifier.padding(12.dp),verticalAlignment=Alignment.CenterVertically) { Poster(h.film,tv,{ onFilm(h.film.id) },Modifier.width(if(tv)100.dp else 72.dp));Column(Modifier.weight(1f).padding(start=16.dp)) { Text(h.film.name,style=MaterialTheme.typography.titleMedium,softWrap=true);Text("已观看 ${formatTime(h.positionMs)} / ${formatTime(h.durationMs)}");Text(h.episodeKey,style=MaterialTheme.typography.labelSmall);Text("继续播放 →") } } } } }
            LibrarySection.Downloads -> {
                Row(Modifier.fillMaxWidth(),verticalAlignment=Alignment.CenterVertically) { Text("仅在 Wi-Fi 下载",Modifier.weight(1f));Switch(wifiOnly,{ Downloads.setWifiOnly(it) }) }
                Text("下载完成后可离线播放；切换账号将暂停原账号队列。",style=MaterialTheme.typography.bodySmall)
                if(rows.isEmpty())EmptyState("暂无下载，在影片详情页选择剧集下载")
                LazyColumn(Modifier.weight(1f),verticalArrangement=Arrangement.spacedBy(10.dp),contentPadding=PaddingValues(vertical=12.dp)) { items(rows,key={ it.download.request.id }) { row ->
                    Card(Modifier.fillMaxWidth()) { Column(Modifier.padding(14.dp),verticalArrangement=Arrangement.spacedBy(8.dp)) {
                        Text(row.metadata.title,style=MaterialTheme.typography.titleMedium)
                        Text("${row.status} · ${(row.download.bytesDownloaded/1024.0/1024).toInt()} MB")
                        LinearProgressIndicator(progress={ (row.download.percentDownloaded.coerceIn(0f,100f)/100f) },modifier=Modifier.fillMaxWidth())
                        Row(horizontalArrangement=Arrangement.spacedBy(6.dp)) {
                            if(row.download.state==androidx.media3.exoplayer.offline.Download.STATE_COMPLETED)Button(onClick={ scope.launch { try { Downloads.play(row);onOffline() } catch(e:Throwable) { repo.error(e) } } }) { Text("离线播放") }
                            else if(row.download.state==androidx.media3.exoplayer.offline.Download.STATE_DOWNLOADING)OutlinedButton(onClick={ Downloads.pause(row.download.request.id) }) { Text("暂停") }
                            else OutlinedButton(onClick={ Downloads.resume(row) }) { Text("继续 / 重试") }
                            TextButton(onClick={ deletion=row }) { Text("删除") }
                        }
                    } }
                } }
            }
        }
    }
    deletion?.let { row -> AlertDialog(onDismissRequest={ deletion=null },title={ Text("删除离线视频？") },text={ Text(row.metadata.title) },confirmButton={ TextButton(onClick={ Downloads.delete(row.download.request.id);deletion=null }) { Text("删除") } },dismissButton={ TextButton(onClick={ deletion=null }) { Text("取消") } }) }
}
