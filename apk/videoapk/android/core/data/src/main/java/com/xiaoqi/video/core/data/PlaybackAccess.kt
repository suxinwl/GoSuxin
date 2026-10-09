package com.xiaoqi.video.core.data

import kotlinx.coroutines.CancellationException

/** Guest playback never waits for or borrows a member's credentials or resume database. */
internal class PlaybackAccess(private val owner:Long) {
    internal fun requireCurrent(currentOwner:Long?) {
        if(owner!=(currentOwner?:0L))throw CancellationException("账号已切换，请重新打开影片")
    }

    suspend fun <T> request(currentOwner:()->Long?,load:suspend (String?)->T):T {
        requireCurrent(currentOwner())
        // null preserves ApiClient's member-token refresh; empty explicitly requests guest access.
        val result=load(if(owner>0) null else "")
        requireCurrent(currentOwner())
        return result
    }

    suspend fun <T> progress(currentOwner:()->Long?,load:suspend (Long)->T?):T? {
        if(owner<=0)return null
        requireCurrent(currentOwner())
        val result=load(owner)
        requireCurrent(currentOwner())
        return result
    }
}
