@file:androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
package com.xiaoqi.video.feature.catalog

import android.app.Activity
import android.app.PictureInPictureParams
import android.content.Context
import android.content.ContextWrapper
import android.content.pm.ActivityInfo
import android.os.Build
import android.util.Rational
import androidx.activity.compose.BackHandler
import androidx.activity.ComponentActivity
import androidx.compose.foundation.*
import androidx.compose.foundation.gestures.detectVerticalDragGestures
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.*
import androidx.compose.foundation.lazy.grid.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.*
import androidx.compose.material.icons.automirrored.filled.VolumeUp
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
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.app.PictureInPictureModeChangedInfo
import androidx.core.util.Consumer
import androidx.media3.ui.PlayerView
import androidx.media3.ui.AspectRatioFrameLayout
import androidx.media3.common.Player
import androidx.media3.common.VideoSize
import com.xiaoqi.video.core.data.AppRepository
import com.xiaoqi.video.core.design.*
import com.xiaoqi.video.core.cast.*
import com.xiaoqi.video.core.model.*
import com.xiaoqi.video.core.player.*
import com.xiaoqi.video.core.network.JsonWire.string
import com.xiaoqi.video.core.network.JsonWire.long
import kotlinx.coroutines.*

internal fun Context.activity():Activity?=when(this) { is Activity->this;is ContextWrapper->baseContext.activity();else->null }

/** The video and its CMS operations share one native screen on every device. */
@Composable fun PlayerScreen(
    repo:AppRepository,
    tv:Boolean,
    onBack:()->Unit,
    onLogin:()->Unit={},
    onFilm:(Long)->Unit={},
    onMembership:()->Unit=onLogin
) {
    val engine=PlayerHub.engine
    val localState by engine.state.collectAsState()
    val context=LocalContext.current
    val activity=context.activity()
    val scope=rememberCoroutineScope()
    val snackbar=remember { SnackbarHostState() }
    val notice by repo.notice.collectAsState()
    LaunchedEffect(notice) { if(notice.isNotBlank()) { snackbar.showSnackbar(notice);repo.notice.value="" } }
    // TV starts with its line/episode panel visible. Fullscreen is a user action.
    var fullscreen by rememberSaveable { mutableStateOf(false) }
    var manualOrientation by rememberSaveable(localState.detail?.film?.id) { mutableStateOf("") }
    var videoSize by remember(engine) { mutableStateOf(engine.player.videoSize) }
    val touchController=remember(activity) { PlayerTouchController(activity) }
    var levelHud by remember { mutableStateOf<Pair<PlayerLevel,Int>?>(null) }
    val originalOrientation=remember(activity) { activity?.requestedOrientation?:ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED }
    DisposableEffect(engine,activity) {
        val listener=object:Player.Listener { override fun onVideoSizeChanged(size:VideoSize) { videoSize=size } }
        engine.player.addListener(listener)
        onDispose {
            engine.player.removeListener(listener)
            touchController.restoreBrightness()
            if(!tv)activity?.requestedOrientation=originalOrientation
        }
    }
    LaunchedEffect(levelHud) { if(levelHud!=null) { delay(1200);levelHud=null } }
    var controls by remember { mutableStateOf(true) }
    var qualityMenu by remember { mutableStateOf(false) }
    var speedMenu by remember { mutableStateOf(false) }
    var subtitleMenu by remember { mutableStateOf(false) }
    var lineMenu by remember { mutableStateOf(false) }
    var castDialog by remember { mutableStateOf(false) }
    var slider by remember { mutableStateOf<Float?>(null) }
    var interaction by remember { mutableLongStateOf(System.currentTimeMillis()) }
    var controlFocused by remember { mutableStateOf(false) }
    val surfaceFocus=remember { FocusRequester() }
    val playFocus=remember { FocusRequester() }
    val panelFocus=remember { FocusRequester() }
    val infoListState=rememberLazyListState()
    val cast=remember { CastHub.device }
    val dlna=remember { CastHub.dlna }
    val castName by CastHub.name.collectAsState()
    val castKind by CastHub.kind.collectAsState()
    val casting=castKind.isNotBlank()
    val remote by cast.remoteStatus.collectAsState()
    val dlnaState by CastHub.dlnaStatus.collectAsState()
    val state=when(castKind) {
        "tv" -> localState.copy(playing=remote.string("playing")=="true",position=remote.long("position_ms",fallback=localState.position),duration=remote.long("duration_ms",fallback=localState.duration),requested=null,descriptor=localState.descriptor?.let { p->
            val line=remote.string("line",fallback=p.line);val key=remote.string("episode_key",fallback=p.episodeKey)
            val source=localState.detail?.sources?.find { it.code==line }
            val version=remote.string("version_key",fallback=if(line==p.line)p.versionKey else source?.versionKey.orEmpty())
            val episode=source?.takeIf { it.versionKey==version }?.episodes?.find { it.key==key }
            p.copy(line=line,episodeKey=key,versionKey=version,
                episode=remote.long("episode",fallback=p.episode.toLong()).toInt(),
                name=remote.string("episode_name",fallback=episode?.name?:p.name.takeIf { line==p.line&&key==p.episodeKey&&version==p.versionKey }.orEmpty()))
        })
        "dlna" -> localState.copy(playing=dlnaState.playing,position=dlnaState.position,duration=dlnaState.duration.takeIf { it>0 }?:localState.duration)
        else -> localState
    }
    // Completed warmup and fast source resolution should not flash a blocking loading panel.
    var showLoading by remember { mutableStateOf(false) }
    LaunchedEffect(state.loading) {
        showLoading=false
        if(state.loading) { delay(350);showLoading=true }
    }
    val playbackTitle=listOf(state.detail?.film?.name.orEmpty(),state.episodeLabel).filter { it.isNotBlank() }.joinToString(" · ").ifBlank { state.descriptor?.name.orEmpty() }
    var pip by remember(activity) { mutableStateOf(Build.VERSION.SDK_INT>=24 && activity?.isInPictureInPictureMode==true) }
    DisposableEffect(activity) {
        val listener=Consumer<PictureInPictureModeChangedInfo> { pip=it.isInPictureInPictureMode }
        (activity as? ComponentActivity)?.addOnPictureInPictureModeChangedListener(listener)
        onDispose { (activity as? ComponentActivity)?.removeOnPictureInPictureModeChangedListener(listener) }
    }
    fun touched() { controls=true;interaction=System.currentTimeMillis() }
    fun tvCommand(action:String,identity:PlaybackIdentity?=null,position:Long=0) { runCatching { cast.command(action,identity,position) }.onFailure(repo::error) }
    fun togglePlay() {
        touched()
        when(castKind) {
            "tv" -> tvCommand(if(state.playing)"pause" else "resume")
            "dlna" -> scope.launch { runCatching { if(state.playing)dlna.pause() else dlna.resume() }.onFailure(repo::error) }
            else -> engine.toggle()
        }
    }
    fun seek(position:Long) {
        touched()
        val target=position.coerceIn(0,state.duration.coerceAtLeast(0))
        when(castKind) {
            "tv" -> tvCommand("seek",position=target)
            "dlna" -> scope.launch { runCatching { dlna.seek(target) }.onFailure(repo::error) }
            else -> engine.seek(target)
        }
    }
    fun selectEpisode(source:Source,episode:Episode) {
        val detail=state.detail?:return
        val identity=PlaybackIdentity(detail.film.id,source.code,episode.key,source.episodes.indexOf(episode),manual=true,versionKey=source.versionKey)
        if(castKind=="tv")tvCommand("play",identity)
        else {
            engine.selectEpisode(source,episode)
            if(castKind=="dlna") { engine.player.pause();castDialog=true }
        }
        touched()
    }
    fun selectSource(source:Source) {
        if(castKind=="tv") {
            val current=state.detail?.sources?.find { it.code==state.descriptor?.line }?.episodes?.find { it.key==state.descriptor?.episodeKey }
            val sameVersion=source.versionKey==state.descriptor?.versionKey
            val episode=if(sameVersion)source.episodes.find { it.number==current?.number }?:source.episodes.getOrNull(state.descriptor?.episode?:0) else source.episodes.firstOrNull()
            if(episode!=null)tvCommand("play",PlaybackIdentity(state.detail?.film?.id?:0,source.code,episode.key,source.episodes.indexOf(episode),positionMs=if(sameVersion)state.position else 0,manual=true,versionKey=source.versionKey))
        } else {
            engine.selectSource(source)
            if(castKind=="dlna") { engine.player.pause();castDialog=true }
        }
        touched()
    }
    fun changeQuality(id:String,label:String) {
        if(castKind=="tv")tvCommand("quality",PlaybackIdentity(state.descriptor?.vodId?:0,quality=id))
        else { engine.quality(id,label);if(castKind=="dlna")castDialog=true }
        qualityMenu=false;touched()
    }
    val next=state.detail?.sources?.find { it.code==state.descriptor?.line }?.let { PlaybackRules.nextEpisode(it,state.descriptor?.episodeKey.orEmpty()) }
    fun nextEpisode() {
        if(next==null)return
        touched()
        if(castKind=="tv")tvCommand("next") else { engine.next();if(castKind=="dlna") { engine.player.pause();castDialog=true } }
    }
    fun hideControls() { controls=false;scope.launch { delay(50);runCatching { surfaceFocus.requestFocus() } } }
    fun fullscreenToggle() { fullscreen=!fullscreen;touched() }
    fun enterPip() {
        if(tv||Build.VERSION.SDK_INT<26)return
        val format=engine.player.videoFormat
        val ratio=if(format!=null&&format.width>0&&format.height>0)Rational(format.width,format.height) else Rational(16,9)
        runCatching { activity?.enterPictureInPictureMode(PictureInPictureParams.Builder().setAspectRatio(ratio.takeIf { it.toFloat() in .418f..2.39f }?:Rational(16,9)).build())==true }
            .onSuccess { if(!it)repo.notice.value="当前设备不支持画中画" }.onFailure(repo::error)
    }
    fun showInfo() { fullscreen=false;touched();scope.launch {
        infoListState.scrollToItem(0)
        delay(180)
        runCatching { panelFocus.requestFocus() }
    } }
    LaunchedEffect(state.playing,interaction,fullscreen,controlFocused,qualityMenu,speedMenu,subtitleMenu,lineMenu) {
        if(fullscreen && state.playing && !controlFocused && !qualityMenu && !speedMenu && !subtitleMenu && !lineMenu) {
            delay(4500)
            if(System.currentTimeMillis()-interaction>=4000)controls=false
        }
    }
    LaunchedEffect(tv,fullscreen,controls) {
        if(tv) {
            delay(80)
            runCatching { if(controls)playFocus.requestFocus() else surfaceFocus.requestFocus() }
        }
    }
    val screenOrientation=PlayerTouchRules.orientation(manualOrientation,videoSize.width*videoSize.pixelWidthHeightRatio,videoSize.height.toFloat(),state.detail?.film?.isShort==true || state.detail?.film?.typeName?.contains("短剧")==true)
    SideEffect {
        if(!tv && activity!=null) {
            val desired=if(fullscreen&&!pip) {
                if(screenOrientation==PlayerOrientation.Portrait)ActivityInfo.SCREEN_ORIENTATION_SENSOR_PORTRAIT else ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE
            } else originalOrientation
            if(activity.requestedOrientation!=desired)activity.requestedOrientation=desired
        }
    }
    DisposableEffect(activity,fullscreen,pip) {
        val controller=activity?.window?.let { WindowCompat.getInsetsController(it,it.decorView) }
        if(fullscreen&&!pip) {
            controller?.hide(WindowInsetsCompat.Type.systemBars())
            controller?.systemBarsBehavior=androidx.core.view.WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        } else {
            controller?.show(WindowInsetsCompat.Type.systemBars())
        }
        onDispose { controller?.show(WindowInsetsCompat.Type.systemBars()) }
    }
    BackHandler { if(fullscreen) { fullscreen=false;touched() } else onBack() }

    val video:@Composable ()->Unit={
        Box(Modifier.fillMaxSize().background(Color.Black).testTag("player_video")) {
            AndroidView(factory={ ctx->PlayerView(ctx).apply {
                player=engine.player;useController=false;resizeMode=AspectRatioFrameLayout.RESIZE_MODE_FIT
                isFocusable=false;isFocusableInTouchMode=false;descendantFocusability=android.view.ViewGroup.FOCUS_BLOCK_DESCENDANTS
                setShutterBackgroundColor(android.graphics.Color.BLACK)
            } },update={ it.player=engine.player;it.keepScreenOn=state.playing },modifier=Modifier.fillMaxSize())
            Box(Modifier.fillMaxSize().testTag("player_surface").focusRequester(surfaceFocus).onPreviewKeyEvent { event->
                if(!tv || event.type!=KeyEventType.KeyDown)return@onPreviewKeyEvent false
                when(event.key) {
                    Key.DirectionLeft -> { seek(state.position-10000);true }
                    Key.DirectionRight -> { seek(state.position+10000);true }
                    Key.DirectionCenter,Key.Enter,Key.NumPadEnter -> { togglePlay();true }
                    Key.DirectionUp,Key.DirectionDown -> { touched();scope.launch { delay(50);runCatching { playFocus.requestFocus() } };true }
                    Key.MediaPlayPause -> { togglePlay();true }
                    else -> false
                }
            }.then(if(tv&&fullscreen&&!controls)Modifier.focusable() else if(!tv)Modifier.clickable { controls=!controls;interaction=System.currentTimeMillis() } else Modifier)
                .then(if(!tv&&!pip&&!casting)Modifier.pointerInput(touchController) {
                    var kind=PlayerLevel.Brightness
                    var initial=0f
                    var movement=0f
                    detectVerticalDragGestures(onDragStart={ offset ->
                        kind=PlayerTouchRules.side(offset.x,size.width.toFloat())
                        initial=touchController.read(kind);movement=0f
                    },onVerticalDrag={ change,amount ->
                        change.consume();movement+=amount
                        val actual=touchController.write(kind,PlayerTouchRules.level(initial,movement,size.height.toFloat()))
                        levelHud=kind to (actual*100).toInt()
                        interaction=System.currentTimeMillis()
                    })
                } else Modifier))
            if(levelHud!=null&&!pip) {
                val (kind,percent)=levelHud!!
                Column(Modifier.align(Alignment.Center).background(Color.Black.copy(alpha=.75f),RoundedCornerShape(16.dp)).padding(20.dp).testTag("player_level_hud"),horizontalAlignment=Alignment.CenterHorizontally) {
                    Icon(if(kind==PlayerLevel.Brightness)Icons.Default.Brightness6 else Icons.AutoMirrored.Filled.VolumeUp,contentDescription=null,tint=Color.White,modifier=Modifier.size(32.dp))
                    Text("${if(kind==PlayerLevel.Brightness)"亮度" else "音量"} $percent%",color=Color.White)
                }
            }
            if(casting&&!pip)Column(Modifier.align(Alignment.Center).background(Color.Black.copy(alpha=.8f)).padding(24.dp),horizontalAlignment=Alignment.CenterHorizontally) {
                Icon(Icons.Default.Cast,"投屏中",tint=CinemaAccent,modifier=Modifier.size(48.dp))
                Text("正在 $castName 播放",color=Color.White)
                Text("${formatTime(state.position)} / ${formatTime(state.duration)}",color=Color.White)
            }
            if(state.loading&&showLoading&&!pip)Column(Modifier.align(Alignment.Center).background(Color.Black.copy(alpha=.55f)).padding(20.dp),horizontalAlignment=Alignment.CenterHorizontally) {
                CircularProgressIndicator(color=CinemaAccent)
                Spacer(Modifier.height(12.dp))
                Text(state.loadingMessage,color=Color.White)
                if(tv)Text("可在右侧选择其他线路",color=Color(0xFFBAC7D7),style=MaterialTheme.typography.bodySmall)
            }
            if(state.error.isNotBlank()&&!pip)Card(Modifier.align(Alignment.Center).padding(12.dp).widthIn(max=440.dp),colors=CardDefaults.cardColors(containerColor=CinemaSurface,contentColor=LocalCinemaStyle.current.text)) {
                Column(Modifier.padding(14.dp),horizontalAlignment=Alignment.CenterHorizontally,verticalArrangement=Arrangement.spacedBy(6.dp)) {
                    Text(if(state.discovery.running)"当前线路暂不可用，正在查找更多片源" else state.error,style=MaterialTheme.typography.bodyMedium)
                    if(state.discovery.summary.isNotBlank())Text(state.discovery.summary,color=MaterialTheme.colorScheme.onSurfaceVariant,style=MaterialTheme.typography.labelSmall)
                    if(state.offline&&state.detail==null)PlayerAction(tv,"返回片库","player_offline_back",onClick=onBack)
                    else FlowRow(horizontalArrangement=Arrangement.spacedBy(8.dp),verticalArrangement=Arrangement.spacedBy(4.dp)) {
                        PlayerAction(tv,"重试","player_retry",onClick={
                            if(!engine.retryCurrent())repo.notice.value="暂无可重试的影片，请返回后重新打开"
                        })
                        if(!state.offline)PlayerAction(tv,if(state.discovery.running)"查找中…" else "查找更多片源","player_error_lines",enabled=!state.discovery.running&&PlaybackRecoveryRules.canDiscover(state.offline,state.errorStatus,state.detail?.film?.id?:0),onClick={ engine.discoverMoreSources();touched() })
                        if(state.errorStatus==401||state.errorStatus==403||state.detail?.film?.vip==true||state.detail?.film?.points?.let { it>0 }==true)PlayerAction(tv,if(state.errorStatus==401)"登录" else "会员 / 解锁","player_error_membership",onClick=onMembership)
                    }
                }
            }
            if(fullscreen&&controls&&!pip) {
                Row(Modifier.align(Alignment.TopStart).fillMaxWidth().background(Color.Black.copy(alpha=.55f)).statusBarsPadding().padding(8.dp),verticalAlignment=Alignment.CenterVertically) {
                    PlayerAction(tv,"退出全屏","player_fullscreen_exit",onClick={ fullscreen=false;touched() })
                    OverflowTitle(playbackTitle,Modifier.weight(1f).padding(start=12.dp),color=Color.White,style=LocalTextStyle.current)
                    if(!tv)PlayerAction(false,if(screenOrientation==PlayerOrientation.Portrait)"切换横屏" else "切换竖屏","player_orientation",onClick={
                        manualOrientation=if(screenOrientation==PlayerOrientation.Portrait)PlayerOrientation.Landscape.name else PlayerOrientation.Portrait.name
                        touched()
                    })
                    if(tv)PlayerAction(true,"隐藏控制","player_hide_controls",onClick=::hideControls)
                }
                Column(Modifier.align(Alignment.BottomCenter).fillMaxWidth().background(Color.Black.copy(alpha=.75f)).navigationBarsPadding().padding(12.dp).onFocusChanged { controlFocused=it.hasFocus }.focusGroup()) {
                    PlayerSeekBar(state,slider,{ slider=it;touched() },{ slider?.let { seek(it.toLong()) };slider=null })
                    PlaybackControls(tv,state,playFocus,fullscreen,next!=null,casting,
                        ::togglePlay,{ seek(state.position-10000) },{ seek(state.position+10000) },::nextEpisode,
                        { lineMenu=true;touched() },{ qualityMenu=true;touched() },{ speedMenu=true;touched() },
                        { subtitleMenu=true;touched() },{ castDialog=true;touched() },::fullscreenToggle,
                        onPip=::enterPip,onInfo=::showInfo)
                }
            }
        }
    }
    val panel:@Composable ()->Unit={
        PlaybackInfoPanel(repo,tv,state,onLogin,onMembership,onFilm,panelFocus,infoListState,::selectEpisode,::selectSource)
    }
    Box(Modifier.fillMaxSize().background(CinemaBackground).onPreviewKeyEvent { event->
        if(tv&&event.type==KeyEventType.KeyDown&&event.key==Key.MediaPlayPause) { togglePlay();true } else false
    }) {
        if(fullscreen||pip)video()
        else BoxWithConstraints(Modifier.fillMaxSize()) {
            val wide=tv||maxWidth>=800.dp
            Column(Modifier.fillMaxSize().systemBarsPadding().padding(if(tv)24.dp else 0.dp)) {
                Row(Modifier.fillMaxWidth().padding(horizontal=if(tv)0.dp else 12.dp,vertical=8.dp),verticalAlignment=Alignment.CenterVertically) {
                    PlayerAction(tv,"返回","player_back",onClick=onBack)
                    OverflowTitle(playbackTitle,Modifier.weight(1f).padding(horizontal=8.dp),style=MaterialTheme.typography.titleMedium,color=LocalCinemaStyle.current.text)
                    if(casting)Text("投屏至 $castName",color=CinemaAccent)
                }
                if(wide)Row(Modifier.weight(1f).fillMaxWidth(),horizontalArrangement=Arrangement.spacedBy(20.dp)) {
                    Column(Modifier.weight(.66f).fillMaxHeight()) {
                        Box(Modifier.weight(1f).fillMaxWidth()) { video() }
                        Column(Modifier.fillMaxWidth().background(CinemaSurface).padding(10.dp)) {
                            PlayerSeekBar(state,slider,{ slider=it;touched() },{ slider?.let { seek(it.toLong()) };slider=null })
                            PlaybackControls(tv,state,playFocus,false,next!=null,casting,::togglePlay,{ seek(state.position-10000) },{ seek(state.position+10000) },::nextEpisode,{ lineMenu=true },{ qualityMenu=true },{ speedMenu=true },{ subtitleMenu=true },{ castDialog=true },::fullscreenToggle,onPip=::enterPip,onInfo=::showInfo)
                        }
                    }
                    Box(Modifier.weight(.34f).fillMaxHeight().testTag("player_info_panel")) { panel() }
                } else {
                    Box(Modifier.fillMaxWidth().aspectRatio(16f/9)) { video() }
                    Column(Modifier.fillMaxWidth().background(CinemaSurface).padding(horizontal=8.dp)) {
                        PlayerSeekBar(state,slider,{ slider=it;touched() },{ slider?.let { seek(it.toLong()) };slider=null })
                        PlaybackControls(tv,state,playFocus,false,next!=null,casting,::togglePlay,{ seek(state.position-10000) },{ seek(state.position+10000) },::nextEpisode,{ lineMenu=true },{ qualityMenu=true },{ speedMenu=true },{ subtitleMenu=true },{ castDialog=true },::fullscreenToggle,
                            onPip=::enterPip,onInfo=::showInfo)
                    }
                    Box(Modifier.weight(1f).fillMaxWidth().testTag("player_info_panel")) { panel() }
                }
            }
        }
        SnackbarHost(snackbar,Modifier.align(Alignment.BottomCenter).navigationBarsPadding())
    }
    if(lineMenu)AlertDialog(onDismissRequest={ lineMenu=false },title={ Text("切换线路") },text={ LazyColumn(Modifier.heightIn(max=360.dp)) {
        items(state.detail?.sources.orEmpty(),key={ it.code }) { source->PlayerAction(tv,(if(source.code==state.descriptor?.line)"✓ " else "")+source.name,"player_line_dialog_${source.code}",modifier=Modifier.fillMaxWidth(),enabled=source.episodes.isNotEmpty(),onClick={ selectSource(source);lineMenu=false }) }
        if(state.detail?.sources.isNullOrEmpty())item { Text("暂无可用线路，请在播放页查找更多片源") }
    } },confirmButton={ PlayerAction(tv,"关闭","player_lines_close",onClick={ lineMenu=false }) },dismissButton={ if(!state.offline)PlayerAction(tv,"查找更多片源","player_lines_discover",enabled=!state.discovery.running,onClick={ engine.discoverMoreSources();lineMenu=false;touched() }) })
    if(qualityMenu)AlertDialog(onDismissRequest={ qualityMenu=false },title={ Text("清晰度") },text={ LazyColumn {
        item { PlayerAction(tv,"自动","player_quality_auto",onClick={ changeQuality("auto","自动") }) }
        items((state.descriptor?.qualities.orEmpty().map { it.id to it.name }+state.trackQualities).distinctBy { it.second }) { (id,name)->PlayerAction(tv,name,"player_quality_$id",onClick={ changeQuality(id,name) }) }
    } },confirmButton={ PlayerAction(tv,"关闭","player_quality_close",onClick={ qualityMenu=false }) })
    if(speedMenu)AlertDialog(onDismissRequest={ speedMenu=false },title={ Text("播放速度") },text={ LazyColumn { items(listOf(.75f,1f,1.25f,1.5f,2f)) { speed->PlayerAction(tv,"${speed}×","player_speed_$speed",onClick={ engine.player.setPlaybackSpeed(speed);speedMenu=false }) } } },confirmButton={ PlayerAction(tv,"关闭","player_speed_close",onClick={ speedMenu=false }) })
    if(subtitleMenu)AlertDialog(onDismissRequest={ subtitleMenu=false },title={ Text("字幕") },text={ LazyColumn {
        item { PlayerAction(tv,"关闭字幕","player_subtitle_off",onClick={ engine.subtitle("off");subtitleMenu=false }) }
        items(state.subtitles) { (id,name)->PlayerAction(tv,name,"player_subtitle_$id",onClick={ engine.subtitle(id);subtitleMenu=false }) }
    } },confirmButton={ PlayerAction(tv,"关闭","player_subtitle_close",onClick={ subtitleMenu=false }) })
    if(castDialog)CastDialog(repo,cast,dlna,state,onDismiss={ castDialog=false },onConnected={ name,kind->CastHub.connected(name,kind);castDialog=false;engine.player.pause() },onStopped={ CastHub.end() })
}

@Composable internal fun PlayerAction(tv:Boolean,label:String,tag:String,modifier:Modifier=Modifier,enabled:Boolean=true,onClick:()->Unit) {
    var focused by remember { mutableStateOf(false) }
    if(tv)androidx.tv.material3.Button(onClick=onClick,enabled=enabled,modifier=modifier.testTag(tag).onFocusChanged { focused=it.isFocused },contentPadding=PaddingValues(horizontal=12.dp,vertical=8.dp),colors=androidx.tv.material3.ButtonDefaults.colors(containerColor=CinemaSurface,contentColor=LocalCinemaStyle.current.text,focusedContainerColor=CinemaAccent,focusedContentColor=Color.White)) { Text(label,maxLines=1,color=if(focused)Color.White else LocalCinemaStyle.current.text) }
    else TextButton(onClick=onClick,enabled=enabled,modifier=modifier.testTag(tag),contentPadding=PaddingValues(horizontal=10.dp,vertical=6.dp)) { Text(label,maxLines=1,overflow=TextOverflow.Ellipsis) }
}

@Composable private fun PlayerSeekBar(state:PlayerState,slider:Float?,onChange:(Float)->Unit,onFinish:()->Unit) {
    Slider(value=(slider?:state.position.toFloat()).coerceIn(0f,state.duration.coerceAtLeast(1).toFloat()),onValueChange=onChange,onValueChangeFinished=onFinish,valueRange=0f..state.duration.coerceAtLeast(1).toFloat(),enabled=state.duration>0,modifier=Modifier.fillMaxWidth().height(30.dp).testTag("player_seek"))
}

@Composable private fun PlaybackControls(
    tv:Boolean,state:PlayerState,playFocus:FocusRequester,fullscreen:Boolean,hasNext:Boolean,casting:Boolean,
    onPlay:()->Unit,onRewind:()->Unit,onForward:()->Unit,onNext:()->Unit,onLines:()->Unit,onQuality:()->Unit,
    onSpeed:()->Unit,onSubtitle:()->Unit,onCast:()->Unit,onFullscreen:()->Unit,onPip:()->Unit={},onInfo:()->Unit={}
) {
    if(!tv) {
        var more by remember { mutableStateOf(false) }
        val foreground=if(fullscreen)Color.White else LocalCinemaStyle.current.text
        Column(Modifier.fillMaxWidth().testTag("player_controls")) {
            Row(Modifier.fillMaxWidth(),verticalAlignment=Alignment.CenterVertically,horizontalArrangement=Arrangement.spacedBy(4.dp)) {
                IconButton(onClick=onPlay,modifier=Modifier.focusRequester(playFocus).testTag("player_play_pause")) {
                    Icon(if(state.playing)Icons.Default.Pause else Icons.Default.PlayArrow,if(state.playing)"暂停" else "播放",tint=foreground)
                }
                Text("${formatTime(state.position)} / ${formatTime(state.duration)}",Modifier.weight(1f),color=foreground,style=MaterialTheme.typography.labelMedium,maxLines=1)
                if(hasNext)IconButton(onClick=onNext,modifier=Modifier.testTag("player_next")) { Icon(Icons.Default.SkipNext,"下一集",tint=foreground) }
                IconButton(onClick=onFullscreen,modifier=Modifier.testTag("player_fullscreen_toggle")) { Icon(if(fullscreen)Icons.Default.FullscreenExit else Icons.Default.Fullscreen,if(fullscreen)"退出全屏" else "全屏",tint=foreground) }
                Box {
                    IconButton(onClick={ more=true },modifier=Modifier.testTag("player_more")) { Icon(Icons.Default.MoreVert,"更多播放功能",tint=foreground) }
                    DropdownMenu(more,{ more=false }) {
                        fun action(block:()->Unit) { more=false;block() }
                        DropdownMenuItem(text={ Text("选集 / 影片信息") },onClick={ action(onInfo) })
                        if(!casting)DropdownMenuItem(text={ Text("播放倍速") },onClick={ action(onSpeed) })
                        if(!casting&&state.subtitles.isNotEmpty())DropdownMenuItem(text={ Text("字幕") },onClick={ action(onSubtitle) })
                        if(!state.offline)DropdownMenuItem(text={ Text("投屏") },onClick={ action(onCast) })
                        if(Build.VERSION.SDK_INT>=26)DropdownMenuItem(text={ Text("画中画") },onClick={ action(onPip) })
                        DropdownMenuItem(text={ Text("后退 10 秒") },enabled=state.duration>0,onClick={ action(onRewind) })
                        DropdownMenuItem(text={ Text("前进 10 秒") },enabled=state.duration>0,onClick={ action(onForward) })
                    }
                }
            }
            FlowRow(Modifier.fillMaxWidth(),horizontalArrangement=Arrangement.spacedBy(6.dp),verticalArrangement=Arrangement.spacedBy(2.dp)) {
                if(!state.offline)PlayerAction(false,"切换线路","player_lines",onClick=onLines)
                PlayerAction(false,"清晰度：${state.quality}","player_quality",modifier=Modifier.widthIn(max=180.dp),onClick=onQuality)
                PlayerAction(false,"选集 / 信息","player_info",onClick=onInfo)
            }
        }
        return
    }
    LazyRow(Modifier.fillMaxWidth().testTag("player_controls"),horizontalArrangement=Arrangement.spacedBy(6.dp),verticalAlignment=Alignment.CenterVertically) {
        item { PlayerAction(tv,if(state.playing)"暂停" else "播放","player_play_pause",Modifier.focusRequester(playFocus),onClick=onPlay) }
        item { Text("${formatTime(state.position)} / ${formatTime(state.duration)}",color=if(fullscreen)Color.White else MaterialTheme.colorScheme.onSurface,style=MaterialTheme.typography.labelMedium,modifier=Modifier.padding(horizontal=4.dp)) }
        item { PlayerAction(tv,if(fullscreen)"退出全屏" else "全屏","player_fullscreen_toggle",onClick=onFullscreen) }
        item { PlayerAction(tv,"选集 / 信息","player_info",onClick=onInfo) }
        if(hasNext)item { PlayerAction(tv,"下一集","player_next",onClick=onNext) }
        item { PlayerAction(tv,"后退10秒","player_rewind",enabled=state.duration>0,onClick=onRewind) }
        item { PlayerAction(tv,"前进10秒","player_forward",enabled=state.duration>0,onClick=onForward) }
        if(!state.offline)item { PlayerAction(tv,"线路","player_lines",onClick=onLines) }
        item { PlayerAction(tv,state.quality,"player_quality",onClick=onQuality) }
        if(!casting)item { PlayerAction(tv,"倍速","player_speed",onClick=onSpeed) }
        if(!casting&&state.subtitles.isNotEmpty())item { PlayerAction(tv,"字幕","player_subtitles",onClick=onSubtitle) }
        if(!tv&&!state.offline)item { PlayerAction(false,"投屏","player_cast",onClick=onCast) }
        if(!tv&&Build.VERSION.SDK_INT>=26)item { PlayerAction(false,"画中画","player_pip",onClick=onPip) }
    }
}

@Composable private fun PlaybackInfoPanel(repo:AppRepository,tv:Boolean,state:PlayerState,onLogin:()->Unit,onMembership:()->Unit,onFilm:(Long)->Unit,panelFocus:FocusRequester,infoListState:LazyListState,onEpisode:(Source,Episode)->Unit,onSource:(Source)->Unit) {
    val detail=state.detail
    if(detail==null) { Column(Modifier.padding(16.dp)) { SelectionContainer { Text(state.descriptor?.name.orEmpty(),style=MaterialTheme.typography.titleLarge) };if(state.offline)Text("离线播放 · 已验证账号与授权有效期") };return }
    val film=detail.film
    val session by repo.session.collectAsState()
    val scope=rememberCoroutineScope()
    var tab by rememberSaveable(film.id) { mutableStateOf("episodes") }
    var sourceCode by rememberSaveable(film.id) { mutableStateOf(state.descriptor?.line.orEmpty().ifBlank { detail.preferredLine }) }
    var favorite by remember(film.id,session?.user?.id) { mutableStateOf(false) }
    var comment by rememberSaveable(film.id) { mutableStateOf("") }
    var comments by remember(film.id,detail.comments) { mutableStateOf(detail.comments) }
    var busy by remember { mutableStateOf(false) }
    var unlocking by remember { mutableStateOf(false) }
    var download by remember { mutableStateOf(false) }
    var unlocked by remember(film.id,session?.user?.id) { mutableStateOf(false) }
    val selected=detail.sources.find { it.code==sourceCode }?:detail.sources.find { it.code==state.descriptor?.line }?:PlaybackRules.defaultSource(film,detail.sources)
    LaunchedEffect(state.requested?.line,state.descriptor?.line) { (state.requested?.line?:state.descriptor?.line)?.takeIf { it.isNotBlank() }?.let { sourceCode=it } }
    LaunchedEffect(film.id,session?.user?.id) { if(session!=null&&film.id>0)runCatching { favorite=repo.favorites().any { it.id==film.id } }.onFailure(repo::error) }
    fun perform(block:suspend ()->Unit) { scope.launch { busy=true;try { block() } catch(e:CancellationException) { throw e } catch(e:Throwable) { repo.error(e) } finally { busy=false } } }
    LazyColumn(Modifier.fillMaxSize().background(CinemaSurface).padding(if(tv)12.dp else 16.dp).testTag("player_info_list"),state=infoListState,verticalArrangement=Arrangement.spacedBy(12.dp)) {
        item {
            SelectionContainer { Text(film.name,style=MaterialTheme.typography.titleLarge,color=LocalCinemaStyle.current.text) }
            Text(listOf(film.year,film.area,film.typeName).filter { it.isNotBlank() }.joinToString(" / "),style=MaterialTheme.typography.bodySmall,color=MaterialTheme.colorScheme.onSurfaceVariant)
            if(state.episodeLabel.isNotBlank())Text("${if(state.loading)"准备播放" else "正在播放"}：${state.episodeLabel}",color=CinemaAccent,style=MaterialTheme.typography.bodyMedium,modifier=Modifier.testTag("player_current_episode"))
            if(film.remarks.isNotBlank())Text("更新状态：${film.remarks}",style=MaterialTheme.typography.bodySmall,color=MaterialTheme.colorScheme.onSurfaceVariant)
            Text(if(film.score>0)"评分：%.1f".format(film.score) else "暂无评分",color=CinemaAccent)
            if(film.vip)Text("VIP 影片",color=Color(0xFFF2CC61))
        }
        if(!state.offline&&!(state.loading&&state.descriptor==null&&detail.sources.isEmpty()))item { PlaybackSourceStatus(tv,state) { PlayerHub.engine.discoverMoreSources() } }
        item { LazyRow(horizontalArrangement=Arrangement.spacedBy(8.dp)) {
            item { PlayerAction(tv,if(favorite)"取消收藏" else "收藏","player_favorite",enabled=!busy,onClick={ if(session==null)onLogin() else perform { repo.favorite(film.id,!favorite);favorite=!favorite } }) }
            item { PlayerAction(tv,if(session==null)"登录" else "账号 / 会员","player_account",onClick=onMembership) }
            if(film.points>0)item { PlayerAction(tv,if(unlocked)"已解锁" else "积分解锁","player_unlock",enabled=!busy&&!unlocked,onClick={ if(session==null)onLogin() else unlocking=true }) }
            if(!tv&&!state.offline&&selected?.episodes?.isNotEmpty()==true)item { PlayerAction(false,"下载选集","player_download",onClick={ if(session==null)onLogin() else download=true }) }
        } }
        item { LazyRow(horizontalArrangement=Arrangement.spacedBy(8.dp)) {
            items(listOf("episodes" to "剧集","about" to "简介","comments" to "评论 (${comments.size})")) { (key,label)->PlayerAction(tv,(if(tab==key)"● " else "")+label,"player_tab_$key",modifier=if(key=="episodes")Modifier.focusRequester(panelFocus) else Modifier,onClick={ tab=key }) }
        } }
        when(tab) {
            "episodes" -> {
                item { Text(if(state.episodeLabel.isNotBlank())"${detail.sources.find { it.code==(state.requested?.line?:state.descriptor?.line) }?.name.orEmpty()} · ${state.episodeLabel}" else "选择资源线路与剧集",color=CinemaAccent,style=MaterialTheme.typography.bodySmall) }
                item { LazyRow(horizontalArrangement=Arrangement.spacedBy(8.dp)) {
                    items(detail.sources,key={ it.code }) { source->PlayerAction(tv,(if(selected?.code==source.code)"✓ " else "")+source.name,"player_source_${source.code}",modifier=Modifier.background(if(selected?.code==source.code)CinemaAccent.copy(alpha=.14f) else LocalCinemaStyle.current.surfaceVariant,RoundedCornerShape(8.dp)),enabled=source.episodes.isNotEmpty(),onClick={ sourceCode=source.code;onSource(source) }) }
                } }
                if(selected!=null)item {
                    LazyVerticalGrid(GridCells.Adaptive(if(tv)82.dp else 88.dp),Modifier.fillMaxWidth().height(if(tv)240.dp else 220.dp).testTag("player_episodes"),horizontalArrangement=Arrangement.spacedBy(8.dp),verticalArrangement=Arrangement.spacedBy(8.dp),contentPadding=PaddingValues(4.dp)) {
                        items(selected.episodes,key={ it.key }) { episode->
                            val active=(state.requested?.line?:state.descriptor?.line)==selected.code && (state.requested?.episodeKey?:state.descriptor?.episodeKey)==episode.key && (state.requested?.versionKey?:state.descriptor?.versionKey).let { it.isNullOrBlank()||it==selected.versionKey }
                            PlayerAction(tv,(if(active)"▶ " else "")+episode.name.ifBlank { "第${episode.number}集" },"player_episode_${selected.code}_${episode.key}",modifier=Modifier.fillMaxWidth().background(if(active)CinemaAccent.copy(alpha=.14f) else LocalCinemaStyle.current.surfaceVariant,RoundedCornerShape(8.dp)).border(if(active)1.dp else 0.dp,if(active)CinemaAccent else Color.Transparent,RoundedCornerShape(8.dp)),onClick={ onEpisode(selected,episode) })
                        }
                    }
                } else if(!state.loading)item { Text("暂无可用线路，可查找更多片源") }
            }
            "about" -> {
                if(film.actor.isNotBlank()||film.director.isNotBlank())item { Text(listOf("导演：${film.director}","主演：${film.actor}").filterNot { it.endsWith("：") }.joinToString("\n")) }
                item { Text(film.content.replace(Regex("<[^>]+>"),"").replace("&nbsp;"," ").ifBlank { "暂无影片简介" },style=MaterialTheme.typography.bodyMedium) }
                if(detail.related.isNotEmpty())item { Text("猜你喜欢",style=MaterialTheme.typography.titleMedium);LazyRow(horizontalArrangement=Arrangement.spacedBy(12.dp)) { items(detail.related,key={ it.id }) { related->Poster(related,tv,{ onFilm(related.id) },Modifier.width(if(tv)128.dp else 112.dp)) } } }
            }
            "comments" -> {
                item { OutlinedTextField(comment,{ comment=it },label={ Text("分享观影感受") },modifier=Modifier.fillMaxWidth().testTag("player_comment_input"),maxLines=3) }
                item { PlayerAction(tv,if(session==null)"登录后发表评论" else "发表评论","player_comment_submit",enabled=!busy&&(session==null||comment.isNotBlank()),onClick={ if(session==null)onLogin() else perform { repo.comment(film.id,comment.trim());comment="";comments=repo.detail(film.id,fresh=true).comments;repo.notice.value="评论已提交" } }) }
                if(comments.isEmpty())item { Text("暂无评论，分享你的观影感受") }
                items(comments,key={ it.id }) { c->Column(verticalArrangement=Arrangement.spacedBy(4.dp)) { Text(c.name,style=MaterialTheme.typography.titleSmall,color=CinemaAccent);Text(c.content);Text(c.createdAt,style=MaterialTheme.typography.labelSmall);HorizontalDivider() } }
            }
        }
    }
    if(unlocking)AlertDialog(onDismissRequest={ unlocking=false },title={ Text("积分解锁") },text={ Text("确认使用 ${film.points} 积分解锁《${film.name}》？当前积分：${session?.user?.points?:0}") },confirmButton={ PlayerAction(tv,"确认解锁","player_unlock_confirm",enabled=!busy,onClick={ perform {
        repo.unlock(film.id);repo.me();unlocked=true;unlocking=false;repo.notice.value="解锁成功"
        if(!PlayerHub.engine.retryCurrent())repo.notice.value="解锁成功，请重新打开影片"
    } }) },dismissButton={ PlayerAction(tv,"取消","player_unlock_cancel",onClick={ unlocking=false }) })
    if(download&&selected?.episodes?.isNotEmpty()==true)DownloadSelection(repo,detail,selected,{ download=false })
}

/** Always above the episode tabs: recovery must not require scrolling to the bottom. */
@Composable internal fun PlaybackSourceStatus(tv:Boolean,state:PlayerState,onDiscover:()->Unit) {
    val progress=state.discovery
    val canDiscover=PlaybackRecoveryRules.canDiscover(state.offline,state.errorStatus,state.detail?.film?.id?:0)
    Surface(color=LocalCinemaStyle.current.surfaceVariant,contentColor=LocalCinemaStyle.current.text,shape=RoundedCornerShape(10.dp),modifier=Modifier.fillMaxWidth().testTag("player_source_status")) {
        Column(Modifier.padding(12.dp),verticalArrangement=Arrangement.spacedBy(6.dp)) {
            Row(verticalAlignment=Alignment.CenterVertically,horizontalArrangement=Arrangement.spacedBy(8.dp)) {
                if(progress.running)CircularProgressIndicator(Modifier.size(18.dp),strokeWidth=2.dp)
                else Icon(Icons.Default.ManageSearch,null,tint=CinemaAccent,modifier=Modifier.size(20.dp))
                Text(when { progress.running->"正在查找其他可用片源";state.detail?.sources.isNullOrEmpty()->"暂无可用线路";else->"${state.detail?.sources?.size?:0} 条资源线路" },style=MaterialTheme.typography.titleSmall,modifier=Modifier.weight(1f))
            }
            if(progress.summary.isNotBlank())Text(progress.summary,style=MaterialTheme.typography.bodySmall,color=MaterialTheme.colorScheme.onSurfaceVariant,modifier=Modifier.testTag("player_discovery_progress"))
            else Text(if(canDiscover)"播放失败时会自动补充片源，也可手动查找" else "请先处理登录、观看权限或影片下架提示",style=MaterialTheme.typography.bodySmall,color=MaterialTheme.colorScheme.onSurfaceVariant)
            if(tv)PlayerAction(true,if(progress.running)"查找中…" else "查找更多片源","player_discover",enabled=canDiscover&&!progress.running,onClick=onDiscover)
            else OutlinedButton(onClick=onDiscover,enabled=canDiscover&&!progress.running,modifier=Modifier.fillMaxWidth().testTag("player_discover")) {
                Icon(Icons.Default.Search,null,modifier=Modifier.size(18.dp));Spacer(Modifier.width(8.dp));Text(if(progress.running)"查找中…" else "查找更多片源")
            }
        }
    }
}
