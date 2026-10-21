package com.jk.family

import android.content.Context
import android.os.PowerManager
import android.util.Log
import java.util.concurrent.Executor

/** Bounded handoff avoids sleeping between the location callback and queued network work. */
internal class NetworkWorkExecutor(context: Context, private val delegate: Executor) : Executor {
    private val power = context.applicationContext.getSystemService(PowerManager::class.java)

    override fun execute(command: Runnable) {
        val handoff = acquire("family:network-handoff")
        try {
            delegate.execute {
                try {
                    val lock = acquire("family:network")
                    if (handoff.isHeld) handoff.release()
                    try {
                        Log.i("FamilyLocation", "network_work_start")
                        command.run()
                    } finally {
                        Log.i("FamilyLocation", "network_work_end")
                        if (lock.isHeld) lock.release()
                    }
                } finally {
                    if (handoff.isHeld) handoff.release()
                }
            }
        } catch (error: Exception) {
            if (handoff.isHeld) handoff.release()
            throw error
        }
    }

    private fun acquire(tag: String) = power.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, tag).apply {
        setReferenceCounted(false)
        acquire(15_000L)
    }
}
