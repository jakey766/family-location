package com.jk.family

import android.Manifest
import android.app.Notification
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.PackageManager
import android.os.BatteryManager
import android.os.Handler
import android.os.Looper
import com.amap.api.location.AMapLocationClient
import com.amap.api.location.AMapLocationClientOption
import com.jk.family.core.Fix
import com.jk.family.core.MotionPolicy
import com.jk.family.core.QuietHours

internal class AmapSource(private val context: Context, private val notification: Notification) : PositionSource {
    private var client: AMapLocationClient? = null
    private val handler = Handler(Looper.getMainLooper())
    private var generation = 0
    private var currentInterval = MotionPolicy.MOVING_INTERVAL

    override fun start(onFix: (Fix) -> Unit, onError: (String) -> Unit) {
        check(CredentialStore(context).consent && hasLocationPermission(context))
        // No SDK object is constructed before explicit privacy consent.
        AMapLocationClient.updatePrivacyShow(context, true, true)
        AMapLocationClient.updatePrivacyAgree(context, true)
        val epoch = ++generation
        val created = AMapLocationClient(context)
        client = created
        created.setLocationOption(options())
        created.setLocationListener { location ->
            if (QuietHours.isQuiet()) {
                handler.post {
                    if (generation != epoch || client == null) return@post
                    onError("夜间暂停定位与上报，08:00 自动恢复")
                }
                return@setLocationListener
            }
            // Copy data while in the SDK callback; do not keep a mutable SDK location.
            val fix = if (location != null && location.errorCode == 0 && location.coordType == "GCJ02") {
                Fix(location.latitude, location.longitude, location.accuracy.toDouble(), location.time,
                    battery(), if (location.hasSpeed()) location.speed else 0f)
            } else null
            val code = location?.errorCode ?: -1
            handler.post {
                if (generation != epoch || client == null) return@post
                if (fix != null) onFix(fix)
                else onError("定位暂不可用（代码 $code），等待下一次定位；请检查系统定位和高德配置")
            }
        }
        created.enableBackgroundLocation(2, notification)
        created.startLocation()
    }

    override fun interval(millis: Long) {
        if (currentInterval == millis) return
        currentInterval = millis
        client?.setLocationOption(options())
    }

    internal fun options() = AMapLocationClientOption().apply {
        locationMode = if (currentInterval == MotionPolicy.STILL_INTERVAL &&
            context.checkSelfPermission(Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED) {
            AMapLocationClientOption.AMapLocationMode.Battery_Saving
        } else {
            // AMap's battery-saving mode does not support approximate-only permission.
            AMapLocationClientOption.AMapLocationMode.Hight_Accuracy
        }
        interval = currentInterval
        isOnceLocation = true
        httpTimeOut = 10_000L
        isNeedAddress = false
        isOffset = true
        isLocationCacheEnable = false
        isMockEnable = false
    }

    override fun stop() {
        generation++
        val old = client
        client = null
        handler.removeCallbacksAndMessages(null)
        try { old?.stopLocation() } finally {
            try { old?.disableBackgroundLocation(true) } finally { old?.onDestroy() }
        }
    }

    private fun battery(): Int? {
        val battery = context.registerReceiver(null, IntentFilter(Intent.ACTION_BATTERY_CHANGED)) ?: return null
        val level = battery.getIntExtra(BatteryManager.EXTRA_LEVEL, -1)
        val scale = battery.getIntExtra(BatteryManager.EXTRA_SCALE, -1)
        return if (level >= 0 && scale > 0) (level * 100 / scale).coerceIn(0, 100) else null
    }

    companion object {
        fun hasLocationPermission(context: Context) = context.checkSelfPermission(Manifest.permission.ACCESS_COARSE_LOCATION) == PackageManager.PERMISSION_GRANTED ||
            context.checkSelfPermission(Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED
    }
}
