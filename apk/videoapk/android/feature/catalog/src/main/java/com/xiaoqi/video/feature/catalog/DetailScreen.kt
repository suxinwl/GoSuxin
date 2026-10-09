package com.xiaoqi.video.feature.catalog

import androidx.compose.foundation.*
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.*
import androidx.compose.foundation.lazy.grid.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.Alignment
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import com.xiaoqi.video.core.data.AppRepository
import com.xiaoqi.video.core.design.*
import com.xiaoqi.video.core.download.Downloads
import com.xiaoqi.video.core.model.*
import com.xiaoqi.video.core.network.JsonWire.long
import com.xiaoqi.video.core.network.JsonWire.string
import kotlinx.coroutines.*

@Composable fun DetailScreen(repo:AppRepository,id:Long,tv:Boolean,onFilm:(Long)->Unit,onLogin:()->Unit,onPlay:(Detail,PlaybackIdentity)->Unit) {
    val scope=rememberCoroutineScope();val session by repo.session.collectAsState()
    var detail by remember(id) { mutableStateOf<Detail?>(null) };var error by remember(id) { mutableStateOf("") };var reload by remember(id) { mutableIntStateOf(0) }
    var sourceCode by remember(id) { mutableStateOf("") };var favorite by remember(id) { mutableStateOf(false) };var comment by remember(id) { mutableStateOf("") };var download by remember { mutableStateOf(false) };var unlocking by remember { mutableStateOf(false) };var busy by remember { mutableStateOf(false) }
    LaunchedEffect(id,reload) {
        try { val d=repo.detail(id);detail=d;sourceCode=sourceCode.takeIf { c->d.sources.any { it.code==c } }?:d.preferredLine.takeIf { c->d.sources.any { it.code==c } }?:PlaybackRules.defaultSource(d.film,d.sources)?.code.orEmpty();error="";if(session!=null)runCatching { favorite=repo.favorites().any { it.id==id } } } catch(e:Throwable) { error=e.localizedMessage?:"加载失败" }
    }
    fun perform(block:suspend ()->Unit) { scope.launch { busy=true;try { block() } catch(e:Throwable) { repo.error(e) } finally { busy=false } } }
    if(detail==null) { if(error.isBlank())Loading() else EmptyState(error,"重试",{ reload++ });return }
    val d=detail!!;val f=d.film
    val source=d.sources.firstOrNull { it.code==sourceCode }?:d.sources.firstOrNull()
    LazyColumn(Modifier.fillMaxSize(),contentPadding=PaddingValues(if(tv)36.dp else 16.dp),verticalArrangement=Arrangement.spacedBy(20.dp)) {
        item { Row(verticalAlignment=Alignment.Top,horizontalArrangement=Arrangement.spacedBy(if(tv)32.dp else 16.dp)) {
            Poster(f,tv,{},Modifier.width(if(tv)192.dp else 108.dp),interactive=false)
            Column(Modifier.weight(1f),verticalArrangement=Arrangement.spacedBy(10.dp)) {
                Text(f.name,style=if(tv)MaterialTheme.typography.headlineLarge else MaterialTheme.typography.headlineSmall)
                Text(listOf(f.year,f.area,f.typeName,f.remarks).filter { it.isNotBlank() }.joinToString(" / "),style=MaterialTheme.typography.bodyMedium)
                Text(if(f.score>0)"评分：%.1f".format(f.score) else "暂无评分",color=CinemaAccent)
                if(f.vip)Text("VIP 影片",color=Color(0xFFF2CC61));if(f.points>0)Text("解锁积分：${f.points}")
                Button(enabled=source?.episodes?.isNotEmpty()==true,onClick={ perform {
                    val progress=repo.progress(id);val savedLine=progress?.string("line").orEmpty();val selected=d.sources.find { it.code==savedLine }?:source!!
                    val key=progress?.string("episode_key").orEmpty();val episode=selected.episodes.find { it.key==key }?:selected.episodes.first()
                    val resume=if(episode.key==key && progress?.string("version_key")==selected.versionKey)progress?.long("position_ms")?:0 else 0
                    onPlay(d,PlaybackIdentity(id,selected.code,episode.key,selected.episodes.indexOf(episode),quality=progress?.string("quality").orEmpty(),positionMs=resume,versionKey=selected.versionKey))
                } }) { Text("立即播放 / 续播") }
                LazyRow(horizontalArrangement=Arrangement.spacedBy(8.dp)) {
                    item { OutlinedButton(onClick={ if(session==null)onLogin() else perform { repo.favorite(id,!favorite);favorite=!favorite } }) { Text(if(favorite)"取消收藏" else "收藏") } }
                    if(!tv)item { OutlinedButton(enabled=source?.episodes?.isNotEmpty()==true,onClick={ if(session==null)onLogin() else download=true }) { Text("下载选集") } }
                    if(f.points>0)item { OutlinedButton(onClick={ if(session==null)onLogin() else unlocking=true }) { Text("积分解锁") } }
                }
            }
        } }
        if(f.actor.isNotBlank()||f.director.isNotBlank())item { Text(listOf("导演：${f.director}","主演：${f.actor}").filterNot { it.endsWith("：") }.joinToString("\n")) }
        if(f.content.isNotBlank())item { Text(f.content.replace(Regex("<[^>]+>"),"").replace("&nbsp;"," "),color=Color(0xFFABB9C9),style=MaterialTheme.typography.bodyMedium) }
        item { Row(verticalAlignment=Alignment.CenterVertically) { Text("资源线路",style=MaterialTheme.typography.titleLarge,modifier=Modifier.weight(1f));TextButton(enabled=!busy,onClick={ perform {
            val o=repo.api.request("films/$id/discover","POST").asJsonObject
            repo.notice.value=o.string("message",fallback="后台正在查找更多可用片源")
            repeat(12) { delay(2000);val next=repo.detail(id);if(next.sources.size>d.sources.size) { detail=next;return@perform } };detail=repo.detail(id)
        } }) { Text(if(busy)"正在查找…" else "查找更多片源") } } }
        if(d.sources.isEmpty())item { EmptyState("暂无可用线路，可尝试后台查找片源") }
        else {
            item { LazyRow(horizontalArrangement=Arrangement.spacedBy(8.dp)) { items(d.sources,key={ it.code }) { s->FilterChip(s.code==sourceCode,{ sourceCode=s.code },label={ Text(s.name) }) } } }
            item { source?.let { s->LazyVerticalGrid(GridCells.Adaptive(if(tv)100.dp else 82.dp),Modifier.fillMaxWidth().heightIn(min=60.dp,max=300.dp),horizontalArrangement=Arrangement.spacedBy(8.dp),verticalArrangement=Arrangement.spacedBy(8.dp)) { items(s.episodes,key={ it.key }) { ep->OutlinedButton(onClick={ onPlay(d,PlaybackIdentity(id,s.code,ep.key,s.episodes.indexOf(ep),manual=true,versionKey=s.versionKey)) }) { Text(ep.name.ifBlank { "第 ${ep.number} 集" },maxLines=2) } } } } }
        }
        item { Text("评论 (${d.comments.size})",style=MaterialTheme.typography.titleLarge) }
        if(!tv)item { Row(verticalAlignment=Alignment.CenterVertically,horizontalArrangement=Arrangement.spacedBy(8.dp)) { OutlinedTextField(comment,{ comment=it },label={ Text("分享观影感受") },modifier=Modifier.weight(1f),maxLines=3);Button(enabled=comment.isNotBlank()&&!busy,onClick={ if(session==null)onLogin() else perform { repo.comment(id,comment.trim());comment="";reload++ } }) { Text("发表") } } }
        items(d.comments,key={ it.id }) { c->Column(verticalArrangement=Arrangement.spacedBy(4.dp)) { Text(c.name,style=MaterialTheme.typography.titleSmall,color=CinemaAccent);Text(c.content);Text(c.createdAt,style=MaterialTheme.typography.labelSmall);HorizontalDivider() } }
        if(d.related.isNotEmpty())item { Text("猜你喜欢",style=MaterialTheme.typography.titleLarge);LazyRow(horizontalArrangement=Arrangement.spacedBy(12.dp)) { items(d.related,key={ it.id }) { r->Poster(r,tv,{ onFilm(r.id) },Modifier.width(if(tv)172.dp else 132.dp)) } } }
    }
    if(unlocking)AlertDialog(onDismissRequest={ unlocking=false },title={ Text("积分解锁") },text={ Text("确认使用 ${f.points} 积分解锁《${f.name}》？") },confirmButton={ TextButton(enabled=!busy,onClick={ perform { repo.unlock(id);repo.me();unlocking=false;repo.notice.value="解锁成功" } }) { Text("确认") } },dismissButton={ TextButton(onClick={ unlocking=false }) { Text("取消") } })
    if(download&&source!=null)DownloadSelection(repo,d,source,{ download=false })
}

@Composable internal fun DownloadSelection(repo:AppRepository,d:Detail,source:Source,onDismiss:()->Unit) {
    var selected by remember { mutableStateOf(setOf(source.episodes.first().key)) };var quality by remember { mutableStateOf("") };var qualities by remember { mutableStateOf<List<Quality>>(emptyList()) };var busy by remember { mutableStateOf(false) };var progress by remember { mutableStateOf("") };val scope=rememberCoroutineScope()
    LaunchedEffect(source.code) { runCatching { val p=repo.resolve(PlaybackIdentity(d.film.id,source.code,source.episodes.first().key,manual=true,versionKey=source.versionKey));qualities=p.qualities }.onFailure(repo::error) }
    AlertDialog(onDismissRequest={ if(!busy)onDismiss() },title={ Text("下载 ${d.film.name}") },text={ Column(verticalArrangement=Arrangement.spacedBy(10.dp)) {
        Text("线路：${source.name}")
        LazyRow(horizontalArrangement=Arrangement.spacedBy(8.dp)) { item { FilterChip(quality.isBlank(),{ quality="" },label={ Text("原画") }) };items(qualities,key={ it.id }) { q->FilterChip(quality==q.id,{ quality=q.id },label={ Text(q.name) }) } }
        Row { TextButton(onClick={ selected=source.episodes.map { it.key }.toSet() }) { Text("全选") };TextButton(onClick={ selected=emptySet() }) { Text("取消全选") } }
        LazyColumn(Modifier.heightIn(max=270.dp)) { items(source.episodes,key={ it.key }) { e->Row(Modifier.fillMaxWidth().clickable(enabled=!busy) { selected=if(e.key in selected)selected-e.key else selected+e.key },verticalAlignment=Alignment.CenterVertically) { Checkbox(e.key in selected,{ checked->selected=if(checked)selected+e.key else selected-e.key });Text(e.name.ifBlank { "第${e.number}集" }) } } }
        Text(if(progress.isBlank())"已选 ${selected.size} 集 · 默认仅 Wi-Fi 下载" else progress,style=MaterialTheme.typography.bodySmall)
    } },confirmButton={ TextButton(enabled=!busy&&selected.isNotEmpty(),onClick={ scope.launch { busy=true;try { val list=source.episodes.filter { it.key in selected };list.forEachIndexed { i,e->progress="正在加入队列 ${i+1}/${list.size}";Downloads.enqueue(PlaybackIdentity(d.film.id,source.code,e.key,source.episodes.indexOf(e),quality,manual=true,versionKey=source.versionKey),"${d.film.name} · ${e.name}") };repo.notice.value="${selected.size} 集已加入下载队列";onDismiss() } catch(e:Throwable) { repo.error(e) } finally { busy=false } } }) { Text(if(busy)"正在加入…" else "加入下载") } },dismissButton={ TextButton(enabled=!busy,onClick=onDismiss) { Text("取消") } })
}
