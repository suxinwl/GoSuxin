package com.xiaoqi.video.core.network

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import java.io.IOException
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean

data class EndpointDiscoveryState(val checking:Boolean=false,val verifiedCandidate:String="",val status:String="")

/** One bounded anonymous discovery at a time. A candidate never changes the active site's session. */
internal class EndpointDiscovery(
    private val scope:CoroutineScope,
    private val fetch:suspend (Long)->String?,
    private val verify:suspend (String,Long)->Boolean,
    private val remember:(String)->Boolean,
    private val now:()->Long=System::nanoTime,
    private val recentlyVerified:(String)->Boolean={ false },
    private val currentOrigin:()->String={ "" }
) {
    private class Attempt(interactive:Boolean) { val interactive=AtomicBoolean(interactive) }
    private var active:Attempt?=null
    private val mutableState=MutableStateFlow(EndpointDiscoveryState())
    val state:StateFlow<EndpointDiscoveryState> = mutableState.asStateFlow()

    fun check(onAddressUpdated:()->Unit={}):Boolean = run(interactive=true,onAddressUpdated=onAddressUpdated)

    /** Startup refresh does not delay or announce a working cached origin. Manual recovery stays visible. */
    fun refresh(onAddressUpdated:()->Unit={}):Boolean = run(interactive=false,onAddressUpdated=onAddressUpdated)

    @Synchronized
    private fun run(interactive:Boolean,onAddressUpdated:()->Unit):Boolean {
        active?.let { attempt->
            if(interactive&&attempt.interactive.compareAndSet(false,true))
                mutableState.value=EndpointDiscoveryState(checking=true,status="正在检查服务地址…")
            return false
        }
        val attempt=Attempt(interactive)
        active=attempt
        if(interactive)mutableState.value=EndpointDiscoveryState(checking=true,status="正在检查服务地址…")
        scope.launch {
            try {
                val deadline=now()+TimeUnit.SECONDS.toNanos(4)
                val candidate=fetch(deadline)?:throw IOException("配置文件无效，已保留当前服务地址")
                val fresh=!attempt.interactive.get()&&recentlyVerified(candidate)
                if(now()>=deadline||!fresh&&!verify(candidate,deadline))throw IOException("配置中的服务暂时无法连接，已保留当前地址")
                if(now()>=deadline)throw IOException("服务地址检查超时，请重试")
                // Do not extend the verification window without actually validating the service.
                val changed=if(fresh)candidate!=currentOrigin() else remember(candidate)
                mutableState.value=if(!attempt.interactive.get()&&!changed)EndpointDiscoveryState() else
                    EndpointDiscoveryState(verifiedCandidate=candidate,status=if(changed)"发现可用的新地址，请点击切换" else "当前服务地址有效，可以重试加载")
                if(changed)onAddressUpdated()
            } catch(e:CancellationException) {
                if(attempt.interactive.get())mutableState.value=EndpointDiscoveryState(status="服务地址检查已取消")
                throw e
            } catch(e:Exception) {
                val message=e.message?.takeIf { Regex("[\\u3400-\\u9FFF]").containsMatchIn(it) }?:"服务地址检查失败，请检查网络后重试"
                if(attempt.interactive.get())mutableState.value=EndpointDiscoveryState(status=message)
            } finally { synchronized(this@EndpointDiscovery) { if(active===attempt)active=null } }
        }
        return true
    }
}
