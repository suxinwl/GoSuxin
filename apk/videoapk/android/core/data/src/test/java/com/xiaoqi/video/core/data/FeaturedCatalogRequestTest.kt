package com.xiaoqi.video.core.data

import kotlinx.coroutines.*
import org.junit.Assert.*
import org.junit.Test

class FeaturedCatalogRequestTest {
    @Test fun featuredUsesTheRealProviderContractWithoutLocalTaxonomyFilters() {
        val request=CatalogRequest(query="仙逆",channel=8,type=24,page=2,sort="score",area="大陆",year="2026",size=30,source="featured",topic=41).normalized()
        assertEquals("catalog/featured",request.apiPath())
        assertEquals(mapOf("channel_id" to "8","topic_id" to "41","page" to "2","size" to "30","order" to "time"),request.queryParameters())
        assertEquals("",request.query);assertEquals(0L,request.type)
        assertEquals("",request.area);assertEquals("",request.year)
    }

    @Test fun allFeaturedHasAnExplicitZeroChannelAndOnlySupportedOrders() {
        val request=CatalogRequest(source="featured",topic=41,sort="hot",size=48).normalized()
        assertEquals("0",request.queryParameters()["channel_id"])
        assertEquals("0",request.queryParameters()["topic_id"])
        assertEquals("hits",request.queryParameters()["order"])
        assertEquals("48",request.queryParameters()["size"])
        assertEquals("time",request.copy(sort="score").normalized().sort)
    }

    @Test fun localSearchIsUnaffectedAndInvalidSourceCannotPickAnotherRoute() {
        val request=CatalogRequest(query=" 仙逆 ",type=24,source="unknown",topic=41).normalized()
        assertEquals("films",request.apiPath());assertEquals("仙逆",request.queryParameters()["q"])
        assertFalse(request.queryParameters().containsKey("topic_id"))
        assertFalse(request.queryParameters().containsKey("channel_id"))
        assertEquals(0L,request.topic)
    }

    @Test fun localFeaturedAndTopicsKeepSeparateCacheEntries()=runBlocking {
        val workers=CoroutineScope(SupervisorJob()+Dispatchers.Unconfined)
        try {
            var loads=0
            val pages=CatalogPages<CatalogRequest,String>(workers,revision={ "r1" },load={ key->
                loads++;CatalogResult("${key.source}:${key.topic}","r1")
            })
            val local=CatalogRequest(channel=8,size=30)
            val featured=local.copy(source="featured")
            val topic=featured.copy(topic=41)
            assertEquals("local:0",pages.get(local))
            assertEquals("featured:0",pages.get(featured))
            assertEquals("featured:41",pages.get(topic))
            assertEquals("local:0",pages.get(local));assertEquals(3,loads)
        } finally { workers.cancel() }
    }
}
