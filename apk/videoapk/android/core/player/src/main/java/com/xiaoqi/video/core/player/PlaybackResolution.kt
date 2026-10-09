package com.xiaoqi.video.core.player

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.TimeoutCancellationException
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.isActive
import kotlinx.coroutines.withTimeout

/** A dependency cancellation must end loading; navigation cancellation stays silent. */
internal suspend fun <T> resolvePlaybackRequest(
    load:suspend ()->T,
    onInterrupted:(String)->Unit,
    timeoutMs:Long=50_000
):T = try {
    withTimeout(timeoutMs) {
        val result=load()
        currentCoroutineContext().ensureActive()
        result
    }
} catch(e:CancellationException) {
    if(currentCoroutineContext().isActive) {
        onInterrupted(if(e is TimeoutCancellationException)"播放源解析超时，请重试"
            else "播放源解析已中断，请重试")
    }
    throw e
}
