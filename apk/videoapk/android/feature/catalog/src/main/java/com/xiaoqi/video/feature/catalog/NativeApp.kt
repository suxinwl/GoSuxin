package com.xiaoqi.video.feature.catalog

import android.os.Build
import android.content.Intent
import android.net.Uri
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.*
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.*
import androidx.compose.foundation.lazy.grid.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.saveable.rememberSaveableStateHolder
import androidx.compose.ui.Modifier
import androidx.compose.ui.Alignment
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.unit.dp
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import com.xiaoqi.video.core.data.*
import com.xiaoqi.video.core.design.*
import com.xiaoqi.video.core.model.*
import com.xiaoqi.video.core.network.JsonWire
import com.xiaoqi.video.core.network.SiteHttp
import com.xiaoqi.video.core.network.EndpointBootstrap
import com.xiaoqi.video.core.network.JsonWire.array
import com.xiaoqi.video.core.network.JsonWire.obj
import com.xiaoqi.video.core.network.JsonWire.string
import com.xiaoqi.video.core.network.JsonWire.long
import com.xiaoqi.video.core.cast.*
import com.xiaoqi.video.core.player.*
import com.xiaoqi.video.feature.account.AccountScreen
import com.xiaoqi.video.feature.library.LibraryScreen
import com.xiaoqi.video.feature.live.LiveChannelsScreen
import com.xiaoqi.video.feature.live.LivePlayerScreen
import kotlinx.coroutines.*

@Composable fun NativeApp(tv:Boolean=false,initialFilmId:Long=0,pairCode:String="",filmIntentVersion:Int=0) {
    val repo=AppGraph.repository;val context=LocalContext.current;val scope=rememberCoroutineScope()
    val snack=remember { SnackbarHostState() }
    var tab by rememberSaveable { mutableStateOf(if(pairCode.isNotBlank())"account" else "home") }
    var activeFilmId by rememberSaveable { mutableLongStateOf(0) }
    var openingFilmId by remember { mutableLongStateOf(0) };var openJob by remember { mutableStateOf<Job?>(null) }
    var openGeneration by remember { mutableLongStateOf(0) }
    var playing by rememberSaveable { mutableStateOf(false) }
    var returnFilmId by rememberSaveable { mutableLongStateOf(0) };var resumeOnSignIn by rememberSaveable { mutableStateOf(false) }
    var returnLiveChannel by remember { mutableStateOf<LiveChannel?>(null) }
    var returnLiveProgramme by remember { mutableStateOf<LiveProgramme?>(null) }
    var returnLivePosition by remember { mutableLongStateOf(0) }
    var resumeLiveOnSignIn by remember { mutableStateOf(false) }
    var config by remember { mutableStateOf(SiteConfig()) };var channels by remember { mutableStateOf<List<Category>>(emptyList()) }
    var channel by rememberSaveable { mutableLongStateOf(0) }
    var channelMode by rememberSaveable { mutableStateOf("vod") }
    val liveState by PlayerHub.engine.live.state.collectAsState()
    var home by remember { mutableStateOf<HomeData?>(null) };var loadError by remember { mutableStateOf("") }
    val serverOrigin by repo.serverOrigin.collectAsState()
    var homeOrigin by remember { mutableStateOf(serverOrigin) }
    val addressDiscovery by EndpointBootstrap.state.collectAsState()
    val castKind by CastHub.kind.collectAsState()
    var switchingServer by remember { mutableStateOf(false) }
    var reload by remember { mutableIntStateOf(0) };var release by remember { mutableStateOf<Release?>(null) }
    var updateError by remember { mutableStateOf("") };var checkingUpdate by remember { mutableStateOf(false) };var installingUpdate by remember { mutableStateOf(false) }
    val updateProgress by UpdateInstaller.progress.collectAsState()
    val pendingUpdate by UpdateInstaller.pendingPermission.collectAsState()
    val lifecycleOwner=LocalLifecycleOwner.current
    var pairingConfirm by remember { mutableStateOf(pairCode.isNotBlank()) };var appearanceDialog by remember { mutableStateOf(false) };var displayDialog by remember { mutableStateOf(false) }
    val preference by repo.store.appearance.collectAsState(initial="follow")
    val catalogDisplay by repo.store.catalogDisplay.collectAsState(initial=CatalogDisplay())
    val appearance=CmsAppearance.resolve(preference,config.theme)
    val notice by repo.notice.collectAsState();val session by repo.session.collectAsState()
    val tvCast=remember { CastHub.device };val screenStates=rememberSaveableStateHolder()
    val installedVersion=remember(context) { runCatching { val p=context.packageManager.getPackageInfo(context.packageName,0);if(Build.VERSION.SDK_INT>=28)p.longVersionCode.toInt() else @Suppress("DEPRECATION") p.versionCode }.getOrDefault(0) }

    fun checkUpdate() {
        if(checkingUpdate||installingUpdate)return
        checkingUpdate=true;updateError=""
        scope.launch {
            try {
                release=repo.release(if(tv)"tv" else "mobile",installedVersion)
                release?.let { UpdateRules.validate(it,if(tv)"tv" else "mobile",installedVersion.toLong(),Build.VERSION.SDK_INT) }
                if(release==null)repo.notice.value="已是最新版本"
            } catch(e:CancellationException) { throw e }
            catch(e:Throwable) { release=null;updateError=UpdateRules.errorMessage(e) }
            finally { checkingUpdate=false }
        }
    }
    fun beginUpdate(r:Release) {
        if(installingUpdate)return
        installingUpdate=true;updateError=""
        scope.launch {
            try { if(installRelease(context,r,repo)==UpdateInstallResult.INSTALLER_OPENED)release=null }
            catch(e:CancellationException) { throw e }
            catch(e:Throwable) { updateError=UpdateRules.errorMessage(e) }
            finally { installingUpdate=false }
        }
    }
    fun openDownloadPage() {
        try { context.startActivity(Intent(Intent.ACTION_VIEW,Uri.parse("${SiteHttp.currentBase}/suxinvideo/app-download"))) }
        catch(e:Throwable) { updateError="无法打开浏览器，请用浏览器访问 ${SiteHttp.currentBase}/suxinvideo/app-download" }
    }

    fun openFilmCard(summary:Film,requested:PlaybackIdentity?=null) {
        if(!summary.hasPlayableIdentity)return
        val id=summary.id
        if(id>0&&openingFilmId==id&&openJob?.isActive==true&&requested==null)return
        repo.cancelCataloguePrefetch()
        openJob?.cancel();openGeneration++;val generation=openGeneration;openingFilmId=id
        PlayerHub.engine.beginOpening(summary,onRetry={ openFilmCard(summary,requested) })
        activeFilmId=id;playing=true;returnFilmId=0;resumeOnSignIn=false
        openJob=scope.launch {
            try {
                val opening=prepareFilmPlaybackOpening(summary,requested,
                    hasAccount={ repo.session.value!=null },resolveFilm=repo::resolveFeatured,
                    detail={ repo.detail(it) },progress=repo::progress,cachedDetail=repo::cachedDetail,
                    phase={ message->if(generation==openGeneration)PlayerHub.engine.updateOpeningMessage(id,message) })
                ensureActive();if(generation!=openGeneration)return@launch
                activeFilmId=opening.detail.film.id
                PlayerHub.engine.open(opening.detail,opening.identity)
            } catch(e:TimeoutCancellationException) {
                if(generation==openGeneration)PlayerHub.engine.failOpening(id,IllegalStateException("影片信息加载超时，请重试"))
            } catch(e:CancellationException) {
                // A dependency can cancel itself while navigation is still active.
                // That must end its spinner; cancellation by Back or a newer film stays silent.
                if(currentCoroutineContext().isActive&&generation==openGeneration)PlayerHub.engine.failOpening(id,IllegalStateException("影片信息加载已中断，请重试"))
                throw e
            }
            catch(e:Throwable) { if(generation==openGeneration)PlayerHub.engine.failOpening(id,e) }
            finally { if(generation==openGeneration)openingFilmId=0 }
        }
    }
    fun openFilm(id:Long,requested:PlaybackIdentity?=null) {
        if(id<=0)return
        val summary=home?.sections?.asSequence()?.flatMap { it.items.asSequence() }?.find { it.id==id }
            ?:PlayerHub.engine.state.value.detail?.film?.takeIf { it.id==id }
            ?:Film(id=id,name="")
        openFilmCard(summary,requested)
    }
    fun leavePlayer() { openJob?.cancel();openGeneration++;openingFilmId=0;PlayerHub.engine.stop();playing=false }
    fun openLive(channel:LiveChannel) {
        openJob?.cancel();openGeneration++;openingFilmId=0
        playing=false;returnFilmId=0;resumeOnSignIn=false
        repo.cancelCataloguePrefetch()
        if(CastHub.kind.value.isNotBlank())CastHub.end()
        PlayerHub.engine.live.open(channel)
    }
    fun showLiveChannels() { channelMode="live";tab=if(tv)"live" else "category" }
    fun showAccount(signIn:Boolean) { returnFilmId=activeFilmId;resumeOnSignIn=signIn&&session==null;leavePlayer();tab="account" }
    fun showLiveAccount() {
        returnLiveChannel=liveState.channel;returnLiveProgramme=liveState.programme;returnLivePosition=liveState.position;resumeLiveOnSignIn=session==null
        returnFilmId=0;resumeOnSignIn=false;leavePlayer();tab="account"
    }
    fun resumeLive() {
        val savedChannel=returnLiveChannel?:return
        val programme=returnLiveProgramme
        returnLiveChannel=null;returnLiveProgramme=null;resumeLiveOnSignIn=false
        openLive(savedChannel);if(programme!=null)PlayerHub.engine.live.openReplay(programme,returnLivePosition)
    }
    fun switchServiceAddress() {
        val discovery=EndpointBootstrap.state.value
        if(!ServiceAddressRecoveryRules.canSwitch(discovery,SiteHttp.currentBase,playing,PlayerHub.engine.live.state.value.active,CastHub.kind.value.isNotBlank(),updateProgress>=0||installingUpdate||checkingUpdate,switchingServer))return
        switchingServer=true
        scope.launch {
            try {
                repo.changeServer(discovery.verifiedCandidate)
                home=null;channels=emptyList();config=SiteConfig();channel=0;loadError="";reload++
            } catch(e:CancellationException) { throw e }
            catch(e:Throwable) { loadError=ServiceAddressRecoveryRules.errorMessage(e) }
            finally { switchingServer=false }
        }
    }
    DisposableEffect(lifecycleOwner,context) {
        val observer=LifecycleEventObserver { _,event->
            if(event==Lifecycle.Event.ON_RESUME)UpdateInstaller.pendingPermission.value?.let { pending->
                release=pending
                if(UpdateInstaller.canInstall(context))beginUpdate(pending)
            }
        }
        lifecycleOwner.lifecycle.addObserver(observer)
        onDispose { lifecycleOwner.lifecycle.removeObserver(observer) }
    }
    LaunchedEffect(pairCode) { if(pairCode.isNotBlank()) { leavePlayer();tab="account";pairingConfirm=true } }
    LaunchedEffect(initialFilmId,filmIntentVersion) { if(initialFilmId>0)openFilm(initialFilmId) }
    LaunchedEffect(session?.user?.id) { if(session!=null&&resumeOnSignIn&&returnFilmId>0)openFilm(returnFilmId) }
    LaunchedEffect(session?.user?.id,resumeLiveOnSignIn) { if(session!=null&&resumeLiveOnSignIn&&returnLiveChannel!=null)resumeLive() }
    LaunchedEffect(Unit) { if(playing&&activeFilmId>0&&PlayerHub.engine.state.value.detail==null)openFilm(activeFilmId) }

    // Optional branding requests run independently from the first visible home catalogue.
    LaunchedEffect(reload,serverOrigin) {
        if(homeOrigin!=serverOrigin) { home=null;channels=emptyList();config=SiteConfig();homeOrigin=serverOrigin }
        loadError=""
        supervisorScope {
            launch { try { config=repo.config() } catch(e:CancellationException) { throw e } catch(_:Throwable) {} }
            launch { try { channels=repo.channels() } catch(e:CancellationException) { throw e } catch(_:Throwable) {} }
            launch { loadHomeOpening(cached=repo::cachedHome,fresh=repo::home,
                onHome={ home=it },onError={ loadError=ServiceAddressRecoveryRules.errorMessage(it) },onRejectCached={ home=null }) }
            launch { runCatching { release=repo.release(if(tv)"tv" else "mobile",installedVersion) } }
        }
    }
    LaunchedEffect(notice,playing) { if(notice.isNotBlank()&&!playing) { snack.showSnackbar(notice);repo.notice.value="" } }
    LaunchedEffect(tv) {
        if(tv) {
            tvCast.watchTv()
            launch { tvCast.messages.collect { o ->
                if(o.string("type")!="command")return@collect
                val payload=o.obj("payload");val engine=PlayerHub.engine
                if(engine.live.state.value.active&&o.string("action")!="play")return@collect
                when(o.string("action")) {
                    "play"->{ val id=payload.long("vod_id");openFilm(id,PlaybackIdentity(id,payload.string("line"),payload.string("episode_key"),payload.long("episode").toInt(),payload.string("quality"),payload.long("position_ms"),true,payload.string("version_key"))) }
                    "pause"->engine.player.pause();"resume"->engine.player.play();"seek"->engine.seek(payload.long("position_ms"));"next"->engine.next();"quality"->engine.quality(payload.string("quality"),payload.string("quality"));"stop"->leavePlayer()
                }
            } }
            launch { while(isActive) { delay(2000);if(PlayerHub.engine.live.state.value.active)continue;val p=PlayerHub.engine.state.value;tvCast.publishStatus(mapOf("playing" to p.playing,"position_ms" to p.position,"duration_ms" to p.duration,"vod_id" to (p.descriptor?.vodId?:0),"line" to p.descriptor?.line.orEmpty(),"episode_key" to p.descriptor?.episodeKey.orEmpty(),"version_key" to p.descriptor?.versionKey.orEmpty(),"episode" to (p.descriptor?.episode?:0),"episode_name" to p.episodeLabel,"error" to p.error)) } }
        }
    }
    BackHandler(enabled=playing||liveState.active||openingFilmId>0||tab!="home") { when { playing||liveState.active||openingFilmId>0->leavePlayer();else->tab="home" } }

    CinemaTheme(appearance) {
        if(liveState.active) {
            LivePlayerScreen(repo,tv,onBack={ leavePlayer() },onChannel={ openLive(it) },onAccount={ showLiveAccount() })
            return@CinemaTheme
        }
        if(playing) {
            PlayerScreen(repo,tv,onBack={ leavePlayer() },onLogin={ showAccount(true) },onMembership={ showAccount(false) },onFilm={ openFilm(it) })
            return@CinemaTheme
        }
        val style=LocalCinemaStyle.current
        val tabs=listOf("home" to "首页","category" to "频道","search" to "搜索","library" to "我的片库","account" to "账号")
        val icons=listOf(Icons.Default.Home,Icons.Default.Category,Icons.Default.Search,Icons.Default.VideoLibrary,Icons.Default.AccountCircle)
        BoxWithConstraints(Modifier.fillMaxSize().background(style.background).testTag("appearance-${appearance.code}")) {
            val wide=maxWidth>=700.dp&&!tv
            val extendedTabs=listOf("home" to "首页","category" to "频道","live" to "直播","search" to "搜索","library" to "我的片库","account" to "账号")
            val extendedIcons=listOf(Icons.Default.Home,Icons.Default.Category,Icons.Default.LiveTv,Icons.Default.Search,Icons.Default.VideoLibrary,Icons.Default.AccountCircle)
            val catalogueWidth=(maxWidth-(if(wide)80.dp else 0.dp)-24.dp).value
            val catalogueSize=CatalogLayoutRules.pageSize(CatalogLayoutRules.columns(catalogueWidth,catalogDisplay.columns),catalogDisplay.rows)
            Scaffold(containerColor=style.background,snackbarHost={ SnackbarHost(snack) },topBar={
                Row(Modifier.fillMaxWidth().background(style.surface).statusBarsPadding().padding(horizontal=if(tv)30.dp else 12.dp,vertical=8.dp),verticalAlignment=Alignment.CenterVertically,horizontalArrangement=Arrangement.spacedBy(8.dp)) {
                    CinemaBrandLogo(config.logo,Modifier.size(32.dp))
                    Text(config.name.ifBlank { "小柒影视" },style=MaterialTheme.typography.titleLarge,maxLines=1,modifier=if(tv)Modifier.widthIn(max=180.dp) else Modifier.weight(1f))
                    if(tv) { extendedTabs.forEach { (key,label)->var focused by remember(key) { mutableStateOf(false) };androidx.tv.material3.Button(onClick={ tab=key;if(key=="live")channelMode="live" else if(key=="category")channelMode="vod" },modifier=Modifier.onFocusChanged { focused=it.isFocused },colors=androidx.tv.material3.ButtonDefaults.colors(containerColor=style.surface,contentColor=style.text,focusedContainerColor=style.accent,focusedContentColor=Color.White)) { Text(label,color=if(focused)Color.White else if(tab==key)style.accent else style.text) } };Spacer(Modifier.weight(1f)) }
                    if(session?.user?.isVip()==true)Text("VIP",color=Color(0xFFD8A947))
                    if(!tv&&(tab=="home"||tab=="search"||tab=="category"&&channelMode=="vod"))IconButton(onClick={ displayDialog=true },modifier=Modifier.testTag("catalog-display-settings")) { Icon(Icons.Default.GridView,"影片显示数量") }
                    IconButton(onClick={ appearanceDialog=true }) { Icon(Icons.Default.Palette,"模板样式") }
                    IconButton(enabled=!checkingUpdate&&!installingUpdate,onClick={ checkUpdate() }) { if(checkingUpdate)CircularProgressIndicator(Modifier.size(20.dp),strokeWidth=2.dp) else Icon(Icons.Default.SystemUpdate,"检查更新") }
                }
            },bottomBar={ if(!tv&&!wide)NavigationBar(containerColor=style.surface) { tabs.forEachIndexed { i,(key,label)->NavigationBarItem(tab==key||(key=="category"&&tab=="live"),{ tab=key },icon={ Icon(icons[i],null) },label={ Text(label) }) } } }) { padding ->
                Row(Modifier.padding(padding).fillMaxSize()) {
                    if(wide)NavigationRail(containerColor=style.surface) { extendedTabs.forEachIndexed { i,(key,label)->NavigationRailItem(tab==key,{ tab=key;if(key=="live")channelMode="live" else if(key=="category")channelMode="vod" },icon={ Icon(extendedIcons[i],null) },label={ Text(label) }) } }
                    Column(Modifier.weight(1f).fillMaxHeight()) {
                        if(tab=="account"&&returnFilmId>0)TextButton(onClick={ openFilm(returnFilmId) },modifier=Modifier.fillMaxWidth()) { Icon(Icons.Default.PlayArrow,null);Text("返回正在观看的影片") }
                        if(tab=="account"&&returnLiveChannel!=null)TextButton(onClick={ resumeLive() },modifier=Modifier.fillMaxWidth()) { Icon(Icons.Default.LiveTv,null);Text("返回 ${returnLiveChannel?.name}") }
                        screenStates.SaveableStateProvider("page_${serverOrigin}_$tab") { when(tab) {
                            "home"->Column(Modifier.fillMaxSize()) {
                                if(ServiceAddressRecoveryRules.showOnHome(loadError))ServiceAddressRecoveryPanel(addressDiscovery,serverOrigin,ServiceAddressRecoveryRules.canSwitch(addressDiscovery,serverOrigin,playing,liveState.active,castKind.isNotBlank(),updateProgress>=0||installingUpdate||checkingUpdate,switchingServer),onCheck={ EndpointBootstrap.check() },onSwitch={ switchServiceAddress() },modifier=Modifier.padding(12.dp))
                                Box(Modifier.weight(1f).fillMaxWidth()) {
                            if(home!=null)NativeHomeScreen(home!!,tv,channels,{ openFilm(it) },{ channel=it;channelMode="vod";tab="category" },{ tab="topics" },onRetry={ reload++ },onBanner={ b->
                                if(b.filmId>0)openFilm(b.filmId) else {
                                    val u=runCatching { Uri.parse(b.url) }.getOrNull();val id=u?.getQueryParameter("id")?.toLongOrNull()?:0
                                    when { u?.path?.contains("detail")==true&&id>0->openFilm(id);u?.path?.contains("type")==true||u?.path?.contains("category")==true->{ channel=u?.getQueryParameter("channel_id")?.toLongOrNull()?:id;channelMode="vod";tab="category" };u?.path?.contains("topic")==true->tab="topics";else->repo.notice.value="该推荐暂未关联可播放影片" }
                                }
                            },onPrefetch={ id->repo.scope.launch { repo.prefetchDetail(id) } },onWarmChannel={ id->repo.prefetchFilms(CatalogRequest(channel=id,size=if(tv)30 else catalogueSize));Unit },display=catalogDisplay,warmPageSize=if(tv)30 else catalogueSize,onLive={ showLiveChannels() },onFilmCard={ openFilmCard(it) }) else if(loadError.isNotBlank())EmptyState(loadError,"重试",{ reload++ }) else Loading()
                                }
                            }
                            "category"->Column(Modifier.fillMaxSize()) {
                                Row(Modifier.fillMaxWidth().padding(horizontal=12.dp),horizontalArrangement=Arrangement.spacedBy(12.dp)) {
                                    FilterChip(channelMode=="vod",{ channelMode="vod" },label={ Text("影视") },modifier=Modifier.testTag("channel_mode_vod"))
                                    FilterChip(channelMode=="live",{ channelMode="live" },label={ Text("直播") },modifier=Modifier.testTag("channel_mode_live"))
                                }
                                Box(Modifier.weight(1f).fillMaxWidth()) {
                                    screenStates.SaveableStateProvider("channel_mode_$channelMode") {
                                        if(channelMode=="live")LiveChannelsScreen(repo,tv,{ openLive(it) })
                                        else CatalogueScreen(repo,tv,channels,channel,{ channel=it },{ openFilm(it) },display=catalogDisplay,onFilmCard={ openFilmCard(it) })
                                    }
                                }
                            }
                            "live"->LiveChannelsScreen(repo,tv,{ openLive(it) })
                            "search"->CatalogueScreen(repo,tv,channels,0,{}, { openFilm(it) },search=true,display=catalogDisplay,onFilmCard={ openFilmCard(it) })
                            "library"->LibraryScreen(repo,tv,{ openFilm(it) },{ activeFilmId=PlayerHub.engine.state.value.descriptor?.vodId?:activeFilmId;playing=true },{ tab="account" })
                            "topics"->TopicsScreen(repo,tv,{ openFilm(it) })
                            else->AccountScreen(repo,tv)
                        } }
                    }
                }
            }
        }
        if(appearanceDialog)AppearanceDialog(preference,config.theme,onSelect={ value->scope.launch { repo.store.setAppearance(value) };appearanceDialog=false },onDismiss={ appearanceDialog=false })
        if(displayDialog)CatalogDisplayDialog(catalogDisplay.columns,catalogDisplay.rows,onApply={ columns,rows->scope.launch { repo.store.setCatalogDisplay(CatalogDisplay(columns,rows));displayDialog=false } },onDismiss={ displayDialog=false })
        release?.let { r->AlertDialog(onDismissRequest={ if(!installingUpdate) { release=null;updateError="";UpdateInstaller.dismissPermission() } },title={ Text("发现新版本 ${r.versionName}") },text={ Column(verticalArrangement=Arrangement.spacedBy(8.dp)) {
            Text("${r.changelog}\n安装包 ${(r.size/1024.0/1024).toInt()} MB")
            Text("覆盖安装即可保留本地数据，无需卸载 APP",style=MaterialTheme.typography.bodySmall)
            if(pendingUpdate!=null)Text("安装包已保存。请允许安装应用，返回后将继续安装。",color=MaterialTheme.colorScheme.primary)
            if(updateError.isNotBlank())Text(updateError,color=MaterialTheme.colorScheme.error)
            if(updateProgress>=0) { LinearProgressIndicator(progress={ updateProgress/100f },modifier=Modifier.fillMaxWidth());Text("下载进度 $updateProgress%") }
        } },confirmButton={ TextButton(enabled=!installingUpdate,onClick={ beginUpdate(r) }) { Text(if(pendingUpdate!=null)"继续安装" else if(updateError.isNotBlank())"重试安装" else "下载并安装") } },dismissButton={ TextButton(enabled=!installingUpdate,onClick={ release=null;updateError="";UpdateInstaller.dismissPermission() }) { Text("稍后") } }) }
        if(release==null&&updateError.isNotBlank())AlertDialog(onDismissRequest={ updateError="" },title={ Text("检查更新失败") },text={ Text(updateError) },confirmButton={ TextButton(enabled=!checkingUpdate,onClick={ checkUpdate() }) { Text("重试") } },dismissButton={ Row { TextButton(onClick={ openDownloadPage() }) { Text("打开下载页") };TextButton(onClick={ updateError="" }) { Text("关闭") } } })
        if(pairingConfirm&&session!=null)AlertDialog(onDismissRequest={ pairingConfirm=false },title={ Text("确认连接 TV") },text={ Text("配对码：$pairCode") },confirmButton={ TextButton(onClick={ scope.launch { try { tvCast.approve(pairCode);pairingConfirm=false;repo.notice.value="TV 已连接" } catch(e:Throwable) { repo.error(e) } } }) { Text("连接") } },dismissButton={ TextButton(onClick={ pairingConfirm=false }) { Text("取消") } })
    }
}

@Composable private fun AppearanceDialog(preference:String,website:String,onSelect:(String)->Unit,onDismiss:()->Unit) {
    AlertDialog(onDismissRequest=onDismiss,title={ Text("模板样式") },text={ Column(verticalArrangement=Arrangement.spacedBy(4.dp)) {
        val choices=listOf("follow" to "跟随网站（${CmsAppearance.fromCode(website).label}）")+CmsAppearance.entries.map { it.code to it.label }
        choices.forEach { (code,label)->Row(Modifier.fillMaxWidth().clickable { onSelect(code) }.testTag("choose-$code").padding(vertical=6.dp),verticalAlignment=Alignment.CenterVertically) { RadioButton(preference==code,onClick=null);Column { Text(label);if(code!="follow")Text(when(code) { "suxinlite"->"浅色卡片 · 紫色精选";"suxinpro"->"沉浸轮播 · 红色海报墙";"iqiyi"->"绿色推荐 · 海报网格";else->"粉色轮播 · 热播推荐" },style=MaterialTheme.typography.labelSmall) } }
        }
    } },confirmButton={ TextButton(onClick=onDismiss) { Text("关闭") } })
}

@Composable private fun CatalogueScreen(repo:AppRepository,tv:Boolean,channels:List<Category>,channel:Long,onChannel:(Long)->Unit,onFilm:(Long)->Unit,search:Boolean=false,display:CatalogDisplay=CatalogDisplay(),onFilmCard:((Film)->Unit)?=null) {
    val source=remember(repo) { RepositoryCatalogSource(repo) }
    CatalogueScreen(source,tv,channels,channel,onChannel,onFilm,search,display,onFilmCard)
}

@Composable fun CatalogueScreen(source:CatalogDataSource,tv:Boolean,channels:List<Category>,channel:Long,onChannel:(Long)->Unit,onFilm:(Long)->Unit,search:Boolean=false,display:CatalogDisplay=CatalogDisplay(),onFilmCard:((Film)->Unit)?=null) {
    val style=LocalCinemaStyle.current
    BoxWithConstraints(Modifier.fillMaxSize()) {
    val columns=CatalogLayoutRules.columns((maxWidth-24.dp).value,display.columns)
    val pageSize=if(tv)30 else CatalogLayoutRules.pageSize(columns,display.rows)
    var q by rememberSaveable { mutableStateOf("") };var searchTerm by rememberSaveable { mutableStateOf("") }
    var catalogueSource by rememberSaveable { mutableStateOf("local") }
    var topic by rememberSaveable(channel,catalogueSource) { mutableLongStateOf(0) }
    val featured=!search&&catalogueSource=="featured"
    var page by rememberSaveable(channel,searchTerm,pageSize,catalogueSource,topic) { mutableIntStateOf(1) }
    val vm:CatalogViewModel=viewModel(key="catalog-${SiteHttp.currentBase}-${if(search)"search" else "browse"}",factory=CatalogViewModel.Factory(source))
    DisposableEffect(vm) { onDispose { vm.cancelLoads() } }
    val catalogState by vm.state.collectAsState();val result=catalogState.page;val loading=catalogState.loading;val error=catalogState.error
    var sort by remember { mutableStateOf("time") };var area by remember { mutableStateOf("") };var year by remember { mutableStateOf("") };var type by remember(channel) { mutableLongStateOf(0) };var types by remember { mutableStateOf<List<Category>>(emptyList()) };var filters by remember { mutableStateOf(false) }
    var draftArea by remember { mutableStateOf("") };var draftYear by remember { mutableStateOf("") };var draftType by remember { mutableLongStateOf(0) }
    val selectedFilter=CatalogFilter(searchTerm,if(search)0 else channel,type,page,sort,area,year,pageSize,if(featured)"featured" else "local",topic)
    val request=selectedFilter.request();val gridState=rememberLazyGridState();val uiScope=rememberCoroutineScope()
    var resolvingFilmKey by remember(request,source) { mutableStateOf("") };var resolvingFilmName by remember(request,source) { mutableStateOf("") }
    var resolveError by remember(request,source) { mutableStateOf("") };var retryFilm by remember(request,source) { mutableStateOf<Film?>(null) }
    var resolveJob by remember(request,source) { mutableStateOf<Job?>(null) }
    var resolveGeneration by remember(request,source) { mutableLongStateOf(0) }
    fun cancelFilmResolve() {
        resolveGeneration++;resolveJob?.cancel();resolvingFilmKey="";resolveError="";retryFilm=null
    }
    fun openCatalogueFilm(film:Film) {
        if(onFilmCard!=null&&film.hasPlayableIdentity) { cancelFilmResolve();onFilmCard(film);return }
        if(film.id>0) { cancelFilmResolve();onFilm(film.id);return }
        if(resolvingFilmKey.isNotBlank()||!film.hasPlayableIdentity)return
        resolveGeneration++;val generation=resolveGeneration
        resolvingFilmKey=film.cardKey;resolvingFilmName=film.name;resolveError="";retryFilm=film
        resolveJob=uiScope.launch {
            try {
                val localId=source.resolveFilm(film)
                ensureActive();require(localId>0) { "精选影片尚未载入，请重试" }
                if(generation==resolveGeneration&&catalogState.request==request)onFilm(localId)
            } catch(e:CancellationException) { throw e }
            catch(e:Throwable) { if(generation==resolveGeneration)resolveError=e.localizedMessage?.takeIf { Regex("[\\u3400-\\u9FFF]").containsMatchIn(it) }?:"精选影片连接失败，请重试或刷新列表" }
            finally { if(generation==resolveGeneration)resolvingFilmKey="" }
        }
    }
    DisposableEffect(request,source) { onDispose { cancelFilmResolve() } }
    var topicChoices by remember(channel,catalogueSource) { mutableStateOf<List<Category>>(emptyList()) }
    var showProgress by remember { mutableStateOf(false) };var displayedRequest by remember(vm) { mutableStateOf(catalogState.request) }
    LaunchedEffect(source) { runCatching { types=source.categories() } }
    LaunchedEffect(request) { vm.load(selectedFilter) }
    LaunchedEffect(loading,catalogState.request) {
        if(!loading&&error.isBlank()&&catalogState.request==request&&featured)topicChoices=result.topics
    }
    LaunchedEffect(loading) { showProgress=false;if(loading) { delay(140);showProgress=true } }
    LaunchedEffect(loading,catalogState.request) {
        if(!loading&&error.isBlank()&&catalogState.request!=displayedRequest) { gridState.scrollToItem(0);displayedRequest=catalogState.request }
    }
    LaunchedEffect(loading,catalogState.request,channels) {
        val active=catalogState.request
        if(!loading&&error.isBlank()&&!search&&active!=null&&active.type==0L&&active.topic==0L) {
            val ids=channels.map { it.id }.distinct();val index=ids.indexOf(active.channel)
            listOfNotNull(ids.getOrNull(index+1),ids.getOrNull(index-1)).distinct().take(2).forEach { id->launch { source.prefetch(active.copy(channel=id,page=1)) } }
        }
    }
    val catalogueHeader:@Composable ()->Unit = {
        Column(Modifier.fillMaxWidth(),verticalArrangement=Arrangement.spacedBy(10.dp)) {
        if(search)Row(verticalAlignment=Alignment.CenterVertically,horizontalArrangement=Arrangement.spacedBy(8.dp)) { OutlinedTextField(q,{ q=it },label={ Text("搜索影片、演员") },singleLine=true,modifier=Modifier.weight(1f));Button(onClick={ searchTerm=q.trim();page=1 }) { Text("搜索") } }
        else LazyRow(Modifier.padding(top=8.dp),horizontalArrangement=Arrangement.spacedBy(8.dp)) { item { FilterChip(channel==0L,{ type=0;page=1;onChannel(0) },label={ Text("全部") }) };items(channels.distinctBy { it.id },key={ it.id }) { c->FilterChip(channel==c.id,{ type=0;page=1;onChannel(c.id) },modifier=Modifier.onFocusChanged { if(it.isFocused&&channel!=c.id)uiScope.launch { source.prefetch(request.copy(channel=c.id,type=0,topic=0,page=1)) } },label={ Text(c.name) }) } }
        if(!search)LazyRow(Modifier.fillMaxWidth(),horizontalArrangement=Arrangement.spacedBy(8.dp)) {
            item { FilterChip(!featured,{ catalogueSource="local";page=1 },modifier=Modifier.testTag("catalog-source-local"),label={ Text("本站片库") }) }
            item { FilterChip(featured,{ catalogueSource="featured";page=1 },modifier=Modifier.testTag("catalog-source-featured"),leadingIcon={ Icon(Icons.Default.AutoAwesome,null,Modifier.size(18.dp)) },label={ Text("小柒精选") }) }
        }
        if(featured&&channel>0&&topicChoices.isNotEmpty())LazyRow(Modifier.fillMaxWidth().testTag("catalog-featured-topics"),horizontalArrangement=Arrangement.spacedBy(8.dp)) {
            item { FilterChip(topic==0L,{ topic=0;page=1 },modifier=Modifier.testTag("catalog-topic-0"),label={ Text("精选推荐") }) }
            items(topicChoices.distinctBy { it.id },key={ it.id }) { choice->FilterChip(topic==choice.id,{ topic=choice.id;page=1 },modifier=Modifier.testTag("catalog-topic-${choice.id}"),label={ Text(choice.name) }) }
        }
        if(!featured||channel==0L)LazyRow(Modifier.fillMaxWidth().then(if(style.appearance==CmsAppearance.SuxinLite)Modifier.background(style.surface,RoundedCornerShape(14.dp)).padding(8.dp) else Modifier),horizontalArrangement=Arrangement.spacedBy(6.dp)) {
            item { FilterChip(request.sort=="time",{ sort="time";page=1 },label={ Text("最近更新") }) };item { FilterChip(request.sort=="hits",{ sort="hits";page=1 },label={ Text("热播") }) }
            if(!featured) {
                item { FilterChip(sort=="score",{ sort="score";page=1 },label={ Text("评分") }) }
                item { OutlinedButton(onClick={ draftArea=area;draftYear=year;draftType=type;filters=true },contentPadding=PaddingValues(horizontal=10.dp)) { Text("筛选") } }
            }
        }
        if(featured)Text(if(!loading&&result.notice.isNotBlank())result.notice else "小柒推荐片库 · 按源站精选顺序",style=MaterialTheme.typography.labelSmall,color=style.muted,modifier=Modifier.testTag("catalog-featured-notice"))
        if(!tv)Text("每行 $columns 部 · 每页 ${display.rows} 行"+(if(!loading&&error.isBlank())" · 本页 ${result.items.size} 部 · 下滑浏览" else "（$pageSize 部）"),style=MaterialTheme.typography.labelSmall,color=style.muted,modifier=Modifier.testTag("catalog-density"))
        if(resolvingFilmKey.isNotBlank())Row(Modifier.fillMaxWidth().testTag("catalog-resolving-film"),verticalAlignment=Alignment.CenterVertically,horizontalArrangement=Arrangement.spacedBy(8.dp)) {
            CircularProgressIndicator(Modifier.size(20.dp),strokeWidth=2.dp)
            Text("正在载入 $resolvingFilmName，准备播放…",Modifier.weight(1f),color=style.text)
            TextButton(onClick={ cancelFilmResolve() }) { Text("取消") }
        }
        if(resolveError.isNotBlank())Column(Modifier.fillMaxWidth().testTag("catalog-resolve-error")) {
            Text(resolveError,color=MaterialTheme.colorScheme.error)
            Row { TextButton(onClick={ retryFilm?.let(::openCatalogueFilm) }) { Text("重试") };TextButton(onClick={ resolveError="";vm.load(selectedFilter,true) }) { Text("刷新列表") } }
        }
        Box(Modifier.fillMaxWidth().height(4.dp)) { if(showProgress)LinearProgressIndicator(Modifier.fillMaxWidth().testTag("catalog-loading"),color=style.accent) }
        if(error.isNotBlank())EmptyState(error,"重试",{ vm.load(selectedFilter,true) })
        if(!loading&&result.items.isEmpty()&&error.isBlank())EmptyState("暂无符合条件的影片")
        }
    }
    val catalogueFooter:@Composable ()->Unit = {
        Column(Modifier.fillMaxWidth(),horizontalAlignment=Alignment.CenterHorizontally) {
        Row(Modifier.fillMaxWidth().padding(vertical=8.dp),verticalAlignment=Alignment.CenterVertically,horizontalArrangement=Arrangement.Center) { OutlinedButton(enabled=page>1&&!loading,onClick={ page-- },modifier=Modifier.testTag("catalog-previous-page")) { Text("上一页") };Text(if(loading||!result.exactPages)"第 $page 页" else "$page / ${result.pages.coerceAtLeast(1)}",Modifier.padding(horizontal=20.dp));OutlinedButton(enabled=page<result.pages&&!loading,onClick={ page++ },modifier=Modifier.testTag("catalog-next-page")) { Text("下一页") } }
        if(catalogState.nextPageReady&&!loading)Text("下一页已预加载",color=style.muted,style=MaterialTheme.typography.labelSmall,modifier=Modifier.padding(bottom=4.dp).testTag("catalog-next-ready"))
        }
    }
    Column(Modifier.fillMaxSize().padding(horizontal=if(tv)30.dp else 12.dp),verticalArrangement=Arrangement.spacedBy(10.dp)) {
        if(tv)catalogueHeader()
        val posterMin=when(style.appearance) { CmsAppearance.SuxinPro->190.dp;else->168.dp }
        LazyVerticalGrid(if(tv)GridCells.Adaptive(posterMin) else GridCells.Fixed(columns),Modifier.weight(1f).testTag("catalog-${style.appearance.code}"),state=gridState,horizontalArrangement=Arrangement.spacedBy(if(tv)20.dp else CatalogLayoutRules.gapDp.dp),verticalArrangement=Arrangement.spacedBy(if(style.appearance==CmsAppearance.SuxinLite)16.dp else 20.dp),contentPadding=PaddingValues(vertical=8.dp)) {
            if(!tv)item(key="catalog-controls",span={ GridItemSpan(maxLineSpan) }) { catalogueHeader() }
            items(result.items.filter { it.hasPlayableIdentity }.distinctBy { it.cardKey },key={ it.cardKey }) { f->
                Box {
                    Poster(f,tv,{ openCatalogueFilm(f) },Modifier.testTag(if(f.id>0)"poster-${f.id}" else "poster-${f.cardKey}"),focusKey="${if(search)"search:$searchTerm" else "catalog:$channel"}:$page:${f.cardKey}",onFocused={ if(f.id>0)uiScope.launch { source.prefetchDetail(f.id) } })
                    if(resolvingFilmKey==f.cardKey)Box(Modifier.fillMaxWidth().aspectRatio(style.posterRatio).background(Color.Black.copy(alpha=.65f)),contentAlignment=Alignment.Center) { CircularProgressIndicator(color=style.accent) }
                }
            }
            if(!tv)item(key="catalog-pagination",span={ GridItemSpan(maxLineSpan) }) { catalogueFooter() }
        }
        if(tv)catalogueFooter()
    }
    if(filters)AlertDialog(onDismissRequest={ filters=false },title={ Text("筛选影片") },text={ Column(verticalArrangement=Arrangement.spacedBy(12.dp)) {
        OutlinedTextField(draftArea,{ draftArea=it },label={ Text("地区（留空为全部）") },singleLine=true);OutlinedTextField(draftYear,{ draftYear=it },label={ Text("年份（留空为全部）") },singleLine=true);Text("细分类")
        LazyColumn(Modifier.heightIn(max=220.dp)) { item { TextButton(onClick={ draftType=0 }) { Text(if(draftType==0L)"✓ 全部" else "全部") } };items(types.distinctBy { it.id },key={ it.id }) { c->TextButton(onClick={ draftType=c.id }) { Text((if(draftType==c.id)"✓ " else "")+c.name) } } }
    } },confirmButton={ TextButton(onClick={ area=draftArea.trim();year=draftYear.trim();type=draftType;page=1;filters=false }) { Text("应用") } },dismissButton={ TextButton(onClick={ area="";year="";type=0;page=1;filters=false }) { Text("清空筛选") } })
    }
}

@Composable private fun TopicsScreen(repo:AppRepository,tv:Boolean,onFilm:(Long)->Unit) {
    var topics by remember { mutableStateOf<List<com.google.gson.JsonObject>>(emptyList()) };var selected by remember { mutableLongStateOf(0) };var films by remember { mutableStateOf<List<Film>>(emptyList()) }
    LaunchedEffect(selected) { runCatching { if(selected==0L) { val d=repo.api.request("topics");topics=(if(d.isJsonArray)d.asJsonArray else d.asJsonObject.array("items","topics")).map { it.asJsonObject } } else films=JsonWire.films(repo.api.request("topics/$selected").asJsonObject.array("items","films")) }.onFailure(repo::error) }
    if(selected==0L)LazyColumn(Modifier.padding(20.dp),verticalArrangement=Arrangement.spacedBy(10.dp)) { item { Text("精选片单",style=MaterialTheme.typography.headlineSmall) };items(topics.distinctBy { it.long("id") }) { t->Card(onClick={ selected=t.long("id") },modifier=Modifier.fillMaxWidth()) { Column(Modifier.padding(20.dp)) { Text(t.string("name","topic_name"),style=MaterialTheme.typography.titleLarge);Text(t.string("content","description")) } } } }
    else Column { TextButton(onClick={ selected=0 }) { Text("← 返回片单") };LazyVerticalGrid(GridCells.Adaptive(if(tv)172.dp else 138.dp),Modifier.padding(16.dp),horizontalArrangement=Arrangement.spacedBy(12.dp),verticalArrangement=Arrangement.spacedBy(12.dp)) { items(films.filter { it.id>0 }.distinctBy { it.id },key={ it.id }) { f->Poster(f,tv,{ onFilm(f.id) },Modifier.testTag("poster-${f.id}"),focusKey="topic:$selected:${f.id}") } } }
}
