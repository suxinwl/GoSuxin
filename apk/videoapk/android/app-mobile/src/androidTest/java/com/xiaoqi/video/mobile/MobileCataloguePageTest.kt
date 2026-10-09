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
import androidx.test.platform.app.InstrumentationRegistry
import com.google.gson.JsonArray
import com.google.gson.JsonParser
import com.xiaoqi.video.core.data.CatalogRequest
import com.xiaoqi.video.core.design.CinemaTheme
import com.xiaoqi.video.core.design.CmsAppearance
import com.xiaoqi.video.core.model.CatalogDisplay
import com.xiaoqi.video.core.model.Category
import com.xiaoqi.video.core.model.FilmPage
import com.xiaoqi.video.core.network.JsonWire
import com.xiaoqi.video.feature.catalog.CatalogDataSource
import com.xiaoqi.video.feature.catalog.CatalogueScreen
import java.util.Collections
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

/** These tests render the actual channel screen, not a home section with hand-built Film objects. */
@RunWith(AndroidJUnit4::class)
class MobileCataloguePageTest {
    @get:Rule val compose=createAndroidComposeRule<MainActivity>()

    private class FixtureSource:CatalogDataSource {
        private val raw=InstrumentationRegistry.getInstrumentation().context.assets
            .open("catalog-channel-30-response.json").bufferedReader().use { JsonParser.parseString(it.readText()).asJsonObject.getAsJsonObject("data") }
        val sent=Collections.synchronizedList(mutableListOf<Map<String,String>>())
        val warmed=Collections.synchronizedList(mutableListOf<Map<String,String>>())
        private fun response(query:Map<String,String>):FilmPage {
            val page=query.getValue("page").toInt();val size=query.getValue("size").toInt()
            val data=raw.deepCopy();val items=JsonArray()
            raw.getAsJsonArray("items").take(size).forEach { item->
                val film=item.asJsonObject.deepCopy()
                if(page>1)film.addProperty("id",film.get("id").asLong+1_000_000L*(page-1))
                items.add(film)
            }
            data.add("items",items);data.addProperty("total",60);data.addProperty("page",page)
            data.addProperty("size",size);data.addProperty("pages",(60+size-1)/size)
            return JsonWire.filmPage(data,page,size)
        }
        override suspend fun films(request:CatalogRequest,fresh:Boolean):FilmPage {
            val query=request.queryParameters();sent+=query
            assertEquals("Use the real HTTP query builder",request.size.toString(),query["size"])
            return response(query)
        }
        override suspend fun prefetch(request:CatalogRequest):Boolean {
            val query=request.queryParameters();warmed+=query
            assertEquals(request.size.toString(),query["size"])
            return true
        }
        fun lastId(size:Int,page:Int=1)=raw.getAsJsonArray("items")[size-1].asJsonObject.get("id").asLong+1_000_000L*(page-1)
    }

    private fun show(source:FixtureSource,appearance:CmsAppearance,display:CatalogDisplay,width:Int=360,height:Int=620) {
        compose.activityRule.scenario.onActivity { activity->activity.setContent {
            CinemaTheme(appearance) { Box(Modifier.width(width.dp).height(height.dp)) {
                CatalogueScreen(source,false,listOf(Category(65,"奈飞Netflix")),65,{}, {},display=display)
            } }
        } }
        compose.waitUntil(5000) { source.sent.isNotEmpty() }
        compose.waitForIdle()
    }

    @Test fun allFourTemplatesRenderThirtyChannelCardsAndPaginateAtTheSameCapacity() {
        val source=FixtureSource()
        CmsAppearance.entries.forEach { appearance->
            show(source,appearance,CatalogDisplay(3,10))
            compose.onNodeWithTag("catalog-density").assertTextContains("本页 30 部",substring=true)
            val list=compose.onNodeWithTag("catalog-${appearance.code}")
            list.performScrollToNode(hasTestTag("poster-${source.lastId(30)}"))
            compose.onNodeWithTag("poster-${source.lastId(30)}").assertIsDisplayed()
            list.performScrollToNode(hasTestTag("catalog-next-page"))
            compose.onNodeWithTag("catalog-next-page").performClick()
            compose.waitForIdle()
            list.performScrollToNode(hasTestTag("poster-${source.lastId(30,2)}"))
            compose.onNodeWithTag("poster-${source.lastId(30,2)}").assertIsDisplayed()
            assertEquals("30",source.sent.last()["size"]);assertEquals("2",source.sent.last()["page"])
            assertTrue(source.warmed.any { it["size"]=="30"&&it["page"]=="2" })
        }
    }

    @Test fun twoColumnsSixRowsRequestTwelveAndAllowReachingTheLastPoster() {
        val source=FixtureSource();show(source,CmsAppearance.SuxinPro,CatalogDisplay(2,6),390)
        compose.onNodeWithTag("catalog-density").assertTextContains("本页 12 部",substring=true)
        compose.onNodeWithTag("catalog-suxinpro").performScrollToNode(hasTestTag("poster-${source.lastId(12)}"))
        compose.onNodeWithTag("poster-${source.lastId(12)}").assertIsDisplayed()
        assertEquals("12",source.sent.first()["size"])
    }

    @Test fun shortLandscapeHasOneScrollableCatalogueContainingControlsRowsAndPagination() {
        val source=FixtureSource();show(source,CmsAppearance.Guoguo,CatalogDisplay(3,10),640,280)
        val list=compose.onNodeWithTag("catalog-guoguo")
        list.performScrollToNode(hasTestTag("poster-${source.lastId(30)}"))
        compose.onNodeWithTag("poster-${source.lastId(30)}").assertIsDisplayed()
        list.performScrollToNode(hasTestTag("catalog-next-page"))
        compose.onNodeWithTag("catalog-next-page").assertIsDisplayed()
        assertEquals("30",source.sent.first()["size"])
    }
}
