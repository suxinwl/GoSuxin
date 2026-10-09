package com.xiaoqi.video.feature.catalog

import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import com.xiaoqi.video.core.data.AppRepository
import com.xiaoqi.video.core.data.CatalogRequest
import com.xiaoqi.video.core.model.*
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch

data class CatalogFilter(val query:String="",val channel:Long=0,val type:Long=0,val page:Int=1,val sort:String="time",val area:String="",val year:String="",val size:Int=30,val source:String="local",val topic:Long=0) {
    fun request()=CatalogRequest(query,channel,type,page,sort,area,year,size,source,topic).normalized()
}
data class CatalogState(val page:FilmPage=FilmPage(),val loading:Boolean=false,val error:String="",val request:CatalogRequest?=null,val nextPageReady:Boolean=false)

/** The production screen and its UI tests use this exact request and render path. */
interface CatalogDataSource {
    suspend fun films(request:CatalogRequest,fresh:Boolean=false):FilmPage
    suspend fun prefetch(request:CatalogRequest):Boolean=false
    suspend fun categories():List<Category> = emptyList()
    suspend fun prefetchDetail(id:Long) {}
    suspend fun resolveFilm(film:Film):Long {
        require(film.id>0) { "精选影片暂时无法载入，请重试" }
        return film.id
    }
}

internal class RepositoryCatalogSource(private val repo:AppRepository):CatalogDataSource {
    override suspend fun films(request:CatalogRequest,fresh:Boolean)=repo.films(request,fresh)
    override suspend fun prefetch(request:CatalogRequest)=repo.prefetchFilms(request)
    override suspend fun categories()=repo.categories()
    override suspend fun prefetchDetail(id:Long) { repo.prefetchDetail(id) }
    override suspend fun resolveFilm(film:Film)=repo.resolveFeatured(film)
}

/** Separate visible page and warmup jobs: a failed prefetch never becomes the visible page's error. */
internal class CatalogSession(
    private val scope:CoroutineScope,
    private val fetch:suspend (CatalogRequest,Boolean)->FilmPage,
    private val prefetch:suspend (CatalogRequest)->Boolean
) {
    val state=MutableStateFlow(CatalogState())
    private var filter:CatalogRequest?=null
    private var job:Job?=null
    private var warmupJob:Job?=null
    private var warmupTarget:CatalogRequest?=null
    private var generation=0L
    fun cancel() {
        generation++;filter=null;job?.cancel();job=null;warmupJob?.cancel();warmupJob=null;warmupTarget=null
        state.value=state.value.copy(loading=false,nextPageReady=false)
    }
    fun load(next:CatalogFilter,force:Boolean=false) {
        val request=next.request()
        if(!force&&filter==request)return
        val previous=filter;filter=request;val version=++generation;job?.cancel()
        // Preserve a running next-page request so the visible page can join its HTTP call.
        if(warmupTarget!=request) { warmupJob?.cancel();warmupTarget=null }
        state.value=CatalogState(page=if(previous==request)state.value.page else FilmPage(page=request.page),loading=true,request=request)
        job=scope.launch {
            try {
                val page=fetch(request,force)
                if(version!=generation||filter!=request)return@launch
                state.value=CatalogState(page=page,request=request)
                if(page.page<page.pages) {
                    warmupJob?.cancel()
                    val target=request.copy(page=page.page+1);warmupTarget=target
                    warmupJob=scope.launch {
                        val ready=try { prefetch(target) }
                        catch(e:CancellationException) { throw e }
                        catch(_:Exception) { false }
                        if(version==generation&&filter==request)state.value=state.value.copy(nextPageReady=ready)
                    }
                }
            }
            catch(e:CancellationException) { throw e }
            catch(e:Exception) { if(version==generation&&filter==request)state.value=CatalogState(page=FilmPage(page=request.page),error=e.localizedMessage?:"加载失败",request=request) }
        }
    }
}

class CatalogViewModel(source:CatalogDataSource):ViewModel() {
    private val session=CatalogSession(viewModelScope,{ request,fresh->source.films(request,fresh) },source::prefetch)
    val state=session.state
    fun load(next:CatalogFilter,force:Boolean=false)=session.load(next,force)
    fun cancelLoads()=session.cancel()
    class Factory(private val source:CatalogDataSource):ViewModelProvider.Factory {
        @Suppress("UNCHECKED_CAST") override fun <T:ViewModel>create(modelClass:Class<T>):T=CatalogViewModel(source) as T
    }
}
