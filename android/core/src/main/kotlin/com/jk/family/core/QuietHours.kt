package com.jk.family.core

import java.time.Instant
import java.time.ZoneId

/** Device-local quiet hours: [22:00, next day's 08:00). Re-read the zone on every call. */
object QuietHours {
    fun isQuiet(nowMillis: Long = System.currentTimeMillis(), zone: ZoneId = ZoneId.systemDefault()): Boolean {
        val hour = Instant.ofEpochMilli(nowMillis).atZone(zone).hour
        return hour >= 22 || hour < 8
    }

    /** Identifies a local 08:00-to-next-08:00 window, including timezone/DST changes. */
    fun activityWindow(nowMillis: Long = System.currentTimeMillis(), zone: ZoneId = ZoneId.systemDefault()): Long {
        val now = Instant.ofEpochMilli(nowMillis).atZone(zone)
        val date = if (now.hour < 8) now.toLocalDate().minusDays(1) else now.toLocalDate()
        return date.atTime(8, 0).atZone(zone).toInstant().toEpochMilli()
    }

    fun nextBoundary(nowMillis: Long = System.currentTimeMillis(), zone: ZoneId = ZoneId.systemDefault()): Long {
        val now = Instant.ofEpochMilli(nowMillis).atZone(zone)
        val date = if (now.hour >= 22) now.toLocalDate().plusDays(1) else now.toLocalDate()
        val hour = if (now.hour < 8 || now.hour >= 22) 8 else 22
        return date.atTime(hour, 0).atZone(zone).toInstant().toEpochMilli()
    }
}
