package com.jk.family

import android.app.AlarmManager
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.os.Build
import com.jk.family.core.QuietHours
import com.jk.family.core.DeviceConfig
import java.time.ZoneId

/** Local day/night boundaries are independent of monotonic sample/retry alarms. */
internal object SharingSchedule {
    const val BOUNDARY = "com.jk.family.QUIET_HOURS_BOUNDARY"

    fun canScheduleExactly(context: Context): Boolean = Build.VERSION.SDK_INT < 31 ||
        context.getSystemService(AlarmManager::class.java).canScheduleExactAlarms()

    fun schedule(context: Context, nowMillis: Long = System.currentTimeMillis(), zone: ZoneId = ZoneId.systemDefault(),
        config: DeviceConfig = AppRuntime.controller?.takeIf { it.active }?.config ?: CredentialStore(context).readConfig(),
    ) {
        if (!CredentialStore(context).sharingRequested) { cancel(context); return }
        val alarm = context.getSystemService(AlarmManager::class.java)
        val next = QuietHours.nextBoundary(nowMillis, zone, config)
        val pending = pending(context)
        if (canScheduleExactly(context)) {
            try {
                alarm.setExactAndAllowWhileIdle(AlarmManager.RTC_WAKEUP, next, pending)
                return
            } catch (_: SecurityException) { /* Permission may be revoked between check and scheduling. */ }
        }
        alarm.setAndAllowWhileIdle(AlarmManager.RTC_WAKEUP, next, pending)
    }

    fun cancel(context: Context) {
        context.getSystemService(AlarmManager::class.java).cancel(pending(context))
        WakeupTimer.cancelAll(context)
    }

    private fun pending(context: Context) = PendingIntent.getBroadcast(context, 22,
        Intent(context, SharingScheduleReceiver::class.java).setAction(BOUNDARY),
        PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
}

class SharingScheduleReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action !in listOf(SharingSchedule.BOUNDARY, Intent.ACTION_TIME_CHANGED,
                Intent.ACTION_TIMEZONE_CHANGED, AlarmManager.ACTION_SCHEDULE_EXACT_ALARM_PERMISSION_STATE_CHANGED)) return
        SharingSchedule.schedule(context)
        if (!CredentialStore(context).sharingRequested) return
        val current = AppRuntime.controller
        if (current?.active == true) current.updateSchedule()
        else SharingService.requestResume(context, fromBackground = true)
    }
}
