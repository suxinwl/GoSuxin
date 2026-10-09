package com.xiaoqi.video.feature.library

import org.junit.Assert.*
import org.junit.Test

class LibrarySectionTest {
    @Test fun mobileStartsWithHistoryAndRetainsDistinctContentIdentities() {
        val sections=LibrarySection.available(false)
        assertEquals(listOf("观看历史","我的收藏","离线下载"),sections.map { it.title })
        assertEquals(listOf("history","favorites","downloads"),sections.map { it.key })
        assertEquals(LibrarySection.History,sections.first())
    }

    @Test fun televisionKeepsHistoryAndFavoritesWithoutOfflineDownloads() {
        assertEquals(listOf(LibrarySection.History,LibrarySection.Favorites),LibrarySection.available(true))
    }
}
