package com.jk.family

import com.jk.family.core.Fix
import com.jk.family.core.Upload
import java.util.concurrent.Executor
import org.junit.Assert.*
import org.junit.Test

class PowerSavingControllerTest {
    private class Queue : Executor {
        val tasks = ArrayDeque<Runnable>()
        override fun execute(command: Runnable) { tasks.add(command) }
        fun run() { tasks.removeFirst().run() }
    }
    private class Rig {
        val io = Queue()
        val main = Queue()
        var now = 0L
        var quiet = false
        var window = 0L
        var starts = 0
        var sourceStarts = 0
        var sourceStops = 0
        var cancellations = 0
        var failures = false
        var message = ""
        var callback: ((Fix) -> Unit)? = null
        val sent = mutableListOf<Upload>()
        val stopped = mutableListOf<String>()
        val c = SharingController(object : SharingRemote {
            override fun start(): String { starts++; if (failures) throw java.io.IOException(); return "session" }
            override fun stop(session: String) { stopped.add(session) }
            override fun upload(point: Upload): Boolean { sent.add(point); if (failures) throw java.io.IOException(); return true }
            override fun cancel() { cancellations++ }
        }, object : PositionSource {
            override fun start(onFix: (Fix) -> Unit, onError: (String) -> Unit) { sourceStarts++; callback = onFix }
            override fun interval(millis: Long) {}
            override fun stop() { sourceStops++ }
        }, io, main, { now }, { message = it }, {}, { quiet }, {}, { window })
        fun settle() { while (io.tasks.isNotEmpty() || main.tasks.isNotEmpty()) {
            if (io.tasks.isNotEmpty()) io.run()
            if (main.tasks.isNotEmpty()) main.run()
        } }
        fun fix(time: Long) = Fix(30.0, 110.0, 10.0, time, 80)
        fun start() { c.start(); settle() }
    }

    @Test fun nightStartWaitsUntilMorningWithoutConnectingOrCollecting() {
        val r = Rig(); r.quiet = true; r.start()
        assertTrue(r.c.active)
        assertTrue(r.c.paused)
        assertEquals(0, r.starts)
        assertEquals(0, r.sourceStarts)
        assertTrue(r.message.contains("暂停时段"))
        assertNull(r.c.retryDelayMillis())
        r.quiet = false; r.c.updateSchedule(); r.settle()
        assertEquals(1, r.starts)
        assertEquals(1, r.sourceStarts)
        assertFalse(r.c.paused)
    }

    @Test fun nightDropsPendingAndLateCallbacksButKeepsRemoteSession() {
        val r = Rig(); r.start()
        val yesterday = r.callback!!
        yesterday(r.fix(1000)) // upload still queued
        r.quiet = true; r.c.updateSchedule()
        yesterday(r.fix(2000)); r.settle()
        assertTrue(r.sent.isEmpty())
        assertTrue(r.stopped.isEmpty())
        assertTrue(r.c.active)
        assertTrue(r.sourceStops > 0)
        r.quiet = false; r.c.updateSchedule(); r.settle()
        assertEquals("same live session survives the night", 1, r.starts)
        yesterday(r.fix(3000)); r.settle()
        assertTrue("old source callback must stay invalid next morning", r.sent.isEmpty())
        r.callback!!(r.fix(4000)); r.settle()
        assertEquals(listOf(4000L), r.sent.map { it.fix.capturedAtMillis })
    }

    @Test fun queuedRequestsCheckClockEvenBeforeAlarmArrives() {
        val r = Rig(); r.start()
        r.callback!!(r.fix(1000))
        r.quiet = true // deliberately no alarm/updateSchedule call
        r.settle()
        assertTrue(r.sent.isEmpty())
        assertTrue(r.c.paused)
        val s = Rig(); s.c.start(); s.quiet = true; s.settle()
        assertEquals(0, s.starts)
        assertEquals(0, s.sourceStarts)
    }

    @Test fun lateStartSuccessDuringNightWaitsForMorningAndStopNeverResumes() {
        val r = Rig(); r.c.start(); r.io.run()
        r.quiet = true; r.main.run()
        assertEquals(0, r.sourceStarts)
        assertTrue(r.c.paused)
        r.c.stop(); r.settle()
        r.quiet = false; r.c.updateSchedule(); r.settle()
        assertFalse(r.c.active)
        assertEquals(0, r.sourceStarts)
        assertEquals(listOf("session"), r.stopped)
    }

    @Test fun repeatedFailuresBackOffAndNewFixDoesNotBypassDelay() {
        val r = Rig(); r.start(); r.failures = true
        r.callback!!(r.fix(1000)); r.settle()
        assertEquals(15000L, r.c.retryDelayMillis())
        r.callback!!(r.fix(2000)); r.c.retry(); r.settle()
        assertEquals(1, r.sent.size)
        r.now = 15000; r.c.retry(); r.settle()
        assertEquals(30000L, r.c.retryDelayMillis())
        r.now += 30000; r.failures = false; r.c.retry(); r.settle()
        assertEquals(2000L, r.sent.last().fix.capturedAtMillis)
        assertNull(r.c.retryDelayMillis())
        r.failures = true; r.callback!!(r.fix(3000)); r.settle()
        assertEquals(15000L, r.c.retryDelayMillis())
    }

    @Test fun offlinePausesRequestsAndNetworkRecoveryRetriesImmediately() {
        val r = Rig(); r.c.setNetworkAvailable(false); r.start()
        assertEquals(0, r.starts)
        assertNull(r.c.retryDelayMillis())
        r.c.setNetworkAvailable(true); r.settle()
        assertEquals(1, r.starts)
        r.failures = true; r.callback!!(r.fix(1000)); r.settle()
        r.c.setNetworkAvailable(false); r.c.setNetworkAvailable(true)
        r.failures = false; r.settle()
        assertEquals(2, r.sent.size)
        r.c.setNetworkAvailable(true); r.settle()
        assertEquals("duplicate available callback cannot duplicate uploads", 2, r.sent.size)
    }

    @Test fun sleepingAcrossWholeNightCannotUploadYesterdayOrReuseOldSource() {
        val r = Rig(); r.start()
        val yesterday = r.callback!!
        yesterday(r.fix(1000))
        r.window++ // first CPU work after sleep is next morning: no quiet callback was delivered
        r.settle()
        assertTrue("queued IO must reject the previous day's point", r.sent.isEmpty())
        assertEquals(2, r.sourceStarts)
        yesterday(r.fix(2000)); r.settle()
        assertTrue(r.sent.isEmpty())
        r.callback!!(r.fix(3000)); r.settle()
        assertEquals(listOf(3000L), r.sent.map { it.fix.capturedAtMillis })
    }

    @Test fun queuedStartAndUploadDoNotRunAfterNetworkDisappears() {
        val r = Rig(); r.c.start(); r.c.setNetworkAvailable(false); r.settle()
        assertEquals(0, r.starts)
        r.c.setNetworkAvailable(true); r.settle()
        r.callback!!(r.fix(1000)); r.c.setNetworkAvailable(false); r.settle()
        assertTrue(r.sent.isEmpty())
        r.c.setNetworkAvailable(true); r.settle()
        assertEquals(1, r.sent.size)
    }

    @Test fun oldFailureCannotOverrideNetworkRecoveryBeforeMainCallback() {
        val r = Rig(); r.start(); r.failures = true
        r.callback!!(r.fix(1000)); r.io.run()
        r.c.setNetworkAvailable(false); r.c.setNetworkAvailable(true)
        r.main.run()
        assertEquals(0L, r.c.retryDelayMillis())
        val s = Rig(); s.failures = true; s.c.start(); s.io.run()
        s.c.setNetworkAvailable(false); s.c.setNetworkAvailable(true); s.main.run()
        assertEquals(0L, s.c.retryDelayMillis())
    }

    @Test fun startFailuresAlsoBackOffAndNightDoesNotReconnect() {
        val r = Rig(); r.failures = true; r.start()
        r.c.retry(); r.settle(); assertEquals(1, r.starts)
        r.now = 15000; r.c.retry(); r.settle(); assertEquals(2, r.starts)
        assertEquals(30000L, r.c.retryDelayMillis())
        r.quiet = true; r.c.updateSchedule()
        r.c.setNetworkAvailable(false); r.c.setNetworkAvailable(true); r.settle()
        assertEquals(2, r.starts)
        assertNull(r.c.retryDelayMillis())
    }
}
