package com.jk.family

import android.os.PowerManager
import android.os.SystemClock
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.robolectric.annotation.LooperMode
import org.robolectric.shadows.ShadowPowerManager
import java.util.concurrent.RejectedExecutionException

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [25, 33])
@LooperMode(LooperMode.Mode.PAUSED)
class NetworkWorkExecutorTest {
    @Test fun queuedWorkHoldsLockOnlyDuringExecution() {
        val queue = ArrayDeque<Runnable>()
        val executor = NetworkWorkExecutor(RuntimeEnvironment.getApplication()) { queue.add(it) }
        var lock: PowerManager.WakeLock? = null
        executor.execute {
            lock = ShadowPowerManager.getLatestWakeLock()
            assertTrue(lock!!.isHeld)
            assertEquals("family:network", shadowOf(lock!!).tag)
        }
        assertNull(ShadowPowerManager.getLatestWakeLock())
        queue.removeFirst().run()
        assertNotNull(lock)
        assertFalse(lock!!.isHeld)
    }

    @Test fun exceptionalWorkReleasesItsLockAndPropagatesFailure() {
        val executor = NetworkWorkExecutor(RuntimeEnvironment.getApplication()) { it.run() }
        val failure = IllegalStateException("network failure")
        try {
            executor.execute { throw failure }
            fail("the task failure must propagate")
        } catch (actual: IllegalStateException) {
            assertSame(failure, actual)
        }
        assertFalse(ShadowPowerManager.getLatestWakeLock().isHeld)
    }

    @Test fun everyRunningTaskOwnsAnIndependentLock() {
        val executor = NetworkWorkExecutor(RuntimeEnvironment.getApplication()) { it.run() }
        executor.execute {
            val outer = ShadowPowerManager.getLatestWakeLock()
            executor.execute {
                val inner = ShadowPowerManager.getLatestWakeLock()
                assertNotSame(outer, inner)
                assertTrue(outer.isHeld)
                assertTrue(inner.isHeld)
            }
            assertFalse(ShadowPowerManager.getLatestWakeLock().isHeld)
            assertTrue(outer.isHeld)
        }
    }

    @Test fun lockExpiresAfterFifteenSecondsEvenIfWorkHasNotReturned() {
        val executor = NetworkWorkExecutor(RuntimeEnvironment.getApplication()) { it.run() }
        executor.execute {
            val lock = ShadowPowerManager.getLatestWakeLock()
            SystemClock.sleep(14_999)
            assertTrue(lock.isHeld)
            // Robolectric expires timed locks after their deadline, at millisecond precision.
            SystemClock.sleep(2)
            assertFalse(lock.isHeld)
        }
    }

    @Test fun rejectedWorkNeverAcquiresLock() {
        val executor = NetworkWorkExecutor(RuntimeEnvironment.getApplication()) { throw RejectedExecutionException() }
        try {
            executor.execute { fail("rejected work must not run") }
            fail("executor rejection must propagate")
        } catch (_: RejectedExecutionException) {
            assertNull(ShadowPowerManager.getLatestWakeLock())
        }
    }
}
