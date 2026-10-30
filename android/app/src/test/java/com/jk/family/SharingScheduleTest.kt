package com.jk.family

import android.Manifest
import android.app.AlarmManager
import android.content.Intent
import android.os.Build
import com.jk.family.core.QuietHours
import java.time.Instant
import java.time.ZoneId
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [25, 33])
class SharingScheduleTest {
    private val app get() = RuntimeEnvironment.getApplication()
    private val alarms get() = app.getSystemService(AlarmManager::class.java)

    @Test fun schedulesOnlyNextLocalBoundaryAndStopCancelsIt() {
        CredentialStore(app).sharingRequested = true
        val now = Instant.parse("2026-10-19T13:00:00Z").toEpochMilli()
        val zone = ZoneId.of("Asia/Shanghai")
        SharingSchedule.schedule(app, now, zone)
        val alarm = shadowOf(alarms).scheduledAlarms.single()
        assertEquals(AlarmManager.RTC_WAKEUP, alarm.type)
        assertEquals(QuietHours.nextBoundary(now, zone), alarm.triggerAtTime)
        assertEquals(SharingSchedule.BOUNDARY, shadowOf(alarm.operation).savedIntent.action)
        SharingSchedule.schedule(app, now + 3600000L, zone)
        assertEquals(1, shadowOf(alarms).scheduledAlarms.size)
        assertEquals(QuietHours.nextBoundary(now + 3600000L, zone), shadowOf(alarms).scheduledAlarms.single().triggerAtTime)
        SharingService.requestStop(app)
        assertTrue(shadowOf(alarms).scheduledAlarms.isEmpty())
    }

    @Test fun persistedMinuteBoundaryAndZoneChangeReplaceExistingAlarm() {
        val prefs = app.getSharedPreferences("device", android.content.Context.MODE_PRIVATE)
        prefs.edit().putString("credential", "encrypted-identity").commit()
        val store = CredentialStore(app)
        store.sharingRequested = true
        val config = com.jk.family.core.DeviceConfig(60, 600, false, "12:15", "13:45", 2)
        assertTrue(store.saveConfig(config, store.configBinding()))
        val now = Instant.parse("2026-10-30T12:00:00Z").toEpochMilli()
        SharingSchedule.schedule(app, now, ZoneId.of("UTC"))
        assertEquals(Instant.parse("2026-10-30T12:15:00Z").toEpochMilli(), shadowOf(alarms).scheduledAlarms.single().triggerAtTime)
        SharingSchedule.schedule(app, now, ZoneId.of("Asia/Shanghai"))
        assertEquals(Instant.parse("2026-10-31T04:15:00Z").toEpochMilli(), shadowOf(alarms).scheduledAlarms.single().triggerAtTime)
        store.sharingRequested = false
        SharingSchedule.schedule(app, now, ZoneId.of("UTC"))
        assertTrue(shadowOf(alarms).scheduledAlarms.isEmpty())
    }

    @Test fun missingExactAlarmPermissionFallsBackWithoutEnablingSharing() {
        if (Build.VERSION.SDK_INT >= 31) org.robolectric.shadows.ShadowAlarmManager.setCanScheduleExactAlarms(false)
        SharingSchedule.schedule(app)
        assertTrue(shadowOf(alarms).scheduledAlarms.isEmpty())
        CredentialStore(app).sharingRequested = true
        SharingSchedule.schedule(app)
        assertEquals(1, shadowOf(alarms).scheduledAlarms.size)
        assertEquals(Build.VERSION.SDK_INT < 31, SharingSchedule.canScheduleExactly(app))
    }

    @Test fun boundaryAndClockChangesResumeOnlySavedChoice() {
        CredentialStore(app).apply { consent = true; sharingRequested = true }
        shadowOf(app).grantPermissions(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_BACKGROUND_LOCATION)
        for (action in listOf(SharingSchedule.BOUNDARY, Intent.ACTION_TIME_CHANGED, Intent.ACTION_TIMEZONE_CHANGED,
            AlarmManager.ACTION_SCHEDULE_EXACT_ALARM_PERMISSION_STATE_CHANGED)) {
            val intent = Intent(action).setPackage(app.packageName)
            val matches = app.packageManager.queryBroadcastReceivers(intent, 0)
            assertEquals(1, matches.size)
            val receiver = Class.forName(matches.single().activityInfo.name).getDeclaredConstructor().newInstance() as android.content.BroadcastReceiver
            receiver.onReceive(app, intent)
            assertEquals(SharingService.RESUME, shadowOf(app).nextStartedService?.action)
            assertEquals(1, shadowOf(alarms).scheduledAlarms.size)
            CredentialStore(app).sharingRequested = false
            receiver.onReceive(app, intent)
            assertNull(shadowOf(app).nextStartedService)
            assertTrue(shadowOf(alarms).scheduledAlarms.isEmpty())
            CredentialStore(app).sharingRequested = true
        }
    }
}
