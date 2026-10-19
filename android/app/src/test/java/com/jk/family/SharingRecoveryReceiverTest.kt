package com.jk.family

import android.Manifest
import android.content.BroadcastReceiver
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [25, 33])
class SharingRecoveryReceiverTest {
    private val app get() = RuntimeEnvironment.getApplication()

    // Resolve from the real manifest: missing registration must fail too.
    private fun receiver(action: String): BroadcastReceiver {
        val matches = app.packageManager.queryBroadcastReceivers(Intent(action).setPackage(app.packageName), 0)
        assertEquals("system recovery must have a manifest receiver", 1, matches.size)
        assertFalse("only system broadcasts may reach the receiver", matches.single().activityInfo.exported)
        return Class.forName(matches.single().activityInfo.name).getDeclaredConstructor().newInstance() as BroadcastReceiver
    }

    private fun enableSharing() {
        CredentialStore(app).apply { consent = true; sharingRequested = true }
        shadowOf(app).grantPermissions(Manifest.permission.ACCESS_FINE_LOCATION)
        if (Build.VERSION.SDK_INT >= 29) shadowOf(app).grantPermissions(Manifest.permission.ACCESS_BACKGROUND_LOCATION)
    }

    @Test fun bootAndAppUpgradeResumeOnlyPreviouslyEnabledSharing() {
        val permissions = app.packageManager.getPackageInfo(app.packageName, PackageManager.GET_PERMISSIONS).requestedPermissions.orEmpty().toList()
        assertTrue(permissions.contains(Manifest.permission.RECEIVE_BOOT_COMPLETED))
        for (action in listOf(Intent.ACTION_BOOT_COMPLETED, Intent.ACTION_MY_PACKAGE_REPLACED)) {
            enableSharing()
            val target = receiver(action)
            target.onReceive(app, Intent(action))
            assertEquals("com.jk.family.RESUME", shadowOf(app).nextStartedService?.action)
            CredentialStore(app).sharingRequested = false
            target.onReceive(app, Intent(action))
            assertNull("stop or initial pairing must not auto-enable sharing", shadowOf(app).nextStartedService)
        }
    }

    @Test fun modernBackgroundRecoveryWaitsForBackgroundPermissionButKeepsIntent() {
        enableSharing()
        if (Build.VERSION.SDK_INT >= 29) shadowOf(app).denyPermissions(Manifest.permission.ACCESS_BACKGROUND_LOCATION)
        receiver(Intent.ACTION_BOOT_COMPLETED).onReceive(app, Intent(Intent.ACTION_BOOT_COMPLETED))
        val started = shadowOf(app).nextStartedService
        if (Build.VERSION.SDK_INT >= 29) assertNull(started) else assertNotNull(started)
        assertTrue(CredentialStore(app).sharingRequested)
    }

    @Test fun noConsentOrLocationPermissionNeverRestarts() {
        enableSharing()
        val target = receiver(Intent.ACTION_BOOT_COMPLETED)
        CredentialStore(app).consent = false
        target.onReceive(app, Intent(Intent.ACTION_BOOT_COMPLETED))
        assertNull(shadowOf(app).nextStartedService)
        enableSharing()
        shadowOf(app).denyPermissions(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_COARSE_LOCATION)
        target.onReceive(app, Intent(Intent.ACTION_BOOT_COMPLETED))
        assertNull(shadowOf(app).nextStartedService)
    }

    @Test fun unrelatedBroadcastDoesNotStartService() {
        enableSharing()
        receiver(Intent.ACTION_BOOT_COMPLETED).onReceive(app, Intent(Intent.ACTION_TIME_CHANGED))
        assertNull(shadowOf(app).nextStartedService)
    }
}
