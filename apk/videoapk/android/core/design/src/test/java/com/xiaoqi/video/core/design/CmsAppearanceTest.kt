package com.xiaoqi.video.core.design

import org.junit.Assert.*
import org.junit.Test

class CmsAppearanceTest {
    @Test fun followTracksWebsiteWhileManualChoiceRemainsIndependent() {
        assertEquals(CmsAppearance.Iqiyi,CmsAppearance.resolve("follow","iqiyi"))
        assertEquals(CmsAppearance.Guoguo,CmsAppearance.resolve("guoguo","iqiyi"))
        assertEquals(CmsAppearance.Guoguo,CmsAppearance.resolve("guoguo","suxinpro"))
    }
    @Test fun unknownWebsiteFallsBackToSupportedNativeTemplate() {
        assertEquals(CmsAppearance.SuxinLite,CmsAppearance.resolve("follow","missing-custom-theme"))
    }
}
