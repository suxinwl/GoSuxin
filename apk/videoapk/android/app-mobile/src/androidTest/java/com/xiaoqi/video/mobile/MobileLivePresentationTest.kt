package com.xiaoqi.video.mobile

import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import com.xiaoqi.video.core.data.AppGraph
import com.xiaoqi.video.core.design.*
import com.xiaoqi.video.core.model.*
import com.xiaoqi.video.core.player.*
import com.xiaoqi.video.feature.live.LivePlayerScreen
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.*
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class MobileLivePresentationTest {
    @get:Rule val compose=createAndroidComposeRule<MainActivity>()
    @Before fun ready()=runBlocking { AppGraph.repository.ready.first { it } }
    @After fun close() { compose.activityRule.scenario.onActivity { it.setContent {};PlayerHub.engine.stop(save=false) } }

    @Test fun narrowLiveControlsKeepBothPrimaryAndChannelActionsVisibleInAllThemes() {
        CmsAppearance.entries.forEach { theme->
            showPlayer(theme)
            listOf("live_play_pause","live_return_to_edge","live_fullscreen","live_channels_button","live_lines_button","live_quality_button").forEach {
                compose.onNodeWithTag(it).assertIsDisplayed()
            }
            compose.onNodeWithTag("live_panel_channels_tab").assertIsDisplayed()
            compose.onNodeWithTag("live_panel_streams_tab").assertIsDisplayed()
        }
    }

    @Test fun lineDialogShowsChineseFallbackNameAndItsActualConnectionStatus() {
        showPlayer(CmsAppearance.SuxinPro)
        compose.onNodeWithTag("live_lines_button").performClick()
        compose.onNodeWithTag("live_dialog_stream_11").assertIsDisplayed().assertTextContains("主线路")
        compose.onNodeWithTag("live_dialog_stream_11").assertTextContains("原始清晰度")
        compose.onNodeWithTag("live_dialog_stream_12").assertIsDisplayed().assertTextContains("备用线路 1")
        compose.onNodeWithTag("live_dialog_stream_12").assertTextContains("本轮连接失败")
    }

    private fun showPlayer(theme:CmsAppearance) {
        compose.activityRule.scenario.onActivity { activity->
            PlayerHub.engine.stop(save=false)
            val streams=listOf(LiveStream(11,"8AVC Primary","healthy"),LiveStream(12,"Backup","healthy"))
            val channel=LiveChannel(91909,"凤凰中文",groupName="港台频道",streams=streams)
            PlayerHub.engine.live.state.value=LivePlayerState(active=true,channel=channel,descriptor=LivePlayback(channelId=channel.id,streamId=11,streams=streams),failedStreamIds=setOf(12))
            activity.setContent { CinemaTheme(theme) { Box(Modifier.width(320.dp).fillMaxHeight()) { LivePlayerScreen(AppGraph.repository,false,{},{}) } } }
        }
        compose.waitForIdle()
    }
}
