package com.jk.family

import android.content.Context
import android.os.Handler
import android.os.Looper
import android.os.PowerManager
import android.os.SystemClock
import android.util.Log
import com.jk.family.core.Fix
import com.jk.family.core.MotionPolicy
import com.jk.family.core.QuietHours

/** One bounded acquisition per wake. No SDK listener or CPU lock between samples. */
internal class ScheduledPositionSource(
    context: Context,
    private val source: PositionSource,
    private val quietNow: () -> Boolean = { QuietHours.isQuiet() },
    private val elapsed: () -> Long = SystemClock::elapsedRealtime,
) : PositionSource {
    private val power = context.getSystemService(PowerManager::class.java)
    private val handler = Handler(Looper.getMainLooper())
    private val timer = WakeupTimer(context, WakeupTimer.SAMPLE, elapsed) { sample() }
    private var active = false
    private var epoch = 0L
    private var collecting = false
    private var interval = MotionPolicy.MOVING_INTERVAL
    private var highAccuracy = false
    private var startedAt = 0L
    private var lock: PowerManager.WakeLock? = null
    private var onFix: ((Fix) -> Unit)? = null
    private var onError: ((String) -> Unit)? = null
    private val timeout = Runnable { finish(epoch, null, "定位超时，等待下一次采样") }

    override fun start(onFix: (Fix) -> Unit, onError: (String) -> Unit) {
        stop()
        this.onFix = onFix
        this.onError = onError
        active = true
        sample()
    }

    override fun interval(millis: Long) {
        interval = millis.coerceIn(60_000L, 3_600_000L)
        if (active) timer.schedule(startedAt + interval)
    }

    override fun accuracy(high: Boolean) { highAccuracy = high }

    override fun wake() {
        if (!active) return
        if (quietNow()) { stop(); return }
        if (collecting && elapsed() - startedAt >= SAMPLE_TIMEOUT) {
            finish(epoch, null, "定位超时，等待下一次采样")
        }
        timer.fire()
    }

    private fun sample() {
        if (!active) return
        if (quietNow()) { stop(); return }
        if (collecting) return
        collecting = true
        val token = ++epoch
        startedAt = elapsed()
        lock = power.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "family:sample").apply {
            setReferenceCounted(false)
            acquire(SAMPLE_TIMEOUT + 5_000)
        }
        // Arm the next wake before entering the SDK, including if this acquisition times out.
        timer.schedule(startedAt + interval)
        handler.postDelayed(timeout, SAMPLE_TIMEOUT)
        Log.i("FamilyLocation", "sample_start interval_ms=$interval")
        try {
            source.interval(interval)
            source.accuracy(highAccuracy)
            source.start({ fix -> finish(token, fix, null) }, { error -> finish(token, null, error) })
        } catch (_: Exception) {
            finish(token, null, "定位暂不可用，等待下一次采样")
        }
    }

    private fun finish(token: Long, fix: Fix?, error: String?) {
        if (!active || !collecting || token != epoch) return
        if (quietNow()) { stop(); return }
        val expired = elapsed() - startedAt >= SAMPLE_TIMEOUT
        collecting = false
        epoch++
        handler.removeCallbacks(timeout)
        try { source.stop() } catch (_: Exception) { /* token already invalidates callbacks */ }
        val held = lock
        lock = null
        try {
            Log.i("FamilyLocation", "sample_end result=${if (expired) "timeout" else if (fix != null) "fix" else "error"}")
            if (expired) onError?.invoke("定位超时，等待下一次采样")
            else if (fix != null) onFix?.invoke(fix)
            else onError?.invoke(error ?: "定位暂不可用")
        } finally {
            // onFix can update the motion interval or explicitly stop this source.
            if (active) {
                // Preserve an already-due wake: it must start one fresh sample now, not
                // spend the wake only cleaning up a timeout and postpone another interval.
                timer.schedule(startedAt + interval)
            }
            if (held?.isHeld == true) held.release()
        }
    }

    override fun stop() {
        active = false
        epoch++
        timer.cancel()
        handler.removeCallbacks(timeout)
        if (collecting) {
            collecting = false
            try { source.stop() } catch (_: Exception) { /* no callback is valid after stop */ }
        }
        lock?.let { if (it.isHeld) it.release() }
        lock = null
        onFix = null
        onError = null
    }

    companion object { private const val SAMPLE_TIMEOUT = 30_000L }
}
