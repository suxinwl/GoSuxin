package com.xiaoqi.video.tv

import android.view.KeyEvent
import androidx.activity.compose.setContent
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import com.xiaoqi.video.core.data.AppGraph
import com.xiaoqi.video.core.design.CinemaTheme
import com.xiaoqi.video.core.model.*
import com.xiaoqi.video.core.player.PlayerHub
import com.xiaoqi.video.core.player.PlayerState
import com.xiaoqi.video.feature.catalog.PlayerScreen
import org.junit.*
import org.junit.runner.RunWith

/** Native layout/remote tests: fixtures never create a CMS film or a collection source. */
@RunWith(AndroidJUnit4::class)
class TvPlaybackScreenTest {
    @get:Rule val compose=createAndroidComposeRule<MainActivity>()
    private var exited=false
    private var loginRequested=false
    private var selectedFilm=0L
    private val first=Source("yqk_1","yqk","vod:91800","小柒APP",listOf(Episode("e1",1,"第01集"),Episode("e2",2,"第02集")))
    private val second=Source("hnm3u8","hnm3u8","vod:91800","红牛",listOf(Episode("h1",1,"第01集"),Episode("h2",2,"第02集")))
    private val detail=Detail(Film(id=91800,name="播放界面测试",year="2026",typeName="电视剧",actor="演员测试",director="导演测试",content="这是播放页内的影片简介。",score=8.8),listOf(first,second),"yqk_1",listOf(Comment(1,"观众","评论面板测试","2026-10-03"))+(2..25).map { i->Comment(i.toLong(),"观众$i","评论面板测试 $i","2026-10-03") },listOf(Film(91801,"相关推荐")))

    private fun show(loading:Boolean=false,error:String="",descriptor:Boolean=true) {
        compose.activityRule.scenario.onActivity { activity->
            PlayerHub.engine.stop(save=false)
            AppGraph.repository.session.value=null
            AppGraph.repository.api.token=""
            PlayerHub.engine.state.value=PlayerState(
                detail=detail,
                descriptor=if(descriptor)Playback(vodId=detail.film.id,line=first.code,versionKey=first.versionKey,episodeKey="e1",episode=0,name="第01集",durationMs=180000) else null,
                position=12000,duration=180000,loading=loading,error=error
            )
            activity.setContent { CinemaTheme { PlayerScreen(AppGraph.repository,tv=true,onBack={ exited=true },onLogin={ loginRequested=true },onFilm={ selectedFilm=it },onMembership={ loginRequested=true }) } }
        }
        compose.mainClock.advanceTimeBy(300)
        compose.waitForIdle()
    }
    private fun remote(key:Int) {
        InstrumentationRegistry.getInstrumentation().sendKeyDownUpSync(key)
        compose.mainClock.advanceTimeBy(150)
        compose.waitForIdle()
    }

    @After fun stopFixture() { compose.runOnIdle { PlayerHub.engine.stop(save=false) } }

    @Test fun startsWithVideoAndEpisodePanelSideBySide() {
        show()
        compose.onNodeWithTag("player_info_panel").assertIsDisplayed()
        compose.onNodeWithTag("player_source_yqk_1").assertIsDisplayed()
        compose.onNodeWithTag("player_episode_yqk_1_e1").assertIsDisplayed()
        compose.onNodeWithTag("player_next").assertIsDisplayed()
        compose.onNodeWithTag("player_fullscreen_exit").assertDoesNotExist()
        val video=compose.onNodeWithTag("player_video").fetchSemanticsNode().boundsInRoot
        val panel=compose.onNodeWithTag("player_info_panel").fetchSemanticsNode().boundsInRoot
        Assert.assertTrue("TV information panel must be beside the video",video.right<=panel.left)
    }

    @Test fun remoteEntersFullscreenAndBackRestoresPanelBeforeLeaving() {
        show()
        compose.onNodeWithTag("player_play_pause").assertIsFocused()
        remote(KeyEvent.KEYCODE_DPAD_RIGHT)
        compose.onNodeWithTag("player_fullscreen_toggle").assertIsFocused()
        remote(KeyEvent.KEYCODE_DPAD_CENTER)
        compose.onNodeWithTag("player_info_panel").assertDoesNotExist()
        compose.onNodeWithTag("player_fullscreen_exit").assertIsDisplayed()
        remote(KeyEvent.KEYCODE_BACK)
        compose.onNodeWithTag("player_info_panel").assertIsDisplayed()
        compose.runOnIdle { Assert.assertFalse(exited) }
        remote(KeyEvent.KEYCODE_BACK)
        compose.runOnIdle { Assert.assertTrue(exited) }
    }

    @Test fun hiddenFullscreenControlsCanBeRestoredByRemote() {
        show()
        compose.onNodeWithTag("player_fullscreen_toggle").performClick()
        compose.onNodeWithTag("player_hide_controls").performClick()
        compose.mainClock.advanceTimeBy(250)
        compose.onNodeWithTag("player_controls").assertDoesNotExist()
        compose.onNodeWithTag("player_surface").assertIsFocused()
        remote(KeyEvent.KEYCODE_DPAD_DOWN)
        compose.onNodeWithTag("player_play_pause").assertIsDisplayed().assertIsFocused()
        compose.onNodeWithTag("player_fullscreen_toggle").assertIsDisplayed()
    }

    @Test fun remoteCanMoveFromVideoControlsToEpisodeInformation() {
        show()
        remote(KeyEvent.KEYCODE_DPAD_RIGHT)
        remote(KeyEvent.KEYCODE_DPAD_RIGHT)
        compose.onNodeWithTag("player_info").assertIsFocused()
        remote(KeyEvent.KEYCODE_DPAD_CENTER)
        compose.mainClock.advanceTimeBy(250)
        compose.onNodeWithTag("player_tab_episodes").assertIsFocused()
        remote(KeyEvent.KEYCODE_DPAD_RIGHT)
        compose.onNodeWithTag("player_tab_about").assertIsFocused()
        remote(KeyEvent.KEYCODE_DPAD_CENTER)
        compose.onNodeWithText("这是播放页内的影片简介。").assertIsDisplayed()
    }

    @Test fun informationControlRestoresPanelFocusAfterDeepCommentScroll() {
        show()
        compose.onNodeWithTag("player_tab_comments").performClick()
        compose.onNodeWithTag("player_info_list").performScrollToNode(hasText("评论面板测试 25"))
        compose.onNodeWithTag("player_tab_episodes").assertDoesNotExist()
        compose.onNodeWithTag("player_info").performClick()
        compose.mainClock.advanceTimeBy(400)
        compose.onNodeWithTag("player_tab_episodes").assertIsDisplayed().assertIsFocused()
    }

    @Test fun firstResolveErrorStillOffersSourceAndEpisodeControls() {
        show(error="资源线路暂时不可用",descriptor=false)
        compose.onNodeWithTag("player_source_hnm3u8").assertIsDisplayed()
        compose.onNodeWithTag("player_episode_yqk_1_e2").assertIsDisplayed()
        compose.onNodeWithTag("player_error_lines").performClick()
        compose.onNodeWithTag("player_line_dialog_hnm3u8").assertIsDisplayed()
        remote(KeyEvent.KEYCODE_BACK)
        compose.onNodeWithTag("player_info_panel").assertIsDisplayed()
        compose.runOnIdle { Assert.assertFalse(exited) }
    }

    @Test fun playbackPageKeepsLoginSynopsisCommentsAndRecommendations() {
        show()
        compose.onNodeWithTag("player_favorite").performClick()
        compose.runOnIdle { Assert.assertTrue(loginRequested) }
        compose.onNodeWithTag("player_tab_about").performClick()
        compose.onNodeWithText("这是播放页内的影片简介。").assertIsDisplayed()
        compose.onNodeWithTag("player_info_list").performScrollToNode(hasText("相关推荐"))
        compose.onNodeWithText("相关推荐").performClick()
        compose.runOnIdle { Assert.assertEquals(91801,selectedFilm) }
        compose.onNodeWithTag("player_info_list").performScrollToIndex(2)
        compose.onNodeWithTag("player_tab_comments").performClick()
        compose.onNodeWithTag("player_comment_submit").assertIsDisplayed()
        compose.onNodeWithTag("player_info_list").performScrollToNode(hasText("评论面板测试"))
        compose.onNodeWithText("评论面板测试").assertIsDisplayed()
    }
}
