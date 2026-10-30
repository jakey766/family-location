package com.jk.family.core

import kotlin.math.*

/** One instance per sharing attempt; elapsedMillis must come from a monotonic clock. */
class MotionPolicy(var config: DeviceConfig = DeviceConfig()) {
    var moving: Boolean = true
        private set
    val currentInterval get() = config.intervalMillis(moving)
    private fun select(isMoving: Boolean): Long { moving = isMoving; return currentInterval }
    private var anchor: Fix? = null
    private var stillSince: Long? = null
    private var lastCapturedAt = Long.MIN_VALUE
    private var lastElapsed = Long.MIN_VALUE

    fun intervalMillis(fix: Fix, elapsedMillis: Long): Long {
        if (!fix.isValid() || fix.capturedAtMillis <= lastCapturedAt || elapsedMillis < lastElapsed) {
            anchor = null
            stillSince = null
            // Do not count this point as stillness, but permit a corrected clock's
            // subsequent increasing timestamps to start a fresh observation.
            lastCapturedAt = if (fix.isValid()) fix.capturedAtMillis else Long.MIN_VALUE
            lastElapsed = elapsedMillis
            return select(true)
        }
        lastCapturedAt = fix.capturedAtMillis
        lastElapsed = elapsedMillis
        if (fix.speedMps >= 1f) {
            anchor = null
            stillSince = null
            return select(true)
        }
        val previous = anchor
        if (previous == null || distanceMeters(previous, fix) > max(50.0, previous.accuracyM + fix.accuracyM)) {
            anchor = fix
            stillSince = elapsedMillis
            return select(true)
        }
        return select(elapsedMillis - (stillSince ?: elapsedMillis) < STILL_INTERVAL)
    }

    private fun distanceMeters(a: Fix, b: Fix): Double {
        val latDelta = Math.toRadians(b.latitude - a.latitude)
        val lonDelta = Math.toRadians(b.longitude - a.longitude)
        val h = sin(latDelta / 2).pow(2) + cos(Math.toRadians(a.latitude)) *
            cos(Math.toRadians(b.latitude)) * sin(lonDelta / 2).pow(2)
        return 6_371_000.0 * 2 * asin(sqrt(h.coerceIn(0.0, 1.0)))
    }

    companion object {
        const val MOVING_INTERVAL = 120_000L
        const val STILL_INTERVAL = 300_000L
    }
}
