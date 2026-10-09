package com.xiaoqi.video.feature.catalog

import com.xiaoqi.video.core.network.EndpointDiscoveryState
import org.junit.Assert.*
import org.junit.Test

class ServiceAddressRecoveryTest {
    @Test fun routineStartupNeverShowsTheRecoveryPanel() {
        assertFalse(ServiceAddressRecoveryRules.showOnHome(""))
        assertFalse(ServiceAddressRecoveryRules.showOnHome(" "))
        assertTrue(ServiceAddressRecoveryRules.showOnHome("首页连接超时，请重试"))
    }
    @Test fun switchingRequiresAVerifiedDifferentAddressAndAnIdleClient() {
        val state=EndpointDiscoveryState(verifiedCandidate="https://new.example:8600")
        fun allowed(s:EndpointDiscoveryState=state,current:String="https://old.example:8600",playing:Boolean=false,live:Boolean=false,casting:Boolean=false,updating:Boolean=false,switching:Boolean=false)=
            ServiceAddressRecoveryRules.canSwitch(s,current,playing,live,casting,updating,switching)
        assertTrue(allowed())
        assertFalse(allowed(s=EndpointDiscoveryState()))
        assertFalse(allowed(s=state.copy(checking=true)))
        assertFalse(allowed(current=state.verifiedCandidate))
        assertFalse(allowed(playing=true));assertFalse(allowed(live=true));assertFalse(allowed(casting=true))
        assertFalse(allowed(updating=true));assertFalse(allowed(switching=true))
    }

    @Test fun connectionFailuresHaveActionableChineseMessages() {
        assertEquals("首页连接超时，请重试或检查服务地址",ServiceAddressRecoveryRules.errorMessage(java.net.SocketTimeoutException("timeout")))
        assertEquals("无法找到服务地址，请检查网络或更新服务地址",ServiceAddressRecoveryRules.errorMessage(java.net.UnknownHostException("missing.example")))
        assertEquals("安全连接失败，请检查服务地址或更新 APP",ServiceAddressRecoveryRules.errorMessage(javax.net.ssl.SSLException("untrusted")))
        assertEquals("首页加载失败，请重试",ServiceAddressRecoveryRules.errorMessage(IllegalStateException("foreign text")))
    }
}
