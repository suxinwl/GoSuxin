package com.xiaoqi.video.core.data

import kotlinx.coroutines.*
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

/** A tiny in-memory metadata warmup. Playback URLs and authorization never enter it. */
internal class DetailRequests<K,V>(
    private val scope:CoroutineScope,
    private val ttlMs:Long=3000,
    private val maxEntries:Int=6,
    private val now:()->Long={ System.nanoTime()/1_000_000 },
    private val load:suspend (K)->V
) {
    private class Entry<V>(val task:Deferred<V>,var readers:Int=0,var completedAt:Long=Long.MAX_VALUE)
    private val lock=Mutex()
    private val entries=LinkedHashMap<K,Entry<V>>()

    suspend fun get(key:K,fresh:Boolean=false):V {
        val entry=lock.withLock {
            val at=now()
            val expired=entries.filter { (stored,item)->item.readers==0 && item.task.isCompleted && ((fresh && stored==key) || at-item.completedAt>=ttlMs || item.task.isCancelled) }.keys
            expired.forEach { entries.remove(it) }
            val existing=entries[key]
            val found=existing?:Entry(scope.async(start=CoroutineStart.LAZY) { load(key) }).also { entries[key]=it }
            found.readers++
            found
        }
        var succeeded=false
        try {
            val value=entry.task.await()
            succeeded=true
            return value
        } finally {
            // Navigation cancellation must release its reader even while its Job is cancelled.
            withContext(NonCancellable) { lock.withLock {
                entry.readers--
                if(succeeded&&entry.completedAt==Long.MAX_VALUE)entry.completedAt=now()
                if(entry.readers==0 && (!entry.task.isCompleted || !succeeded)) {
                    if(entries[key]===entry)entries.remove(key)
                    entry.task.cancel()
                }
                while(entries.size>maxEntries) {
                    val evict=entries.entries.firstOrNull { it.value.readers==0&&it.value.task.isCompleted }?:break
                    entries.remove(evict.key)
                }
            } }
        }
    }

    /** Only an already completed warmup can seed a visible screen; this never starts or waits for HTTP. */
    suspend fun peek(key:K):V? {
        val task=lock.withLock {
            val entry=entries[key]?:return@withLock null
            if(!entry.task.isCompleted)return@withLock null
            if(entry.task.isCancelled || now()-entry.completedAt !in 0 until ttlMs) {
                if(entry.readers==0)entries.remove(key)
                return@withLock null
            }
            entry.task
        }?:return null
        return task.await()
    }

    /** A successful local metadata mutation must not reuse the previous film snapshot. */
    suspend fun invalidate(matches:(K)->Boolean)=lock.withLock {
        val stale=entries.keys.filter(matches)
        stale.forEach { key->entries.remove(key)?.task?.cancel() }
    }

    suspend fun clear()=lock.withLock {
        entries.values.forEach { it.task.cancel() }
        entries.clear()
    }
}
