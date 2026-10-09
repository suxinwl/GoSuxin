package com.xiaoqi.video.core.network

import org.junit.Assert.*
import org.junit.Test
import java.util.concurrent.TimeUnit

class EndpointVerificationTest {
    private val origin="https://current.example:8600"
    private val verifiedAt=1_000_000L

    @Test fun onlyMatchingSuccessfulOriginWithinSixHoursCanSkipBackgroundHealthRequest() {
        assertTrue(EndpointVerification.isFresh(origin,origin,verifiedAt,verifiedAt))
        assertTrue(EndpointVerification.isFresh(origin,origin,verifiedAt,verifiedAt+TimeUnit.HOURS.toMillis(6)-1))
        assertFalse(EndpointVerification.isFresh("https://different.example:8600",origin,verifiedAt,verifiedAt))
        assertFalse(EndpointVerification.isFresh(origin,"",verifiedAt,verifiedAt))
        assertFalse(EndpointVerification.isFresh("","",verifiedAt,verifiedAt))
    }

    @Test fun missingExpiredAndFutureTimestampsRequireActualVerification() {
        assertFalse(EndpointVerification.isFresh(origin,origin,0,verifiedAt))
        assertFalse(EndpointVerification.isFresh(origin,origin,-1,verifiedAt))
        assertFalse(EndpointVerification.isFresh(origin,origin,verifiedAt,verifiedAt-1))
        assertFalse(EndpointVerification.isFresh(origin,origin,verifiedAt,verifiedAt+TimeUnit.HOURS.toMillis(6)))
    }
}
