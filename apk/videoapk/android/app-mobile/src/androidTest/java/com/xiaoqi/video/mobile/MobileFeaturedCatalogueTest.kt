package com.xiaoqi.video.mobile

import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import com.xiaoqi.video.core.data.CatalogRequest
import com.xiaoqi.video.core.design.CinemaTheme
import com.xiaoqi.video.core.design.CmsAppearance
import com.xiaoqi.video.core.model.Category
import com.xiaoqi.video.core.model.Film
import com.xiaoqi.video.core.model.FilmPage
import com.xiaoqi.video.feature.catalog.CatalogDataSource
import com.xiaoqi.video.feature.catalog.CatalogueScreen
import java.util.Collections
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

/** Render the production catalogue and exercise the source/topic request path. */
@RunWith(AndroidJUnit4::class)
class MobileFeaturedCatalogueTest {
    @get:Rule val compose=createAndroidComposeRule<MainActivity>()
    private class FixtureSource:CatalogDataSource {
        val sent=Collections.synchronizedList(mutableListOf<CatalogRequest>())
        override suspend fun films(request:CatalogRequest,fresh:Boolean):FilmPage {
            sent+=request
            val featured=request.source=="featured"
            val first=if(featured)1_000L+request.topic else 1L
            return FilmPage(List(request.size) { Film(id=first+it,name="${if(featured)"小柒" else "本站"}影片${it+1}") },60,request.page,2,
                topics=if(featured)listOf(Category(41,"热门国漫"),Category(42,"经典动画")) else emptyList(),
                notice=if(featured)"频道精选" else "")
        }
    }

    @Test fun remotePosterEntersPlayerImmediatelyWithoutListPageImport() {
        val poster=Film(name="精选影片",provider="yqk",remoteId=123,resolveToken="fixture-token")
        var opened:Film?=null;var imports=0
        val source=object:CatalogDataSource {
            override suspend fun films(request:CatalogRequest,fresh:Boolean)=FilmPage(listOf(poster),1,1,1)
            override suspend fun resolveFilm(film:Film):Long { imports++;error("Import belongs to the player opening") }
        }
        compose.activityRule.scenario.onActivity { activity->activity.setContent {
            CinemaTheme(CmsAppearance.Guoguo) { Box(Modifier.width(390.dp).height(640.dp)) {
                CatalogueScreen(source,false,emptyList(),0,{}, {},onFilmCard={ opened=it })
            } }
        } }
        val grid=compose.onNodeWithTag("catalog-guoguo")
        grid.performScrollToNode(hasTestTag("poster-yqk:123"))
        compose.onNodeWithTag("poster-yqk:123").performClick()
        compose.runOnIdle { assertEquals(poster,opened);assertEquals(0,imports) }
        compose.onNodeWithTag("catalog-resolving-film").assertDoesNotExist()
    }

    @Test fun channelOffersFeaturedAndRealTopicsWithStablePlayableFilmIds() {
        val source=FixtureSource();var opened=0L
        compose.activityRule.scenario.onActivity { activity->activity.setContent {
            CinemaTheme(CmsAppearance.Guoguo) { Box(Modifier.width(390.dp).height(640.dp)) {
                CatalogueScreen(source,false,listOf(Category(8,"动漫")),8,{}, { opened=it })
            } }
        } }
        compose.waitUntil(5000) { source.sent.isNotEmpty() }
        compose.onNodeWithTag("catalog-source-featured").performClick()
        compose.waitUntil(5000) { source.sent.any { it.source=="featured" } }
        compose.onNodeWithTag("catalog-topic-41").assertIsDisplayed().performClick()
        compose.waitUntil(5000) { source.sent.any { it.source=="featured"&&it.topic==41L } }
        val grid=compose.onNodeWithTag("catalog-guoguo")
        grid.performScrollToNode(hasTestTag("poster-1041"))
        compose.onNodeWithTag("poster-1041").performClick()
        compose.runOnIdle {
            assertEquals(1041L,opened)
            assertEquals("catalog/featured",source.sent.last().apiPath())
            assertEquals("41",source.sent.last().queryParameters()["topic_id"])
            assertEquals("30",source.sent.last().queryParameters()["size"])
        }
        grid.performScrollToNode(hasTestTag("catalog-source-local"))
        compose.onNodeWithTag("catalog-source-local").performClick()
        compose.waitUntil(5000) { source.sent.last().source=="local" }
        compose.runOnIdle { assertEquals("films",source.sent.last().apiPath()) }
    }

    @Test fun televisionUsesTheSameFeaturedContractAndCanReturnToLocalLibrary() {
        val source=FixtureSource()
        compose.activityRule.scenario.onActivity { activity->activity.setContent {
            CinemaTheme(CmsAppearance.Iqiyi) { Box(Modifier.width(960.dp).height(540.dp)) {
                CatalogueScreen(source,true,listOf(Category(8,"动漫")),8,{}, {})
            } }
        } }
        compose.waitUntil(5000) { source.sent.isNotEmpty() }
        compose.onNodeWithTag("catalog-source-featured").assertIsDisplayed().performClick()
        compose.waitUntil(5000) { source.sent.any { it.source=="featured" } }
        compose.onNodeWithTag("catalog-topic-42").assertIsDisplayed().performClick()
        compose.waitUntil(5000) { source.sent.any { it.topic==42L } }
        compose.runOnIdle {
            assertEquals("30",source.sent.last().queryParameters()["size"])
            assertEquals("8",source.sent.last().queryParameters()["channel_id"])
        }
        compose.onNodeWithTag("catalog-source-local").performClick()
        compose.waitUntil(5000) { source.sent.last().source=="local" }
    }
}
