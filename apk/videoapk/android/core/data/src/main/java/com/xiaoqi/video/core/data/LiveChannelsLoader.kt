package com.xiaoqi.video.core.data

import com.xiaoqi.video.core.model.LiveChannelPage
import com.xiaoqi.video.core.network.LiveNetworkErrors
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow

data class LiveChannelLoadState(
    val groupId:Long=0,val query:String="",val page:Int=1,
    val loading:Boolean=true,val result:LiveChannelPage?=null,val error:String=""
)

/** Network completion is independent of any grid, scroll, focus or layout callback. */
class LiveChannelsLoader(
    private val scope:CoroutineScope,
    private val request:suspend (Long,String,Int)->LiveChannelPage
) {
    private val mutable=MutableStateFlow(LiveChannelLoadState())
    val state:StateFlow<LiveChannelLoadState> = mutable
    private var pending:Job?=null
    @Volatile private var version=0L

    fun load(groupId:Long,query:String,page:Int):Job {
        val current=++version
        pending?.cancel()
        mutable.value=LiveChannelLoadState(groupId,query,page,loading=true)
        return scope.launch {
            try {
                val pageResult=request(groupId,query,page)
                ensureActive()
                if(current==version)mutable.value=mutable.value.copy(result=pageResult)
            } catch(e:CancellationException) { throw e }
            catch(e:Throwable) {
                if(current==version)mutable.value=mutable.value.copy(result=null,error=LiveNetworkErrors.message(e))
            } finally {
                if(current==version)mutable.value=mutable.value.copy(loading=false)
            }
        }.also { pending=it }
    }

    fun close() {
        version++;pending?.cancel();pending=null
        mutable.value=mutable.value.copy(loading=false)
    }
}
