package com.xiaoqi.video.feature.catalog

import org.junit.Assert.*
import org.junit.Test

class CatalogLayoutRulesTest {
    @Test fun defaultsFillPhoneLandscapeAndTablet() {
        assertEquals(3,CatalogLayoutRules.columns(336f))
        assertEquals(5,CatalogLayoutRules.columns(640f))
        assertEquals(6,CatalogLayoutRules.columns(900f))
    }

    @Test fun manualColumnsCannotOverflowNarrowPhoneOrSplitScreen() {
        assertEquals(3,CatalogLayoutRules.columns(296f,6))
        assertEquals(2,CatalogLayoutRules.columns(200f,4))
        assertEquals(1,CatalogLayoutRules.columns(72f,3))
        assertEquals(4,CatalogLayoutRules.columns(700f,4))
    }

    @Test fun rowsDriveActualRequestSizeWithoutExceedingServerLimit() {
        assertEquals(30,CatalogLayoutRules.pageSize(3,10))
        assertEquals(48,CatalogLayoutRules.pageSize(4,12))
        assertEquals(96,CatalogLayoutRules.pageSize(8,12))
        assertEquals(100,CatalogLayoutRules.pageSize(20,12))
    }
}
