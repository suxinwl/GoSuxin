package com.xiaoqi.video.tv

import android.view.KeyEvent
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import com.xiaoqi.video.core.design.*
import com.xiaoqi.video.core.model.*
import com.xiaoqi.video.feature.live.LiveProgrammePanel
import org.junit.*
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class TvLiveProgrammeTest {
    @get:Rule val compose=createAndroidComposeRule<MainActivity>()
    @After fun close() { compose.activityRule.scenario.onActivity { it.setContent {} } }
    @Test fun remoteConfirmsAProgrammeWithoutTouch() {
        var opened=0L
        val now=System.currentTimeMillis()/1000
        compose.activityRule.scenario.onActivity { activity->activity.setContent { CinemaTheme(CmsAppearance.SuxinPro) {
            Box(Modifier.width(560.dp).height(650.dp)) { LiveProgrammePanel(2,true,loadProgrammes={ id,date->LiveEpg(id,date,items=listOf(LiveProgramme(321,id,"遥控回看",now-120,now-60,true,"catchup",4))) },onReplay={ opened=it.id }) }
        } } }
        compose.waitUntil(5000) { compose.onAllNodesWithTag("live_epg_replay_321").fetchSemanticsNodes().isNotEmpty() }
        val programme=compose.onNodeWithTag("live_epg_replay_321")
        programme.performScrollTo().performSemanticsAction(androidx.compose.ui.semantics.SemanticsActions.RequestFocus) { it() }
        programme.assertIsFocused()
        InstrumentationRegistry.getInstrumentation().sendKeyDownUpSync(KeyEvent.KEYCODE_DPAD_CENTER)
        compose.runOnIdle { Assert.assertEquals(321L,opened) }
        compose.onNodeWithTag("live_epg_dates").assertIsDisplayed()
    }
}
