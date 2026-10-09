package com.xiaoqi.video.feature.catalog

import com.google.gson.JsonObject
import com.xiaoqi.video.core.model.Detail
import com.xiaoqi.video.core.model.Film
import com.xiaoqi.video.core.model.PlaybackIdentity
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.withTimeout
import kotlinx.coroutines.withTimeoutOrNull

internal data class PlaybackOpening(val detail:Detail,val identity:PlaybackIdentity)

/** Public metadata never waits for a local account. Optional resume storage cannot block playback. */
internal suspend fun preparePlaybackOpening(
    id:Long,
    requested:PlaybackIdentity?=null,
    hasAccount:()->Boolean,
    detail:suspend (Long)->Detail,
    progress:suspend (Long)->JsonObject?,
    phase:(String)->Unit,
    timeoutMs:Long=40_000,
    historyTimeoutMs:Long=1_500,
    cachedDetail:suspend (Long)->Detail?={ null }
):PlaybackOpening=withTimeout(timeoutMs) {
    val metadata=cachedDetail(id)?:run {
        phase("正在加载影片信息")
        detail(id)
    }
    currentCoroutineContext().ensureActive()
    val saved=if(requested==null&&hasAccount()) {
        phase("正在读取续播记录")
        withTimeoutOrNull(historyTimeoutMs) {
            try { progress(id) }
            catch(e:CancellationException) { throw e }
            catch(_:Exception) { null }
        }
    } else null
    currentCoroutineContext().ensureActive()
    PlaybackOpening(metadata,requested?:PlaybackLaunch.identity(metadata,saved))
}

/** Remote catalogue identity and metadata belong to the same player opening, not two destinations. */
internal suspend fun prepareFilmPlaybackOpening(
    film:Film,
    requested:PlaybackIdentity?=null,
    hasAccount:()->Boolean,
    resolveFilm:suspend (Film)->Long,
    detail:suspend (Long)->Detail,
    progress:suspend (Long)->JsonObject?,
    phase:(String)->Unit,
    cachedDetail:suspend (Long)->Detail?={ null },
    timeoutMs:Long=40_000
):PlaybackOpening=withTimeout(timeoutMs) {
    require(film.hasPlayableIdentity) { "影片暂时无法载入，请刷新列表" }
    val id=if(film.id>0)film.id else {
        phase("正在连接片源")
        resolveFilm(film).also { require(it>0) { "精选影片尚未载入，请重试" } }
    }
    currentCoroutineContext().ensureActive()
    preparePlaybackOpening(id,requested,hasAccount,detail,progress,phase,
        timeoutMs=timeoutMs,cachedDetail=cachedDetail)
}
