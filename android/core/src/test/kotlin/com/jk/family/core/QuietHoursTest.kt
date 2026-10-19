package com.jk.family.core

import java.time.Instant
import java.time.ZoneId
import org.junit.Assert.*
import org.junit.Test

class QuietHoursTest {
    private val zone = ZoneId.of("Asia/Shanghai")
    private fun time(value: String) = Instant.parse(value).toEpochMilli()

    @Test fun localBoundariesAndNextDayAreExact() {
        for ((utc, quiet, next) in listOf(
            Triple("2026-10-19T13:59:59Z", false, "2026-10-19T14:00:00Z"),
            Triple("2026-10-19T14:00:00Z", true, "2026-10-20T00:00:00Z"),
            Triple("2026-10-19T23:59:59Z", true, "2026-10-20T00:00:00Z"),
            Triple("2026-10-20T00:00:00Z", false, "2026-10-20T14:00:00Z")
        )) {
            assertEquals(quiet, QuietHours.isQuiet(time(utc), zone))
            assertEquals(time(next), QuietHours.nextBoundary(time(utc), zone))
        }
    }

    @Test fun dailyWindowChangesAtEightAndAlsoTracksTimezone() {
        val evening = time("2026-10-19T13:59:59Z")
        val morning = time("2026-10-20T00:00:00Z")
        assertEquals(QuietHours.activityWindow(evening, zone), QuietHours.activityWindow(morning - 1, zone))
        assertNotEquals(QuietHours.activityWindow(evening, zone), QuietHours.activityWindow(morning, zone))
        assertNotEquals(QuietHours.activityWindow(evening, zone), QuietHours.activityWindow(evening, ZoneId.of("UTC")))
    }

    @Test fun timezoneChangesAndDaylightSavingUseLocalClock() {
        val now = time("2026-10-19T14:00:00Z")
        assertTrue(QuietHours.isQuiet(now, zone))
        assertFalse(QuietHours.isQuiet(now, ZoneId.of("UTC")))
        // New York rolls back during this night: 22:00 to 08:00 spans 11 hours.
        val start = time("2026-11-01T02:00:00Z")
        assertEquals(time("2026-11-01T13:00:00Z"), QuietHours.nextBoundary(start, ZoneId.of("America/New_York")))
    }
}
