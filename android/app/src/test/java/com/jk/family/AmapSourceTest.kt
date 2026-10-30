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

    @Test fun precisePermissionDefaultsToLowPowerWithoutRemoteOptIn() {
        shadowOf(application).grantPermissions(Manifest.permission.ACCESS_FINE_LOCATION)
        val source = AmapSource(application, Notification())
        assertOptions(source, MotionPolicy.MOVING_INTERVAL, AMapLocationClientOption.AMapLocationMode.Battery_Saving)
        source.interval(MotionPolicy.STILL_INTERVAL)
        assertOptions(source, MotionPolicy.STILL_INTERVAL, AMapLocationClientOption.AMapLocationMode.Battery_Saving)
        source.interval(MotionPolicy.MOVING_INTERVAL)
        assertOptions(source, MotionPolicy.MOVING_INTERVAL, AMapLocationClientOption.AMapLocationMode.Battery_Saving)
    }

    @Test fun explicitAccuracyIsIndependentOfInterval() {
        shadowOf(application).grantPermissions(Manifest.permission.ACCESS_FINE_LOCATION)
        val source = AmapSource(application, Notification())
        source.interval(60000L)
        source.accuracy(true)
        assertOptions(source, 60000L, AMapLocationClientOption.AMapLocationMode.Hight_Accuracy)
        source.accuracy(false)
        assertOptions(source, 60000L, AMapLocationClientOption.AMapLocationMode.Battery_Saving)
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
        assertTrue("each alarm must trigger a fresh bounded sample instead of SDK delayed polling", options.isOnceLocation)
        assertFalse(options.isNeedAddress)
        assertTrue(options.isOffset)
        assertFalse(options.isLocationCacheEnable)
        assertFalse(options.isMockEnable)
    }
}
