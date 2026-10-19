package com.jk.family

import android.content.Context
import android.os.PowerManager
import java.util.concurrent.Executor

/** Waiting in the delegate's queue never holds a wake lock. */
internal class NetworkWorkExecutor(context: Context, private val delegate: Executor) : Executor {
    private val power = context.applicationContext.getSystemService(PowerManager::class.java)

    override fun execute(command: Runnable) {
        delegate.execute {
            val lock = power.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "family:network")
            lock.setReferenceCounted(false)
            lock.acquire(15_000L)
            try {
                command.run()
            } finally {
                if (lock.isHeld) lock.release()
            }
        }
    }
}
