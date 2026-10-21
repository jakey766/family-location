package com.jk.family

import com.jk.family.core.ApiException
import com.jk.family.core.Fix
import com.jk.family.core.RetryBackoff
import com.jk.family.core.MotionPolicy
import com.jk.family.core.SharingState
import com.jk.family.core.Upload
import java.util.concurrent.Executor
import java.util.concurrent.atomic.AtomicBoolean

internal interface SharingRemote {
    fun start(): String
    fun stop(session: String)
    fun upload(point: Upload): Boolean
    fun cancel()
}

internal interface PositionSource {
    fun start(onFix: (Fix) -> Unit, onError: (String) -> Unit)
    fun interval(millis: Long)
    fun stop()
    fun wake() {}
}

/** All public methods and location callbacks run on the main executor. IO is serial. */
internal class SharingController(
    private val remote: SharingRemote,
    private val source: PositionSource,
    private val io: Executor,
    private val main: Executor,
    private val elapsed: () -> Long,
    private val status: (String) -> Unit,
    private val activeChanged: (Boolean) -> Unit,
    private val quietNow: () -> Boolean = { false },
    private val workChanged: () -> Unit = {},
    private val activityWindow: () -> Long = { 0L },
) {
    private val state = SharingState()
    private val backoff = RetryBackoff()
    private var motion = MotionPolicy()
    private var flight: AtomicBoolean? = null
    private var starting = false
    private var released = false
    private var sourceRunning = false
    private var sourceEpoch = 0L
    @Volatile private var networkAvailable = true
    private var networkGeneration = 0L
    private var lastWindow = 0L
    var paused = false
        private set
    val active get() = state.active

    fun start() {
        if (state.active) return
        released = false
        state.begin()
        lastWindow = activityWindow()
        motion = MotionPolicy()
        backoff.reset()
        activeChanged(true)
        retry()
    }

    /** Night is a pause, not stop: keep intent/session, drop accumulated location data. */
    private fun syncSchedule() {
        if (!active) return
        val quiet = quietNow()
        val window = activityWindow()
        if (quiet == paused && window == lastWindow) return
        lastWindow = window
        paused = quiet
        backoff.reset()
        motion = MotionPolicy()
        stopSource()
        state.pending?.let { state.rejected(it) }
        flight?.set(false)
        if (flight != null) remote.cancel()
        if (quiet) status("共享已开启 · 夜间暂停（22:00–08:00），08:00 后恢复采集和上报")
        else {
            status("共享已开启，正在恢复白天定位")
            if (state.sessionId != null) startSource(state.generation)
        }
        workChanged()
    }

    fun updateSchedule() {
        if (!active) return
        syncSchedule()
        if (!paused) {
            if (state.sessionId != null) startSource(state.generation)
            retry()
        }
        workChanged()
    }

    fun wake() {
        updateSchedule()
        if (active && !paused) source.wake()
    }

    fun setNetworkAvailable(available: Boolean, recovered: Boolean = false) {
        if (networkAvailable == available && !recovered) return
        networkAvailable = available
        if (available) { networkGeneration++; backoff.reset(); updateSchedule() }
        workChanged()
    }

    fun retryDelayMillis(): Long? {
        if (!active || paused || !networkAvailable || starting || flight != null) return null
        if (state.sessionId != null && state.pending == null) return null
        return backoff.remaining(elapsed())
    }

    private fun connect(generation: Long) {
        if (starting) return
        starting = true
        val networkEpoch = networkGeneration
        val window = activityWindow()
        status("正在建立共享连接…")
        io.execute {
            // IO may have waited behind another request across the quiet-hours boundary.
            val result = runCatching { if (quietNow() || !networkAvailable || activityWindow() != window) null else remote.start() }
            main.execute {
                starting = false
                result.fold(onSuccess = { session ->
                    if (session != null && !state.started(generation, session)) {
                        if (!released) closeSession(session, "已停止本地共享")
                        return@fold
                    }
                    if (!active || state.generation != generation) return@fold
                    backoff.reset()
                    syncSchedule()
                    if (session != null && !paused) startSource(generation)
                }, onFailure = { error ->
                    if (state.generation == generation && active) {
                        syncSchedule()
                        if ((error as? ApiException)?.status in listOf(400, 401, 409))
                            stop(errorMessage(error, "开启失败，请检查配置后重试"))
                        else if (!paused) {
                            if (networkEpoch == networkGeneration) backoff.failed(elapsed())
                            status("暂时无法建立连接，后台逐步延长重试间隔，等待网络恢复")
                        }
                    }
                })
                workChanged()
            }
        }
        workChanged()
    }

    private fun startSource(generation: Long) {
        if (sourceRunning || paused || !active) return
        sourceRunning = true
        val epoch = ++sourceEpoch
        status("共享已开启，正在等待定位")
        try {
            source.start({ fix ->
                if (state.generation == generation && active && sourceEpoch == epoch) {
                    syncSchedule()
                    if (!paused && sourceEpoch == epoch) {
                        val point = state.capture(fix)
                        if (point != null) {
                            source.interval(motion.intervalMillis(fix, elapsed()))
                            retry()
                        }
                    }
                }
            }, { message ->
                if (state.generation == generation && active && sourceEpoch == epoch) {
                    syncSchedule()
                    if (!paused) status(message)
                }
            })
        } catch (_: Exception) {
            stop("定位初始化失败，请检查授权和高德配置")
        }
    }

    private fun stopSource() {
        sourceEpoch++
        sourceRunning = false
        try { source.stop() } catch (_: Exception) { /* callbacks are already invalid */ }
    }

    fun retry() {
        if (!active) return
        syncSchedule()
        if (paused || !networkAvailable || flight != null || backoff.remaining(elapsed()) > 0) {
            workChanged()
            return
        }
        if (state.sessionId == null) { connect(state.generation); return }
        val point = state.pending ?: return
        val generation = state.generation
        val gate = AtomicBoolean(true)
        val networkEpoch = networkGeneration
        val window = activityWindow()
        flight = gate
        io.execute {
            val result = runCatching { if (gate.get() && !quietNow() && networkAvailable && activityWindow() == window) remote.upload(point) else null }
            main.execute {
                if (flight === gate) flight = null
                if (state.generation != generation || !active) return@execute
                syncSchedule()
                if (!gate.get() || paused) { workChanged(); return@execute }
                result.fold(onSuccess = { accepted ->
                    if (accepted != null) {
                        backoff.reset()
                        state.acknowledged(point)
                        status("共享中 · 最近定位 ${java.text.DateFormat.getTimeInstance().format(java.util.Date(point.fix.capturedAtMillis))} · 精度约 ${point.fix.accuracyM.toInt()} 米")
                    }
                }, onFailure = { error ->
                    when ((error as? ApiException)?.status) {
                        401, 409 -> stop(errorMessage(error, "共享失效，请手动重新开启"))
                        400 -> {
                            backoff.reset()
                            state.rejected(point)
                            motion = MotionPolicy()
                            status("位置被拒绝，请检查手机时间；等待新的定位")
                        }
                        else -> {
                            if (networkEpoch == networkGeneration) backoff.failed(elapsed())
                            status("上传暂未成功，保留最新位置，逐步延长重试间隔（最多 5 分钟）")
                        }
                    }
                })
                workChanged()
            }
        }
        workChanged()
    }

    fun stop(reason: String = "已停止本地共享") {
        if (!state.active) return
        val session = state.stop()
        flight?.set(false)
        // The SDK and pending point are stopped before any remote cleanup.
        stopSource()
        // Let an in-flight start finish so its session can be closed. Only uploads are cancelled.
        if (flight != null) remote.cancel()
        status("$reason。正在尝试清除服务器位置；离线时仍保留最后位置，可在网页撤销设备。")
        activeChanged(false)
        workChanged()
        if (session != null) closeSession(session, reason)
    }

    /** System teardown is not a user stop: preserve the remote last point and restart intent. */
    fun release() {
        if (!state.active) return // Preserve explicit-stop cleanup for late start responses.
        released = true
        state.stop()
        flight?.set(false)
        stopSource()
        if (flight != null) remote.cancel()
        activeChanged(false)
        workChanged()
    }

    private fun closeSession(session: String, reason: String) {
        val generation = state.generation
        io.execute {
            val result = runCatching { remote.stop(session) }
            main.execute {
                if (state.active || state.generation != generation) return@execute
                val error = result.exceptionOrNull() as? ApiException
                if (result.isSuccess || error?.status == 401) status("$reason。服务器位置已清除。")
                else status("$reason。未确认服务器清除，最后位置仍会保留；请在网页撤销设备，或联网重新开启后停止共享。")
            }
        }
    }

    private fun errorMessage(error: Throwable, fallback: String) = when ((error as? ApiException)?.status) {
        401 -> "设备凭证失效或已撤销，请解除本机配对后重新配对"
        409 -> "共享会话已失效，请手动重新开启"
        else -> fallback
    }
}
