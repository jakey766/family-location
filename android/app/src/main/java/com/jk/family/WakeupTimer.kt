package com.jk.family

import android.app.AlarmManager
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import android.util.Log

/** Handler is a fast path while awake; the alarm can wake a sleeping CPU. */
internal class WakeupTimer(
    private val context: Context,
    private val action: String,
    private val elapsed: () -> Long = SystemClock::elapsedRealtime,
    private val callback: () -> Unit,
) {
    private val handler = Handler(Looper.getMainLooper())
    private val task = Runnable { fire() }
    private var deadline: Long? = null

    fun schedule(atElapsed: Long) {
        if (deadline == atElapsed) return
        cancel()
        deadline = atElapsed
        val manager = context.getSystemService(AlarmManager::class.java)
        val pending = pending(context, action)
        if (SharingSchedule.canScheduleExactly(context)) {
            try {
                manager.setExactAndAllowWhileIdle(AlarmManager.ELAPSED_REALTIME_WAKEUP, atElapsed, pending)
            } catch (_: SecurityException) {
                manager.setAndAllowWhileIdle(AlarmManager.ELAPSED_REALTIME_WAKEUP, atElapsed, pending)
            }
        } else manager.setAndAllowWhileIdle(AlarmManager.ELAPSED_REALTIME_WAKEUP, atElapsed, pending)
        handler.postDelayed(task, (atElapsed - elapsed()).coerceAtLeast(1))
    }

    fun fire() {
        val due = deadline ?: return
        if (elapsed() < due) return
        cancel()
        callback()
    }

    fun cancel() {
        deadline = null
        handler.removeCallbacks(task)
        context.getSystemService(AlarmManager::class.java).cancel(pending(context, action))
    }

    companion object {
        const val SAMPLE = "com.jk.family.SAMPLE"
        const val RETRY = "com.jk.family.RETRY"

        private fun pending(context: Context, action: String) = PendingIntent.getBroadcast(
            context, 23, Intent(context, WakeWorkReceiver::class.java).setAction(action),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )

        fun cancelAll(context: Context) {
            val manager = context.getSystemService(AlarmManager::class.java)
            for (action in listOf(SAMPLE, RETRY)) manager.cancel(pending(context, action))
        }
    }
}

class WakeWorkReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action !in listOf(WakeupTimer.SAMPLE, WakeupTimer.RETRY)) return
        val store = CredentialStore(context)
        if (!store.sharingRequested) { WakeupTimer.cancelAll(context); return }
        if (!store.consent || !AmapSource.hasLocationPermission(context)) {
            SharingService.requestStop(context, "隐私或定位授权已撤回")
            return
        }
        Log.i("FamilyLocation", "wake action=${intent.action}")
        val current = AppRuntime.controller
        if (current?.active == true) current.wake()
        else SharingService.requestResume(context, fromBackground = true)
    }
}
