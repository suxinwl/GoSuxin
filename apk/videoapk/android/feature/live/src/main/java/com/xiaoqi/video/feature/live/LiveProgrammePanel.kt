package com.xiaoqi.video.feature.live

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp
import com.xiaoqi.video.core.data.LiveEpgLoader
import com.xiaoqi.video.core.design.LocalCinemaStyle
import com.xiaoqi.video.core.model.*
import kotlinx.coroutines.delay

/** Native schedule UI; the server, not an EPG title or date, authorizes each replay. */
@Composable fun LiveProgrammePanel(
    channelId:Long,tv:Boolean,activeProgrammeId:Long=0,ownerKey:Long=0,
    loadProgrammes:suspend (Long,String)->LiveEpg,onReplay:(LiveProgramme)->Unit
) {
    val style=LocalCinemaStyle.current
    var now by remember { mutableLongStateOf(System.currentTimeMillis()/1000) }
    var date by rememberSaveable(channelId) { mutableStateOf(LiveProgrammeRules.today()) }
    var reload by remember { mutableIntStateOf(0) }
    val scope=rememberCoroutineScope()
    val currentLoad by rememberUpdatedState(loadProgrammes)
    val loader=remember(scope) { LiveEpgLoader(scope) { channel,day->currentLoad(channel,day) } }
    val schedule by loader.state.collectAsState()
    val grid=rememberLazyListState()
    val dates=rememberLazyListState()
    val dateFocus=remember(date) { FocusRequester() }
    val epg=schedule.result
    val timezone=epg?.timezone?:"Asia/Shanghai"
    val choices=LiveProgrammeRules.dates(now,timezone)
    DisposableEffect(loader) { onDispose { loader.close() } }
    LaunchedEffect(Unit) { while(true) { delay(30000);now=System.currentTimeMillis()/1000 } }
    LaunchedEffect(channelId,date,reload,ownerKey) { if(channelId>0)loader.load(channelId,date) }
    // Layout work cannot block the request's completion, including empty EPG results.
    LaunchedEffect(schedule.result,schedule.loading) { if(!schedule.loading&&!epg?.items.isNullOrEmpty())grid.scrollToItem(0) }
    LaunchedEffect(date,choices) { val index=choices.indexOf(date);if(index>=0)dates.scrollToItem(index) }
    LaunchedEffect(tv) { if(tv) { delay(120);runCatching { dateFocus.requestFocus() } } }
    Column(Modifier.fillMaxSize().testTag("live_programme_panel"),verticalArrangement=Arrangement.spacedBy(8.dp)) {
        Text("节目单与回看",style=MaterialTheme.typography.titleMedium,color=style.text)
        val current=epg?.now;val next=epg?.next
        if(current!=null)Text("正在播出：${current.title} · ${LiveProgrammeRules.range(current,timezone)}",color=style.accent,style=MaterialTheme.typography.bodySmall)
        if(next!=null)Text("下一档：${next.title} · ${LiveProgrammeRules.clock(next.start,timezone)}",color=style.muted,style=MaterialTheme.typography.bodySmall)
        LazyRow(state=dates,horizontalArrangement=Arrangement.spacedBy(6.dp),modifier=Modifier.fillMaxWidth().testTag("live_epg_dates")) {
            items(choices,key={ it }) { day->
                val today=LiveProgrammeRules.today(now,timezone)
                LiveAction(tv,if(day==today)"今天" else day.substring(5),"live_epg_date_$day",if(day==date)Modifier.focusRequester(dateFocus) else Modifier) { date=day }
            }
        }
        Row(Modifier.fillMaxWidth(),verticalAlignment=Alignment.CenterVertically) {
            Text(date,Modifier.weight(1f),color=style.muted,style=MaterialTheme.typography.labelMedium)
            LiveAction(tv,"刷新","live_epg_refresh") { reload++ }
        }
        when {
            schedule.loading->Box(Modifier.fillMaxWidth().weight(1f),contentAlignment=Alignment.Center) { Column(horizontalAlignment=Alignment.CenterHorizontally) { CircularProgressIndicator();Text("正在读取节目单",color=style.muted) } }
            schedule.error.isNotBlank()->Column(Modifier.fillMaxWidth().weight(1f),verticalArrangement=Arrangement.spacedBy(8.dp)) {
                Text(schedule.error,color=style.text);LiveAction(tv,"重试节目单","live_epg_retry") { reload++ }
            }
            epg?.items.isNullOrEmpty()->Column(Modifier.fillMaxWidth().weight(1f),verticalArrangement=Arrangement.spacedBy(8.dp)) {
                Text("该频道当天暂无节目单",color=style.text,modifier=Modifier.testTag("live_epg_empty"))
                Text("节目单与回看由源站提供，未提供的节目不会显示回看入口。直播仍可正常观看。",color=style.muted,style=MaterialTheme.typography.bodySmall)
            }
            else->LazyColumn(Modifier.weight(1f).fillMaxWidth().testTag("live_epg_list"),state=grid,verticalArrangement=Arrangement.spacedBy(8.dp)) {
                items(epg!!.items,key={ it.id }) { programme->
                    val playable=LiveProgrammeRules.replayable(programme,now)
                    val detail=LiveProgrammeRules.range(programme,timezone)+" · "+LiveProgrammeRules.phase(programme,now)+(if(programme.accessLevel=="member")" · 会员" else "")
                    if(playable)LiveChoiceRow(tv,programme.title,detail,activeProgrammeId==programme.id,"live_epg_replay_${programme.id}") { onReplay(programme) }
                    else Column(Modifier.fillMaxWidth().padding(12.dp).testTag("live_epg_programme_${programme.id}")) {
                        Text(programme.title,color=style.text,style=MaterialTheme.typography.titleSmall)
                        Text(detail,color=style.muted,style=MaterialTheme.typography.labelSmall)
                    }
                }
            }
        }
    }
}
