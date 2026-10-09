package com.xiaoqi.video.core.data

import androidx.datastore.preferences.core.mutablePreferencesOf
import org.junit.Assert.*
import org.junit.Test

class SessionPartitionsTest {
    private val original="https://xq.suxinwl.com:8600"
    private val replacement="https://next.example:8600"

    @Test fun unmarkedLegacyAccountMigratesOnlyToTheOriginalDefaultSite() {
        val prefs=mutablePreferencesOf(SessionPartitions.legacyKey to "encrypted-old-account")
        SessionPartitions.migrate(prefs,original) { it }
        assertEquals("encrypted-old-account",prefs[SessionPartitions.key(original)])
        assertNull(prefs[SessionPartitions.key(replacement)])
        assertNull(prefs[SessionPartitions.legacyKey])
    }

    @Test fun migrationPreservesAnAccountAlreadySavedForTheSameSite() {
        val prefs=mutablePreferencesOf(
            SessionPartitions.legacyKey to "stale-account",
            SessionPartitions.key(original) to "new-account",
            SessionPartitions.key(replacement) to "different-site-account"
        )
        SessionPartitions.migrate(prefs,original) { it }
        assertEquals("new-account",prefs[SessionPartitions.key(original)])
        assertEquals("different-site-account",prefs[SessionPartitions.key(replacement)])
        assertNull(prefs[SessionPartitions.legacyKey])
    }

    @Test fun explicitlyMarkedLegacyAccountRetainsItsOwnOrigin() {
        val prefs=mutablePreferencesOf(
            SessionPartitions.legacyKey to "replacement-account",
            SessionPartitions.legacyOriginKey to replacement
        )
        SessionPartitions.migrate(prefs,original) { it }
        assertEquals("replacement-account",prefs[SessionPartitions.key(replacement)])
        assertNull(prefs[SessionPartitions.key(original)])
        assertNull(prefs[SessionPartitions.legacyOriginKey])
    }

    @Test fun invalidMarkedOriginCannotAssignAnAccountToTheDefaultSite() {
        val prefs=mutablePreferencesOf(
            SessionPartitions.legacyKey to "unknown-site-account",
            SessionPartitions.legacyOriginKey to "invalid-origin"
        )
        SessionPartitions.migrate(prefs,original) { null }
        assertNull(prefs[SessionPartitions.key(original)])
        assertNull(prefs[SessionPartitions.key(replacement)])
    }

    @Test fun loginAndLogoutRevisionsAdvanceIndependentlyForEachSite() {
        val prefs=mutablePreferencesOf()
        assertEquals(1L,SessionPartitions.nextRevision(prefs,original))
        assertEquals(2L,SessionPartitions.nextRevision(prefs,original))
        assertEquals(1L,SessionPartitions.nextRevision(prefs,replacement))
        assertEquals(3L,SessionPartitions.nextRevision(prefs,original))
        assertEquals(1L,prefs[SessionPartitions.revisionKey(replacement)])
    }
}
