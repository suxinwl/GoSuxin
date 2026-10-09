package com.xiaoqi.video.tv

import android.view.KeyEvent
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.runtime.*
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import com.xiaoqi.video.core.design.CinemaTheme
import com.xiaoqi.video.core.design.Poster
import com.xiaoqi.video.core.model.Film
import org.junit.*
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class TvRemoteFocusTest {
    @get:Rule val compose=createAndroidComposeRule<MainActivity>()
    @Test fun dpadMovesAcrossTvButtonsAndConfirmsWithEnter() {
        var activated=""
        compose.activityRule.scenario.onActivity { activity ->
            activity.setContent {
                CinemaTheme {
                    val first=remember { FocusRequester() }
                    Row(Modifier.padding(32.dp),horizontalArrangement=Arrangement.spacedBy(24.dp)) {
                        androidx.tv.material3.Button(onClick={ activated="home" },modifier=Modifier.focusRequester(first)) { androidx.compose.material3.Text("首页按钮") }
                        androidx.tv.material3.Button(onClick={ activated="channels" }) { androidx.compose.material3.Text("频道按钮") }
                    }
                    LaunchedEffect(Unit) { first.requestFocus() }
                }
            }
        }
        compose.onNodeWithText("首页按钮").assertIsFocused()
        InstrumentationRegistry.getInstrumentation().sendKeyDownUpSync(KeyEvent.KEYCODE_DPAD_RIGHT)
        compose.waitForIdle()
        compose.onNodeWithText("频道按钮").assertIsFocused()
        InstrumentationRegistry.getInstrumentation().sendKeyDownUpSync(KeyEvent.KEYCODE_DPAD_CENTER)
        compose.runOnIdle { Assert.assertEquals("channels",activated) }
    }
    @Test fun posterIsFocusableAndUsesRemoteConfirm() {
        var opened=false
        compose.activityRule.scenario.onActivity { activity ->
            activity.setContent { CinemaTheme { val focus=remember { FocusRequester() };Poster(Film(9,"焦点测试"),tv=true,onClick={ opened=true },modifier=Modifier.width(160.dp),requester=focus);LaunchedEffect(Unit) { focus.requestFocus() } } }
        }
        compose.onNodeWithText("焦点测试",useUnmergedTree=false).assertExists()
        InstrumentationRegistry.getInstrumentation().sendKeyDownUpSync(KeyEvent.KEYCODE_DPAD_CENTER)
        compose.runOnIdle { Assert.assertTrue(opened) }
    }
}
