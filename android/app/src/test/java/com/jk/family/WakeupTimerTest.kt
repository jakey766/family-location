package com.jk.family

import android.Manifest
import android.app.AlarmManager
import android.content.Intent
import android.os.Build
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.robolectric.annotation.LooperMode
import org.robolectric.shadows.ShadowAlarmManager

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [25, 33])
@LooperMode(LooperMode.Mode.PAUSED)
class WakeupTimerTest {
    private val app = RuntimeEnvironment.getApplication()
    private val alarms get() = shadowOf(app.getSystemService(AlarmManager::class.java)).scheduledAlarms

    @Test fun elapsedAlarmAndDuplicateDeliveryUseSameDeadline() {
        var now = 1000L
        var calls = 0
        val timer = WakeupTimer(app, WakeupTimer.SAMPLE, { now }) { calls++ }
        timer.schedule(61_000)
        timer.schedule(61_000)
        assertEquals(1, alarms.size)
        assertEquals(AlarmManager.ELAPSED_REALTIME_WAKEUP, alarms.single().type)
        assertEquals(61_000L, alarms.single().triggerAtTime)
        timer.fire()
        assertEquals(0, calls)
        now = 61_000
        timer.fire(); timer.fire()
        assertEquals(1, calls)
        assertTrue(alarms.isEmpty())
    }

    @Test fun missingExactPermissionStillSchedulesWakeupAndCancelRemovesIt() {
        if (Build.VERSION.SDK_INT >= 31) ShadowAlarmManager.setCanScheduleExactAlarms(false)
        val timer = WakeupTimer(app, WakeupTimer.SAMPLE, { 1000L }) {}
        timer.schedule(61_000)
        assertEquals(1, alarms.size)
        assertEquals(AlarmManager.ELAPSED_REALTIME_WAKEUP, alarms.single().type)
        timer.cancel()
        assertTrue(alarms.isEmpty())
    }

    @Test fun stoppedChoiceCancelsBothAlarmsAndLateBroadcastCannotResume() {
        val sample = WakeupTimer(app, WakeupTimer.SAMPLE, { 1000L }) {}
        val retry = WakeupTimer(app, WakeupTimer.RETRY, { 1000L }) {}
        sample.schedule(61_000); retry.schedule(16_000)
        assertEquals(2, alarms.size)
        CredentialStore(app).sharingRequested = false
        WakeWorkReceiver().onReceive(app, Intent(WakeupTimer.SAMPLE))
        assertTrue(alarms.isEmpty())
        assertNull(shadowOf(app).nextStartedService)
        sample.cancel(); retry.cancel()
    }

    @Test fun manifestWakeReceiverRecoversOnlySavedAuthorizedSharing() {
        CredentialStore(app).apply { sharingRequested = true; consent = true }
        shadowOf(app).grantPermissions(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_BACKGROUND_LOCATION)
        val intent = Intent(WakeupTimer.SAMPLE).setPackage(app.packageName)
        val matches = app.packageManager.queryBroadcastReceivers(intent, 0)
        assertEquals(1, matches.size)
        assertFalse(matches.single().activityInfo.exported)
        WakeWorkReceiver().onReceive(app, intent)
        assertEquals(SharingService.RESUME, shadowOf(app).nextStartedService?.action)
    }
}
