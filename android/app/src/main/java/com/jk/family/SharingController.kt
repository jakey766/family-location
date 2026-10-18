package com.jk.family

import com.jk.family.core.ApiException
import com.jk.family.core.Fix
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
) {
    private val state = SharingState()
    private var motion = MotionPolicy()
    private var flight: AtomicBoolean? = null
    val active get() = state.active

    fun start() {
        if (state.active) return
        val generation = state.begin()
        motion = MotionPolicy()
        activeChanged(true)
        status("正在建立共享连接…")
        io.execute {
            val result = runCatching { remote.start() }
            main.execute {
                result.fold(onSuccess = { session ->
                    if (!state.started(generation, session)) {
                        closeSession(session, "已停止本地共享")
                        return@fold
                    }
                    status("共享已开启，正在等待首次定位")
                    try {
                        source.start({ fix ->
                            if (state.generation == generation && state.active) {
                                val point = state.capture(fix)
                                if (point != null) {
                                    source.interval(motion.intervalMillis(fix, elapsed()))
                                    retry()
                                }
                            }
                        }, { message -> if (state.generation == generation && state.active) status(message) })
                    } catch (_: Exception) {
                        stop("定位初始化失败，请检查授权和高德配置")
                    }
                }, onFailure = { error ->
                    if (state.generation == generation && state.active) stop(errorMessage(error, "开启失败，请检查网络后手动重试"))
                })
            }
        }
    }

    fun retry() {
        if (!state.active || flight != null) return
        val point = state.pending ?: return
        val generation = state.generation
        val gate = AtomicBoolean(true)
        flight = gate
        io.execute {
            val result = runCatching { if (gate.get()) remote.upload(point) else false }
            main.execute {
                if (flight === gate) flight = null
                if (state.generation != generation || !state.active) return@execute
                result.fold(onSuccess = {
                    state.acknowledged(point)
                    status("共享中 · 最近定位 ${java.text.DateFormat.getTimeInstance().format(java.util.Date(point.fix.capturedAtMillis))} · 精度约 ${point.fix.accuracyM.toInt()} 米")
                }, onFailure = { error ->
                    when ((error as? ApiException)?.status) {
                        401, 409 -> stop(errorMessage(error, "共享失效，请手动重新开启"))
                        400 -> {
                            state.rejected(point)
                            motion = MotionPolicy()
                            status("位置被拒绝，请检查手机时间；等待新的定位")
                        }
                        else -> status("上传暂未成功，保留最新位置，等待重试")
                    }
                })
            }
        }
    }

    fun stop(reason: String = "已停止本地共享") {
        if (!state.active) return
        val session = state.stop()
        flight?.set(false)
        // The SDK and pending point are stopped before any remote cleanup.
        try { source.stop() } catch (_: Exception) { /* local state already invalid */ }
        // Let an in-flight start finish so its session can be closed. Only uploads are cancelled.
        if (flight != null) remote.cancel()
        status("$reason。正在尝试清除服务器位置；无法连接时最迟 7 分钟后隐藏。")
        activeChanged(false)
        if (session != null) closeSession(session, reason)
    }

    private fun closeSession(session: String, reason: String) {
        val generation = state.generation
        io.execute {
            val result = runCatching { remote.stop(session) }
            main.execute {
                if (state.active || state.generation != generation) return@execute
                val error = result.exceptionOrNull() as? ApiException
                if (result.isSuccess || error?.status == 401) status("$reason。服务器位置已清除。")
                else status("$reason。未确认服务器清除，最迟在最后成功上传后 7 分钟隐藏。")
            }
        }
    }

    private fun errorMessage(error: Throwable, fallback: String) = when ((error as? ApiException)?.status) {
        401 -> "设备凭证失效或已撤销，请解除本机配对后重新配对"
        409 -> "共享会话已失效，请手动重新开启"
        else -> fallback
    }
}
