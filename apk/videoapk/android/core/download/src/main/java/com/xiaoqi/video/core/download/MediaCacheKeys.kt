package com.xiaoqi.video.core.download

import java.net.URI
import java.net.URLDecoder
import java.net.URLEncoder
import java.security.MessageDigest

/** Only our own authorization parameters are volatile. Upstream queries identify real assets. */
object MediaCacheKeys {
    /** Keep legacy bytes accessible only for the original site; other servers get distinct namespaces. */
    fun namespace(origin:String,defaultOrigin:String,owner:Long,device:String):String =
        if(origin==defaultOrigin)"$owner:$device" else "server:${hash(origin)}:$owner:$device"
    private val volatile=setOf("grant","app_grant","token","exp","sig","access_token","expires","authorization","download_key")
    private fun parameters(uri:URI):List<Pair<String,String>> = uri.rawQuery.orEmpty().split('&').filter { it.isNotEmpty() }.map {
        URLDecoder.decode(it.substringBefore('='),"UTF-8") to URLDecoder.decode(it.substringAfter('=',""),"UTF-8")
    }
    private fun hash(value:String)=MessageDigest.getInstance("SHA-256").digest(value.toByteArray()).joinToString("") { "%02x".format(it) }
    private fun stable(uri:URI,stripOwnAuthorization:Boolean):String {
        val query=parameters(uri).filterNot { stripOwnAuthorization && it.first in volatile }.sortedWith(compareBy<Pair<String,String>> { it.first }.thenBy { it.second }).joinToString("&") {
            URLEncoder.encode(it.first,"UTF-8")+"="+URLEncoder.encode(it.second,"UTF-8")
        }
        return "${uri.scheme.orEmpty().lowercase()}://${uri.rawAuthority.orEmpty().lowercase()}${uri.rawPath.orEmpty()}"+(if(query.isEmpty())"" else "?$query")
    }
    fun build(address:String,namespace:String,explicitKey:String?=null):String {
        if(explicitKey!=null)return "$namespace:$explicitKey"
        return runCatching {
            val outer=URI(address)
            val params=parameters(outer)
            val downloadKey=params.firstOrNull { it.first=="download_key" }?.second
            if(downloadKey==null)return@runCatching "$namespace:${stable(outer,false)}"
            val nested=params.firstOrNull { it.first=="url" }?.second?.let(::URI)
            val resource=nested?:outer
            val isNativeManifest=outer.path.orEmpty().endsWith("/native/hls") && params.none { it.first=="segment" }
            val manifest=isNativeManifest || resource.path.orEmpty().endsWith(".m3u8",true)
            // Renewal must fetch a manifest with fresh child grants, while retaining completed media bytes.
            val credentials=if(manifest)":manifest:${hash(address)}" else ""
            "$namespace:$downloadKey:${stable(resource,nested==null)}$credentials"
        }.getOrElse { "$namespace:$address" }
    }
}
