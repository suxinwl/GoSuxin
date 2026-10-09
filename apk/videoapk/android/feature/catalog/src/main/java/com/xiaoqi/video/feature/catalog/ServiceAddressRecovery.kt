package com.xiaoqi.video.feature.catalog

import androidx.compose.foundation.layout.*
import androidx.compose.material3.*
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp
import com.xiaoqi.video.core.network.EndpointDiscoveryState
import com.xiaoqi.video.core.network.ApiException
import java.io.IOException

internal object ServiceAddressRecoveryRules {
    fun showOnHome(loadError:String):Boolean=loadError.isNotBlank()
    fun canSwitch(state:EndpointDiscoveryState,current:String,playing:Boolean,live:Boolean,casting:Boolean,updating:Boolean,switching:Boolean):Boolean=
        !state.checking&&state.verifiedCandidate.isNotBlank()&&state.verifiedCandidate!=current&&!playing&&!live&&!casting&&!updating&&!switching
    fun errorMessage(error:Throwable):String=when(error) {
        is ApiException->error.message
        is java.net.SocketTimeoutException->"首页连接超时，请重试或检查服务地址"
        is java.net.UnknownHostException->"无法找到服务地址，请检查网络或更新服务地址"
        is javax.net.ssl.SSLException->"安全连接失败，请检查服务地址或更新 APP"
        is IOException->"首页连接失败，请重试或检查服务地址"
        else->error.localizedMessage?.takeIf { Regex("[\\u3400-\\u9FFF]").containsMatchIn(it) }?:"首页加载失败，请重试"
    }
}

@Composable fun ServiceAddressRecoveryPanel(state:EndpointDiscoveryState,currentOrigin:String,canSwitch:Boolean,onCheck:()->Unit,onSwitch:()->Unit,modifier:Modifier=Modifier) {
    Surface(modifier.fillMaxWidth().testTag("service-address-recovery"),color=MaterialTheme.colorScheme.surfaceContainer,shape=MaterialTheme.shapes.medium) {
        Column(Modifier.padding(12.dp),verticalArrangement=Arrangement.spacedBy(6.dp)) {
            Text("服务连接",style=MaterialTheme.typography.titleSmall)
            Text(state.status.ifBlank { "连接不上时，可检查是否有新的可用服务地址" },style=MaterialTheme.typography.bodySmall)
            if(state.verifiedCandidate.isNotBlank()&&state.verifiedCandidate!=currentOrigin)Text("新地址：${state.verifiedCandidate}",style=MaterialTheme.typography.bodySmall)
            Row(horizontalArrangement=Arrangement.spacedBy(8.dp)) {
                OutlinedButton(enabled=!state.checking,onClick=onCheck,modifier=Modifier.testTag("service-address-check")) { Text(if(state.checking)"检查中…" else "检查服务地址") }
                if(state.verifiedCandidate.isNotBlank()&&state.verifiedCandidate!=currentOrigin)Button(enabled=canSwitch,onClick=onSwitch,modifier=Modifier.testTag("service-address-switch")) { Text("切换并重试") }
            }
        }
    }
}
