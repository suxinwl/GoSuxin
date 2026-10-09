package com.xiaoqi.video.mobile

import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import com.xiaoqi.video.core.data.AppGraph
import com.xiaoqi.video.core.design.CinemaTheme
import com.xiaoqi.video.core.design.CmsAppearance
import com.xiaoqi.video.core.model.*
import com.xiaoqi.video.feature.catalog.NativeHomeScreen
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.*
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class MobileCatalogDisplayTest {
    @get:Rule val compose=createAndroidComposeRule<MainActivity>()
    private var original=CatalogDisplay()
    @Before fun ready()=runBlocking {
        val repo=AppGraph.repository
        repo.ready.first { it }
        original=repo.store.catalogDisplay.first()
    }
    @After fun restore()=runBlocking { AppGraph.repository.store.setCatalogDisplay(original) }

    @Test fun displayChoiceIsSavedAndSurvivesActivityRecreation() {
        compose.onNodeWithContentDescription("影片显示数量").performClick()
        compose.onNodeWithTag("catalog-columns-2").performClick()
        compose.onNodeWithTag("catalog-rows-6").performClick()
        compose.onNodeWithTag("catalog-display-apply").performClick()
        runBlocking { withTimeout(5000) { AppGraph.repository.store.catalogDisplay.first { it==CatalogDisplay(2,6) } } }
        compose.activityRule.scenario.recreate()
        compose.onNodeWithContentDescription("影片显示数量").performClick()
        compose.onNodeWithTag("catalog-columns-2").assertIsSelected()
        compose.onNodeWithTag("catalog-rows-6").assertIsSelected()
    }

    @Test fun everyTemplatePlacesCategoryPostersInRowsAndAllowsVerticalScroll() {
        val films=(1L..30L).map { Film(it,"影片 $it") }
        val home=HomeData(emptyList(),listOf(HomeSection("电影",1,films,"movie")))
        CmsAppearance.entries.forEach { appearance->
            compose.activityRule.scenario.onActivity { activity->activity.setContent {
                CinemaTheme(appearance) { Box(Modifier.width(360.dp).fillMaxHeight()) {
                    NativeHomeScreen(home,false,emptyList(),{}, {}, {},display=CatalogDisplay(3,10))
                } }
            } }
            compose.waitForIdle()
            val first=compose.onNodeWithTag("poster-1").fetchSemanticsNode().boundsInRoot
            val fourth=compose.onNodeWithTag("poster-4").fetchSemanticsNode().boundsInRoot
            Assert.assertTrue("${appearance.code} must wrap after the first three posters",fourth.top>first.top)
            compose.onNodeWithTag("poster-30").performScrollTo().assertIsDisplayed()
        }
    }

    @Test fun pageRowsLimitRenderedCardsInsteadOfForcingOneHorizontalRow() {
        val home=HomeData(emptyList(),listOf(HomeSection("电影",1,(1L..30L).map { Film(it,"影片 $it") },"movie")))
        compose.activityRule.scenario.onActivity { activity->activity.setContent {
            CinemaTheme(CmsAppearance.SuxinPro) { NativeHomeScreen(home,false,emptyList(),{}, {}, {},display=CatalogDisplay(2,6)) }
        } }
        compose.onNodeWithTag("poster-12").performScrollTo().assertIsDisplayed()
        compose.onNodeWithTag("poster-13").assertDoesNotExist()
    }
}
