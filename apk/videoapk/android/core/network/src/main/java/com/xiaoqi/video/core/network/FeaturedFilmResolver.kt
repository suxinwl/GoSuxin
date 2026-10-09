package com.xiaoqi.video.core.network

import com.xiaoqi.video.core.model.Film
import com.xiaoqi.video.core.network.JsonWire.long

/** Only a server-verified import may turn a source catalogue identity into a CMS film ID. */
suspend fun ApiClient.resolveFeaturedFilm(film: Film, expectedOrigin: String? = null): Long {
    if (film.id > 0) return film.id
    require(film.hasPlayableIdentity) { "精选影片标识无效，请刷新列表" }
    val value = request("catalog/featured/resolve", "POST", mapOf("remote_id" to film.remoteId.toString(), "resolve_token" to film.resolveToken),
        accessToken = "", retry = false, expectedOrigin = expectedOrigin)
    val id = value.takeIf { it.isJsonObject }?.asJsonObject?.long("vod_id") ?: 0
    require(id > 0) { "精选影片尚未载入，请重试" }
    return id
}
