package com.jk.family

import android.app.AlarmManager
import android.os.Looper
import com.jk.family.core.Fix
import com.jk.family.core.MotionPolicy
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.robolectric.annotation.LooperMode
import org.robolectric.shadows.ShadowPowerManager
import java.time.Duration

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [25, 33])
@LooperMode(LooperMode.Mode.PAUSED)
class ScheduledPositionSourceTest {
    private class FakeSource : PositionSource {
        var starts = 0
        var stops = 0
        var selectedInterval = 0L
        lateinit var fix: (Fix) -> Unit
        lateinit var error: (String) -> Unit
        override fun start(onFix: (Fix) -> Unit, onError: (String) -> Unit) {
            starts++; fix = onFix; error = onError
        }
        override fun stop() { stops++ }
        override fun interval(millis: Long) { selectedInterval = millis }
    }
    private val app = RuntimeEnvironment.getApplication()
    private val raw = FakeSource()
    private var now = 1000L
    private var quiet = false
    private val fixes = mutableListOf<Fix>()
    private val errors = mutableListOf<String>()
    private val source = ScheduledPositionSource(app, raw, { quiet }, { now })
    private val point = Fix(30.0, 110.0, 15.0, 1_000L, 80)
    private val alarms get() = shadowOf(app.getSystemService(AlarmManager::class.java)).scheduledAlarms

    private fun start() {
        CredentialStore(app).sharingRequested = true
        source.start({ fixes.add(it) }, { errors.add(it) })
    }

    @Test fun elapsedWakeTriggersNextSampleWithoutAdvancingHandler() {
        start()
        val lock = ShadowPowerManager.getLatestWakeLock()
        assertTrue(lock.isHeld)
        assertEquals(AlarmManager.ELAPSED_REALTIME_WAKEUP, alarms.single().type)
        raw.fix(point)
        assertFalse("waiting between samples must not hold a lock", lock.isHeld)
        assertEquals(1, raw.stops)
        now += MotionPolicy.MOVING_INTERVAL
        source.wake() // elapsed advances while the Handler looper has not run: deep sleep regression.
        assertEquals(2, raw.starts)
        assertTrue(ShadowPowerManager.getLatestWakeLock().isHeld)
        source.wake()
        assertEquals("duplicate broadcasts cannot start concurrent samples", 2, raw.starts)
        source.stop()
        assertTrue(alarms.isEmpty())
        assertFalse(ShadowPowerManager.getLatestWakeLock().isHeld)
    }

    @Test fun modeSelectedByCallbackSurvivesNextOneShotStart() {
        CredentialStore(app).sharingRequested = true
        source.start({ source.interval(MotionPolicy.STILL_INTERVAL) }, { errors.add(it) })
        raw.fix(point)
        assertEquals(now + MotionPolicy.STILL_INTERVAL, alarms.single().triggerAtTime)
        now += MotionPolicy.STILL_INTERVAL
        source.wake()
        assertEquals(MotionPolicy.STILL_INTERVAL, raw.selectedInterval)
        source.stop()
    }

    @Test fun timeoutReleasesLockAndLateResultCannotBeUploaded() {
        start()
        val late = raw.fix
        now += 30_000
        shadowOf(Looper.getMainLooper()).idleFor(Duration.ofSeconds(30))
        assertFalse(ShadowPowerManager.getLatestWakeLock().isHeld)
        assertEquals(1, raw.stops)
        late(point)
        assertTrue(fixes.isEmpty())
        assertEquals(1, errors.size)
        now = 61_000
        source.wake()
        assertEquals(2, raw.starts)
        late(point)
        assertTrue(fixes.isEmpty())
        source.stop()
    }

    @Test fun overdueResultIsRejectedEvenBeforeTimeoutHandlerRuns() {
        start()
        now += 31_000
        raw.fix(point)
        assertTrue(fixes.isEmpty())
        assertFalse(ShadowPowerManager.getLatestWakeLock().isHeld)
        source.stop()
    }

    @Test fun wakeAfterDelayedTimeoutStartsOneFreshSampleImmediately() {
        start()
        val late = raw.fix
        // Both the timeout Handler and SDK callback were suspended; the next alarm wakes us.
        now += 600_000
        source.wake()
        assertEquals("an overdue acquisition must not consume the new wake", 2, raw.starts)
        late(point)
        assertTrue(fixes.isEmpty())
        source.wake()
        assertEquals("do not replay ten missed intervals", 2, raw.starts)
        source.stop()
    }

    @Test fun quietHoursAndExplicitStopCancelWakeAndRejectLateResults() {
        start()
        val late = raw.fix
        quiet = true
        now += 60_000
        source.wake()
        late(point)
        assertTrue(fixes.isEmpty())
        assertTrue(alarms.isEmpty())
        assertFalse(ShadowPowerManager.getLatestWakeLock().isHeld)
        quiet = false
        source.wake()
        assertEquals(1, raw.starts)
        source.start({ fixes.add(it) }, { errors.add(it) })
        val secondLate = raw.fix
        source.stop()
        secondLate(point)
        assertTrue(fixes.isEmpty())
        assertTrue(alarms.isEmpty())
    }

    @Test fun errorReleasesWindowAndNextSampleRemainsScheduled() {
        start()
        raw.error("unavailable")
        assertEquals(listOf("unavailable"), errors)
        assertFalse(ShadowPowerManager.getLatestWakeLock().isHeld)
        assertEquals(1, alarms.size)
        source.stop()
    }
}
