package com.xiaoqi.video.core.player

import com.xiaoqi.video.core.model.*
import org.junit.Assert.*
import org.junit.Test

class PlaybackMediaRulesTest {
    @Test fun failedFirstResolveCanManuallySwitchAndKeepTheEpisode() {
        val old=Source("failed",versionKey="season:1",episodes=listOf(Episode("old:2",2)))
        val next=Source("working",versionKey="season:1",episodes=listOf(Episode("new:2",2)))
        val request=PlaybackIdentity(24,"failed","old:2",positionMs=12000,versionKey="season:1")
        val selected=PlaybackMediaRules.manualSource(listOf(old,next),next,request,12000)
        assertEquals("working",selected?.line);assertEquals("new:2",selected?.episodeKey)
        assertEquals(12000L,selected?.positionMs);assertTrue(selected!!.manual)
    }
    @Test fun explicitDifferentCutStartsAtTheBeginning() {
        val target=Source("other-cut",versionKey="feature:extended",episodes=listOf(Episode("feature",-1)))
        val request=PlaybackIdentity(24,"failed","feature",positionMs=12000,versionKey="feature:theatrical")
        assertEquals(0L,PlaybackMediaRules.manualSource(listOf(target),target,request,12000)?.positionMs)
    }
    @Test fun manualSameVersionSwitchNeverOpensAnUnrelatedEpisode() {
        val old=Source("failed",versionKey="season:1",episodes=listOf(Episode("old:2",2)))
        val target=Source("working",versionKey="season:1",episodes=listOf(Episode("new:1",1)))
        assertNull(PlaybackMediaRules.manualSource(listOf(old,target),target,PlaybackIdentity(24,"failed","old:2",versionKey="season:1"),0))
    }
    @Test fun extensionlessNativeHlsIsRecognized() {
        assertTrue(PlaybackMediaRules.isHls("m3u8","https://site/native/hls?token=opaque"))
        assertTrue(PlaybackMediaRules.isHls("","https://cdn/movie.M3U8?sig=1"))
        assertTrue(PlaybackMediaRules.isHls("application/vnd.apple.mpegurl","https://site/media"))
        assertFalse(PlaybackMediaRules.isHls("mp4","https://cdn/video.mp4"))
    }
    @Test fun movieFallbackDoesNotRequireAnEpisodeNumber() {
        val source=Source("new",versionKey="movie",episodes=listOf(Episode("movie:feature",-1,"正片")))
        val alternate=PlaybackMediaRules.alternate(listOf(source),PlaybackIdentity(1,"failed","movie:feature"),"movie",-1,setOf("failed"))
        assertEquals("new",alternate?.first?.code)
        assertEquals("movie:feature",alternate?.second?.key)
    }
    @Test fun automaticFallbackCannotCrossSeasonsOrCuts() {
        val wrong=Source("season2",versionKey="season:2",episodes=listOf(Episode("episode:1",1)))
        val good=Source("season1",versionKey="season:1",episodes=listOf(Episode("other-key:1",1)))
        val request=PlaybackIdentity(1,"old","episode:1",versionKey="season:1")
        assertNull(PlaybackMediaRules.alternate(listOf(wrong),request,"season:1",1,emptySet()))
        assertEquals("season1",PlaybackMediaRules.alternate(listOf(wrong,good),request,"season:1",1,emptySet())?.first?.code)
        assertNull(PlaybackMediaRules.alternate(listOf(good),request,"season:1",1,setOf("season1")))
    }
    @Test fun missingEpisodeCannotFallBackByPosition() {
        val source=Source("candidate",versionKey="tv",episodes=listOf(Episode("episode:2",2)))
        assertNull(PlaybackMediaRules.alternate(listOf(source),PlaybackIdentity(1,"old","episode:1",0),"tv",1,emptySet()))
    }
}
