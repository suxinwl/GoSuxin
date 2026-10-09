package com.xiaoqi.video.feature.catalog

import com.xiaoqi.video.core.data.CatalogRequest
import com.xiaoqi.video.core.model.Film
import com.xiaoqi.video.core.model.FilmPage
import com.xiaoqi.video.core.network.JsonWire
import com.google.gson.JsonParser
import kotlinx.coroutines.*
import org.junit.Assert.*
import org.junit.Test
import java.io.IOException

class CatalogSessionTest {
    @Test fun disposedSiteCannotPublishLateCardsAndReturningCanReloadTheSameFilter()=runBlocking {
        val workers=CoroutineScope(SupervisorJob()+Dispatchers.Unconfined)
        val release=CompletableDeferred<Unit>();var loads=0
        try {
            val session=CatalogSession(workers,fetch={ _,_->
                if(++loads==1)withContext(NonCancellable) { release.await() }
                FilmPage(listOf(Film(id=loads.toLong(),name="result-$loads")))
            },prefetch={ false })
            session.load(CatalogFilter(channel=8));session.cancel();release.complete(Unit);yield()
            assertTrue(session.state.value.page.items.isEmpty());assertFalse(session.state.value.loading)
            session.load(CatalogFilter(channel=8));yield()
            assertEquals(2,loads);assertEquals(2L,session.state.value.page.items.single().id)
        } finally { release.complete(Unit);workers.cancel() }
    }

    @Test fun sourceAndTopicSwitchReloadsAndPreservesTheFeaturedNextPageContract()=runBlocking {
        val workers=CoroutineScope(SupervisorJob()+Dispatchers.Unconfined)
        val visible=mutableListOf<CatalogRequest>();val warmed=mutableListOf<CatalogRequest>()
        try {
            val session=CatalogSession(workers,fetch={ request,_->
                visible+=request
                FilmPage(listOf(Film(id=if(request.source=="local")1 else 2,name=request.source)),60,request.page,2)
            },prefetch={ request->warmed+=request;true })
            session.load(CatalogFilter(channel=8,size=30));yield()
            session.load(CatalogFilter(channel=8,size=30,source="featured"));yield()
            assertEquals(2L,session.state.value.page.items.single().id)
            assertEquals("catalog/featured",visible.last().apiPath())
            assertEquals("featured",warmed.last().source);assertEquals(2,warmed.last().page)
            session.load(CatalogFilter(channel=8,size=30,source="featured",topic=41));yield()
            assertEquals(41L,visible.last().topic);assertEquals(41L,warmed.last().topic)
            assertEquals("30",warmed.last().queryParameters()["size"])
            session.load(CatalogFilter(channel=8,size=30));yield()
            assertEquals(1L,session.state.value.page.items.single().id)
            assertEquals(listOf("local","featured","featured","local"),visible.map { it.source })
        } finally { workers.cancel() }
    }

    @Test fun actualWireCapacityAndPageSurviveVisibleLoadsAndPreloading()=runBlocking {
        val workers=CoroutineScope(SupervisorJob()+Dispatchers.Unconfined)
        val sent=mutableListOf<Map<String,String>>();val warmed=mutableListOf<Map<String,String>>()
        try {
            val session=CatalogSession(workers,fetch={ request,_->
                val query=request.queryParameters();sent+=query
                val size=query.getValue("size").toInt();val page=query.getValue("page").toInt()
                val rows=(0 until size).joinToString(",") { index->"""{"id":${(page-1)*size+index+1},"name":"影片 $index"}""" }
                JsonWire.filmPage(JsonParser.parseString("""{"items":[$rows],"total":1000,"page":$page,"size":$size}"""),page,size)
            },prefetch={ request->warmed+=request.queryParameters();true })
            session.load(CatalogFilter(channel=65,size=30));yield()
            assertEquals(30,session.state.value.page.items.size)
            assertEquals("30",sent.last()["size"]);assertEquals("30",warmed.last()["size"])
            session.load(CatalogFilter(channel=65,page=2,size=30));yield()
            assertEquals(30,session.state.value.page.items.size)
            assertEquals(31L,session.state.value.page.items.first().id)
            assertEquals("2",sent.last()["page"]);assertEquals("30",sent.last()["size"])
            session.load(CatalogFilter(channel=65,size=12));yield()
            assertEquals(12,session.state.value.page.items.size)
            assertEquals("12",sent.last()["size"]);assertEquals("12",warmed.last()["size"])
        } finally { workers.cancel() }
    }
    @Test fun displaySizeChangeReloadsAndKeepsTheSameSizeForNextPageWarmup()=runBlocking {
        val workers=CoroutineScope(SupervisorJob()+Dispatchers.Unconfined)
        val visible=mutableListOf<CatalogRequest>();val warmed=mutableListOf<CatalogRequest>()
        try {
            val session=CatalogSession(workers,fetch={ request,_->
                visible+=request
                FilmPage(List(request.size) { Film(id=it+1L) },1000,request.page,100)
            },prefetch={ request->warmed+=request;true })
            session.load(CatalogFilter(channel=2,size=30));yield()
            assertEquals(30,session.state.value.page.items.size)
            assertEquals(2,warmed.last().page);assertEquals(30,warmed.last().size)
            session.load(CatalogFilter(channel=2,size=48));yield()
            assertEquals(listOf(30,48),visible.map { it.size })
            assertEquals(48,session.state.value.page.items.size)
            assertEquals(2,warmed.last().page);assertEquals(48,warmed.last().size)
            session.load(CatalogFilter(channel=2,page=2,size=48));yield()
            assertEquals(2,visible.last().page);assertEquals(48,visible.last().size)
        } finally { workers.cancel() }
    }

    @Test fun slowCancelledCategoryCannotOverwriteNewCategory()=runBlocking {
        val workers=CoroutineScope(SupervisorJob()+Dispatchers.Unconfined)
        val old=CompletableDeferred<Unit>();val current=CompletableDeferred<Unit>()
        try {
            val session=CatalogSession(workers,fetch={ request,_->
                if(request.channel==2L)withContext(NonCancellable) { old.await() } else current.await()
                FilmPage(listOf(Film(id=request.channel,name="channel-${request.channel}")))
            },prefetch={ false })
            session.load(CatalogFilter(channel=2));session.load(CatalogFilter(channel=8))
            current.complete(Unit);yield();assertEquals(8L,session.state.value.page.items.single().id)
            old.complete(Unit);yield();assertEquals(8L,session.state.value.page.items.single().id)
            assertFalse(session.state.value.loading)
        } finally { old.complete(Unit);workers.cancel() }
    }

    @Test fun warmupErrorsAreInvisibleAndActiveNextPageWarmupIsNotCancelledByClick()=runBlocking {
        val workers=CoroutineScope(SupervisorJob()+Dispatchers.Unconfined)
        val started=CompletableDeferred<CatalogRequest>();val finish=CompletableDeferred<Unit>();var warmupCancelled=false
        try {
            val session=CatalogSession(workers,fetch={ request,_->
                if(request.page==2) { finish.await();FilmPage(listOf(Film(id=2,name="next")),2,2,2) }
                else FilmPage(listOf(Film(id=1,name="current")),2,1,2)
            },prefetch={ request->started.complete(request);try { finish.await();throw IOException("warmup failed") }catch(e:CancellationException) { warmupCancelled=true;throw e } })
            session.load(CatalogFilter(channel=2));started.await()
            assertEquals(1L,session.state.value.page.items.single().id);assertEquals("",session.state.value.error)
            session.load(CatalogFilter(channel=2,page=2));assertFalse(warmupCancelled)
            finish.complete(Unit);yield();assertEquals(2L,session.state.value.page.items.single().id)
            assertEquals("",session.state.value.error);assertFalse(session.state.value.loading)
        } finally { finish.complete(Unit);workers.cancel() }
    }

    @Test fun failedOldRequestCannotSetErrorForNewFiltersAndForceReachesLoader()=runBlocking {
        val workers=CoroutineScope(SupervisorJob()+Dispatchers.Unconfined)
        val gate=CompletableDeferred<Unit>();val forces=mutableListOf<Boolean>()
        try {
            val session=CatalogSession(workers,fetch={ request,fresh->
                forces+=fresh
                if(request.channel==2L)withContext(NonCancellable) { gate.await();throw IOException("old timeout") }
                FilmPage(listOf(Film(id=8,name="anime")))
            },prefetch={ false })
            session.load(CatalogFilter(channel=2));session.load(CatalogFilter(channel=8));gate.complete(Unit);yield()
            assertEquals("",session.state.value.error);assertEquals(8L,session.state.value.page.items.single().id)
            session.load(CatalogFilter(channel=8),true);yield();assertEquals(listOf(false,false,true),forces)
        } finally { gate.complete(Unit);workers.cancel() }
    }
}
