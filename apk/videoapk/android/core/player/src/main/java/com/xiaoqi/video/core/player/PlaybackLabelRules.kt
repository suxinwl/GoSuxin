package com.xiaoqi.video.core.player

import com.xiaoqi.video.core.model.*

/** Display identities follow the actual episode, never a film's update remark or list position. */
object PlaybackLabelRules {
    fun episodeName(detail: Detail?, descriptor: Playback?, requested: PlaybackIdentity?): String {
        // A new selection must not keep the previous episode's label while it resolves.
        if (requested != null && !matches(requested, descriptor))
            return findEpisode(detail, requested)?.let(::label).orEmpty()
        if (descriptor != null) {
            val episode = findEpisode(detail, PlaybackIdentity(descriptor.vodId, descriptor.line,
                descriptor.episodeKey, descriptor.episode, versionKey = descriptor.versionKey))
            // Resolve is authoritative even if cached catalogue metadata predates this episode.
            return descriptor.name.trim().takeIf { it.isNotBlank() }?.let { name ->
                if (name.all(Char::isDigit) && episode?.number?.let { it >= 0 } == true)
                    "第${episode.number}集" else name
            } ?: episode?.let(::label).orEmpty()
        }
        return requested?.let { findEpisode(detail, it)?.let(::label) }.orEmpty()
    }

    private fun matches(request: PlaybackIdentity, descriptor: Playback?): Boolean =
        descriptor != null && request.vodId == descriptor.vodId && request.line == descriptor.line &&
            (request.versionKey.isBlank() || request.versionKey == descriptor.versionKey) &&
            (if (request.episodeKey.isNotBlank()) request.episodeKey == descriptor.episodeKey
             else request.episode == descriptor.episode)

    private fun findEpisode(detail: Detail?, identity: PlaybackIdentity): Episode? {
        if (detail?.film?.id != identity.vodId) return null
        val source = detail.sources.firstOrNull { it.code == identity.line &&
            (identity.versionKey.isBlank() || it.versionKey == identity.versionKey) } ?: return null
        // Index fallback is only for legacy requests without a stable key. A missing key
        // must not silently label another episode at the same position.
        return if (identity.episodeKey.isNotBlank()) source.episodes.firstOrNull { it.key == identity.episodeKey }
            else source.episodes.getOrNull(identity.episode)
    }

    private fun label(episode: Episode): String = episode.name.trim().let { name ->
        if ((name.isBlank() || name.all(Char::isDigit)) && episode.number >= 0) "第${episode.number}集" else name
    }
}
