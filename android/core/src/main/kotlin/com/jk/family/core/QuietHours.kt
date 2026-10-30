package com.jk.family.core

import java.time.Instant
import java.time.LocalTime
import java.time.ZoneId

/** One device-local daily [start, end) interval, including intervals crossing midnight. */
object QuietHours {
    fun isQuiet(nowMillis: Long = System.currentTimeMillis(), zone: ZoneId = ZoneId.systemDefault(), config: DeviceConfig = DeviceConfig()): Boolean {
        val time = Instant.ofEpochMilli(nowMillis).atZone(zone).toLocalTime()
        val start = LocalTime.parse(config.quietHoursStart)
        val end = LocalTime.parse(config.quietHoursEnd)
        return if (start < end) time >= start && time < end else time >= start || time < end
    }

    /** Last actual resume boundary, including repeated/missing wall-clock times at DST changes. */
    fun activityWindow(nowMillis: Long = System.currentTimeMillis(), zone: ZoneId = ZoneId.systemDefault(), config: DeviceConfig = DeviceConfig()): Long =
        boundaries(nowMillis, zone, config).filter { it <= nowMillis && !isQuiet(it, zone, config) }.max()

    fun nextBoundary(nowMillis: Long = System.currentTimeMillis(), zone: ZoneId = ZoneId.systemDefault(), config: DeviceConfig = DeviceConfig()): Long =
        boundaries(nowMillis, zone, config).filter { it > nowMillis }.min()

    private fun boundaries(nowMillis: Long, zone: ZoneId, config: DeviceConfig): List<Long> {
        val date = Instant.ofEpochMilli(nowMillis).atZone(zone).toLocalDate()
        val candidates = (-3L..3L).flatMap { day ->
            listOf(config.quietHoursStart, config.quietHoursEnd).flatMap { value ->
                val local = date.plusDays(day).atTime(LocalTime.parse(value))
                // A missing wall time is handled by the zone transition itself below.
                zone.rules.getValidOffsets(local).map { local.toInstant(it).toEpochMilli() }
            }
        }.toMutableList()
        var cursor = date.minusDays(3).atStartOfDay(zone).toInstant()
        val until = date.plusDays(4).atStartOfDay(zone).toInstant()
        while (true) {
            val transition = zone.rules.nextTransition(cursor) ?: break
            if (transition.instant >= until) break
            candidates.add(transition.instant.toEpochMilli())
            cursor = transition.instant.plusSeconds(1)
        }
        return candidates.distinct().filter { isQuiet(it - 1, zone, config) != isQuiet(it, zone, config) }
    }
}
