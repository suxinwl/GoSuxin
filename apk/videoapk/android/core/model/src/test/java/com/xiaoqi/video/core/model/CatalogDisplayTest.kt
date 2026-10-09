package com.xiaoqi.video.core.model

import org.junit.Assert.assertEquals
import org.junit.Test

class CatalogDisplayTest {
    @Test fun oldInstallDefaultsToAutomaticColumnsAndTenRows() {
        assertEquals(CatalogDisplay(0, 10), CatalogDisplay().normalized())
    }

    @Test fun allSupportedDisplayChoicesRemainUnchanged() {
        for (columns in CatalogDisplay.COLUMN_OPTIONS) {
            for (rows in CatalogDisplay.ROW_OPTIONS) {
                val value = CatalogDisplay(columns, rows)
                assertEquals(value, value.normalized())
            }
        }
    }

    @Test fun invalidSavedColumnValuesReturnToAutomaticWithoutLosingRows() {
        for (columns in listOf(Int.MIN_VALUE, -1, 1, 7, Int.MAX_VALUE)) {
            assertEquals(CatalogDisplay(0, 8), CatalogDisplay(columns, 8).normalized())
        }
    }

    @Test fun invalidSavedRowValuesReturnToTenWithoutLosingColumns() {
        for (rows in listOf(Int.MIN_VALUE, -1, 0, 1, 7, 9, 11, 13, Int.MAX_VALUE)) {
            assertEquals(CatalogDisplay(4, 10), CatalogDisplay(4, rows).normalized())
        }
    }
}
