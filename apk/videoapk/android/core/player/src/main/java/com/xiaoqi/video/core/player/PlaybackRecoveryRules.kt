package com.xiaoqi.video.core.player

import com.xiaoqi.video.core.model.*

data class SourceDiscoveryProgress(
    val status:String="",val checked:Int=0,val total:Int=0,val added:Int=0,
    val updated:Int=0,val failed:Int=0,val message:String=""
) {
    val running:Boolean get()=status in setOf("starting","queued","running")
    val summary:String get()=buildString {
        append(message.ifBlank { if(running)"正在查找更多片源" else "" })
        if(total>0)append(" · 已检查 ${checked.coerceIn(0,total)}/$total")
        if(added>0||updated>0)append(" · 新增 $added · 更新 $updated")
    }
}

/** Automatic recovery must preserve the edition and episode and never grant access. */
object PlaybackRecoveryRules {
    fun canDiscover(offline:Boolean,status:Int,filmId:Long)=
        !offline && filmId>0 && status !in setOf(401,403,404,410,451)

    /** A descriptor may still belong to the previous episode while parsing fails. */
    fun currentIdentity(requested:PlaybackIdentity?,descriptor:Playback?,position:Long,quality:String,pending:Boolean=false):PlaybackIdentity? {
        if(pending&&requested!=null)return requested
        if(descriptor==null)return requested
        val resolved=PlaybackIdentity(descriptor.vodId,descriptor.line,descriptor.episodeKey,descriptor.episode,
            quality,position.coerceAtLeast(0),true,descriptor.versionKey)
        if(requested==null)return resolved
        val same=requested.vodId==descriptor.vodId && requested.line==descriptor.line &&
            (if(requested.episodeKey.isNotBlank())requested.episodeKey==descriptor.episodeKey else requested.episode==descriptor.episode) &&
            (requested.versionKey.isBlank()||requested.versionKey==descriptor.versionKey)
        return if(same)resolved else requested
    }

    fun attemptKey(request:PlaybackIdentity,sources:List<Source>):String {
        val source=sources.find { it.code==request.line }
        val episode=source?.episodes?.find { it.key==request.episodeKey }?:source?.episodes?.getOrNull(request.episode)
        return "${request.vodId}|${request.versionKey.ifBlank { source?.versionKey.orEmpty() }}|${episode?.number?:request.episodeKey.ifBlank { request.episode.toString() }}"
    }

    fun discoveredIdentity(
        request:PlaybackIdentity,previous:List<Source>,detail:Detail,attempted:Set<String>,updated:Boolean
    ):PlaybackIdentity? {
        val old=previous.find { it.code==request.line }
        val oldEpisode=old?.episodes?.find { it.key==request.episodeKey }?:old?.episodes?.getOrNull(request.episode)
        val version=request.versionKey.ifBlank { old?.versionKey.orEmpty() }
        val preferred=detail.sources.find { it.code==detail.preferredLine }?:PlaybackRules.defaultSource(detail.film,detail.sources)
        val sources=(listOfNotNull(preferred)+detail.sources).distinctBy { it.code }
        val initial=request.line.isBlank() && request.episodeKey.isBlank() && request.episode==0
        return sources.asSequence()
            .filter { (updated||it.code !in attempted) && (version.isBlank()||it.versionKey==version) }
            .mapNotNull { source ->
                val episode=PlaybackMediaRules.matchingEpisode(source,request.episodeKey,oldEpisode?.number)
                    ?:if(initial)source.episodes.firstOrNull() else null
                episode?.let { request.copy(line=source.code,episodeKey=it.key,episode=source.episodes.indexOf(it),
                    versionKey=source.versionKey,manual=true) }
            }.firstOrNull()
    }
}
