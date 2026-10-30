package com.jk.family

import com.jk.family.core.*
import org.junit.Assert.*
import org.junit.Test

class DeviceConfigControllerTest {
    private class Rig {
        val io = ArrayDeque<Runnable>()
        val main = ArrayDeque<Runnable>()
        var config = DeviceConfig()
        var result = UploadResult(false)
        var onFix: ((Fix) -> Unit)? = null
        var stopped = 0
        var highAccuracy = false
        var interval = 0L
        var saved = 0
        var saveSucceeds = true
        var saveThrows = false
        var message = ""
        var elapsed = 0L
        val controller = SharingController(object : SharingRemote {
            override fun start() = "session"
            override fun stop(session: String) {}
            override fun upload(point: Upload) = result.accepted
            override fun uploadResult(point: Upload) = result
            override fun cancel() {}
        }, object : PositionSource {
            override fun start(onFix: (Fix) -> Unit, onError: (String) -> Unit) { this@Rig.onFix = onFix }
            override fun stop() { stopped++ }
            override fun interval(millis: Long) { interval = millis }
            override fun accuracy(high: Boolean) { highAccuracy = high }
        }, { io.add(it) }, { main.add(it) }, { elapsed }, { message = it }, {},
            { config.quietHoursStart == "12:00" }, initialConfig = config,
            configChanged = { config = it; saved++; if (saveThrows) throw IllegalStateException("storage unavailable"); saveSucceeds })
        fun settle() { while (io.isNotEmpty() || main.isNotEmpty()) { if (io.isNotEmpty()) io.removeFirst().run(); if (main.isNotEmpty()) main.removeFirst().run() } }
        fun start() { controller.start(); settle() }
        fun fix(t: Long = elapsed) { onFix!!(Fix(30.0, 120.0, 5.0, 1700000000000L + t, null)) }
    }
    @Test fun duplicateResponseAppliesConfigAndRejectsVersionRollback() {
        val r = Rig(); r.start()
        r.result = UploadResult(false, DeviceConfig(60, 600, true, version = 3))
        r.fix(); r.settle()
        assertEquals(60000L, r.interval); assertTrue(r.highAccuracy); assertEquals(1, r.saved)
        r.result = UploadResult(true, DeviceConfig(version = 2)); r.fix(1); r.settle()
        assertEquals(3L, r.config.version); assertEquals(1, r.saved)
        r.result = UploadResult(true); r.fix(2); r.settle()
        assertEquals(3L, r.config.version)
    }
    @Test fun newlyQuietConfigImmediatelyStopsAndCannotResumeExplicitStop() {
        val r = Rig(); r.start()
        r.result = UploadResult(true, DeviceConfig(60, 600, false, "12:00", "13:00", 2))
        r.fix(); r.settle()
        assertTrue(r.controller.paused); assertTrue(r.controller.active)
        assertNull(r.controller.retryDelayMillis()); assertTrue(r.stopped > 0)
        r.controller.stop(); r.settle()
        r.config = DeviceConfig(); r.controller.updateSchedule(); r.settle()
        assertFalse(r.controller.active)
    }
    @Test fun lateResponseAfterStopReleaseOrNewSessionCannotPersistConfig() {
        for (transition in 0..2) {
            val r = Rig(); r.start(); r.result = UploadResult(true, DeviceConfig(version = 9)); r.fix()
            r.io.removeFirst().run()
            if (transition == 1) r.controller.release() else r.controller.stop()
            if (transition == 2) r.controller.start()
            r.settle()
            assertEquals(0, r.saved); assertEquals(1L, r.config.version)
        }
    }
    @Test fun failedPersistenceRetriesSameVersionWithoutRetryingSuccessfulPoint() {
        val r = Rig(); r.start(); r.saveSucceeds = false
        r.result = UploadResult(true, DeviceConfig(60, 600, true, version = 2))
        r.fix(); r.settle()
        assertEquals(2L, r.config.version)
        assertNull("successful upload must stay acknowledged", r.controller.retryDelayMillis())
        assertTrue(r.message, r.message.contains("尚未保存"))
        r.saveSucceeds = true
        r.fix(1); r.settle()
        assertEquals("the same version must retry an unconfirmed disk write", 2, r.saved)
        assertFalse(r.message, r.message.contains("尚未保存"))
        r.fix(2); r.settle()
        assertEquals("a confirmed version should not rewrite on every point", 2, r.saved)
    }
    @Test fun persistenceExceptionKeepsNewQuietRestrictionAndDoesNotCrashUpload() {
        val r = Rig(); r.start(); r.saveThrows = true
        r.result = UploadResult(true, DeviceConfig(60, 600, false, "12:00", "13:00", 2))
        r.fix(); r.settle()
        assertTrue(r.controller.paused)
        assertTrue(r.controller.active)
        assertEquals(2L, r.config.version)
        assertNull(r.controller.retryDelayMillis())
        assertTrue(r.message, r.message.contains("尚未保存"))
    }

    @Test fun equalIntervalsSwitchAccuracyBasedOnMotionRatherThanInterval() {
        val r = Rig(); r.start(); r.result = UploadResult(true, DeviceConfig(60, 60, true, version = 2))
        r.fix(); r.settle(); assertTrue(r.highAccuracy)
        r.elapsed = 300000; r.fix(); r.settle()
        assertEquals(60000L, r.interval); assertFalse(r.highAccuracy)
    }
}
