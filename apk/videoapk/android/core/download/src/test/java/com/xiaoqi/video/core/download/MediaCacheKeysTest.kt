package com.xiaoqi.video.core.download

import java.net.URLEncoder
import org.junit.Assert.*
import org.junit.Test

class MediaCacheKeysTest {
    @Test fun serverNamespacesPreserveOriginalDownloadsAndSeparateIdenticalAccounts() {
        val original="https://xq.suxinwl.com:8600"
        assertEquals("1:phone",MediaCacheKeys.namespace(original,original,1,"phone"))
        val other=MediaCacheKeys.namespace("https://other.example:8600",original,1,"phone")
        assertNotEquals("1:phone",other)
        assertNotEquals(other,MediaCacheKeys.namespace("https://another.example:8600",original,1,"phone"))
        assertNotEquals(key(proxied("https://cdn/video/1.ts")),key(proxied("https://cdn/video/1.ts"),other))
    }
    private fun key(url:String,owner:String="1:phone")=MediaCacheKeys.build(url,owner)
    private fun proxied(upstream:String,grant:String="old")="https://site/suxinvideo/proxy?url="+URLEncoder.encode(upstream,"UTF-8")+"&app_grant=$grant&token=$grant&sig=$grant&download_key=edition1"
    @Test fun segmentBytesSurviveOwnGrantRenewal() {
        assertEquals(key(proxied("https://cdn/video/0001.ts")),key(proxied("https://cdn/video/0001.ts","new")))
        assertEquals(key("https://site/suxinvideo/native/hls?token=old&segment=0&app_grant=old&download_key=film"),key("https://site/suxinvideo/native/hls?token=new&segment=0&app_grant=new&download_key=film"))
    }
    @Test fun manifestRenewalFetchesFreshChildUrls() {
        assertNotEquals(key(proxied("https://cdn/movie.m3u8")),key(proxied("https://cdn/movie.m3u8","new")))
        assertNotEquals(key("https://site/suxinvideo/native/hls?token=old&download_key=film"),key("https://site/suxinvideo/native/hls?token=new&download_key=film"))
    }
    @Test fun upstreamTokensOriginsSegmentsAndOwnersCannotCollide() {
        assertNotEquals(key(proxied("https://cdn/download?token=assetA")),key(proxied("https://cdn/download?token=assetB")))
        assertNotEquals(key(proxied("https://cdn-a/video/1.ts")),key(proxied("https://cdn-b/video/1.ts")))
        assertNotEquals(key(proxied("https://cdn/video/1.ts")),key(proxied("https://cdn/video/1.ts"),"2:phone"))
        assertNotEquals(key("https://site/suxinvideo/native/hls?token=x&segment=0&download_key=film"),key("https://site/suxinvideo/native/hls?token=x&segment=1&download_key=film"))
    }
    @Test fun sameResourceRetainsByteRangeIdentity() {
        // DataSpec position/length must not be part of a key: Media3 stores ranges within this key.
        assertEquals(key(proxied("https://cdn/file.mp4?b=2&a=1")),key(proxied("https://cdn/file.mp4?a=1&b=2")))
        assertNotEquals(key(proxied("https://cdn/file.mp4?b=2&a=1")),key(proxied("https://cdn/file.mp4?b=3&a=1")))
    }
}
