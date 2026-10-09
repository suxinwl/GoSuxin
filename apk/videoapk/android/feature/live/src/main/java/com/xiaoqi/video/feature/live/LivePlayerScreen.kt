@file:androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
package com.xiaoqi.video.feature.live

import android.app.Activity
import android.content.Context
import android.content.ContextWrapper
import android.content.pm.ActivityInfo
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.*
import androidx.compose.foundation.gestures.detectVerticalDragGestures
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.*
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.key.*
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.media3.common.Player
import androidx.media3.common.VideoSize
import androidx.media3.ui.AspectRatioFrameLayout
import androidx.media3.ui.PlayerView
import com.xiaoqi.video.core.data.*
import com.xiaoqi.video.core.design.*
import com.xiaoqi.video.core.model.*
import com.xiaoqi.video.core.player.*
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay

private tailrec fun Context.liveActivity():Activity?=when(this) { is Activity->this;is ContextWrapper->baseContext.liveActivity();else->null }

/** Native live player: no film progress, episode navigation, downloads or VOD discovery. */
@Composable fun LivePlayerScreen(repo:AppRepository,tv:Boolean,onBack:()->Unit,onChannel:(LiveChannel)->Unit,onAccount:()->Unit={}) {
    val engine=PlayerHub.engine.live
    val state by engine.state.collectAsState()
    val activity=LocalContext.current.liveActivity()
    val style=LocalCinemaStyle.current
    var fullscreen by rememberSaveable { mutableStateOf(false) }
    var controls by remember { mutableStateOf(true) }
    var interaction by remember { mutableLongStateOf(System.currentTimeMillis()) }
    var controlFocused by remember { mutableStateOf(false) }
    var lines by remember { mutableStateOf(false) }
    var qualities by remember { mutableStateOf(false) }
    var channelMenu by remember { mutableStateOf(false) }
    var programmeMenu by remember { mutableStateOf(false) }
    var panelTab by rememberSaveable { mutableStateOf("channels") }
    var manualOrientation by rememberSaveable { mutableStateOf("") }
    var videoSize by remember(engine) { mutableStateOf(engine.player.videoSize) }
    var levelHud by remember { mutableStateOf<Pair<PlayerLevel,Int>?>(null) }
    var channels by remember { mutableStateOf<List<LiveChannel>>(emptyList()) }
    val touch=remember(activity) { PlayerTouchController(activity) }
    val originalOrientation=remember(activity) { activity?.requestedOrientation?:ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED }
    val surfaceFocus=remember { FocusRequester() }
    val playFocus=remember { FocusRequester() }
    val streams=state.descriptor?.streams?.takeIf { it.isNotEmpty() }?:state.channel?.streams.orEmpty()
    val member by repo.session.collectAsState()
    val heading=state.programme?.title?:state.descriptor?.programmeTitle?.takeIf { it.isNotBlank() }?:state.channel?.name.orEmpty()
    var todayEpg by remember(state.channel?.id) { mutableStateOf<LiveEpg?>(null) }
    val listChannels=(listOfNotNull(state.channel)+channels).distinctBy { it.id }
    val currentLine=state.descriptor?.let { descriptor->
        val index=streams.indexOfFirst { it.id==descriptor.streamId }.coerceAtLeast(0)
        LivePresentationRules.streamName(streams.find { it.id==descriptor.streamId }?:LiveStream(name=descriptor.streamName),index)
    }.orEmpty()

    LaunchedEffect(state.channel?.groupId,member?.user?.id) {
        try { channels=repo.liveChannels(group=state.channel?.groupId?:0,size=100).items }
        catch(e:CancellationException) { throw e }
        catch(_:Throwable) { channels=emptyList() }
    }
    LaunchedEffect(state.channel?.id,member?.user?.id) {
        val id=state.channel?.id?:return@LaunchedEffect
        while(true) {
            try { todayEpg=repo.liveEpg(id,LiveProgrammeRules.today()) }
            catch(e:CancellationException) { throw e } catch(_:Throwable) { todayEpg=null }
            delay(60000)
        }
    }
    DisposableEffect(engine,activity) {
        val listener=object:Player.Listener { override fun onVideoSizeChanged(size:VideoSize) { videoSize=size } }
        engine.player.addListener(listener)
        onDispose { engine.player.removeListener(listener);touch.restoreBrightness();if(!tv)activity?.requestedOrientation=originalOrientation }
    }
    LaunchedEffect(levelHud) { if(levelHud!=null) { delay(1200);levelHud=null } }
    LaunchedEffect(state.playing,interaction,fullscreen,controlFocused,lines,qualities,channelMenu,programmeMenu) {
        if(fullscreen&&state.playing&&!controlFocused&&!lines&&!qualities&&!channelMenu&&!programmeMenu) {
            delay(4500);if(System.currentTimeMillis()-interaction>=4000)controls=false
        }
    }
    LaunchedEffect(tv,controls,fullscreen,channelMenu,lines,qualities,programmeMenu) {
        if(tv&&!channelMenu&&!lines&&!qualities&&!programmeMenu) { delay(100);runCatching { if(controls)playFocus.requestFocus() else surfaceFocus.requestFocus() } }
    }
    val orientation=PlayerTouchRules.orientation(manualOrientation,videoSize.width*videoSize.pixelWidthHeightRatio,videoSize.height.toFloat(),false)
    SideEffect {
        if(!tv&&activity!=null) {
            val desired=if(fullscreen) { if(orientation==PlayerOrientation.Portrait)ActivityInfo.SCREEN_ORIENTATION_SENSOR_PORTRAIT else ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE } else originalOrientation
            if(activity.requestedOrientation!=desired)activity.requestedOrientation=desired
        }
    }
    DisposableEffect(activity,fullscreen) {
        val insets=activity?.window?.let { WindowCompat.getInsetsController(it,it.decorView) }
        if(fullscreen) {
            insets?.hide(WindowInsetsCompat.Type.systemBars())
            insets?.systemBarsBehavior=androidx.core.view.WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        } else insets?.show(WindowInsetsCompat.Type.systemBars())
        onDispose { insets?.show(WindowInsetsCompat.Type.systemBars()) }
    }
    fun touched() { controls=true;interaction=System.currentTimeMillis() }
    fun changeChannel(channel:LiveChannel) { LiveFocus.channelId=channel.id;onChannel(channel);touched();channelMenu=false }
    fun replay(programme:LiveProgramme) { engine.openReplay(programme);programmeMenu=false;touched() }
    fun returnLive() { if(state.eventReplay&&state.programme==null)channelMenu=true else engine.returnToLive();touched() }
    fun clock(ms:Long):String { val seconds=ms.coerceAtLeast(0)/1000;return if(seconds>=3600)"${seconds/3600}:${(seconds%3600/60).toString().padStart(2,'0')}:${(seconds%60).toString().padStart(2,'0')}" else "${seconds/60}:${(seconds%60).toString().padStart(2,'0')}" }
    BackHandler { if(fullscreen) { fullscreen=false;touched() } else onBack() }

    val controlBar:@Composable ()->Unit={
        Column(Modifier.fillMaxWidth().onFocusChanged { controlFocused=it.hasFocus }.padding(horizontal=8.dp,vertical=4.dp).testTag("live_player_controls")) {
            if(state.finite) {
                Slider(value=state.position.coerceIn(0,state.duration.coerceAtLeast(1)).toFloat(),onValueChange={ engine.seek(it.toLong());touched() },valueRange=0f..state.duration.coerceAtLeast(1).toFloat(),enabled=state.duration>0&&engine.player.isCurrentMediaItemSeekable,modifier=Modifier.fillMaxWidth().testTag("live_replay_seek"))
                Row(Modifier.fillMaxWidth(),horizontalArrangement=Arrangement.spacedBy(8.dp),verticalAlignment=Alignment.CenterVertically) {
                    Text("${clock(state.position)} / ${clock(state.duration)}",Modifier.weight(1f),color=if(fullscreen)Color.White else style.text,style=MaterialTheme.typography.labelSmall)
                    LiveAction(tv,"后退10秒","live_replay_back") { engine.seek(state.position-10000);touched() }
                    LiveAction(tv,"快进10秒","live_replay_forward") { engine.seek(state.position+10000);touched() }
                }
            }
            Row(Modifier.fillMaxWidth().then(if(tv)Modifier.horizontalScroll(rememberScrollState()) else Modifier),verticalAlignment=Alignment.CenterVertically,horizontalArrangement=Arrangement.spacedBy(if(tv)8.dp else 2.dp)) {
                LiveAction(tv,if(state.playing)"暂停" else "播放","live_play_pause",Modifier.focusRequester(playFocus)) { engine.toggle();touched() }
                Text(if(state.loading)"连接中" else if(state.ended)"回看结束" else if(state.finite)"回看" else if(state.playing)"● 实时" else "已暂停",Modifier.then(if(!tv)Modifier.weight(1f) else Modifier),color=if(fullscreen)Color.White else style.accent,style=MaterialTheme.typography.labelMedium)
                LiveAction(tv,if(state.eventReplay&&state.programme==null)"选择直播" else "返回直播","live_return_to_edge") { returnLive() }
                if(tv) {
                    LiveAction(true,"频道","live_channels_button") { channelMenu=true;touched() }
                    LiveAction(true,"线路（${streams.size}）","live_lines_button") { lines=true;touched() }
                    LiveAction(true,"清晰度：${state.quality}","live_quality_button") { qualities=true;touched() }
                    LiveAction(true,"节目单","live_epg_button") { programmeMenu=true;touched() }
                }
                LiveAction(tv,if(fullscreen)"退出全屏" else "全屏","live_fullscreen") { fullscreen=!fullscreen;touched() }
            }
            if(!tv)Row(Modifier.fillMaxWidth(),verticalAlignment=Alignment.CenterVertically) {
                LiveAction(false,"切换频道","live_channels_button",Modifier.weight(1f)) { channelMenu=true;touched() }
                LiveAction(false,"线路（${streams.size}）","live_lines_button",Modifier.weight(1f)) { lines=true;touched() }
                LiveAction(false,"清晰度","live_quality_button",Modifier.weight(1f)) { qualities=true;touched() }
                LiveAction(false,"节目单","live_epg_button",Modifier.weight(1f)) { programmeMenu=true;touched() }
            }
        }
    }
    val video:@Composable ()->Unit={
        Box(Modifier.fillMaxSize().background(Color.Black).testTag("live_player_video")) {
            AndroidView(factory={ context->PlayerView(context).apply {
                player=engine.player;useController=false;resizeMode=AspectRatioFrameLayout.RESIZE_MODE_FIT
                isFocusable=false;isFocusableInTouchMode=false;descendantFocusability=android.view.ViewGroup.FOCUS_BLOCK_DESCENDANTS
                setShutterBackgroundColor(android.graphics.Color.BLACK)
            } },update={ it.player=engine.player;it.keepScreenOn=state.playing },modifier=Modifier.fillMaxSize())
            Box(Modifier.fillMaxSize().focusRequester(surfaceFocus).onPreviewKeyEvent { key->
                if(!tv||key.type!=KeyEventType.KeyDown)return@onPreviewKeyEvent false
                when(key.key) {
                    Key.DirectionCenter,Key.Enter,Key.NumPadEnter,Key.DirectionLeft,Key.DirectionRight->{ touched();true }
                    Key.DirectionUp,Key.DirectionDown->{ channelMenu=true;touched();true }
                    Key.MediaPlayPause->{ engine.toggle();touched();true }
                    else->false
                }
            }.then(if(tv&&fullscreen&&!controls)Modifier.focusable() else if(!tv)Modifier.clickable { controls=!controls;interaction=System.currentTimeMillis() } else Modifier)
                .then(if(!tv)Modifier.pointerInput(touch) {
                    var side=PlayerLevel.Brightness;var initial=0f;var movement=0f
                    detectVerticalDragGestures(onDragStart={ point->side=PlayerTouchRules.side(point.x,size.width.toFloat());initial=touch.read(side);movement=0f },onVerticalDrag={ change,amount->
                        change.consume();movement+=amount
                        val value=touch.write(side,PlayerTouchRules.level(initial,movement,size.height.toFloat()))
                        levelHud=side to (value*100).toInt();interaction=System.currentTimeMillis()
                    })
                } else Modifier).testTag("live_player_surface"))
            if(levelHud!=null)Column(Modifier.align(Alignment.Center).background(Color.Black.copy(alpha=.75f),RoundedCornerShape(16.dp)).padding(20.dp),horizontalAlignment=Alignment.CenterHorizontally) {
                val (side,level)=levelHud!!
                Icon(if(side==PlayerLevel.Brightness)Icons.Default.Brightness6 else Icons.Default.VolumeUp,null,tint=Color.White)
                Text("${if(side==PlayerLevel.Brightness)"亮度" else "音量"} $level%",color=Color.White)
            }
            if(state.loading)Column(Modifier.align(Alignment.Center).background(Color.Black.copy(alpha=.6f)).padding(20.dp),horizontalAlignment=Alignment.CenterHorizontally,verticalArrangement=Arrangement.spacedBy(10.dp)) {
                CircularProgressIndicator(color=style.accent);Text(state.message,color=Color.White)
                if(state.failedStreamIds.isNotEmpty())Text("已跳过 ${state.failedStreamIds.size} 条不可用线路",color=Color.White,style=MaterialTheme.typography.labelSmall)
            }
            if(state.error.isNotBlank())Card(Modifier.align(Alignment.Center).padding(16.dp).widthIn(max=440.dp),colors=CardDefaults.cardColors(containerColor=style.surface,contentColor=style.text)) {
                Column(Modifier.padding(16.dp),horizontalAlignment=Alignment.CenterHorizontally,verticalArrangement=Arrangement.spacedBy(10.dp)) {
                    Text(state.error)
                    Row(horizontalArrangement=Arrangement.spacedBy(10.dp)) {
                        LiveAction(tv,"重试","live_retry") { engine.retry();touched() }
                        if(state.errorStatus==401||state.errorStatus==403)LiveAction(tv,"账号／会员","live_error_account",onClick=onAccount)
                        else if(state.finite)LiveAction(tv,if(state.eventReplay&&state.programme==null)"选择直播" else "返回直播","live_error_return") { returnLive() }
                        else LiveAction(tv,"选择线路","live_error_lines") { lines=true;touched() }
                        LiveAction(tv,"切换频道","live_error_channels") { channelMenu=true;touched() }
                    }
                }
            }
            if(fullscreen&&controls) {
                CompositionLocalProvider(LocalCinemaStyle provides style.copy(surface=Color.Black.copy(alpha=.6f),text=Color.White)) {
                    Row(Modifier.align(Alignment.TopCenter).fillMaxWidth().background(Color.Black.copy(alpha=.55f)).statusBarsPadding().padding(8.dp),verticalAlignment=Alignment.CenterVertically) {
                        LiveAction(tv,"退出全屏","live_exit_fullscreen") { fullscreen=false;touched() }
                        OverflowTitle(heading,Modifier.weight(1f).padding(horizontal=8.dp),color=Color.White)
                        if(!tv)LiveAction(false,if(orientation==PlayerOrientation.Portrait)"切换横屏" else "切换竖屏","live_orientation") { manualOrientation=if(orientation==PlayerOrientation.Portrait)PlayerOrientation.Landscape.name else PlayerOrientation.Portrait.name;touched() }
                        if(tv)LiveAction(true,"隐藏控制","live_hide_controls") { controls=false }
                    }
                    Box(Modifier.align(Alignment.BottomCenter).fillMaxWidth().background(Color.Black.copy(alpha=.75f)).navigationBarsPadding()) { controlBar() }
                }
            }
        }
    }
    val panel:@Composable ()->Unit={
        Column(Modifier.fillMaxSize().background(style.surface).padding(12.dp).testTag("live_player_channel_panel"),verticalArrangement=Arrangement.spacedBy(8.dp)) {
                Column(verticalArrangement=Arrangement.spacedBy(5.dp)) {
                    Text(heading,style=MaterialTheme.typography.titleLarge,color=style.text)
                    Text(if(currentLine.isBlank())state.message else "当前：$currentLine · ${state.quality}",color=style.muted,style=MaterialTheme.typography.bodySmall)
                    todayEpg?.now?.let { Text("正在播出：${it.title}",color=style.accent,style=MaterialTheme.typography.labelSmall) }
                    todayEpg?.next?.let { Text("下一档：${it.title} · ${LiveProgrammeRules.clock(it.start,todayEpg?.timezone?:"Asia/Shanghai")}",color=style.muted,style=MaterialTheme.typography.labelSmall) }
                    Row(horizontalArrangement=Arrangement.spacedBy(10.dp)) {
                        LiveGroupChoice(tv,"频道（${listChannels.size}）",panelTab=="channels","live_panel_channels_tab") { panelTab="channels" }
                        LiveGroupChoice(tv,"线路（${streams.size}）",panelTab=="streams","live_panel_streams_tab") { panelTab="streams" }
                        LiveGroupChoice(tv,"节目单",panelTab=="programmes","live_panel_programmes_tab") { panelTab="programmes" }
                    }
                }
            if(panelTab=="programmes")LiveProgrammePanel(state.channel?.id?:0,tv,state.programme?.id?:0,member?.user?.id?:0,loadProgrammes={ id,date->repo.liveEpg(id,date) },onReplay={ replay(it) })
            else LazyColumn(Modifier.weight(1f).fillMaxWidth(),verticalArrangement=Arrangement.spacedBy(8.dp)) {
            if(panelTab=="streams") {
                item { Text("连接失败会先刷新当前线路，再尝试备用。也可手动选择。",color=style.muted,style=MaterialTheme.typography.labelSmall) }
                if(streams.isEmpty())item { Text(if(state.loading)"正在获取线路…" else "当前频道暂无可用线路",color=style.muted) }
                itemsIndexed(streams,key={ _,it->it.id }) { index,stream->
                    LiveChoiceRow(tv,LivePresentationRules.streamName(stream,index),LivePresentationRules.qualityLabel(stream.quality)+" · "+LivePresentationRules.streamStatus(stream,stream.id in state.failedStreamIds),state.descriptor?.streamId==stream.id,"live_panel_stream_${stream.id}") { engine.selectStream(stream.id);touched() }
                }
            } else {
                item { Text("${state.channel?.groupName?.ifBlank { "电视" }?:"电视"} · 点击换台",color=style.muted,style=MaterialTheme.typography.labelSmall) }
                items(listChannels,key={ it.id }) { channel->LiveChoiceRow(tv,channel.name,if(channel.id==state.channel?.id)"正在观看" else channel.groupName.ifBlank { "电视直播" },channel.id==state.channel?.id,"live_panel_channel_${channel.id}") { changeChannel(channel) } }
                item { LiveAction(tv,"返回完整频道列表","live_all_channels",Modifier.fillMaxWidth(),onClick=onBack) }
            }
            }
        }
    }
    BoxWithConstraints(Modifier.fillMaxSize().background(style.background).testTag("live_player")) {
        val availableWidth=maxWidth
        if(fullscreen)video()
        else Column(Modifier.fillMaxSize().statusBarsPadding().navigationBarsPadding()) {
            Row(Modifier.fillMaxWidth().padding(8.dp),verticalAlignment=Alignment.CenterVertically) {
                LiveAction(tv,"返回","live_back",onClick=onBack)
                OverflowTitle(heading,Modifier.weight(1f).padding(horizontal=8.dp),style=MaterialTheme.typography.titleMedium)
                Text(if(state.finite)"回看" else "直播",color=style.accent,style=MaterialTheme.typography.labelMedium)
            }
            if(tv||availableWidth>=800.dp)Row(Modifier.weight(1f).fillMaxWidth(),horizontalArrangement=Arrangement.spacedBy(12.dp)) {
                Column(Modifier.weight(.7f).fillMaxHeight()) { Box(Modifier.weight(1f).fillMaxWidth()) { video() };controlBar() }
                Box(Modifier.weight(.3f).fillMaxHeight()) { panel() }
            } else {
                Box(Modifier.fillMaxWidth().aspectRatio(16f/9)) { video() }
                controlBar()
                Box(Modifier.weight(1f).fillMaxWidth()) { panel() }
            }
        }
    }
    if(lines)AlertDialog(onDismissRequest={ lines=false },title={ Text("选择直播线路") },text={ LazyColumn(Modifier.heightIn(max=360.dp)) {
        item { Text("仅显示源站提供的真实清晰度，备用线路可手动重试。",color=style.muted,style=MaterialTheme.typography.labelSmall,modifier=Modifier.padding(bottom=10.dp)) }
        itemsIndexed(streams,key={ _,it->it.id }) { index,stream->LiveChoiceRow(tv,LivePresentationRules.streamName(stream,index),LivePresentationRules.qualityLabel(stream.quality)+" · "+LivePresentationRules.streamStatus(stream,stream.id in state.failedStreamIds),stream.id==state.descriptor?.streamId,"live_dialog_stream_${stream.id}",Modifier.padding(bottom=8.dp)) { engine.selectStream(stream.id);lines=false;touched() } }
        if(streams.isEmpty())item { Text("暂无可用线路，可重试或切换频道") }
    } },confirmButton={ LiveAction(tv,"关闭","live_lines_close") { lines=false } })
    if(qualities)AlertDialog(onDismissRequest={ qualities=false },title={ Text("直播清晰度") },text={ LazyColumn(Modifier.heightIn(max=360.dp)) {
        item { LiveAction(tv,"自动","live_quality_auto") { engine.quality("auto","自动");qualities=false;touched() } }
        val available=(state.descriptor?.qualities.orEmpty().filter { it.url.isNotBlank() }.map { it.id to LivePresentationRules.qualityLabel(it.label,it.height,it.width) }+state.trackQualities).distinctBy { it.second }
        items(available,key={ it.first }) { (id,label)->LiveAction(tv,label,"live_quality_$id") { engine.quality(id,label);qualities=false;touched() } }
        if(available.isEmpty())item { Text(if(state.loading)"连接成功后会检测清晰度" else "当前线路只有原始清晰度",color=style.muted,style=MaterialTheme.typography.bodySmall) }
    } },confirmButton={ LiveAction(tv,"关闭","live_quality_close") { qualities=false } })
    if(channelMenu)AlertDialog(onDismissRequest={ channelMenu=false },title={ Text("选择直播频道") },text={ LazyColumn(Modifier.heightIn(max=360.dp)) {
        items(listChannels,key={ it.id }) { channel->LiveChoiceRow(tv,channel.name,channel.groupName.ifBlank { "电视直播" },state.channel?.id==channel.id,"live_dialog_channel_${channel.id}",Modifier.padding(bottom=8.dp)) { changeChannel(channel) } }
    } },confirmButton={ LiveAction(tv,"关闭","live_channels_close") { channelMenu=false } })
    if(programmeMenu)AlertDialog(onDismissRequest={ programmeMenu=false },title={ Text("节目单／回看") },text={
        Box(Modifier.height(430.dp).fillMaxWidth()) {
            LiveProgrammePanel(state.channel?.id?:0,tv,state.programme?.id?:0,member?.user?.id?:0,loadProgrammes={ id,date->repo.liveEpg(id,date) },onReplay={ replay(it) })
        }
    },confirmButton={ LiveAction(tv,"关闭","live_epg_close") { programmeMenu=false } })
}
