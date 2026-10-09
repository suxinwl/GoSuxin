package com.xiaoqi.video.mobile

import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import com.xiaoqi.video.core.design.*
import com.xiaoqi.video.core.model.*
import com.xiaoqi.video.feature.live.LiveProgrammePanel
import org.junit.*
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class MobileLiveProgrammeTest {
    @get:Rule val compose=createAndroidComposeRule<MainActivity>()
    @After fun close() { compose.activityRule.scenario.onActivity { it.setContent {} } }
    @Test fun pastProgrammeIsPlayableOnlyWithServerCapabilityInEveryTheme() {
        CmsAppearance.entries.forEach { theme->
            var opened=0L
            val now=System.currentTimeMillis()/1000
            compose.activityRule.scenario.onActivity { activity->activity.setContent {
                CinemaTheme(theme) { Box(Modifier.width(320.dp).height(650.dp)) {
                    LiveProgrammePanel(2,false,loadProgrammes={ id,date->LiveEpg(id,date,items=listOf(
                        LiveProgramme(321,id,"可回看新闻",now-120,now-60,true,"catchup",4),
                        LiveProgramme(322,id,"无回看节目",now-240,now-180),
                        LiveProgramme(323,id,"未来节目",now+120,now+180,true,"catchup",4))) },onReplay={ opened=it.id })
                } }
            } }
            compose.waitUntil(5000) { compose.onAllNodesWithTag("live_epg_replay_321").fetchSemanticsNodes().isNotEmpty() }
            compose.onNodeWithTag("live_epg_replay_321").performScrollTo().performClick()
            compose.runOnIdle { Assert.assertEquals(321L,opened) }
            compose.onNodeWithTag("live_epg_replay_322").assertDoesNotExist()
            compose.onNodeWithTag("live_epg_replay_323").assertDoesNotExist()
        }
    }
    @Test fun emptyScheduleHasAnExplanationAndDateChangeStillCompletes() {
        var requested=""
        compose.activityRule.scenario.onActivity { activity->activity.setContent { CinemaTheme {
            Box(Modifier.width(320.dp).height(650.dp)) { LiveProgrammePanel(2,false,loadProgrammes={ id,date->requested=date;LiveEpg(id,date) },onReplay={}) }
        } } }
        compose.waitUntil(5000) { compose.onAllNodesWithTag("live_epg_empty").fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithTag("live_epg_empty").assertIsDisplayed()
        val today=LiveProgrammeRules.today()
        compose.onNodeWithTag("live_epg_date_$today").performScrollTo().performClick()
        compose.onNodeWithTag("live_epg_refresh").performClick()
        compose.waitUntil(5000) { requested==today }
        compose.onNodeWithTag("live_epg_empty").assertIsDisplayed()
    }
}
