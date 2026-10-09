package com.xiaoqi.video.feature.catalog

import com.google.gson.JsonParser
import com.xiaoqi.video.core.model.*
import org.junit.Assert.*
import org.junit.Test

class PlaybackLaunchTest {
    private val source=Source("yqk_1","yqk","tv-cut","小柒APP",listOf(Episode("first",1,"第1集"),Episode("second",2,"第2集")))
    private fun detail(sources:List<Source> = listOf(source))=Detail(Film(42,"原生入口"),sources,"yqk_1",emptyList(),emptyList())
    @Test fun browsingEntryRestoresStableEpisodeAndQuality() {
        val progress=JsonParser.parseString("""{"line":"yqk_1","version_key":"tv-cut","episode_key":"second","position_ms":75000,"quality":"1080p"}""").asJsonObject
        val launch=PlaybackLaunch.identity(detail(),progress)
        assertEquals("second",launch.episodeKey);assertEquals(1,launch.episode);assertEquals(75000L,launch.positionMs);assertEquals("1080p",launch.quality)
        assertTrue("A restored line must survive server category preference",launch.manual)
    }
    @Test fun changedVersionStartsCurrentCutAndDropsStaleResume() {
        val progress=JsonParser.parseString("""{"line":"yqk_1","version_key":"old-cut","episode_key":"second","position_ms":75000,"quality":"1080p"}""").asJsonObject
        val launch=PlaybackLaunch.identity(detail(),progress)
        assertEquals("first",launch.episodeKey);assertEquals("tv-cut",launch.versionKey);assertEquals(0L,launch.positionMs);assertEquals("",launch.quality)
        assertFalse(launch.manual)
    }
    @Test fun emptyPreferredLineDoesNotPreventPlayableFallback() {
        val launch=PlaybackLaunch.identity(detail(listOf(source.copy(episodes=emptyList()),source.copy(code="hnm3u8"))),null)
        assertEquals("hnm3u8",launch.line);assertEquals("first",launch.episodeKey)
    }
}
