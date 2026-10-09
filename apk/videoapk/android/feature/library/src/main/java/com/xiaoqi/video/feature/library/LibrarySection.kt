package com.xiaoqi.video.feature.library

/** Stable identities keep tab order separate from the selected content and its data endpoint. */
enum class LibrarySection(val title:String,val key:String) {
    History("观看历史","history"),
    Favorites("我的收藏","favorites"),
    Downloads("离线下载","downloads");

    companion object {
        fun available(tv:Boolean):List<LibrarySection> = if(tv)listOf(History,Favorites) else entries
    }
}
