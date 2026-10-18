package com.jk.family.core

import org.junit.Assert.assertEquals
import org.junit.Test

class MotionPolicyTest {
    private fun fix(time: Long, latitude: Double = 30.0, speed: Float = 0f) =
        Fix(latitude, 120.0, 5.0, 1_700_000_000_000L + time, null, speed)

    @Test fun becomesStationaryAfterThreeMinutesAndSpeedImmediatelyRestoresMoving() {
        val policy = MotionPolicy()
        assertEquals(30_000L, policy.intervalMillis(fix(0), 0))
        assertEquals(30_000L, policy.intervalMillis(fix(179_999), 179_999))
        assertEquals(180_000L, policy.intervalMillis(fix(180_000), 180_000))
        assertEquals(30_000L, policy.intervalMillis(fix(180_001, speed = 1f), 180_001))
        assertEquals(30_000L, policy.intervalMillis(fix(180_002), 180_002))
    }

    @Test fun comparesAgainstAnchorSoSmallIncrementalMovementDoesNotLookStationary() {
        val policy = MotionPolicy()
        policy.intervalMillis(fix(0), 0)
        assertEquals(30_000L, policy.intervalMillis(fix(90_000, 30.0003), 90_000))
        assertEquals(30_000L, policy.intervalMillis(fix(180_000, 30.0006), 180_000))
        assertEquals(180_000L, policy.intervalMillis(fix(360_000, 30.0006), 360_000))
    }

    @Test fun badCoordinatesOrBackwardsTimesRestartStationaryObservation() {
        for (bad in listOf(fix(1).copy(latitude = Double.NaN), fix(-1), fix(0))) {
            val policy = MotionPolicy()
            policy.intervalMillis(fix(0), 0)
            assertEquals(30_000L, policy.intervalMillis(bad, 180_000))
            assertEquals(30_000L, policy.intervalMillis(fix(180_001), 180_001))
        }
        val policy = MotionPolicy()
        policy.intervalMillis(fix(0), 100)
        assertEquals(30_000L, policy.intervalMillis(fix(180_000), 99))
    }

    @Test fun displacementWithinCombinedAccuracyDoesNotForceMotion() {
        val policy = MotionPolicy()
        policy.intervalMillis(fix(0).copy(accuracyM = 100.0), 0)
        assertEquals(180_000L, policy.intervalMillis(fix(180_000, 30.001).copy(accuracyM = 100.0), 180_000))
    }

    @Test fun correctedClockCanBuildANewStationaryObservation() {
        val policy = MotionPolicy()
        policy.intervalMillis(fix(18_000_000), 0)
        // The rollback itself cannot count as stationary evidence.
        assertEquals(30_000L, policy.intervalMillis(fix(0), 180_000))
        assertEquals(30_000L, policy.intervalMillis(fix(30_000), 210_000))
        assertEquals(30_000L, policy.intervalMillis(fix(209_999), 389_999))
        assertEquals(180_000L, policy.intervalMillis(fix(210_000), 390_000))
    }
}
