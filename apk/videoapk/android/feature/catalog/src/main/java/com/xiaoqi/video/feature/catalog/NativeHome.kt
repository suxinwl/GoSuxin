package com.xiaoqi.video.feature.catalog

import androidx.compose.foundation.*
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.*
import androidx.compose.foundation.pager.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import coil.ImageLoader
import coil.compose.AsyncImage
import com.xiaoqi.video.core.design.*
import com.xiaoqi.video.core.model.*
import com.xiaoqi.video.core.network.SiteHttp
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/** Shared native home; each appearance changes layout as well as palette. */
@Composable fun NativeHomeScreen(home:HomeData,tv:Boolean,channels:List<Category>,onFilm:(Long)->Unit,onChannel:(Long)->Unit,onTopics:()->Unit,onRetry:()->Unit={},onBanner:(Banner)->Unit={ onFilm(it.filmId) },onPrefetch:((Long)->Unit)?=null,onWarmChannel:(suspend (Long)->Unit)?=null,display:CatalogDisplay=CatalogDisplay(),warmPageSize:Int=30,onLive:()->Unit={},onFilmCard:((Film)->Unit)?=null) {
    val style=LocalCinemaStyle.current
    val sections=home.sections.map { it.copy(items=it.items.filter { f->f.id>0 }.distinctBy { f->f.id }) }
    val openPoster:(Long)->Unit={ id->
        val film=sections.asSequence().flatMap { it.items.asSequence() }.find { it.id==id }
        if(onFilmCard!=null&&film!=null)onFilmCard(film) else onFilm(id)
    }
    val banners=home.banners.filter { it.title.isNotBlank()||it.filmId>0 }
    val side=if(tv)30.dp else if(style.appearance==CmsAppearance.SuxinPro)0.dp else 12.dp
    val warmupScope=rememberCoroutineScope()
    LaunchedEffect(channels.map { it.id },display,warmPageSize) {
        // One likely destination after home is visible; focus may warm another, with a global cap of two.
        channels.distinctBy { it.id }.take(1).forEach { c->launch { onWarmChannel?.invoke(c.id) } }
    }
    LazyColumn(Modifier.fillMaxSize().testTag("home-${style.appearance.code}"),contentPadding=PaddingValues(vertical=12.dp),verticalArrangement=Arrangement.spacedBy(if(style.appearance==CmsAppearance.SuxinLite)18.dp else 24.dp)) {
        if(banners.isNotEmpty())item(key="banners") { HomeHero(banners,sections,tv,onBanner,Modifier.padding(horizontal=side),onPrefetch) }
        if(channels.isNotEmpty())item(key="channels") {
            LazyRow(Modifier.fillMaxWidth().padding(horizontal=if(tv)30.dp else 12.dp).then(if(style.appearance==CmsAppearance.SuxinLite)Modifier.background(style.surface,RoundedCornerShape(16.dp)).padding(8.dp) else Modifier),horizontalArrangement=Arrangement.spacedBy(8.dp)) {
                items(channels.distinctBy { it.id },key={ it.id }) { c->
                    var focused by remember(c.id) { mutableStateOf(false) }
                    val warmOnFocus=Modifier.onFocusChanged { if(it.isFocused&&!focused)warmupScope.launch { onWarmChannel?.invoke(c.id) };focused=it.isFocused }
                    if(style.appearance==CmsAppearance.Guoguo)OutlinedButton(onClick={ onChannel(c.id) },modifier=warmOnFocus,shape=RoundedCornerShape(30.dp),contentPadding=PaddingValues(horizontal=16.dp)) { Text(c.name) }
                    else FilterChip(false,{ onChannel(c.id) },modifier=warmOnFocus,label={ Text(c.name) })
                }
            }
        }
        item(key="topics") { Row(Modifier.padding(horizontal=if(tv)30.dp else 12.dp),verticalAlignment=Alignment.CenterVertically) { TextButton(onClick=onTopics) { Icon(Icons.Default.Collections,null);Spacer(Modifier.width(8.dp));Text("精选片单") };TextButton(onClick=onLive,modifier=Modifier.testTag("home_live_entry")) { Icon(Icons.Default.LiveTv,null);Spacer(Modifier.width(6.dp));Text("直播") };Spacer(Modifier.weight(1f));TextButton(onClick=onRetry) { Icon(Icons.Default.Refresh,null);Text("刷新") } } }
        itemsIndexed(sections,key={ index,section->section.key.ifBlank { "section-$index-${section.typeId}" } }) { index,section ->
            val focusScope="home:${section.key.ifBlank { index.toString() }}"
            BoxWithConstraints(Modifier.fillMaxWidth().padding(horizontal=if(tv)30.dp else 12.dp).then(if(style.appearance==CmsAppearance.SuxinLite)Modifier.background(style.surface,RoundedCornerShape(18.dp)).padding(12.dp) else Modifier)) {
            val shownCount=section.items.size.coerceAtMost(CatalogLayoutRules.pageSize(CatalogLayoutRules.columns(maxWidth.value,display.columns),display.rows))
            Column(Modifier.fillMaxWidth(),verticalArrangement=Arrangement.spacedBy(12.dp)) {
                Row(verticalAlignment=Alignment.CenterVertically) {
                    if(style.appearance==CmsAppearance.Iqiyi)Icon(Icons.Default.PlayCircle,null,tint=style.accent,modifier=Modifier.padding(end=8.dp))
                    if(style.appearance==CmsAppearance.Guoguo)Box(Modifier.width(3.dp).height(22.dp).background(style.accent));if(style.appearance==CmsAppearance.Guoguo)Spacer(Modifier.width(8.dp))
                    Text(section.name,style=MaterialTheme.typography.titleLarge,fontWeight=FontWeight.Bold,modifier=Modifier.weight(1f))
                    if(!tv&&section.items.isNotEmpty())Text("显示 $shownCount 部",style=MaterialTheme.typography.labelSmall,color=style.muted)
                    TextButton(onClick={ onChannel(section.typeId) }) { Text("更多 →") }
                }
                when {
                    tv&&style.appearance==CmsAppearance.Guoguo&&(section.key=="hot"||section.name.contains("热播"))->RankedFilms(section.items,tv,openPoster,focusScope,onPrefetch)
                    tv&&style.appearance==CmsAppearance.SuxinPro->LazyRow(horizontalArrangement=Arrangement.spacedBy(18.dp),contentPadding=PaddingValues(4.dp)) { items(section.items,key={ it.id }) { f->Poster(f,tv,{ openPoster(f.id) },Modifier.width(190.dp).testTag("poster-${f.id}"),focusKey="$focusScope:${f.id}",onFocused={ onPrefetch?.invoke(f.id) }) } }
                    else->HomePosterGrid(section.items,tv,openPoster,focusScope,onPrefetch,display)
                }
                if(section.items.isEmpty())EmptyState("暂无影片")
                if(index==sections.lastIndex)Spacer(Modifier.height(8.dp))
            }
            }
        }
        if(sections.none { it.items.isNotEmpty() }&&banners.isEmpty())item(key="empty") { EmptyState("暂无首页推荐，可刷新或在频道中浏览影片","刷新",onRetry) }
    }
}

@Composable private fun HomeHero(banners:List<Banner>,sections:List<HomeSection>,tv:Boolean,onBanner:(Banner)->Unit,modifier:Modifier,onPrefetch:((Long)->Unit)?) {
    val style=LocalCinemaStyle.current;val scope=rememberCoroutineScope()
    val pager=rememberPagerState(pageCount={ banners.size });var focusWithin by remember { mutableStateOf(false) }
    var fullTitle by remember { mutableStateOf<String?>(null) }
    val films=remember(sections) { sections.flatMap { it.items }.associateBy { it.id } }
    LaunchedEffect(banners,pager) { while(true) { delay(6500);if(!focusWithin&&fullTitle==null&&!pager.isScrollInProgress)pager.animateScrollToPage((pager.currentPage+1)%banners.size) } }
    val heroHeight=if(tv)when(style.appearance) { CmsAppearance.SuxinLite->290.dp;CmsAppearance.SuxinPro->360.dp;CmsAppearance.Iqiyi->320.dp;CmsAppearance.Guoguo->300.dp } else when(style.appearance) { CmsAppearance.SuxinLite->230.dp;CmsAppearance.SuxinPro->290.dp;CmsAppearance.Iqiyi->260.dp;CmsAppearance.Guoguo->245.dp }
    Column(modifier.onFocusChanged { focusWithin=it.hasFocus }.focusGroup(),verticalArrangement=Arrangement.spacedBy(8.dp)) {
        HorizontalPager(pager,contentPadding=if(style.appearance==CmsAppearance.Guoguo)PaddingValues(horizontal=if(tv)70.dp else 18.dp) else PaddingValues(0.dp),pageSpacing=if(style.appearance==CmsAppearance.Guoguo)10.dp else 0.dp,modifier=Modifier.fillMaxWidth().height(heroHeight)) { index ->
            val b=banners[index];val f=films[b.filmId]
            val focus=remember(b.filmId,index) { FocusRequester() };var focused by remember { mutableStateOf(false) }
            val focusKey="home:banner:$index:${b.filmId}"
            fun activate() { if(tv) { CinemaFocus.lastFilmId=b.filmId;CinemaFocus.lastTarget=focusKey };onBanner(b) }
            LaunchedEffect(b.filmId) { if(tv&&CinemaFocus.lastTarget==focusKey) { delay(120);runCatching { focus.requestFocus() } } }
            val radius=when(style.appearance) { CmsAppearance.SuxinLite->20.dp;CmsAppearance.Guoguo->12.dp;else->0.dp }
            Box(Modifier.fillMaxSize().focusRequester(focus).onFocusChanged { if(it.isFocused&&!focused&&b.filmId>0)onPrefetch?.invoke(b.filmId);focused=it.isFocused }.clip(RoundedCornerShape(radius)).border(if(focused)3.dp else 0.dp,if(focused)style.accent else Color.Transparent,RoundedCornerShape(radius)).background(Brush.linearGradient(listOf(style.accent.copy(alpha=.48f),style.surface))).combinedClickable(onClick={ activate() },onLongClick={ fullTitle=b.title },onLongClickLabel="查看完整片名").testTag("banner-${b.filmId}-$index")) {
                NetworkImage(b.image,b.title,Modifier.fillMaxSize(),ContentScale.Crop)
                val shade=if(style.appearance==CmsAppearance.Iqiyi)Brush.horizontalGradient(listOf(Color.Black.copy(alpha=.92f),Color.Black.copy(alpha=.4f))) else Brush.verticalGradient(listOf(Color.Transparent,Color.Black.copy(alpha=.85f)))
                Box(Modifier.fillMaxSize().background(shade))
                if(style.appearance==CmsAppearance.Iqiyi) {
                    Row(Modifier.align(Alignment.Center).fillMaxWidth().padding(if(tv)30.dp else 18.dp),verticalAlignment=Alignment.CenterVertically,horizontalArrangement=Arrangement.spacedBy(18.dp)) {
                        Column(Modifier.weight(1f),verticalArrangement=Arrangement.spacedBy(10.dp)) {
                            Text("热播推荐",color=style.accent,fontWeight=FontWeight.Bold)
                            Text(b.title,color=Color.White,style=if(tv)MaterialTheme.typography.headlineLarge else MaterialTheme.typography.headlineSmall,maxLines=2,overflow=TextOverflow.Ellipsis)
                            f?.let { Text(listOf(it.year,it.area,it.remarks).filter { s->s.isNotBlank() }.joinToString(" / "),color=Color.LightGray,maxLines=1,style=MaterialTheme.typography.labelMedium) }
                            FilledIconButton(onClick={ activate() },modifier=Modifier.size(48.dp),colors=IconButtonDefaults.filledIconButtonColors(containerColor=style.accent,contentColor=Color.White)) { Icon(Icons.Default.PlayArrow,if(b.filmId>0)"立即播放" else "查看推荐") }
                        }
                        if(f?.pic?.isNotBlank()==true)NetworkImage(f.pic,f.name,Modifier.width(if(tv)160.dp else 92.dp).height(if(tv)230.dp else 138.dp).clip(RoundedCornerShape(8.dp)),ContentScale.Crop)
                    }
                } else Column(Modifier.align(Alignment.BottomStart).fillMaxWidth().padding(horizontal=if(tv)30.dp else 18.dp,vertical=20.dp),verticalArrangement=Arrangement.spacedBy(10.dp)) {
                    Text(when(style.appearance) { CmsAppearance.SuxinLite->"为你精选";CmsAppearance.SuxinPro->"今日推荐";else->"精选影视" },color=style.accent,fontWeight=FontWeight.Bold,style=MaterialTheme.typography.labelLarge)
                    Text(b.title,color=Color.White,style=if(tv)MaterialTheme.typography.headlineLarge else MaterialTheme.typography.headlineSmall,maxLines=2,overflow=TextOverflow.Ellipsis)
                    Button(onClick={ activate() },shape=RoundedCornerShape(if(style.appearance==CmsAppearance.SuxinLite)30.dp else 7.dp),colors=ButtonDefaults.buttonColors(containerColor=style.accent,contentColor=Color.White)) { Icon(Icons.Default.PlayArrow,null);Text(if(b.filmId>0)"立即播放" else "查看推荐") }
                }
                Text("${index+1}/${banners.size}",Modifier.align(Alignment.TopEnd).padding(14.dp),color=Color.White,style=MaterialTheme.typography.labelMedium)
            }
        }
        if(banners.size>1)Row(Modifier.fillMaxWidth(),horizontalArrangement=Arrangement.Center,verticalAlignment=Alignment.CenterVertically) {
            banners.indices.forEach { index->IconButton(onClick={ scope.launch { pager.animateScrollToPage(index) } },modifier=Modifier.size(32.dp)) { Box(Modifier.width(if(pager.currentPage==index)18.dp else 8.dp).height(if(style.appearance==CmsAppearance.Guoguo)4.dp else 6.dp).clip(RoundedCornerShape(3.dp)).background(if(pager.currentPage==index)style.accent else style.muted.copy(alpha=.4f))) } }
        }
    }
    fullTitle?.let { FilmTitleDialog(title=it,onDismiss={ fullTitle=null }) }
}

@Composable private fun HomePosterGrid(films:List<Film>,tv:Boolean,onFilm:(Long)->Unit,focusScope:String,onPrefetch:((Long)->Unit)?,display:CatalogDisplay) {
    val style=LocalCinemaStyle.current
    BoxWithConstraints(Modifier.fillMaxWidth()) {
        val columns=if(tv)when(style.appearance) { CmsAppearance.SuxinLite,CmsAppearance.Guoguo->6;else->5 } else CatalogLayoutRules.columns(maxWidth.value,display.columns)
        val count=if(tv)12 else CatalogLayoutRules.pageSize(columns,display.rows)
        Column(verticalArrangement=Arrangement.spacedBy(if(style.appearance==CmsAppearance.SuxinLite)12.dp else 18.dp)) {
            films.take(count).chunked(columns).forEach { row->Row(Modifier.fillMaxWidth(),horizontalArrangement=Arrangement.spacedBy(if(tv)18.dp else CatalogLayoutRules.gapDp.dp)) { row.forEach { f->Poster(f,tv,{ onFilm(f.id) },Modifier.weight(1f).testTag("poster-${f.id}"),focusKey="$focusScope:${f.id}",onFocused={ onPrefetch?.invoke(f.id) }) };repeat(columns-row.size) { Spacer(Modifier.weight(1f)) } } }
        }
    }
}

@Composable private fun RankedFilms(films:List<Film>,tv:Boolean,onFilm:(Long)->Unit,focusScope:String,onPrefetch:((Long)->Unit)?) {
    val style=LocalCinemaStyle.current
    LazyRow(horizontalArrangement=Arrangement.spacedBy(12.dp)) {
        itemsIndexed(films.take(10),key={ _,f->f.id }) { index,f->
            val focus=remember(f.id) { FocusRequester() };var focused by remember { mutableStateOf(false) }
            var showFullTitle by remember(f.id) { mutableStateOf(false) }
            LaunchedEffect(f.id) { if(tv&&CinemaFocus.lastTarget=="$focusScope:ranked:${f.id}") { delay(120);runCatching { focus.requestFocus() } } }
            Row(Modifier.width(if(tv)310.dp else 270.dp).focusRequester(focus).onFocusChanged { if(it.isFocused&&!focused)onPrefetch?.invoke(f.id);focused=it.isFocused }.clip(RoundedCornerShape(10.dp)).border(if(focused)3.dp else 0.dp,if(focused)style.accent else Color.Transparent,RoundedCornerShape(10.dp)).background(style.surface).combinedClickable(onClick={ if(tv) { CinemaFocus.lastFilmId=f.id;CinemaFocus.lastTarget="$focusScope:ranked:${f.id}" };onFilm(f.id) },onLongClick={ showFullTitle=true },onLongClickLabel="查看完整片名").testTag("poster-${f.id}").padding(10.dp),horizontalArrangement=Arrangement.spacedBy(10.dp),verticalAlignment=Alignment.CenterVertically) {
                Text("${index+1}",color=if(index<3)style.accent else style.muted,style=MaterialTheme.typography.headlineMedium,fontWeight=FontWeight.Bold)
                NetworkImage(f.pic,f.name,Modifier.width(58.dp).height(84.dp).clip(RoundedCornerShape(5.dp)),ContentScale.Crop)
                Column(Modifier.weight(1f),verticalArrangement=Arrangement.spacedBy(6.dp)) { Text(f.name,maxLines=2,overflow=TextOverflow.Ellipsis);Text(f.remarks.ifBlank { f.typeName },color=style.muted,maxLines=1,style=MaterialTheme.typography.labelSmall);if(f.score>0)Text("评分 %.1f".format(f.score),color=style.accent) }
            }
            if(showFullTitle)FilmTitleDialog(title=f.name,onDismiss={ showFullTitle=false })
        }
    }
}

@Composable private fun NetworkImage(url:String,description:String,modifier:Modifier,scale:ContentScale) {
    val context=LocalContext.current
    val loader=remember(url) { ImageLoader.Builder(context).okHttpClient(SiteHttp.client(SiteHttp.absolute(url))).build() }
    AsyncImage(model=SiteHttp.absolute(url),contentDescription=description,modifier=modifier,imageLoader=loader,contentScale=scale)
}
