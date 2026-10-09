package com.xiaoqi.video.mobile

import android.view.View
import android.view.ViewGroup
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.test.ext.junit.runners.AndroidJUnit4
import com.xiaoqi.video.core.data.AppGraph
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.flow.first
import org.junit.*
import org.junit.runner.RunWith
import androidx.activity.compose.setContent
import com.xiaoqi.video.core.design.CinemaTheme
import com.xiaoqi.video.core.design.CmsAppearance
import com.xiaoqi.video.core.model.*
import com.xiaoqi.video.feature.catalog.NativeHomeScreen

@RunWith(AndroidJUnit4::class)
class NativeNavigationTest {
    @get:Rule val compose=createAndroidComposeRule<MainActivity>()
    @Before fun clearMember() { runBlocking { AppGraph.repository.ready.first { it };AppGraph.repository.logout(false);AppGraph.repository.store.setAppearance("follow") } }
    @Test fun libraryAccountAndSearchUseNativeNavigation() {
        compose.onNodeWithText("我的片库").performClick()
        compose.onNodeWithText("登录后查看收藏、历史和离线下载").assertIsDisplayed()
        compose.onNodeWithText("账号",useUnmergedTree=true).performClick()
        compose.onNodeWithText("登录小柒影视").assertIsDisplayed()
        compose.onNodeWithText("邮箱").assertExists()
        compose.onNodeWithText("搜索",useUnmergedTree=true).performClick()
        compose.onNodeWithText("搜索影片、演员").assertExists()
        compose.onNodeWithText("首页",useUnmergedTree=true).performClick()
        compose.onNodeWithText("我的片库",useUnmergedTree=true).assertExists()
    }
    @Test fun activityContainsNoWebView() {
        compose.runOnUiThread {
            fun containsWebView(view:View):Boolean = view.javaClass.name.contains("WebView") || (view is ViewGroup && (0 until view.childCount).any { containsWebView(view.getChildAt(it)) })
            Assert.assertFalse(containsWebView(compose.activity.window.decorView))
        }
    }
    @Test fun manualTemplatePersistsAfterActivityRecreation() {
        compose.onNodeWithContentDescription("模板样式").performClick()
        compose.onNodeWithTag("choose-guoguo").performClick()
        compose.waitUntil(5000) { compose.onAllNodesWithTag("appearance-guoguo").fetchSemanticsNodes().isNotEmpty() }
        compose.activityRule.scenario.recreate()
        compose.waitUntil(5000) { compose.onAllNodesWithTag("appearance-guoguo").fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithContentDescription("模板样式").performClick()
        compose.onNodeWithTag("choose-follow").performClick()
        runBlocking { kotlinx.coroutines.withTimeout(5000) { AppGraph.repository.store.appearance.first { it=="follow" } } }
    }
    @Test fun homePosterSendsFilmStraightToPlaybackEntry() {
        var selected=0L
        val home=HomeData(emptyList(),listOf(HomeSection("最近更新",0,listOf(Film(42,"点击即播")),"recent")))
        compose.activityRule.scenario.onActivity { activity->activity.setContent { CinemaTheme(CmsAppearance.Iqiyi) { NativeHomeScreen(home,false,emptyList(),{ selected=it },{},{} ) } } }
        compose.onNodeWithTag("poster-42").performClick()
        compose.runOnIdle { Assert.assertEquals(42L,selected) }
        compose.onNodeWithText("立即播放 / 续播").assertDoesNotExist()
    }
}
