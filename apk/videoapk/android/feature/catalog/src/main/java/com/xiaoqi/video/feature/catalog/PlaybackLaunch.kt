package com.xiaoqi.video.feature.catalog

import com.google.gson.JsonObject
import com.xiaoqi.video.core.model.*
import com.xiaoqi.video.core.network.JsonWire.long
import com.xiaoqi.video.core.network.JsonWire.string

/** Browsing always enters playback; metadata is fetched without adding a detail destination. */
object PlaybackLaunch {
    fun identity(detail:Detail,progress:JsonObject?):PlaybackIdentity {
        val savedLine=progress?.string("line").orEmpty()
        val savedVersion=progress?.string("version_key").orEmpty()
        val savedKey=progress?.string("episode_key").orEmpty()
        val saved=detail.sources.firstOrNull { it.code==savedLine && it.versionKey==savedVersion && it.episodes.any { e->e.key==savedKey } }
        val available=detail.sources.filter { it.episodes.isNotEmpty() }
        val source=saved?:available.find { it.code==detail.preferredLine }?:PlaybackRules.defaultSource(detail.film,available)
        val episode=if(saved!=null)saved.episodes.first { it.key==savedKey } else source?.episodes?.firstOrNull()
        return PlaybackIdentity(detail.film.id,source?.code.orEmpty(),episode?.key.orEmpty(),if(source!=null&&episode!=null)source.episodes.indexOf(episode) else 0,
            quality=if(saved!=null)progress?.string("quality").orEmpty() else "",
            positionMs=if(saved!=null)progress?.long("position_ms")?.coerceAtLeast(0)?:0 else 0,
            manual=saved!=null,
            versionKey=source?.versionKey.orEmpty())
    }
}
