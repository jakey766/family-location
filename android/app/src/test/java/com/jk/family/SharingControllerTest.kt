package com.jk.family

import com.jk.family.core.Fix
import com.jk.family.core.ApiException
import com.jk.family.core.Upload
import java.util.concurrent.Executor
import org.junit.Assert.*
import org.junit.Test

class SharingControllerTest {
    private class Queue : Executor {
        val tasks = ArrayDeque<Runnable>()
        override fun execute(command: Runnable) { tasks.add(command) }
        fun run() { tasks.removeFirst().run() }
    }
    private class Remote : SharingRemote {
        var cancelled = false
        val stopped = mutableListOf<String>()
        val sent = mutableListOf<Upload>()
        var failUpload = false
        var uploadError: ApiException? = null
        var failStop = false
        override fun start() = "session"
        override fun stop(session: String) { stopped.add(session); if (failStop) throw java.io.IOException() }
        override fun upload(point: Upload): Boolean {
            sent.add(point)
            uploadError?.let { throw it }
            if (failUpload) throw java.io.IOException()
            return true
        }
        override fun cancel() { cancelled = true }
    }
    private class Source : PositionSource {
        var callback: ((Fix) -> Unit)? = null
        var stopped = false
        override fun start(onFix: (Fix) -> Unit, onError: (String) -> Unit) { callback = onFix }
        override fun interval(millis: Long) {}
        override fun stop() { stopped = true }
    }
    private class Rig {
        val io = Queue()
        val main = Queue()
        val remote = Remote()
        val source = Source()
        var message = ""
        var active = false
        val c = SharingController(remote, source, io, main, { 1000L }, { message = it }, { active = it })
        fun start() { c.start(); io.run(); main.run() }
        fun fix(time: Long) = Fix(30.0, 110.0, 10.0, time, 80)
    }

    @Test fun stoppedWhileStartingDoesNotStartSourceAndCleansReturnedSession() {
        val r = Rig()
        r.c.start()
        r.c.stop()
        assertFalse(r.active)
        assertTrue(r.source.stopped)
        assertFalse("start must finish serially so a late server session can be closed", r.remote.cancelled)
        r.io.run(); r.main.run()
        assertNull(r.source.callback)
        r.io.run(); r.main.run()
        assertEquals(listOf("session"), r.remote.stopped)
    }

    @Test fun stopClearsPendingAndLateLocationAndUploadCannotRestart() {
        val r = Rig(); r.start()
        val callback = r.source.callback!!
        callback(r.fix(1000))
        r.c.stop()
        callback(r.fix(2000))
        r.io.run(); r.main.run() // cancelled/ignored upload completion
        r.io.run(); r.main.run() // server stop
        r.c.retry()
        assertFalse(r.active)
        assertTrue(r.remote.cancelled)
        assertTrue(r.io.tasks.isEmpty())
    }

    @Test fun failedUploadRetriesOnlyLatestWithOriginalTimeAndSequence() {
        val r = Rig(); r.start()
        r.remote.failUpload = true
        r.source.callback!!(r.fix(1000))
        r.source.callback!!(r.fix(2000))
        r.io.run(); r.main.run()
        r.remote.failUpload = false
        r.c.retry(); r.io.run(); r.main.run()
        assertEquals(2000L, r.remote.sent.last().fix.capturedAtMillis)
        assertEquals(2L, r.remote.sent.last().sequence)
        r.c.retry()
        assertTrue(r.io.tasks.isEmpty())
    }

    @Test fun lateSuccessDoesNotClearNewPoint() {
        val r = Rig(); r.start()
        r.source.callback!!(r.fix(1000))
        r.source.callback!!(r.fix(2000))
        r.io.run(); r.main.run()
        r.c.retry(); r.io.run(); r.main.run()
        assertEquals(listOf(1L, 2L), r.remote.sent.map { it.sequence })
    }

    @Test fun sourceErrorStopsLocallyBeforeServerRequest() {
        val r = Rig(); r.start()
        r.c.stop("定位权限已撤回")
        assertTrue(r.source.stopped)
        assertFalse(r.active)
        assertTrue(r.remote.stopped.isEmpty())
        r.io.run(); r.main.run()
        assertEquals(listOf("session"), r.remote.stopped)
    }

    @Test fun invalidFutureTimeCanRecoverAfterClockCorrection() {
        val r = Rig(); r.start()
        r.remote.uploadError = ApiException(400, "invalid_input")
        r.source.callback!!(r.fix(999_000))
        r.io.run(); r.main.run()
        r.remote.uploadError = null
        r.source.callback!!(r.fix(2000))
        assertFalse("corrected time must be uploaded", r.io.tasks.isEmpty())
        r.io.run(); r.main.run()
        assertEquals(2000L, r.remote.sent.last().fix.capturedAtMillis)
        assertTrue(r.active)
    }

    @Test fun revokedCredentialsStopCollectionAndPendingRetries() {
        val r = Rig(); r.start()
        r.remote.uploadError = ApiException(401, "unauthorized")
        r.source.callback!!(r.fix(1000))
        r.io.run(); r.main.run()
        assertFalse(r.active)
        assertTrue(r.source.stopped)
        r.io.run(); r.main.run()
        r.c.retry()
        assertTrue(r.io.tasks.isEmpty())
        assertTrue(r.message.contains("重新配对"))
    }

    @Test fun offlineStopReportsUnconfirmedRemoteCleanup() {
        val r = Rig(); r.start()
        r.remote.failStop = true
        r.c.stop()
        assertFalse(r.active)
        assertTrue(r.source.stopped)
        r.io.run(); r.main.run()
        assertTrue(r.message.contains("7 分钟"))
        assertFalse(r.message.contains("服务器位置已清除"))
    }
}
