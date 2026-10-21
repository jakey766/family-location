package com.jk.family

import android.content.Context
import android.os.Handler
import android.os.Looper
import android.os.PowerManager
import java.util.concurrent.Executor
import java.util.concurrent.RejectedExecutionException

/** Keep result delivery awake until main has armed retry or started the next acquisition. */
internal class WakefulMainExecutor(context: Context) : Executor {
    private val power = context.applicationContext.getSystemService(PowerManager::class.java)
    private val handler = Handler(Looper.getMainLooper())

    override fun execute(command: Runnable) {
        val lock = power.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "family:result").apply {
            setReferenceCounted(false)
            acquire(15_000L)
        }
        try {
            if (!handler.post {
                try {
                    // Main may have been blocked beyond the bounded handoff window.
                    if (!lock.isHeld) lock.acquire(15_000L)
                    command.run()
                } finally {
                    if (lock.isHeld) lock.release()
                }
            }) throw RejectedExecutionException("Main looper is exiting")
        } catch (error: Exception) {
            if (lock.isHeld) lock.release()
            throw error
        }
    }
}
