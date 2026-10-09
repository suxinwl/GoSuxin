package com.xiaoqi.video.core.model

/** Local poster layout preferences. Zero columns lets the current screen choose a fitting count. */
data class CatalogDisplay(val columns: Int = 0, val rows: Int = 10) {
    fun normalized(): CatalogDisplay = CatalogDisplay(
        columns = columns.takeIf { it == 0 || it in 2..6 } ?: 0,
        rows = rows.takeIf { it in ROW_OPTIONS } ?: 10
    )

    companion object {
        val COLUMN_OPTIONS: List<Int> = listOf(0, 2, 3, 4, 5, 6)
        val ROW_OPTIONS: List<Int> = listOf(6, 8, 10, 12)
    }
}
