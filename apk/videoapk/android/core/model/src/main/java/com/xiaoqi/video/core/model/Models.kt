package com.xiaoqi.video.core.model

data class Category(val id: Long = 0, val name: String = "", val parentId: Long = 0)
data class Film(
    val id: Long = 0, val name: String = "", val pic: String = "", val banner: String = "",
    val score: Double = 0.0, val remarks: String = "", val year: String = "", val area: String = "",
    val typeId: Long = 0, val typeName: String = "", val actor: String = "", val director: String = "",
    val content: String = "", val isShort: Boolean = false, val isAnime: Boolean = false,
    val vip: Boolean = false, val points: Int = 0, val totalEpisodes: Int = 0,
    val provider: String = "", val remoteId: Long = 0, val resolveToken: String = ""
) {
    /** Provider IDs never enter the CMS film/detail/playback ID namespace. */
    val hasPlayableIdentity: Boolean get() = id > 0 || (id == 0L && provider == "yqk" && remoteId > 0 && resolveToken.isNotBlank())
    val cardKey: String get() = if (id > 0) "local:$id" else "$provider:$remoteId"
}
data class PayMethod(val code:String,val name:String)
data class SiteConfig(val name: String = "小柒影视", val logo: String = "", val description: String = "", val categories: List<Category> = emptyList(),val paymentMethods:List<PayMethod> = emptyList(),val registrationEnabled:Boolean=true,val memberEnabled:Boolean=true,val commentEnabled:Boolean=true,val theme:String="suxinlite",val registrationRequiresEmailCode:Boolean=true,val passwordResetEnabled:Boolean=true)
data class FilmPage(val items: List<Film> = emptyList(), val total: Int = 0, val page: Int = 1, val pages: Int = 1, val topics: List<Category> = emptyList(), val notice: String = "", val exactPages: Boolean = true)
data class HomeSection(val name: String, val typeId: Long, val items: List<Film>,val key:String="channel:$typeId:$name")
data class Banner(val title: String, val image: String, val filmId: Long, val url: String)
data class HomeData(val banners: List<Banner>, val sections: List<HomeSection>)

object AppearanceRules {
    val themes=setOf("suxinlite","suxinpro","iqiyi","guoguo")
    fun preference(value:String):String=value.trim().lowercase().takeIf { it=="follow" || it in themes }?:"follow"
    fun theme(value:String):String=value.trim().lowercase().takeIf { it in themes }?:"suxinlite"
}
data class Episode(val key: String = "", val number: Int = 0, val name: String = "")
data class Source(val code: String = "", val baseCode: String = "", val versionKey: String = "", val name: String = "", val episodes: List<Episode> = emptyList())
data class Comment(val id: Long = 0, val name: String = "", val content: String = "", val createdAt: String = "")
data class Detail(val film: Film, val sources: List<Source>, val preferredLine: String, val comments: List<Comment>, val related: List<Film>)
data class User(val id: Long = 0, val email: String = "", val name: String = "", val points: Int = 0, val vipExpire: Long = 0, val avatar: String? = null) {
    fun isVip(now: Long = System.currentTimeMillis() / 1000) = vipExpire > now
}
data class Session(val accessToken: String, val refreshToken: String, val expiresAt: Long, val user: User, val deviceId: String)
data class Captcha(val challengeId: String, val imageBase64: String, val imageMime: String, val expiresAt: Long)
data class Quality(val id: String = "", val name: String = "原画", val url: String = "", val width: Int = 0, val height: Int = 0, val bitrate: Long = 0)
data class Playback(
    val vodId: Long = 0, val line: String = "", val baseCode: String = "", val versionKey: String = "",
    val episodeKey: String = "", val episode: Int = 0, val name: String = "", val url: String = "", val type: String = "",
    val durationMs: Long = 0, val seekMode: String = "hls", val resumeOffsetMs: Long = 0,
    val expiresAt: Long = 0, val qualities: List<Quality> = emptyList(), val authorization: String = "",val quality:String=""
)
data class PlaybackIdentity(val vodId: Long, val line: String = "", val episodeKey: String = "", val episode: Int = 0, val quality: String = "", val positionMs: Long = 0, val manual: Boolean = false,val versionKey:String="")
data class History(val film: Film, val line: String, val episodeKey: String, val versionKey: String, val positionMs: Long, val durationMs: Long)
data class Package(val id: Long = 0, val name: String = "", val price: String = "", val days: Int = 0, val points: Int = 0)
data class License(val id: String, val deviceId: String, val expiresAt: Long, val revision: String, val descriptor: Playback,val issuedAt:Long=0)
data class Release(val platform: String = "", val versionName: String = "", val versionCode: Int = 0, val minSdk: Int = 23, val size: Long = 0, val sha256: String = "", val url: String = "", val changelog: String = "")

object PlaybackRules {
    fun defaultSource(film: Film, sources: List<Source>): Source? {
        fun isHongguo(s: Source) = s.code.contains("hongguo", true) || s.baseCode.contains("hongguo", true) || s.name.contains("红果")
        fun isAnime(s: Source) = s.code.contains("erciyuan", true) || s.baseCode.contains("erciyuan", true) || s.name.contains("二次元")
        fun isXiaoqi(s: Source) = s.code.startsWith("yqk_") || s.name.contains("小柒")
        fun app(s: Source) = isXiaoqi(s) && (s.code == "yqk_1" || s.name.contains("APP", true))
        return when {
            film.isShort || film.typeName.contains("短剧") -> sources.firstOrNull(::isHongguo)
            film.isAnime || film.typeName.contains("动漫") -> sources.firstOrNull(::isAnime) ?: sources.firstOrNull(::app)
            else -> sources.firstOrNull(::app) ?: sources.firstOrNull(::isXiaoqi)
        } ?: sources.firstOrNull { it.episodes.isNotEmpty() }
    }
    fun downloadKey(account: Long, film: Long, line: String, version: String, episode: String, quality: String) = "$account|$film|$line|$version|$episode|$quality"
    fun canPlayOffline(owner: Long, active: Long, expiresAt: Long, now: Long) = owner > 0 && owner == active && now < expiresAt
    fun nextEpisode(source: Source, key: String): Episode? = source.episodes.indexOfFirst { it.key == key }.let { i -> if (i >= 0) source.episodes.getOrNull(i + 1) else null }
}
