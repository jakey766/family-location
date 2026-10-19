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
    // No SDK or network needed: leave IO queued so lifecycle transitions can be inspected.
    class TestService : SharingService() {
        val io = ArrayDeque<Runnable>()
        var failStop = false
        internal override fun readCredentials() = Credentials("http://localhost", "test", "test")
        internal override fun amapConfigured() = true
        internal override fun createController(remote: SharingRemote, source: PositionSource,
            status: (String) -> Unit, activeChanged: (Boolean) -> Unit) =
            SharingController(object : SharingRemote {
                override fun start() = "test-session"
                override fun stop(session: String) { if (failStop) throw java.io.IOException() }
                override fun upload(point: com.jk.family.core.Upload) = true
                override fun cancel() {}
            }, object : PositionSource {
                override fun start(onFix: (com.jk.family.core.Fix) -> Unit, onError: (String) -> Unit) {}
                override fun interval(millis: Long) {}
                override fun stop() {}
            }, { io.add(it) }, { it.run() }, { 0L }, status, activeChanged)
    }

    @Test fun freshNullIntentDoesNotStartSharing() {
        val lifecycle = Robolectric.buildService(TestService::class.java).create()
        val service = lifecycle.get()
        try {
            assertEquals(Service.START_NOT_STICKY, service.onStartCommand(null, 0, 1))
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
