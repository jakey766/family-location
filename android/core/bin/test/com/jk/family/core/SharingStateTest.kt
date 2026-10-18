package com.jk.family.core

import org.junit.Assert.*
import org.junit.Test

class SharingStateTest {
    private fun fix(time: Long = 1_700_000_000_000L) = Fix(30.0, 120.0, 5.0, time, 50)

    @Test fun onlyCurrentStartCanActivateSession() {
        val state = SharingState()
        assertFalse(state.active)
        assertNull(state.capture(fix()))
        val first = state.begin()
        assertTrue(state.active)
        assertNull(state.capture(fix()))
        assertNull(state.stop())
        assertFalse(state.started(first, "late"))
        val second = state.begin()
        assertFalse(state.started(first, "old"))
        assertTrue(state.started(second, "new"))
        assertEquals("new", state.stop())
        assertFalse(state.active)
        assertNull(state.sessionId)
        assertNull(state.pending)
    }

    @Test fun latestPointReplacesOfflinePointAndOldAckCannotEraseIt() {
        val state = SharingState()
        state.started(state.begin(), "session")
        val first = state.capture(fix())!!
        val second = state.capture(fix(first.fix.capturedAtMillis + 1))!!
        assertEquals(1L, first.sequence)
        assertEquals(2L, second.sequence)
        assertSame(second, state.pending)
        state.acknowledged(first)
        assertSame(second, state.pending)
        state.acknowledged(second.copy(sessionId = "old"))
        assertSame(second, state.pending)
        assertEquals(first.fix.capturedAtMillis + 1, state.pending!!.fix.capturedAtMillis)
        state.acknowledged(second)
        assertNull(state.pending)
    }

    @Test fun rejectInvalidDuplicateAndBackwardsFixesWithoutAdvancingSequence() {
        val state = SharingState()
        state.started(state.begin(), "session")
        val first = state.capture(fix())!!
        for (invalid in listOf(fix(), fix(first.fix.capturedAtMillis - 1), fix().copy(latitude = Double.NaN),
            fix().copy(longitude = 181.0), fix().copy(accuracyM = -1.0), fix().copy(batteryPercent = 101),
            fix().copy(speedMps = Float.NaN))) assertNull(state.capture(invalid))
        assertSame(first, state.pending)
        assertEquals(2L, state.capture(fix(first.fix.capturedAtMillis + 1))!!.sequence)
        state.started(state.begin(), "next")
        assertEquals(1L, state.capture(fix())!!.sequence)
    }

    @Test fun rejectedFutureFixAllowsCorrectedTimeWithoutRestartingSharing() {
        val state = SharingState()
        state.started(state.begin(), "session")
        val future = state.capture(fix(1_800_000_000_000L))!!
        state.rejected(future)
        assertNull(state.pending)
        val corrected = state.capture(fix())!!
        assertEquals(2L, corrected.sequence)
        assertTrue(state.active)
        assertEquals("session", corrected.sessionId)
    }

    @Test fun rejectionNeverRollsBackAlreadyAcceptedTime() {
        val state = SharingState()
        state.started(state.begin(), "session")
        val accepted = state.capture(fix())!!
        val future = state.capture(fix(1_800_000_000_000L))!!
        // An older in-flight request can succeed after a newer fix replaced pending.
        state.acknowledged(accepted)
        state.rejected(future)
        state.rejected(accepted)
        assertNull(state.capture(fix(accepted.fix.capturedAtMillis - 1)))
        assertNull(state.capture(accepted.fix))
        assertEquals(3L, state.capture(fix(accepted.fix.capturedAtMillis + 1))!!.sequence)
    }

    @Test fun staleRejectionCannotEraseNewPendingOrLowerItsTimeWatermark() {
        val state = SharingState()
        state.started(state.begin(), "session")
        val first = state.capture(fix())!!
        val newest = state.capture(fix(first.fix.capturedAtMillis + 10))!!
        state.rejected(first)
        state.rejected(newest.copy(sessionId = "another-session"))
        assertSame(newest, state.pending)
        assertNull(state.capture(fix(first.fix.capturedAtMillis + 1)))
        state.rejected(newest)
        assertNotNull(state.capture(fix(first.fix.capturedAtMillis + 1)))
    }

    @Test fun lateAcknowledgementFromOldSessionDoesNotPoisonNewTimeWatermark() {
        val state = SharingState()
        state.started(state.begin(), "old")
        val future = state.capture(fix(1_800_000_000_000L))!!
        state.started(state.begin(), "new")
        state.acknowledged(future)
        assertNotNull(state.capture(fix()))
    }
}
