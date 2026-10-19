package com.jk.family

import android.Manifest
import android.app.Notification
import com.amap.api.location.AMapLocationClientOption
import com.jk.family.core.MotionPolicy
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [25, 33])
class AmapSourceTest {
    private val application = RuntimeEnvironment.getApplication()

    @Test fun precisePermissionSwitchesBetweenMovingAndStillModes() {
        shadowOf(application).grantPermissions(Manifest.permission.ACCESS_FINE_LOCATION)
        val source = AmapSource(application, Notification())
        assertOptions(source, MotionPolicy.MOVING_INTERVAL, AMapLocationClientOption.AMapLocationMode.Hight_Accuracy)
        source.interval(MotionPolicy.STILL_INTERVAL)
        assertOptions(source, MotionPolicy.STILL_INTERVAL, AMapLocationClientOption.AMapLocationMode.Battery_Saving)
        source.interval(MotionPolicy.MOVING_INTERVAL)
        assertOptions(source, MotionPolicy.MOVING_INTERVAL, AMapLocationClientOption.AMapLocationMode.Hight_Accuracy)
    }

    @Test fun approximatePermissionKeepsCompatibleModeWhenStill() {
        shadowOf(application).grantPermissions(Manifest.permission.ACCESS_COARSE_LOCATION)
        val source = AmapSource(application, Notification())
        source.interval(MotionPolicy.STILL_INTERVAL)
        assertOptions(source, MotionPolicy.STILL_INTERVAL, AMapLocationClientOption.AMapLocationMode.Hight_Accuracy)
    }

    private fun assertOptions(source: AmapSource, interval: Long, mode: AMapLocationClientOption.AMapLocationMode) {
        val options = source.options()
        assertEquals(mode, options.locationMode)
        assertEquals(interval, options.interval)
        assertFalse(options.isOnceLocation)
        assertFalse(options.isNeedAddress)
        assertTrue(options.isOffset)
        assertFalse(options.isLocationCacheEnable)
        assertFalse(options.isMockEnable)
    }
}
