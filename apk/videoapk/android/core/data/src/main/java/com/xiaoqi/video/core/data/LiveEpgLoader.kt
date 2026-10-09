package com.xiaoqi.video.core.data

import com.xiaoqi.video.core.model.LiveEpg
import com.xiaoqi.video.core.network.LiveNetworkErrors
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow

data class LiveEpgLoadState(val channelId:Long=0,val date:String="",val loading:Boolean=false,val result:LiveEpg?=null,val error:String="")

/** Changing date/channel/account invalidates an old request before cancelling it. */
class LiveEpgLoader(private val scope:CoroutineScope,private val request:suspend (Long,String)->LiveEpg) {
    private val mutable=MutableStateFlow(LiveEpgLoadState())
    val state:StateFlow<LiveEpgLoadState> = mutable
    private var pending:Job?=null
    @Volatile private var version=0L
    fun load(channelId:Long,date:String):Job {
        val current=++version;pending?.cancel()
        mutable.value=LiveEpgLoadState(channelId,date,loading=true)
        return scope.launch {
            try { val result=request(channelId,date);ensureActive();if(current==version)mutable.value=mutable.value.copy(result=result) }
            catch(e:CancellationException) { throw e }
            catch(e:Throwable) { if(current==version)mutable.value=mutable.value.copy(error=LiveNetworkErrors.message(e)) }
            finally { if(current==version)mutable.value=mutable.value.copy(loading=false) }
        }.also { pending=it }
    }
    fun close() { version++;pending?.cancel();pending=null;mutable.value=mutable.value.copy(loading=false) }
}
