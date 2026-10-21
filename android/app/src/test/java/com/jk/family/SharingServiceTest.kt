package com.jk.family

import android.Manifest
import android.app.Service
import android.content.Context
import android.content.Intent
import android.os.Looper
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.robolectric.annotation.LooperMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [25, 33])
@LooperMode(LooperMode.Mode.PAUSED)
class SharingServiceTest {
    @org.junit.Before fun onlineNetwork() {
        val app = org.robolectric.RuntimeEnvironment.getApplication()
        val manager = app.getSystemService(android.net.ConnectivityManager::class.java)
        shadowOf(manager).setNetworkCapabilities(manager.activeNetwork,
            shadowOf(android.net.NetworkCapabilities()).addCapability(android.net.NetworkCapabilities.NET_CAPABILITY_INTERNET))
    }

    @Test fun explicitResumeRequiresSavedIntentAndIsIdempotent() {
        val lifecycle = Robolectric.buildService(TestService::class.java).create()
        val service = lifecycle.get()
        CredentialStore(service).apply { consent = true; sharingRequested = true }
        shadowOf(service.application).grantPermissions(Manifest.permission.ACCESS_FINE_LOCATION)
        try {
            val resume = Intent().setAction("com.jk.family.RESUME")
            assertEquals(Service.START_STICKY, service.onStartCommand(resume, 0, 1))
            assertTrue(AppRuntime.active)
            assertEquals(Service.START_STICKY, service.onStartCommand(resume, 0, 2))
            assertEquals("a second resume must not create another start request", 1, service.io.size)
            service.onStartCommand(Intent().setAction(SharingService.STOP), 0, 3)
            assertEquals("a queued resume after stop must stay stopped", Service.START_NOT_STICKY,
                service.onStartCommand(resume, 0, 4))
            assertFalse(AppRuntime.active)
        } finally { lifecycle.destroy() }
    }

    @Test fun nightStartKeepsIntentWithoutNetworkOrWakeLockAndStopCancelsAlarm() {
        val lifecycle = Robolectric.buildService(TestService::class.java).create()
        val service = lifecycle.get()
        CredentialStore(service).consent = true
        shadowOf(service.application).grantPermissions(Manifest.permission.ACCESS_FINE_LOCATION)
        service.quiet = true
        try {
            service.onStartCommand(Intent().setAction(SharingService.START), 0, 1)
            assertTrue(CredentialStore(service).sharingRequested)
            assertTrue(AppRuntime.active)
            assertTrue(AppRuntime.controller!!.paused)
            assertTrue(service.io.isEmpty())
            val lock = org.robolectric.shadows.ShadowPowerManager.getLatestWakeLock()
            assertTrue("waiting overnight must not hold a CPU wake lock", lock == null || !lock.isHeld)
            assertEquals(1, shadowOf(service.getSystemService(android.app.AlarmManager::class.java)).scheduledAlarms.size)
            service.onStartCommand(Intent().setAction(SharingService.STOP), 0, 2)
            assertFalse(CredentialStore(service).sharingRequested)
            assertTrue(shadowOf(service.getSystemService(android.app.AlarmManager::class.java)).scheduledAlarms.isEmpty())
        } finally { lifecycle.destroy() }
    }

    @Test fun failedStartSchedulesOnlyDueRetryAndReleasesNetworkListenerOnDestroy() {
        val lifecycle = Robolectric.buildService(TestService::class.java).create()
        val service = lifecycle.get()
        CredentialStore(service).consent = true
        shadowOf(service.application).grantPermissions(Manifest.permission.ACCESS_FINE_LOCATION)
        val manager = service.getSystemService(android.net.ConnectivityManager::class.java)
        try {
            service.failStart = true
            service.onStartCommand(Intent().setAction(SharingService.START), 0, 1)
            assertEquals(1, shadowOf(manager).networkCallbacks.size)
            service.io.removeFirst().run()
            assertTrue(shadowOf(service.getSystemService(android.app.AlarmManager::class.java)).scheduledAlarms.any {
                shadowOf(it.operation).savedIntent.action == WakeupTimer.RETRY
            })
            shadowOf(Looper.getMainLooper()).idleFor(java.time.Duration.ofSeconds(14))
            assertTrue(service.io.isEmpty())
            shadowOf(Looper.getMainLooper()).idleFor(java.time.Duration.ofSeconds(1))
            assertEquals("service must arrange retry after the failure callback", 1, service.io.size)
            service.failStart = false
            service.io.removeFirst().run()
            shadowOf(Looper.getMainLooper()).idleFor(java.time.Duration.ofMinutes(1))
            assertTrue("success leaves no periodic network polling", service.io.isEmpty())
            val lock = org.robolectric.shadows.ShadowPowerManager.getLatestWakeLock()
            assertTrue(lock == null || !lock.isHeld)
        } finally { lifecycle.destroy() }
        assertTrue(shadowOf(manager).networkCallbacks.isEmpty())
    }

    @Test fun validationRecoveryAndDefaultNetworkSwitchResetBackoffWithoutOfflineEvent() {
        val lifecycle = Robolectric.buildService(TestService::class.java).create()
        val service = lifecycle.get()
        CredentialStore(service).consent = true
        shadowOf(service.application).grantPermissions(Manifest.permission.ACCESS_FINE_LOCATION)
        val manager = service.getSystemService(android.net.ConnectivityManager::class.java)
        try {
            service.failStart = true
            service.onStartCommand(Intent().setAction(SharingService.START), 0, 1)
            service.io.removeFirst().run()
            assertEquals(15000L, AppRuntime.controller!!.retryDelayMillis())
            val callback = shadowOf(manager).networkCallbacks.single()
            val before = manager.activeNetwork!!
            val caps = android.net.NetworkCapabilities()
            shadowOf(caps).addCapability(android.net.NetworkCapabilities.NET_CAPABILITY_INTERNET)
            shadowOf(caps).addCapability(android.net.NetworkCapabilities.NET_CAPABILITY_VALIDATED)
            shadowOf(manager).setNetworkCapabilities(before, caps)
            callback.onCapabilitiesChanged(before, caps)
            shadowOf(Looper.getMainLooper()).idle()
            assertEquals("validated recovery must bypass the existing delay", 1, service.io.size)
            service.io.removeFirst().run()
            callback.onCapabilitiesChanged(before, caps)
            shadowOf(Looper.getMainLooper()).idle()
            assertTrue("duplicate capability updates must preserve backoff", service.io.isEmpty())
            val type = if (manager.activeNetworkInfo!!.type == android.net.ConnectivityManager.TYPE_WIFI)
                android.net.ConnectivityManager.TYPE_MOBILE else android.net.ConnectivityManager.TYPE_WIFI
            shadowOf(manager).setActiveNetworkInfo(org.robolectric.shadows.ShadowNetworkInfo.newInstance(
                android.net.NetworkInfo.DetailedState.CONNECTED, type, 0, true, android.net.NetworkInfo.State.CONNECTED))
            assertNotEquals(before, manager.activeNetwork)
            shadowOf(manager).setNetworkCapabilities(manager.activeNetwork, caps)
            callback.onAvailable(manager.activeNetwork!!)
            shadowOf(Looper.getMainLooper()).idle()
            assertEquals("switching to a different network must retry immediately", 1, service.io.size)
        } finally { lifecycle.destroy() }
    }

    @Test fun boundaryBroadcastPausesAndResumesExistingSessionWithoutStartingNewService() {
        val lifecycle = Robolectric.buildService(TestService::class.java).create()
        val service = lifecycle.get()
        CredentialStore(service).consent = true
        shadowOf(service.application).grantPermissions(Manifest.permission.ACCESS_FINE_LOCATION)
        try {
            service.onStartCommand(Intent().setAction(SharingService.START), 0, 1)
            service.io.removeFirst().run()
            service.quiet = true
            SharingScheduleReceiver().onReceive(service, Intent(SharingSchedule.BOUNDARY))
            assertTrue(AppRuntime.controller!!.paused)
            assertTrue(AppRuntime.active)
            assertTrue(service.io.isEmpty())
            service.quiet = false
            SharingScheduleReceiver().onReceive(service, Intent(SharingSchedule.BOUNDARY))
            assertFalse(AppRuntime.controller!!.paused)
            assertTrue(service.io.isEmpty())
        } finally { lifecycle.destroy() }
    }

    @Test fun sampleBroadcastWakesExistingSourceButNeverDuringNightOrAfterStop() {
        val lifecycle = Robolectric.buildService(TestService::class.java).create()
        val service = lifecycle.get()
        CredentialStore(service).consent = true
        shadowOf(service.application).grantPermissions(Manifest.permission.ACCESS_FINE_LOCATION)
        try {
            service.onStartCommand(Intent().setAction(SharingService.START), 0, 1)
            service.io.removeFirst().run()
            WakeWorkReceiver().onReceive(service, Intent(WakeupTimer.SAMPLE))
            assertEquals(1, service.sampleWakes)
            assertTrue(service.io.isEmpty())
            service.quiet = true
            WakeWorkReceiver().onReceive(service, Intent(WakeupTimer.SAMPLE))
            assertTrue(AppRuntime.controller!!.paused)
            assertEquals(1, service.sampleWakes)
            service.onStartCommand(Intent().setAction(SharingService.STOP), 0, 2)
            service.quiet = false
            WakeWorkReceiver().onReceive(service, Intent(WakeupTimer.SAMPLE))
            assertFalse(AppRuntime.active)
            assertEquals(1, service.sampleWakes)
        } finally { lifecycle.destroy() }
    }

    // No SDK or network needed: leave IO queued so lifecycle transitions can be inspected.
    class TestService : SharingService() {
        val io = ArrayDeque<Runnable>()
        var quiet = false
        internal override fun isQuietNow() = quiet
        var failStart = false
        var failStop = false
        var failRead = false
        var missingCredentials = false
        var sampleWakes = 0
        internal override fun readCredentials(): Credentials? {
            if (failRead) throw java.security.KeyStoreException("temporarily unavailable")
            return if (missingCredentials) null else Credentials("http://localhost", "test", "test")
        }
        internal override fun amapConfigured() = true
        internal override fun createController(remote: SharingRemote, source: PositionSource,
            status: (String) -> Unit, activeChanged: (Boolean) -> Unit, workChanged: () -> Unit) =
            SharingController(object : SharingRemote {
                override fun start(): String { if (failStart) throw java.io.IOException(); return "test-session" }
                override fun stop(session: String) { if (failStop) throw java.io.IOException() }
                override fun upload(point: com.jk.family.core.Upload) = true
                override fun cancel() {}
            }, object : PositionSource {
                override fun start(onFix: (com.jk.family.core.Fix) -> Unit, onError: (String) -> Unit) {}
                override fun interval(millis: Long) {}
                override fun stop() {}
                override fun wake() { sampleWakes++ }
            }, { io.add(it) }, { it.run() }, android.os.SystemClock::elapsedRealtime, status, activeChanged, { quiet }, workChanged)
    }

    @Test fun freshNullIntentDoesNotStartSharing() {
        val lifecycle = Robolectric.buildService(TestService::class.java).create()
        val service = lifecycle.get()
        try {
            assertEquals(Service.START_NOT_STICKY, service.onStartCommand(null, 0, 1))
            assertFalse(AppRuntime.active)
        } finally { lifecycle.destroy() }
    }

    @Test fun temporaryRestoreFailureDoesNotEraseTheUsersChoice() {
        val lifecycle = Robolectric.buildService(TestService::class.java).create()
        val service = lifecycle.get()
        CredentialStore(service).apply { consent = true; sharingRequested = true }
        shadowOf(service.application).grantPermissions(Manifest.permission.ACCESS_FINE_LOCATION)
        service.failRead = true
        try {
            assertEquals(Service.START_NOT_STICKY, service.onStartCommand(null, 0, 1))
            assertTrue("a temporary startup failure is not an explicit stop", CredentialStore(service).sharingRequested)
            assertFalse(AppRuntime.active)
        } finally { lifecycle.destroy() }
        val restored = Robolectric.buildService(TestService::class.java).create()
        try {
            assertEquals(Service.START_STICKY, restored.get().onStartCommand(Intent().setAction("com.jk.family.RESUME"), 0, 2))
            assertTrue(AppRuntime.active)
        } finally { restored.destroy() }
    }

    @Test fun missingPairingNeverRestoresEvenWithStaleSavedIntent() {
        val lifecycle = Robolectric.buildService(TestService::class.java).create()
        val service = lifecycle.get()
        CredentialStore(service).apply { consent = true; sharingRequested = true }
        shadowOf(service.application).grantPermissions(Manifest.permission.ACCESS_FINE_LOCATION)
        service.missingCredentials = true
        try {
            assertEquals(Service.START_NOT_STICKY, service.onStartCommand(null, 0, 1))
            assertFalse(CredentialStore(service).sharingRequested)
            assertFalse(AppRuntime.active)
        } finally { lifecycle.destroy() }
    }

    @Test fun stickyRestartRequiresPriorStartAndExplicitStopDisablesIt() {
        val lifecycle = Robolectric.buildService(TestService::class.java).create()
        val service = lifecycle.get()
        CredentialStore(service).consent = true
        shadowOf(service.application).grantPermissions(Manifest.permission.ACCESS_FINE_LOCATION)
        try {
            assertEquals(Service.START_STICKY, service.onStartCommand(Intent(service, SharingService::class.java).setAction(SharingService.START), 0, 1))
            assertTrue(AppRuntime.active)
            assertTrue(service.getSharedPreferences("device", Context.MODE_PRIVATE).getBoolean("sharing_requested", false))
        } finally { lifecycle.destroy() }
        assertFalse(AppRuntime.active)
        val restored = Robolectric.buildService(TestService::class.java).create()
        try {
            assertEquals(Service.START_STICKY, restored.get().onStartCommand(null, 0, 2))
            assertTrue(AppRuntime.active)
            assertEquals(Service.START_NOT_STICKY, restored.get().onStartCommand(Intent().setAction(SharingService.STOP), 0, 3))
            assertFalse(AppRuntime.active)
            assertFalse(restored.get().getSharedPreferences("device", Context.MODE_PRIVATE).getBoolean("sharing_requested", true))
            assertEquals(Service.START_NOT_STICKY, restored.get().onStartCommand(null, 0, 4))
        } finally { restored.destroy() }
    }

    @Test fun revokedPermissionPreventsRestoration() {
        val lifecycle = Robolectric.buildService(TestService::class.java).create()
        val service = lifecycle.get()
        CredentialStore(service).consent = true
        service.getSharedPreferences("device", Context.MODE_PRIVATE).edit().putBoolean("sharing_requested", true).commit()
        try {
            assertEquals(Service.START_NOT_STICKY, service.onStartCommand(null, 0, 1))
            assertFalse(service.getSharedPreferences("device", Context.MODE_PRIVATE).getBoolean("sharing_requested", true))
            assertFalse(AppRuntime.active)
            shadowOf(Looper.getMainLooper()).idle()
        } finally { lifecycle.destroy() }
    }
    @Test fun stopCompletionIsShownAfterServiceDestroy() {
        for (fail in listOf(false, true)) {
            val lifecycle = Robolectric.buildService(TestService::class.java).create()
            val service = lifecycle.get()
            CredentialStore(service).consent = true
            shadowOf(service.application).grantPermissions(Manifest.permission.ACCESS_FINE_LOCATION)
            service.onStartCommand(Intent().setAction(SharingService.START), 0, 1)
            service.io.removeFirst().run() // start success
            service.failStop = fail
            service.onStartCommand(Intent().setAction(SharingService.STOP), 0, 2)
            lifecycle.destroy()
            service.io.removeFirst().run() // async stop completion after teardown
            assertTrue(AppRuntime.message, AppRuntime.message.contains(if (fail) "未确认服务器清除" else "服务器位置已清除"))
        }
    }

    @Test fun lateStopCannotOverwriteNewCredentialsStatus() {
        val lifecycle = Robolectric.buildService(TestService::class.java).create()
        val service = lifecycle.get()
        CredentialStore(service).consent = true
        shadowOf(service.application).grantPermissions(Manifest.permission.ACCESS_FINE_LOCATION)
        service.onStartCommand(Intent().setAction(SharingService.START), 0, 1)
        service.io.removeFirst().run()
        service.onStartCommand(Intent().setAction(SharingService.STOP), 0, 2)
        lifecycle.destroy()
        AppRuntime.credentialsRevision++
        AppRuntime.message = "已解除本机配对，请在网页撤销原设备。"
        service.io.removeFirst().run()
        assertEquals("已解除本机配对，请在网页撤销原设备。", AppRuntime.message)
    }

    @Test fun lateStopCannotOverwriteNewSharingStatus() {
        val old = Robolectric.buildService(TestService::class.java).create()
        CredentialStore(old.get()).consent = true
        shadowOf(old.get().application).grantPermissions(Manifest.permission.ACCESS_FINE_LOCATION)
        old.get().onStartCommand(Intent().setAction(SharingService.START), 0, 1)
        old.get().io.removeFirst().run()
        old.get().onStartCommand(Intent().setAction(SharingService.STOP), 0, 2)
        old.destroy()
        val next = Robolectric.buildService(TestService::class.java).create()
        try {
            next.get().onStartCommand(Intent().setAction(SharingService.START), 0, 3)
            val message = AppRuntime.message
            old.get().io.removeFirst().run()
            assertEquals(message, AppRuntime.message)
            assertTrue(AppRuntime.active)
        } finally { next.destroy() }
    }

}
