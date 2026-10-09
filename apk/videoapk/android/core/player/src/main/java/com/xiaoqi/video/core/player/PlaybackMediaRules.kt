package com.xiaoqi.video.core.player

import com.xiaoqi.video.core.model.Episode
import com.xiaoqi.video.core.model.PlaybackIdentity
import com.xiaoqi.video.core.model.Source

/** Shared, platform-independent checks used by the player and download helper. */
object PlaybackMediaRules {
    /** Manual switching also works before the first descriptor has resolved. */
    fun manualSource(sources: List<Source>, target: Source, request: PlaybackIdentity, position: Long): PlaybackIdentity? {
        val previous = sources.firstOrNull { it.code == request.line }
        val oldEpisode = previous?.episodes?.firstOrNull { it.key == request.episodeKey }
            ?: previous?.episodes?.getOrNull(request.episode)
        val previousVersion = request.versionKey.ifBlank { previous?.versionKey.orEmpty() }
        val sameVersion = previousVersion.isNotBlank() && target.versionKey == previousVersion
        val episode = if (sameVersion) matchingEpisode(target, request.episodeKey, oldEpisode?.number)
            ?: return null else target.episodes.firstOrNull() ?: return null
        return request.copy(line = target.code, episodeKey = episode.key, episode = target.episodes.indexOf(episode),
            positionMs = if (sameVersion) position.coerceAtLeast(0) else 0, manual = true, versionKey = target.versionKey)
    }

    fun isHls(type: String, url: String): Boolean =
        type.lowercase() in setOf("hls", "m3u8", "application/vnd.apple.mpegurl", "application/x-mpegurl") ||
            url.substringBefore('?').substringBefore('#').endsWith(".m3u8", ignoreCase = true)

    fun matchingEpisode(source: Source, key: String, number: Int?): Episode? =
        source.episodes.firstOrNull { key.isNotBlank() && it.key == key }
            ?: number?.takeIf { it >= 0 }?.let { n -> source.episodes.firstOrNull { it.number == n && it.number >= 0 } }

    fun alternate(
        sources: List<Source>, identity: PlaybackIdentity, version: String,
        episodeNumber: Int?, attempted: Set<String>
    ): Pair<Source, Episode>? = sources.asSequence()
        .filter { it.code !in attempted && it.versionKey == version }
        .mapNotNull { source -> matchingEpisode(source, identity.episodeKey, episodeNumber)?.let { source to it } }
        .firstOrNull()
}
