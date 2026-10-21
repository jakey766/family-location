package com.jk.family

import android.os.Looper
import com.jk.family.core.Fix
import com.jk.family.core.Upload
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.robolectric.annotation.LooperMode
import org.robolectric.shadows.ShadowPowerManager
import java.util.concurrent.TimeUnit

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [25, 33])
@LooperMode(LooperMode.Mode.PAUSED)
class ResultHandoffTest {
    class DayService : SharingService() {
        internal override fun isQuietNow() = false
    }

    @Test fun realServiceFactoryBridgesNetworkResultUntilMainStartsSampling() {
        val lifecycle = Robolectric.buildService(DayService::class.java).create()
        var sampling = false
        val controller = lifecycle.get().createController(object : SharingRemote {
            override fun start() = "test-session"
            override fun upload(point: Upload) = true
            override fun stop(session: String) {}
            override fun cancel() {}
        }, object : PositionSource {
            override fun start(onFix: (Fix) -> Unit, onError: (String) -> Unit) {
                assertTrue("result processing must stay awake until sampling takes over",
                    ShadowPowerManager.getLatestWakeLock().isHeld)
                sampling = true
            }
            override fun interval(millis: Long) {}
            override fun stop() {}
        }, {}, {}, {})
        try {
            controller.start()
            AppRuntime.io.submit {}.get(5, TimeUnit.SECONDS)
            assertFalse(sampling)
            assertTrue("worker returned, but its result has not run on main yet",
                ShadowPowerManager.getLatestWakeLock().isHeld)
            shadowOf(Looper.getMainLooper()).idle()
            assertTrue(sampling)
            assertFalse(ShadowPowerManager.getLatestWakeLock().isHeld)
        } finally {
            controller.release()
            lifecycle.destroy()
        }
    }
}
