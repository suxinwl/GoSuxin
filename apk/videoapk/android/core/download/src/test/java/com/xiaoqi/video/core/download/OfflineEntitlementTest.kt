package com.xiaoqi.video.core.download

import org.junit.Assert.*
import org.junit.Test

class OfflineEntitlementTest {
    private val issued=1800000000L
    private fun refuse(owner:Long=1,active:Long=1,device:String="phone",current:String="phone",expiry:Long=issued+OfflineEntitlement.MAX_SECONDS,now:Long=issued+10,floor:Long=issued)=OfflineEntitlement.refusal(owner,active,device,current,issued,expiry,now,floor)
    @Test fun ownerAndDeviceAreBothRequired() {
        assertNull(refuse())
        assertNotNull(refuse(active=2))
        assertNotNull(refuse(active=0))
        assertNotNull(refuse(current="tablet"))
        assertNotNull(refuse(device=""))
    }
    @Test fun playbackStopsAtExpiryAndHonorsShortVipBound() {
        assertNull(refuse(now=issued+OfflineEntitlement.MAX_SECONDS-1))
        assertNotNull(refuse(now=issued+OfflineEntitlement.MAX_SECONDS))
        assertNull(refuse(expiry=issued+60,now=issued+59))
        assertNotNull(refuse(expiry=issued+60,now=issued+60))
        assertNotNull(refuse(expiry=issued+OfflineEntitlement.MAX_SECONDS+1))
    }
    @Test fun rollbackCannotExtendAlreadyUsedLicense() {
        assertNotNull(refuse(now=issued+100,floor=issued+500))
        assertNull(refuse(now=issued+250,floor=issued+500))
    }
    @Test fun renewalUsesNewIssuanceAndExpiry() {
        val renewed=issued+OfflineEntitlement.MAX_SECONDS
        assertNotNull(refuse(now=renewed))
        assertNull(OfflineEntitlement.refusal(1,1,"phone","phone",renewed,renewed+OfflineEntitlement.MAX_SECONDS,renewed+1,renewed))
    }
}
