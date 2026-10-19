package com.jk.family

import android.annotation.SuppressLint
import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Intent
import android.content.Context
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.os.PowerManager
import android.os.SystemClock
import com.jk.family.core.DeviceApi
import com.jk.family.core.Upload
import java.util.concurrent.Executors

internal object AppRuntime {
    // One process-wide serial executor also orders old service cleanup before new API work.
    val io = Executors.newSingleThreadExecutor()
    var controller: SharingController? = null
    var active = false
    var message = "共享已关闭。准备好后请主动开启。"
    var pairing = false
    var credentialsRevision = 0
    var serviceGeneration = 0
}

open class SharingService : Service() {
    private val handler = Handler(Looper.getMainLooper())
    private var controller: SharingController? = null
    private var destroying = false
    private var wakeLock: PowerManager.WakeLock? = null
    private val tick = object : Runnable {
        override fun run() {
            val current = controller ?: return
            if (!AmapSource.hasLocationPermission(this@SharingService)) current.stop("定位权限已撤回")
            else {
                // Bounded lock, renewed only while the user-requested service is alive.
                wakeLock?.acquire(10 * 60_000L)
                current.retry()
            }
            if (current.active) handler.postDelayed(this, 15_000)
        }
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val store = CredentialStore(this)
        if (intent?.action == STOP) {
            store.sharingRequested = false
            controller?.stop()
            stopSelf()
            return START_NOT_STICKY
        }
        val restoring = intent == null || intent.action == RESUME
        if (intent?.action != START && !(restoring && store.sharingRequested)) {
            stopSelf(); return START_NOT_STICKY
        }
        if (controller?.active == true) return START_STICKY
        val runtimeGeneration = ++AppRuntime.serviceGeneration
        val credentialsRevision = AppRuntime.credentialsRevision
        try {
            val credentials = readCredentials()
            if (credentials == null || !store.consent || !AmapSource.hasLocationPermission(this) || !amapConfigured()) {
                store.sharingRequested = false
                error("Sharing prerequisites are missing")
            }
            createChannel()
            if (Build.VERSION.SDK_INT >= 29) startForeground(1, notification("正在开启共享"), ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION)
            else startForeground(1, notification("正在开启共享"))
            store.sharingRequested = true
            wakeLock = getSystemService(PowerManager::class.java)
                .newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "family:sharing").apply {
                    setReferenceCounted(false)
                    acquire(10 * 60_000L)
                }
            val api = DeviceApi(credentials.origin, credentials.token)
            val remote = object : SharingRemote {
                override fun start() = api.start()
                override fun stop(session: String) = api.stop(session)
                override fun upload(point: Upload) = api.upload(point)
                override fun cancel() = api.cancel()
            }
            lateinit var created: SharingController
            created = createController(remote, AmapSource(applicationContext, notification("正在后台共享位置")),
                { message ->
                    // Stop completion may arrive after onDestroy. A new run or pairing invalidates it.
                    if (AppRuntime.serviceGeneration == runtimeGeneration && AppRuntime.credentialsRevision == credentialsRevision) {
                        AppRuntime.message = message
                        if (AppRuntime.active) {
                            getSystemService(NotificationManager::class.java).notify(1, notification(message))
                        }
                    }
                }, { active ->
                    if (AppRuntime.controller === created) AppRuntime.active = active
                    if (!active && controller === created && !destroying) {
                        store.sharingRequested = false
                        handler.removeCallbacks(tick)
                        releaseWakeLock()
                        stopForeground(STOP_FOREGROUND_REMOVE)
                        stopSelf()
                    }
                })
            controller = created
            AppRuntime.controller = created
            created.start()
            handler.postDelayed(tick, 15_000)
        } catch (_: Exception) {
            // A transient platform/Keystore failure is not a user stop. Stop this instance
            // without an immediate sticky retry loop; the next recovery entry can try again.
            destroying = true
            controller?.release()
            AppRuntime.active = false
            AppRuntime.message = if (store.sharingRequested)
                "共享仍已开启，但服务暂未恢复；请重新打开 App 重试，并检查系统后台限制。"
            else "无法启动共享，请检查配对、隐私授权、定位权限和高德 Key。"
            stopForeground(STOP_FOREGROUND_REMOVE)
            releaseWakeLock()
            stopSelf()
            return START_NOT_STICKY
        }
        return if (store.sharingRequested) START_STICKY else START_NOT_STICKY
    }

    override fun onDestroy() {
        destroying = true
        handler.removeCallbacks(tick)
        controller?.release()
        releaseWakeLock()
        if (AppRuntime.controller === controller) {
            AppRuntime.controller = null
            AppRuntime.active = false
            if (CredentialStore(this).sharingRequested) AppRuntime.message = "共享仍已开启，服务暂时中断；等待系统恢复，打开 App 也会自动恢复。"
        }
        super.onDestroy()
    }

    private fun releaseWakeLock() {
        wakeLock?.let { if (it.isHeld) it.release() }
        wakeLock = null
    }

    internal open fun readCredentials() = CredentialStore(this).read()

    internal open fun createController(remote: SharingRemote, source: PositionSource,
        status: (String) -> Unit, activeChanged: (Boolean) -> Unit) =
        SharingController(remote, source, AppRuntime.io, { handler.post(it) },
            SystemClock::elapsedRealtime, status, activeChanged)

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

    internal open fun amapConfigured(): Boolean = !packageManager.getApplicationInfo(packageName, PackageManager.GET_META_DATA)
        .metaData?.getString("com.amap.api.v2.apikey").isNullOrBlank()

    companion object {
        const val START = "com.jk.family.START"
        const val RESUME = "com.jk.family.RESUME"
        const val STOP = "com.jk.family.STOP"
        private const val CHANNEL = "sharing"

        fun requestResume(context: Context, fromBackground: Boolean = false) {
            val store = CredentialStore(context)
            if (!store.sharingRequested || AppRuntime.active) return
            if (!store.consent || !AmapSource.hasLocationPermission(context)) {
                store.sharingRequested = false
                AppRuntime.message = "共享已停止：隐私或定位授权已撤回，请授权后重新开启。"
                return
            }
            if (fromBackground && Build.VERSION.SDK_INT >= 29 &&
                context.checkSelfPermission(Manifest.permission.ACCESS_BACKGROUND_LOCATION) != PackageManager.PERMISSION_GRANTED) {
                AppRuntime.message = "共享仍已开启，等待打开 App 恢复；如需重启后自动恢复，请允许始终访问位置。"
                return
            }
            val intent = Intent(context, SharingService::class.java).setAction(RESUME)
            AppRuntime.message = "共享已开启，正在恢复服务…"
            try {
                if (Build.VERSION.SDK_INT >= 26) context.startForegroundService(intent) else context.startService(intent)
            } catch (_: Exception) {
                // A system launch restriction must not erase the user's persistent choice.
                AppRuntime.message = "共享仍已开启，但系统暂未允许恢复；请保持 App 在前台并检查后台运行权限。"
            }
        }

        // Context.stopService matches the explicit component, not Intent object identity.
        @SuppressLint("ImplicitSamInstance")
        fun requestStop(context: Context, reason: String = "已停止本地共享") {
            CredentialStore(context).sharingRequested = false
            val current = AppRuntime.controller
            if (current != null) current.stop(reason)
            else {
                context.stopService(Intent(context, SharingService::class.java))
                AppRuntime.message = "$reason。若网页仍有最后位置，请在网页撤销设备。"
            }
        }
    }
}
