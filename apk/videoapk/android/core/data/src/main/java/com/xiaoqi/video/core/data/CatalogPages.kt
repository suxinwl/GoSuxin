package com.xiaoqi.video.core.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.Semaphore
import kotlinx.coroutines.sync.withLock

/** Every server-side filter belongs to the key. No member entitlements or media URLs are cached. */
data class CatalogRequest(
    val query:String="",val channel:Long=0,val type:Long=0,val page:Int=1,
    val sort:String="time",val area:String="",val year:String="",val size:Int=30,
    val source:String="local",val topic:Long=0
) {
    fun normalized():CatalogRequest {
        val featured=source=="featured"
        return copy(query=if(featured)"" else query.trim(),channel=channel.coerceAtLeast(0),type=if(featured)0 else type.coerceAtLeast(0),page=page.coerceAtLeast(1),
            sort=when { featured&&channel>0->"time";sort=="hits"||sort=="hot"->"hits";!featured&&sort=="score"->"score";else->"time" },
            area=if(featured)"" else area.trim(),year=if(featured)"" else year.trim(),size=size.coerceIn(1,100),
            source=if(featured)"featured" else "local",topic=if(featured&&channel>0)topic.coerceAtLeast(0) else 0)
    }
    fun apiPath()=if(source=="featured")"catalog/featured" else "films"
    fun queryParameters()=buildMap {
        if(source=="featured") {
            put("channel_id",channel.toString());put("topic_id",topic.toString())
            put("page",page.toString());put("size",this@CatalogRequest.size.toString());put("order",sort)
            return@buildMap
        }
        put("q",query);put("type_id",type.toString());put("page",page.toString())
        // Map.size is an implicit receiver property here; it is 3 after the above entries.
        // Qualify the request field or a 30-card page is silently sent as size=3.
        put("size",this@CatalogRequest.size.toString())
        put("order",sort);put("area",area);put("year",year)
        // Absence selects the collected-library search handler; explicit channel 0 uses a different taxonomy query.
        if(channel>0)put("channel_id",channel.toString())
    }
}

internal data class CatalogResult<V>(val value:V,val revision:String)

/** Small bounded public catalogue cache. Policy must be checked before a cached page can be displayed. */
internal class CatalogPages<K,V>(
    scope:CoroutineScope,
    private val revision:suspend (K)->String,
    private val ttlMs:Long=120_000,
    private val maxEntries:Int=32,
    private val now:()->Long={ System.nanoTime()/1_000_000 },
    load:suspend (K)->CatalogResult<V>
) {
    private data class Cached<V>(val result:CatalogResult<V>,val at:Long)
    private data class FlightKey<K>(val key:K,val generation:Long)
    private val lock=Mutex()
    private val cache=LinkedHashMap<K,Cached<V>>(16,.75f,true)
    private var generation=0L
    private val background=Semaphore(2)
    private val warmups=java.util.concurrent.ConcurrentHashMap.newKeySet<Job>()
    @Volatile private var pausedUntil=0L
    private val flights=DetailRequests<FlightKey<K>,CatalogResult<V>>(scope,ttlMs=0,maxEntries=maxEntries,now=now,load={ request->
        val result=load(request.key)
        // An older server without revisions remains usable, but never contributes cached cards.
        lock.withLock {
            requireGeneration(request.generation)
            if(result.revision.isNotBlank()) {
                cache[request.key]=Cached(result,now())
                while(cache.size>maxEntries)cache.remove(cache.keys.first())
            }
        }
        result
    })

    suspend fun get(key:K,fresh:Boolean=false):V {
        val (requestedGeneration,cached)=lock.withLock {
            val value=cache[key]
            generation to if(fresh||value==null||now()-value.at !in 0 until ttlMs) { cache.remove(key);null } else value
        }
        if(cached!=null) {
            val current=try { revision(key) }
            catch(e:CancellationException) { throw e }
            catch(e:com.xiaoqi.video.core.network.ApiException) {
                lock.withLock { requireGeneration(requestedGeneration);cache.clear() }
                if(e.status==401||e.status==403)throw e
                "" // A missing/failed revision route requires live data; never fall back to old cards.
            }
            catch(_:java.io.IOException) { "" }
            if(current.isNotBlank()&&current==cached.result.revision) {
                val valid=lock.withLock { requireGeneration(requestedGeneration);cache[key]===cached && now()-cached.at in 0 until ttlMs }
                if(valid)return cached.result.value
            }
            lock.withLock { requireGeneration(requestedGeneration);cache.clear() }
        }
        // A cache miss starts immediately; the initial page does not wait for an extra policy request.
        val result=flights.get(FlightKey(key,requestedGeneration)).value
        lock.withLock { requireGeneration(requestedGeneration) }
        return result
    }

    /** No waiting queue: background work cannot consume unbounded connections ahead of visible pages. */
    suspend fun prefetch(key:K):Boolean {
        if(now()<pausedUntil)return false
        if(!background.tryAcquire())return false
        val job=currentCoroutineContext()[Job]
        if(job!=null)warmups.add(job)
        return try {
            if(now()<pausedUntil)return false
            get(key)
            lock.withLock { cache[key]?.let { now()-it.at in 0 until ttlMs }==true }
        } catch(e:CancellationException) { throw e }
          catch(_:Exception) { false }
        finally { if(job!=null)warmups.remove(job);background.release() }
    }

    /** Playback navigation has priority. Joined foreground readers still retain their shared HTTP call. */
    fun pausePrefetch(durationMs:Long=15_000) {
        pausedUntil=now()+durationMs
        warmups.toList().forEach { it.cancel() }
    }

    /** Site changes invalidate both cards and work already awaiting an old site's response. */
    suspend fun clear() {
        warmups.toList().forEach { it.cancel() }
        lock.withLock {
            generation++
            cache.clear()
            pausedUntil=0L
            flights.clear()
        }
    }

    private fun requireGeneration(expected:Long) {
        if(generation!=expected)throw CancellationException("Catalogue server changed")
    }
}
