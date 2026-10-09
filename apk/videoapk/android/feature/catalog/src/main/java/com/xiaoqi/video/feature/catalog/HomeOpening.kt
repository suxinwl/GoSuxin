package com.xiaoqi.video.feature.catalog

import com.xiaoqi.video.core.model.HomeData
import com.xiaoqi.video.core.network.ApiException
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import kotlinx.coroutines.supervisorScope

/** Start the anonymous home request immediately; cache verification runs alongside it. */
internal suspend fun loadHomeOpening(
    cached:suspend ()->HomeData?,
    fresh:suspend ()->HomeData,
    onHome:(HomeData)->Unit,
    onError:(Throwable)->Unit,
    onRejectCached:()->Unit
)=supervisorScope {
    var authoritative=false
    launch {
        try { cached()?.let { if(!authoritative)onHome(it) } }
        catch(e:CancellationException) { throw e }
        catch(_:Exception) { /* A cache miss cannot block the visible network request. */ }
    }
    launch {
        try {
            val home=fresh()
            authoritative=true
            onHome(home)
        } catch(e:CancellationException) { throw e }
        catch(e:Exception) {
            // A reachable server's policy or explicit error must win over a previous snapshot.
            if(e is ApiException) { authoritative=true;onRejectCached() }
            onError(e)
        }
    }
}
