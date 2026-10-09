package com.xiaoqi.video.mobile

import android.content.Context
import android.content.pm.ActivityInfo
import android.media.AudioManager
import androidx.activity.compose.setContent
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.test.ext.junit.runners.AndroidJUnit4
import com.xiaoqi.video.core.data.AppGraph
import com.xiaoqi.video.core.design.CinemaTheme
import com.xiaoqi.video.core.model.*
import com.xiaoqi.video.core.player.PlayerHub
import com.xiaoqi.video.core.player.PlayerState
import com.xiaoqi.video.feature.catalog.PlayerScreen
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.flow.first
import org.junit.*
import org.junit.runner.RunWith

/** Local player fixtures: no media or test account is written to the server. */
@RunWith(AndroidJUnit4::class)
class MobilePlayerGestureTest {
    @get:Rule val compose=createAndroidComposeRule<MainActivity>()
    private lateinit var audio:AudioManager
    private var originalVolume=0
    private var originalBrightness=-1f
    private var originalOrientation=ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED
    @Before fun showPlayer() {
        runBlocking { AppGraph.repository.ready.first { it };AppGraph.repository.logout(false) }
        compose.activityRule.scenario.onActivity { activity ->
            audio=activity.getSystemService(Context.AUDIO_SERVICE) as AudioManager
            originalVolume=audio.getStreamVolume(AudioManager.STREAM_MUSIC)
            originalBrightness=activity.window.attributes.screenBrightness
            originalOrientation=activity.requestedOrientation
            activity.window.attributes=activity.window.attributes.apply { screenBrightness=.5f }
            PlayerHub.engine.stop(save=false)
            val film=Film(91802,"竖屏手势测试",typeName="短剧",isShort=true)
            PlayerHub.engine.state.value=PlayerState(detail=Detail(film,emptyList(),"",emptyList(),emptyList()),descriptor=Playback(vodId=film.id,name="第1集",durationMs=180000),position=12000,duration=180000)
            activity.setContent { CinemaTheme { PlayerScreen(AppGraph.repository,false,{}) } }
        }
        compose.waitForIdle()
    }
    @After fun restoreDevice() {
        compose.activityRule.scenario.onActivity { activity ->
            activity.setContent {}
            PlayerHub.engine.stop(save=false)
            audio.setStreamVolume(AudioManager.STREAM_MUSIC,originalVolume,0)
            activity.window.attributes=activity.window.attributes.apply { screenBrightness=originalBrightness }
            activity.requestedOrientation=originalOrientation
        }
    }
    @Test fun leftVerticalSwipeChangesWindowBrightnessAndLeavesVolumeAlone() {
        compose.onNodeWithTag("player_surface").performTouchInput {
            swipe(Offset(width*.25f,height*.8f),Offset(width*.25f,height*.2f),400)
        }
        compose.runOnIdle {
            Assert.assertTrue(compose.activity.window.attributes.screenBrightness>.5f)
            Assert.assertEquals(originalVolume,audio.getStreamVolume(AudioManager.STREAM_MUSIC))
        }
    }
    @Test fun rightVerticalSwipeChangesMediaVolumeAndLeavesBrightnessAlone() {
        Assume.assumeFalse(audio.isVolumeFixed)
        val max=audio.getStreamMaxVolume(AudioManager.STREAM_MUSIC)
        Assume.assumeTrue(max>1)
        val initial=max/2
        compose.runOnIdle { audio.setStreamVolume(AudioManager.STREAM_MUSIC,initial,0) }
        compose.onNodeWithTag("player_surface").performTouchInput {
            swipe(Offset(width*.75f,height*.8f),Offset(width*.75f,height*.2f),400)
        }
        compose.runOnIdle {
            Assert.assertTrue(audio.getStreamVolume(AudioManager.STREAM_MUSIC)>initial)
            Assert.assertEquals(.5f,compose.activity.window.attributes.screenBrightness,.001f)
        }
    }
    @Test fun fullscreenSwitchesOrientationWithoutReplacingPlayback() {
        val originalPlayer=PlayerHub.engine.player
        compose.onNodeWithTag("player_fullscreen_toggle").performClick()
        compose.onNodeWithTag("player_orientation").assertIsDisplayed()
        compose.runOnIdle { Assert.assertEquals(ActivityInfo.SCREEN_ORIENTATION_SENSOR_PORTRAIT,compose.activity.requestedOrientation) }
        compose.onNodeWithTag("player_orientation").performClick()
        compose.runOnIdle {
            Assert.assertEquals(ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE,compose.activity.requestedOrientation)
            Assert.assertEquals(91802L,PlayerHub.engine.state.value.descriptor?.vodId)
            Assert.assertSame(originalPlayer,PlayerHub.engine.player)
        }
        compose.onNodeWithTag("player_fullscreen_exit").performClick()
        compose.runOnIdle { Assert.assertEquals(originalOrientation,compose.activity.requestedOrientation) }
    }
}
