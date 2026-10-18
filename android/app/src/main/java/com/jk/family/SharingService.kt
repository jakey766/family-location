package com.jk.family

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.os.SystemClock
import com.jk.family.core.DeviceApi
import com.jk.family.core.Upload
import java.util.concurrent.Executors

internal object AppRuntime {
    // One process-wide serial executor also orders old service cleanup before new API work.
    val io = Executors.newSingleThreadExecutor()
    var controller: SharingController? = null
    var active = false
    var message = "共享已关闭。每次重新打开后，请主动开启。"
    var pairing = false
    var credentialsRevision = 0
}

class SharingService : Service() {
    private val handler = Handler(Looper.getMainLooper())
    private var controller: SharingController? = null
    private val tick = object : Runnable {
        override fun run() {
            val current = controller ?: return
            if (!AmapSource.hasLocationPermission(this@SharingService)) current.stop("定位权限已撤回")
            else current.retry()
            if (current.active) handler.postDelayed(this, 15_000)
        }
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == STOP) {
            controller?.stop()
            stopSelf()
            return START_NOT_STICKY
        }
        if (intent?.action != START) { stopSelf(); return START_NOT_STICKY }
        if (controller?.active == true) return START_NOT_STICKY
        try {
            val credentials = CredentialStore(this).read()
            check(credentials != null && CredentialStore(this).consent && AmapSource.hasLocationPermission(this))
            check(amapConfigured())
            createChannel()
            if (Build.VERSION.SDK_INT >= 29) startForeground(1, notification("正在开启共享"), ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION)
            else startForeground(1, notification("正在开启共享"))
            val api = DeviceApi(credentials.origin, credentials.token)
            val remote = object : SharingRemote {
                override fun start() = api.start()
                override fun stop(session: String) = api.stop(session)
                override fun upload(point: Upload) = api.upload(point)
                override fun cancel() = api.cancel()
            }
            lateinit var created: SharingController
            created = SharingController(remote, AmapSource(applicationContext), AppRuntime.io,
                { command -> handler.post(command) }, SystemClock::elapsedRealtime,
                { message ->
                    if (AppRuntime.controller === created) {
                        AppRuntime.message = message
                        if (AppRuntime.active) {
                            getSystemService(NotificationManager::class.java).notify(1, notification(message))
                        }
                    }
                }, { active ->
                    if (AppRuntime.controller === created) AppRuntime.active = active
                    if (!active && controller === created) {
                        handler.removeCallbacks(tick)
                        stopForeground(STOP_FOREGROUND_REMOVE)
                        stopSelf()
                    }
                })
            controller = created
            AppRuntime.controller = created
            created.start()
            handler.postDelayed(tick, 15_000)
        } catch (_: Exception) {
            controller?.stop("无法启动定位，请检查权限、系统定位及配置")
            AppRuntime.active = false
            AppRuntime.message = "无法启动共享，请检查配对、隐私授权、定位权限和高德 Key。"
            stopForeground(STOP_FOREGROUND_REMOVE)
            stopSelf()
        }
        return START_NOT_STICKY
    }

    override fun onDestroy() {
        handler.removeCallbacks(tick)
        controller?.stop("共享服务已停止")
        // Keep IO completion callbacks alive to clean late start responses; they cannot restart location.
        super.onDestroy()
    }

    private fun createChannel() {
        if (Build.VERSION.SDK_INT >= 26) getSystemService(NotificationManager::class.java).createNotificationChannel(
            NotificationChannel(CHANNEL, "位置共享", NotificationManager.IMPORTANCE_LOW).apply {
                description = "共享开启时持续显示，可随时停止"
            })
    }

    private fun notification(message: String): Notification {
        val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
        val stop = PendingIntent.getService(this, 1, Intent(this, SharingService::class.java).setAction(STOP), PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
        val builder = if (Build.VERSION.SDK_INT >= 26) Notification.Builder(this, CHANNEL) else Notification.Builder(this)
        return builder.setSmallIcon(R.drawable.ic_location).setContentTitle("家庭位置共享")
            .setContentText(message).setContentIntent(open).setOngoing(true).setOnlyAlertOnce(true)
            .setVisibility(Notification.VISIBILITY_PRIVATE)
            .addAction(Notification.Action.Builder(null, "停止共享", stop).build()).build()
    }

    private fun amapConfigured(): Boolean = !packageManager.getApplicationInfo(packageName, PackageManager.GET_META_DATA)
        .metaData?.getString("com.amap.api.v2.apikey").isNullOrBlank()

    companion object {
        const val START = "com.jk.family.START"
        const val STOP = "com.jk.family.STOP"
        private const val CHANNEL = "sharing"
    }
}
