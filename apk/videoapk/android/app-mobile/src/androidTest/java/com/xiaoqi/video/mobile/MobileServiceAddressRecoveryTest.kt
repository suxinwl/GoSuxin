package com.xiaoqi.video.mobile

import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.width
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import com.xiaoqi.video.core.design.CinemaTheme
import com.xiaoqi.video.core.design.CmsAppearance
import com.xiaoqi.video.core.network.EndpointDiscoveryState
import com.xiaoqi.video.feature.catalog.ServiceAddressRecoveryPanel
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class MobileServiceAddressRecoveryTest {
    @get:Rule val compose=createAndroidComposeRule<MainActivity>()
    @Test fun invalidAddressShowsChineseRetryAndNeverOffersASwitch() {
        var checked=0
        compose.activityRule.scenario.onActivity { activity->activity.setContent {
            CinemaTheme(CmsAppearance.Guoguo) { Box(Modifier.width(360.dp)) {
                ServiceAddressRecoveryPanel(EndpointDiscoveryState(status="配置中的服务暂时无法连接，已保留当前地址"),"https://old.example:8600",false,{ checked++ },{ fail("must not switch") })
            } }
        } }
        compose.onNodeWithTag("service-address-check").assertIsDisplayed().performClick()
        compose.onNodeWithTag("service-address-switch").assertDoesNotExist()
        compose.runOnIdle { assertEquals(1,checked) }
    }

    @Test fun verifiedAddressHasExplicitApplyAndBusyStateDisablesIt() {
        var switched=0
        fun show(enabled:Boolean)=compose.activityRule.scenario.onActivity { activity->activity.setContent {
            CinemaTheme(CmsAppearance.Iqiyi) { Box(Modifier.width(360.dp)) {
                ServiceAddressRecoveryPanel(EndpointDiscoveryState(verifiedCandidate="https://new.example:8600",status="发现可用的新地址，请点击切换"),"https://old.example:8600",enabled,{}, { switched++ })
            } }
        } }
        show(false);compose.onNodeWithTag("service-address-switch").assertIsNotEnabled()
        show(true);compose.onNodeWithTag("service-address-switch").assertIsDisplayed().performClick()
        compose.runOnIdle { assertEquals(1,switched) }
    }
}
