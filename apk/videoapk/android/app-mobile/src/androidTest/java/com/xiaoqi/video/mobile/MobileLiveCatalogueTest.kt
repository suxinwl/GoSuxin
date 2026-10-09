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
import com.xiaoqi.video.core.player.PlayerHub
import com.xiaoqi.video.feature.live.LiveChannelsScreen
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.*
import org.junit.runner.RunWith
import java.io.IOException

/** The actual channel screen must mount its grid before it can await a scroll. */
@RunWith(AndroidJUnit4::class)
class MobileLiveCatalogueTest {
    @get:Rule val compose=createAndroidComposeRule<MainActivity>()
    @Before fun ready()=runBlocking { AppGraph.repository.ready.first { it } }
    @After fun close() { compose.activityRule.scenario.onActivity { it.setContent {};PlayerHub.engine.stop(save=false) } }

    @Test fun initialResponseMountsTheGridInEveryThemeRatherThanWaitingForItsOwnLayout() {
        CmsAppearance.entries.forEach { theme->
            showChannels(theme)
            awaitGrid()
            compose.onNodeWithTag("live_channel_101").assertIsDisplayed()
        }
    }

    @Test fun changingGroupAndPageStillCompletesTheRequestAndResetsTheGrid() {
        showChannels(CmsAppearance.SuxinPro)
        awaitGrid()
        compose.onNodeWithTag("live_group_2").performClick()
        compose.waitUntil(5000) { compose.onAllNodesWithTag("live_channel_20101").fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithTag("live_channel_20101").assertIsDisplayed()
        compose.onNodeWithTag("live_next_page").performScrollTo().performClick()
        compose.waitUntil(5000) { compose.onAllNodesWithTag("live_channel_20201").fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithTag("live_channel_20201").assertIsDisplayed()
    }

    @Test fun retryAfterTransportFailureCanMountTheSuccessfulGrid() {
        var calls=0
        showChannels(CmsAppearance.Iqiyi) { group,_,page->
            if(++calls==1)throw IOException("fixture transport failure")
            channelPage(group,page)
        }
        compose.waitUntil(5000) { compose.onAllNodesWithText("重试").fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithText("重试").performClick()
        awaitGrid()
        compose.onNodeWithTag("live_channel_101").assertIsDisplayed()
    }

    private fun awaitGrid() {
        compose.waitUntil(5000) { compose.onAllNodesWithTag("live_channel_grid").fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithTag("live_channel_grid").assertIsDisplayed()
    }

    private fun showChannels(theme:CmsAppearance,load:suspend (Long,String,Int)->LiveChannelPage = { group,_,page->channelPage(group,page) }) {
        compose.activityRule.scenario.onActivity { activity->
            activity.setContent { CinemaTheme(theme) { Box(Modifier.width(320.dp).fillMaxHeight()) {
                LiveChannelsScreen(AppGraph.repository,false,loadGroups={ listOf(LiveGroup(2,"港台频道")) },loadChannels=load,onChannel={})
            } } }
        }
    }

    private fun channelPage(group:Long,page:Int)=LiveChannelPage(
        (1..4).map { LiveChannel(group*10000+page*100+it,"频道 $it",groupId=group,groupName="港台频道") },
        total=8,page=page,size=4)
}
