package com.xiaoqi.video.core.model

import org.junit.Assert.*
import org.junit.Test

class PlaybackRulesTest {
    private val lines = listOf(Source("hn", name = "红牛"), Source("yqk_1", name = "小柒APP"), Source("hongguo", name = "红果"), Source("erciyuan", name = "二次元"))
    @Test fun categoryDefaultsPrecedeGenericSpeedOptimization() {
        assertEquals("hongguo", PlaybackRules.defaultSource(Film(isShort = true), lines)?.code)
        assertEquals("erciyuan", PlaybackRules.defaultSource(Film(isAnime = true), lines)?.code)
        assertEquals("yqk_1", PlaybackRules.defaultSource(Film(), lines)?.code)
    }
    @Test fun offlineRightsAreBoundToAccountAndExpire() {
        assertTrue(PlaybackRules.canPlayOffline(7, 7, 101, 100))
        assertFalse(PlaybackRules.canPlayOffline(7, 8, 101, 100))
        assertFalse(PlaybackRules.canPlayOffline(7, 7, 100, 100))
    }
    @Test fun stableDownloadIdentitySeparatesCutsAndQualities() {
        assertNotEquals(PlaybackRules.downloadKey(7, 9, "a", "cut1", "ep1", "720"), PlaybackRules.downloadKey(7, 9, "a", "cut2", "ep1", "720"))
    }
    @Test fun finalEpisodeHasNoNextButton() {
        val s = Source(episodes = listOf(Episode("a", 1), Episode("b", 2)))
        assertEquals("b", PlaybackRules.nextEpisode(s, "a")?.key)
        assertNull(PlaybackRules.nextEpisode(s, "b"))
    }
}
