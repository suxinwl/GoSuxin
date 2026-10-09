package com.xiaoqi.video.core.design

import android.app.Activity
import android.content.Context
import android.content.ContextWrapper
import androidx.compose.foundation.*
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.Alignment
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.scale
import androidx.compose.ui.focus.*
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import coil.compose.AsyncImage
import coil.ImageLoader
import coil.request.ImageRequest
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalView
import androidx.core.view.WindowInsetsControllerCompat
import com.xiaoqi.video.core.model.Film
import com.xiaoqi.video.core.network.SiteHttp

enum class CmsAppearance(val code:String,val label:String) {
    SuxinLite("suxinlite","Suxinlite"),SuxinPro("suxinpro","SuxinPro"),Iqiyi("iqiyi","Iqiyi"),Guoguo("guoguo","果果剧库");
    companion object {
        fun fromCode(code:String)=entries.find { it.code==code.trim().lowercase() }?:SuxinLite
        fun resolve(preference:String,website:String)=fromCode(if(preference.isBlank()||preference=="follow")website else preference)
    }
}
data class CinemaStyle(val appearance:CmsAppearance,val dark:Boolean,val background:Color,val surface:Color,val surfaceVariant:Color,val accent:Color,val text:Color,val muted:Color,val posterRadius:Int,val posterRatio:Float) {
    companion object {
        fun of(appearance:CmsAppearance)=when(appearance) {
            CmsAppearance.SuxinLite->CinemaStyle(appearance,false,Color(0xFFF0F2F7),Color.White,Color(0xFFE8EBF2),Color(0xFF6366F1),Color(0xFF1A1D26),Color(0xFF7A8091),14,.68f)
            CmsAppearance.SuxinPro->CinemaStyle(appearance,true,Color(0xFF0D0E12),Color(0xFF1A1C23),Color(0xFF22242D),Color(0xFFE5322D),Color(0xFFE9EAEE),Color(0xFF8B8E99),5,.67f)
            CmsAppearance.Iqiyi->CinemaStyle(appearance,true,Color(0xFF0B0C0E),Color(0xFF1A1C23),Color(0xFF26292F),Color(0xFF1FB875),Color(0xFFE9EAEE),Color(0xFF8B8E99),8,.73f)
            CmsAppearance.Guoguo->CinemaStyle(appearance,true,Color(0xFF12171B),Color(0xFF1A1E24),Color(0xFF23282F),Color(0xFFFF4081),Color(0xFFE5E7EB),Color(0xFF9CA3AF),8,.68f)
        }
    }
}
val LocalCinemaStyle=staticCompositionLocalOf { CinemaStyle.of(CmsAppearance.SuxinLite) }
val CinemaBackground:Color @Composable get()=LocalCinemaStyle.current.background
val CinemaSurface:Color @Composable get()=LocalCinemaStyle.current.surface
val CinemaAccent:Color @Composable get()=LocalCinemaStyle.current.accent
object CinemaFocus { var lastFilmId:Long=0;var lastTarget:String="" }
@Composable fun CinemaTheme(appearance:CmsAppearance=CmsAppearance.SuxinLite,content:@Composable ()->Unit) {
    val style=remember(appearance) { CinemaStyle.of(appearance) }
    val scheme=if(style.dark)darkColorScheme(primary=style.accent,secondary=style.accent,background=style.background,surface=style.surface,surfaceVariant=style.surfaceVariant,onBackground=style.text,onSurface=style.text,onSurfaceVariant=style.muted)
        else lightColorScheme(primary=style.accent,secondary=style.accent,background=style.background,surface=style.surface,surfaceVariant=style.surfaceVariant,onBackground=style.text,onSurface=style.text,onSurfaceVariant=style.muted)
    val view=LocalView.current
    val activity=LocalContext.current.brandActivity()
    SideEffect {
        if(!view.isInEditMode && activity!=null)WindowInsetsControllerCompat(activity.window,view).apply {
            isAppearanceLightStatusBars=!style.dark
            isAppearanceLightNavigationBars=!style.dark
        }
    }
    // MaterialTheme supplies the palette but does not set the colour inherited by
    // bare Text outside a Surface (the standalone native player has no Scaffold).
    CompositionLocalProvider(LocalCinemaStyle provides style) {
        MaterialTheme(colorScheme=scheme) {
            CompositionLocalProvider(LocalContentColor provides style.text,content=content)
        }
    }
}
private tailrec fun Context.brandActivity():Activity?=when(this) {
    is Activity->this
    is ContextWrapper->baseContext.brandActivity()
    else->null
}
@Composable fun Poster(film:Film,tv:Boolean=false,onClick:()->Unit,modifier:Modifier=Modifier,requester:FocusRequester?=null,interactive:Boolean=true,focusKey:String="poster-${film.cardKey}",onFocused:(()->Unit)?=null) {
    var focused by remember { mutableStateOf(false) }
    var showFullTitle by remember(film.cardKey,film.name) { mutableStateOf(false) }
    val style=LocalCinemaStyle.current
    val shape=RoundedCornerShape(style.posterRadius.dp)
    val posterFocus=requester?:remember(film.cardKey) { FocusRequester() }
    LaunchedEffect(film.cardKey,focusKey) { if(interactive&&tv&&(film.id>0&&CinemaFocus.lastFilmId==film.id||film.id==0L&&CinemaFocus.lastTarget==focusKey)&&(CinemaFocus.lastTarget.isBlank()||CinemaFocus.lastTarget==focusKey)) { delay(120);runCatching { posterFocus.requestFocus() } } }
    Column(modifier.focusRequester(posterFocus).onFocusChanged { if(it.isFocused&&!focused)onFocused?.invoke();focused=it.isFocused }.scale(if(focused&&tv)1.035f else 1f).clip(shape).border(if(focused)3.dp else 0.dp,if(focused)CinemaAccent else Color.Transparent,shape).then(if(interactive)Modifier.combinedClickable(
        onClickLabel="播放 ${film.name}",
        onLongClickLabel="查看完整标题",
        onLongClick={ showFullTitle=true },
        onClick={ if(tv) { CinemaFocus.lastFilmId=film.id;CinemaFocus.lastTarget=focusKey };onClick() }
    ) else Modifier).background(if(style.appearance==CmsAppearance.SuxinLite)style.surface else Color.Transparent).padding(if(tv)4.dp else 0.dp)) {
        Box(Modifier.fillMaxWidth().aspectRatio(style.posterRatio).background(CinemaSurface),contentAlignment=Alignment.Center) {
            Text(film.name.take(12),Modifier.padding(16.dp),color=Color.Gray)
            val imageContext=LocalContext.current
            val imageLoader=remember(film.pic) { ImageLoader.Builder(imageContext).okHttpClient(SiteHttp.client(SiteHttp.absolute(film.pic))).build() }
            AsyncImage(model=ImageRequest.Builder(imageContext).data(SiteHttp.absolute(film.pic)).crossfade(true).build(),imageLoader=imageLoader,contentDescription=film.name,contentScale=ContentScale.Crop,modifier=Modifier.fillMaxSize())
            if(film.score>0)Text("%.1f".format(film.score),Modifier.align(Alignment.TopEnd).padding(5.dp).clip(RoundedCornerShape(4.dp)).background(Color.Black.copy(alpha=.75f)).padding(horizontal=5.dp,vertical=2.dp),color=CinemaAccent,fontWeight=FontWeight.Bold)
            if(film.remarks.isNotBlank())Text(film.remarks,Modifier.align(Alignment.BottomEnd).padding(5.dp).background(Color.Black.copy(alpha=.65f)).padding(4.dp),style=MaterialTheme.typography.labelSmall,maxLines=1,color=Color.White)
        }
        val titleModifier=Modifier.fillMaxWidth().padding(top=8.dp,start=if(style.appearance==CmsAppearance.SuxinLite)8.dp else 3.dp,end=3.dp)
        if(tv)OverflowTitle(film.name,titleModifier,style=MaterialTheme.typography.titleMedium,color=style.text,animate=focused)
        else Text(film.name,titleModifier,minLines=2,maxLines=2,overflow=TextOverflow.Ellipsis,style=MaterialTheme.typography.bodyMedium,color=style.text)
        Text(listOf(film.year,film.typeName).filter { it.isNotBlank() }.joinToString(" · "),Modifier.padding(start=if(style.appearance==CmsAppearance.SuxinLite)8.dp else 3.dp,bottom=8.dp,top=3.dp),style=MaterialTheme.typography.labelSmall,color=style.muted,maxLines=1)
    }
    if(showFullTitle)FilmTitleDialog(film.name) { showFullTitle=false }
}
@Composable fun EmptyState(message:String,action:String?=null,onAction:()->Unit={}) {
    Column(Modifier.fillMaxWidth().padding(36.dp),horizontalAlignment=Alignment.CenterHorizontally,verticalArrangement=Arrangement.spacedBy(12.dp)) { Text(message,color=LocalCinemaStyle.current.muted);if(action!=null)Button(onClick=onAction) { Text(action) } }
}
@Composable fun Loading() { Box(Modifier.fillMaxWidth().padding(32.dp),contentAlignment=Alignment.Center) { CircularProgressIndicator() } }
fun formatTime(ms:Long):String { val s=ms.coerceAtLeast(0)/1000;return if(s>=3600)"%d:%02d:%02d".format(s/3600,(s/60)%60,s%60) else "%d:%02d".format(s/60,s%60) }
