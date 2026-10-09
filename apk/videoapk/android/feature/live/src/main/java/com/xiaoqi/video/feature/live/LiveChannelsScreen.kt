package com.xiaoqi.video.feature.live

import androidx.compose.foundation.*
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.grid.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.*
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import coil.ImageLoader
import coil.compose.AsyncImage
import com.xiaoqi.video.core.data.*
import com.xiaoqi.video.core.design.*
import com.xiaoqi.video.core.model.*
import com.xiaoqi.video.core.network.SiteHttp
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay

object LiveFocus { var channelId:Long=0 }

@Composable fun LiveChannelsScreen(
    repo:AppRepository,tv:Boolean,onChannel:(LiveChannel)->Unit
) {
    LiveChannelsScreen(repo,tv,loadGroups={ repo.liveGroups() },
        loadChannels={ group,query,page->repo.liveChannels(group,query,page) },onChannel=onChannel)
}

@Composable fun LiveChannelsScreen(
    repo:AppRepository,tv:Boolean,
    loadGroups:suspend ()->List<LiveGroup>,
    loadChannels:suspend (Long,String,Int)->LiveChannelPage,
    onChannel:(LiveChannel)->Unit
) {
    var groups by remember { mutableStateOf<List<LiveGroup>>(emptyList()) }
    var group by rememberSaveable { mutableLongStateOf(0) }
    var keyword by rememberSaveable { mutableStateOf("") }
    var query by rememberSaveable { mutableStateOf("") }
    var page by rememberSaveable { mutableIntStateOf(1) }
    var reload by remember { mutableIntStateOf(0) }
    var groupError by remember { mutableStateOf(false) }
    val grid=rememberLazyGridState()
    val style=LocalCinemaStyle.current
    val scope=rememberCoroutineScope()
    val currentLoadChannels by rememberUpdatedState(loadChannels)
    val loader=remember(scope) { LiveChannelsLoader(scope) { groupId,text,requestedPage->currentLoadChannels(groupId,text,requestedPage) } }
    val catalogue by loader.state.collectAsState()
    val result=catalogue.result
    val loading=catalogue.loading
    val error=catalogue.error
    val member by repo.session.collectAsState()
    DisposableEffect(loader) { onDispose { loader.close() } }
    LaunchedEffect(reload,member?.user?.id) { try { groups=loadGroups();groupError=false } catch(e:CancellationException) { throw e } catch(_:Throwable) { groupError=true } }
    LaunchedEffect(group,query,page,reload,member?.user?.id) {
        loader.load(group,query,page)
    }
    // scrollToItem waits for the grid's first layout. The loading branch does not
    // compose that grid, so awaiting it inside the request would never finish.
    LaunchedEffect(result,loading) {
        if(!loading&&!result?.items.isNullOrEmpty())grid.scrollToItem(0)
    }
    Column(Modifier.fillMaxSize().background(CinemaBackground).testTag("live_channels"),verticalArrangement=Arrangement.spacedBy(10.dp)) {
        Row(Modifier.fillMaxWidth().padding(horizontal=if(tv)24.dp else 12.dp),verticalAlignment=Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text("电视直播",style=MaterialTheme.typography.titleLarge,color=style.text)
                Text(if(loading)"正在加载频道…" else "${result?.total?:0} 个频道 · 点击频道直接播放",style=MaterialTheme.typography.labelSmall,color=style.muted)
            }
            LiveAction(tv,"刷新","live_refresh") { reload++ }
        }
        Row(Modifier.fillMaxWidth().padding(horizontal=if(tv)24.dp else 12.dp),verticalAlignment=Alignment.CenterVertically,horizontalArrangement=Arrangement.spacedBy(10.dp)) {
            OutlinedTextField(keyword,{ keyword=it },placeholder={ Text("搜索频道名称") },leadingIcon={ Icon(Icons.Default.Search,null) },singleLine=true,keyboardOptions=KeyboardOptions(imeAction=ImeAction.Search),keyboardActions=KeyboardActions(onSearch={ query=keyword.trim();page=1 }),modifier=Modifier.weight(1f).testTag("live_search_input"))
            LiveAction(tv,"搜索","live_search") { query=keyword.trim();page=1 }
        }
        LazyRow(Modifier.fillMaxWidth(),contentPadding=PaddingValues(horizontal=if(tv)24.dp else 12.dp),horizontalArrangement=Arrangement.spacedBy(8.dp)) {
            item { LiveGroupChoice(tv,"全部频道",group==0L,"live_group_all") { group=0;page=1 } }
            items(groups,key={ it.id }) { g->LiveGroupChoice(tv,g.name,group==g.id,"live_group_${g.id}") { group=g.id;page=1 } }
        }
        if(groupError)Text("分组暂未加载，点击刷新可重试",Modifier.padding(horizontal=12.dp),color=style.muted,style=MaterialTheme.typography.labelSmall)
        when {
            loading->Loading()
            error.isNotBlank()->EmptyState(error,"重试",{ reload++ })
            result?.items.isNullOrEmpty()->EmptyState("暂无直播频道，可切换分组或搜索关键词","刷新",{ reload++ })
            else->LazyVerticalGrid(GridCells.Adaptive(if(tv)220.dp else 140.dp),Modifier.weight(1f).fillMaxWidth().testTag("live_channel_grid"),state=grid,contentPadding=PaddingValues(if(tv)24.dp else 12.dp),horizontalArrangement=Arrangement.spacedBy(12.dp),verticalArrangement=Arrangement.spacedBy(12.dp)) {
                items(result!!.items,key={ it.id }) { channel->LiveChannelCard(channel,tv,onClick={ LiveFocus.channelId=channel.id;onChannel(channel) }) }
                item(span={ GridItemSpan(maxLineSpan) }) {
                    Column(Modifier.fillMaxWidth().padding(vertical=12.dp),horizontalAlignment=Alignment.CenterHorizontally) {
                        Row(Modifier.fillMaxWidth(),horizontalArrangement=Arrangement.SpaceEvenly,verticalAlignment=Alignment.CenterVertically) {
                            LiveAction(tv,"上一页","live_previous_page",enabled=page>1) { page-- }
                            Text("$page / ${result!!.pages}",style=MaterialTheme.typography.bodySmall,color=style.text)
                            LiveAction(tv,"下一页","live_next_page",enabled=page<result!!.pages) { page++ }
                        }
                        Text("本页 ${result!!.items.size} 个频道",style=MaterialTheme.typography.labelSmall,color=style.muted)
                    }
                }
            }
        }
    }
}

@Composable internal fun LiveChannelCard(channel:LiveChannel,tv:Boolean,onClick:()->Unit) {
    var focused by remember { mutableStateOf(false) }
    val focus=remember(channel.id) { FocusRequester() }
    val style=LocalCinemaStyle.current
    val shape=RoundedCornerShape(style.posterRadius.dp)
    LaunchedEffect(channel.id) { if(tv&&LiveFocus.channelId==channel.id) { delay(100);runCatching { focus.requestFocus() } } }
    Column(Modifier.fillMaxWidth().focusRequester(focus).onFocusChanged { focused=it.isFocused }.border(if(focused)3.dp else 0.dp,if(focused)style.accent else Color.Transparent,shape)
        .background(style.surface,shape).clickable(onClickLabel="观看 ${channel.name}",onClick=onClick).padding(14.dp).testTag("live_channel_${channel.id}"),horizontalAlignment=Alignment.CenterHorizontally,verticalArrangement=Arrangement.spacedBy(8.dp)) {
        Box(Modifier.size(if(tv)72.dp else 56.dp).background(Color.White,RoundedCornerShape(10.dp)).padding(4.dp),contentAlignment=Alignment.Center) {
            Icon(Icons.Default.LiveTv,null,tint=style.accent,modifier=Modifier.size(32.dp))
            if(channel.logo.isNotBlank()) {
                val context=LocalContext.current
                val target=SiteHttp.absolute(channel.logo)
                val loader=remember(target) { ImageLoader.Builder(context).okHttpClient(SiteHttp.client(target)).build() }
                AsyncImage(target,channel.name,imageLoader=loader,modifier=Modifier.fillMaxSize())
            }
        }
        if(tv)OverflowTitle(channel.name,Modifier.fillMaxWidth(),animate=focused,style=MaterialTheme.typography.titleMedium)
        else Text(channel.name,maxLines=2,minLines=2,overflow=TextOverflow.Ellipsis,style=MaterialTheme.typography.titleMedium,color=style.text)
        Text(channel.groupName.ifBlank { "电视直播" }+(if(channel.accessLevel=="member")" · 会员" else ""),style=MaterialTheme.typography.labelSmall,color=style.muted)
    }
}

@Composable internal fun LiveGroupChoice(tv:Boolean,label:String,selected:Boolean,tag:String,onClick:()->Unit) {
    val style=LocalCinemaStyle.current
    if(tv)LiveAction(true,(if(selected)"● " else "")+label,tag,onClick=onClick)
    else FilterChip(selected,onClick,label={ Text(label,color=style.text) },modifier=Modifier.testTag(tag),colors=FilterChipDefaults.filterChipColors(containerColor=style.surface,selectedContainerColor=style.accent.copy(alpha=.18f)))
}

@Composable internal fun LiveChoiceRow(tv:Boolean,title:String,detail:String,selected:Boolean,tag:String,modifier:Modifier=Modifier,onClick:()->Unit) {
    val style=LocalCinemaStyle.current
    var focused by remember { mutableStateOf(false) }
    val shape=RoundedCornerShape(12.dp)
    val content:@Composable ()->Unit={
        Row(Modifier.fillMaxWidth(),verticalAlignment=Alignment.CenterVertically,horizontalArrangement=Arrangement.spacedBy(12.dp)) {
            Icon(if(selected)Icons.Default.CheckCircle else Icons.Default.PlayCircleOutline,null,tint=if(focused)Color.White else style.accent,modifier=Modifier.size(24.dp))
            Column(Modifier.weight(1f),verticalArrangement=Arrangement.spacedBy(3.dp)) {
                Text(title,color=if(focused)Color.White else style.text,style=MaterialTheme.typography.titleSmall,maxLines=2,overflow=TextOverflow.Ellipsis)
                Text(detail,color=if(focused)Color.White.copy(alpha=.8f) else style.muted,style=MaterialTheme.typography.labelSmall,maxLines=2)
            }
        }
    }
    if(tv)androidx.tv.material3.Button(onClick=onClick,modifier=modifier.fillMaxWidth().onFocusChanged { focused=it.isFocused }.testTag(tag),contentPadding=PaddingValues(12.dp),colors=androidx.tv.material3.ButtonDefaults.colors(containerColor=if(selected)style.accent.copy(alpha=.15f) else style.surface,focusedContainerColor=style.accent,contentColor=style.text,focusedContentColor=Color.White)) { content() }
    else Surface(onClick=onClick,modifier=modifier.fillMaxWidth().testTag(tag),shape=shape,color=if(selected)style.accent.copy(alpha=.13f) else style.surface,border=BorderStroke(1.dp,if(selected)style.accent else style.muted.copy(alpha=.15f))) { Box(Modifier.padding(12.dp)) { content() } }
}

@Composable internal fun LiveAction(tv:Boolean,label:String,tag:String,modifier:Modifier=Modifier,enabled:Boolean=true,onClick:()->Unit) {
    var focused by remember { mutableStateOf(false) }
    if(tv)androidx.tv.material3.Button(onClick=onClick,enabled=enabled,modifier=modifier.onFocusChanged { focused=it.isFocused }.testTag(tag),contentPadding=PaddingValues(horizontal=12.dp,vertical=8.dp),colors=androidx.tv.material3.ButtonDefaults.colors(containerColor=CinemaSurface,contentColor=LocalCinemaStyle.current.text,focusedContainerColor=CinemaAccent,focusedContentColor=Color.White)) { Text(label,maxLines=1,color=if(focused)Color.White else LocalCinemaStyle.current.text) }
    else TextButton(onClick=onClick,enabled=enabled,modifier=modifier.testTag(tag)) { Text(label,maxLines=1) }
}
