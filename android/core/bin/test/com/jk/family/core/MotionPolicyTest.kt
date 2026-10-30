package com.jk.family.core

import org.junit.Assert.assertEquals
import org.junit.Test

class MotionPolicyTest {
    private fun fix(time: Long, latitude: Double = 30.0, speed: Float = 0f) =
        Fix(latitude, 120.0, 5.0, 1_700_000_000_000L + time, null, speed)

    @Test fun becomesStationaryAfterFiveMinutesAndSpeedImmediatelyRestoresMoving() {
        val policy = MotionPolicy()
        assertEquals(120_000L, policy.intervalMillis(fix(0), 0))
        assertEquals(120_000L, policy.intervalMillis(fix(299_999), 299_999))
        assertEquals(300_000L, policy.intervalMillis(fix(300_000), 300_000))
        assertEquals(120_000L, policy.intervalMillis(fix(300_001, speed = 1f), 300_001))
        assertEquals(120_000L, policy.intervalMillis(fix(300_002), 300_002))
    }

    @Test fun comparesAgainstAnchorSoSmallIncrementalMovementDoesNotLookStationary() {
        val policy = MotionPolicy()
        policy.intervalMillis(fix(0), 0)
        assertEquals(120_000L, policy.intervalMillis(fix(150_000, 30.0003), 150_000))
        assertEquals(120_000L, policy.intervalMillis(fix(300_000, 30.0006), 300_000))
        assertEquals(300_000L, policy.intervalMillis(fix(600_000, 30.0006), 600_000))
    }

    @Test fun badCoordinatesOrBackwardsTimesRestartStationaryObservation() {
        for (bad in listOf(fix(1).copy(latitude = Double.NaN), fix(-1), fix(0))) {
            val policy = MotionPolicy()
            policy.intervalMillis(fix(0), 0)
            assertEquals(120_000L, policy.intervalMillis(bad, 300_000))
            assertEquals(120_000L, policy.intervalMillis(fix(300_001), 300_001))
        }
        val policy = MotionPolicy()
        policy.intervalMillis(fix(0), 100)
        assertEquals(120_000L, policy.intervalMillis(fix(300_000), 99))
    }

    @Test fun displacementWithinCombinedAccuracyDoesNotForceMotion() {
        val policy = MotionPolicy()
        policy.intervalMillis(fix(0).copy(accuracyM = 100.0), 0)
        assertEquals(300_000L, policy.intervalMillis(fix(300_000, 30.001).copy(accuracyM = 100.0), 300_000))
    }

    @Test fun correctedClockCanBuildANewStationaryObservation() {
        val policy = MotionPolicy()
        policy.intervalMillis(fix(18_000_000), 0)
        // The rollback itself cannot count as stationary evidence.
        assertEquals(120_000L, policy.intervalMillis(fix(0), 300_000))
        assertEquals(120_000L, policy.intervalMillis(fix(30_000), 330_000))
        assertEquals(120_000L, policy.intervalMillis(fix(329_999), 509_999))
        assertEquals(300_000L, policy.intervalMillis(fix(330_000), 630_000))
    }
}
