package com.xiaoqi.video.core.network

import java.util.concurrent.TimeUnit

/** Only successful TLS/business verification creates this cache, never a fetched manifest alone. */
internal object EndpointVerification {
    private val maxAgeMillis=TimeUnit.HOURS.toMillis(6)

    fun isFresh(candidate:String,verifiedOrigin:String,verifiedAtMillis:Long,nowMillis:Long):Boolean =
        candidate.isNotBlank()&&candidate==verifiedOrigin&&verifiedAtMillis>0&&
            nowMillis>=verifiedAtMillis&&nowMillis-verifiedAtMillis<maxAgeMillis
}
