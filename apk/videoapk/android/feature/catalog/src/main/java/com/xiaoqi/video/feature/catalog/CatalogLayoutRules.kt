package com.xiaoqi.video.feature.catalog

import kotlin.math.floor

/** Use the width available to the grid, including tablet navigation and theme padding. */
internal object CatalogLayoutRules {
    const val gapDp = 10f
    private const val minimumPosterDp = 76f

    fun columns(availableWidthDp:Float, requested:Int=0):Int {
        val width=availableWidthDp.coerceAtLeast(0f)
        val fitting=floor((width+gapDp)/(minimumPosterDp+gapDp)).toInt().coerceIn(1,8)
        val automatic=when {
            width>=760f->6
            width>=620f->5
            width>=480f->4
            width>=248f->3
            else->2
        }
        return (if(requested==0)automatic else requested.coerceIn(2,8)).coerceAtMost(fitting)
    }

    /** The server accepts up to 100 cards. Keep prefetch and pagination on the same page size. */
    fun pageSize(columns:Int, rows:Int):Int=(columns.coerceAtLeast(1)*rows.coerceIn(4,12)).coerceIn(1,100)
}
