package com.xiaoqi.video.mobile

import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.*
import androidx.compose.material3.LocalContentColor
import androidx.compose.material3.Text
import androidx.compose.runtime.SideEffect
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import com.xiaoqi.video.core.data.AppGraph
import com.xiaoqi.video.core.design.*
import com.xiaoqi.video.core.model.*
import com.xiaoqi.video.core.player.*
import com.xiaoqi.video.feature.catalog.PlayerScreen
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.*
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class MobilePlayerPresentationTest {
    @get:Rule val compose=createAndroidComposeRule<MainActivity>()
    @Before fun ready()=runBlocking { AppGraph.repository.ready.first { it } }
    @After fun close() { compose.activityRule.scenario.onActivity { it.setContent {};PlayerHub.engine.stop(save=false) } }

    @Test fun bareTitlesInAllFourThemesInheritAReadableContentColour() {
        CmsAppearance.entries.forEach { appearance->
            var inherited=Color.Unspecified
            compose.activityRule.scenario.onActivity { activity->activity.setContent {
                CinemaTheme(appearance) {
                    val foreground=LocalContentColor.current
                    SideEffect { inherited=foreground }
                    Text("标题和简介")
                }
            } }
            compose.waitForIdle()
            Assert.assertEquals(CinemaStyle.of(appearance).text,inherited)
            if(CinemaStyle.of(appearance).dark)Assert.assertTrue(inherited.luminance()>.6f)
        }
    }

    @Test fun retryOffersVisibleDiscoveryInsteadOfTheOldChooseLineAction() {
        showPlayer(error="当前分集暂无可用线路")
        compose.onNodeWithTag("player_error_lines").assertIsDisplayed().assertTextContains("查找更多片源")
        compose.onNodeWithText("选择线路").assertDoesNotExist()
        compose.onNodeWithTag("player_source_status").assertIsDisplayed()
    }

    @Test fun narrowControlsKeepFullscreenNextEpisodeAndMoreVisible() {
        showPlayer()
        compose.onNodeWithTag("player_fullscreen_toggle").assertIsDisplayed()
        compose.onNodeWithTag("player_next").assertIsDisplayed()
        compose.onNodeWithTag("player_more").assertIsDisplayed().performClick()
        compose.onNodeWithText("投屏").assertIsDisplayed()
        compose.onNodeWithText("播放倍速").assertIsDisplayed()
    }

    @Test fun selected161IsDisplayedWhileAnOlder160DescriptorIsBeingReplaced() {
        compose.activityRule.scenario.onActivity { activity->
            PlayerHub.engine.stop(save=false)
            val film=Film(781,"仙逆",remarks="第160集",typeName="动漫",isAnime=true)
            val source=Source("fixture",versionKey="tv",name="测试线路",episodes=listOf(Episode("episode:160",160,"第160集"),Episode("episode:161",161,"第161集")))
            PlayerHub.engine.state.value=PlayerState(detail=Detail(film,listOf(source),source.code,emptyList(),emptyList()),
                descriptor=Playback(vodId=film.id,line=source.code,versionKey="tv",episodeKey="episode:160",name="第160集"),
                requested=PlaybackIdentity(film.id,source.code,"episode:161",1,versionKey="tv"),loading=true)
            activity.setContent { CinemaTheme(CmsAppearance.Iqiyi) { Box(Modifier.width(360.dp).fillMaxHeight()) { PlayerScreen(AppGraph.repository,false,{}) } } }
        }
        compose.onNodeWithText("仙逆 · 第161集").assertIsDisplayed()
        compose.onNodeWithTag("player_current_episode").assertTextEquals("准备播放：第161集")
        compose.onNodeWithText("更新状态：第160集").assertIsDisplayed()
    }

    private fun showPlayer(error:String="") {
        compose.activityRule.scenario.onActivity { activity->
            PlayerHub.engine.stop(save=false)
            val film=Film(91806,"可读播放标题",typeName="短剧",isShort=true)
            val source=Source("fixture",versionKey="short",name="测试线路",episodes=listOf(Episode("episode:1",1),Episode("episode:2",2)))
            PlayerHub.engine.state.value=PlayerState(detail=Detail(film,listOf(source),source.code,emptyList(),emptyList()),descriptor=Playback(vodId=film.id,line=source.code,versionKey=source.versionKey,episodeKey="episode:1",durationMs=180000),error=error,duration=180000)
            activity.setContent { CinemaTheme(CmsAppearance.SuxinPro) { Box(Modifier.width(360.dp).fillMaxHeight()) { PlayerScreen(AppGraph.repository,false,{}) } } }
        }
        compose.waitForIdle()
    }
}
