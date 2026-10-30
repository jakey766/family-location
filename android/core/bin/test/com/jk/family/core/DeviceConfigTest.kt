package com.jk.family.core

import com.google.gson.JsonParser
import java.time.Instant
import java.time.ZoneId
import org.junit.Assert.*
import org.junit.Test

class DeviceConfigTest {
    private fun time(s: String) = Instant.parse(s).toEpochMilli()
    @Test fun strictWholeConfigValidation() {
        val good = """{"moving_interval_seconds":120,"stationary_interval_seconds":300,"moving_high_accuracy":false,"quiet_hours_start":"22:00","quiet_hours_end":"08:00","version":1}"""
        assertEquals(DeviceConfig(), DeviceConfig.parse(JsonParser.parseString(good)))
        for (bad in listOf(good.replace("120", "61"), good.replace("120", "120.5"), good.replace("300", "60"),
            good.replace("300", "3660"), good.replace("false", "\"false\""), good.replace("22:00", "8:00"),
            good.replace("22:00", "08:00"), good.replace("22:00", "24:00"), good.replace("\"version\":1", "\"version\":0"),
            good.replace("\"version\":1", "\"version\":1.2"), "{}", "null")) {
            assertNull(bad, DeviceConfig.parse(JsonParser.parseString(bad)))
        }
    }
    @Test fun sameDayMinuteBoundariesAndActivityWindow() {
        val c = DeviceConfig(60, 60, true, "12:15", "13:45", 2)
        val zone = ZoneId.of("UTC")
        val start = time("2026-10-30T12:15:00Z")
        val end = time("2026-10-30T13:45:00Z")
        assertFalse(QuietHours.isQuiet(start - 1, zone, c))
        assertTrue(QuietHours.isQuiet(start, zone, c))
        assertTrue(QuietHours.isQuiet(end - 1, zone, c))
        assertFalse(QuietHours.isQuiet(end, zone, c))
        assertEquals(start, QuietHours.nextBoundary(start - 1, zone, c))
        assertEquals(end, QuietHours.nextBoundary(start, zone, c))
        assertEquals(time("2026-10-31T12:15:00Z"), QuietHours.nextBoundary(end, zone, c))
        assertNotEquals(QuietHours.activityWindow(start - 1, zone, c), QuietHours.activityWindow(end, zone, c))
    }
    @Test fun dstGapAndOverlapFollowActualLocalQuietState() {
        val zone = ZoneId.of("America/New_York")
        val gap = DeviceConfig(60, 60, false, "01:45", "02:15", 2)
        val beforeGap = time("2026-03-08T06:45:00Z")
        val transition = time("2026-03-08T07:00:00Z")
        assertTrue(QuietHours.isQuiet(beforeGap, zone, gap))
        assertFalse(QuietHours.isQuiet(transition, zone, gap))
        assertEquals(transition, QuietHours.nextBoundary(beforeGap, zone, gap))
        val overlap = DeviceConfig(60, 60, false, "00:30", "01:30", 3)
        // The clock rolls back from 01:59 to 01:00, entering quiet time again.
        assertEquals(time("2026-11-01T06:00:00Z"),
            QuietHours.nextBoundary(time("2026-11-01T05:45:00Z"), zone, overlap))
        assertEquals(time("2026-11-01T06:30:00Z"),
            QuietHours.nextBoundary(time("2026-11-01T06:00:00Z"), zone, overlap))
    }

    @Test fun equalIntervalsStillHaveIndependentMotionState() {
        val c = DeviceConfig(60, 60, true)
        val policy = MotionPolicy(c)
        fun fix(t: Long, speed: Float = 0f) = Fix(30.0, 120.0, 5.0, 1700000000000L + t, null, speed)
        assertEquals(60000L, policy.intervalMillis(fix(0), 0))
        assertTrue(policy.moving)
        assertEquals(60000L, policy.intervalMillis(fix(300000), 300000))
        assertFalse(policy.moving)
        policy.intervalMillis(fix(300001, 1f), 300001)
        assertTrue(policy.moving)
    }
}
