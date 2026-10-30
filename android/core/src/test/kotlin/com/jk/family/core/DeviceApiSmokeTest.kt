package com.jk.family.core

import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test

/** Only the isolated backend harness supplies these environment variables. */
class DeviceApiSmokeTest {
    @Test fun isolatedBackendContract() {
        val origin = System.getenv("ANDROID_SMOKE_ORIGIN")
        assumeTrue("Run via the isolated Android smoke harness", origin != null)
        val pairingId = requireNotNull(System.getenv("ANDROID_SMOKE_PAIRING_ID"))
        val pairingCode = requireNotNull(System.getenv("ANDROID_SMOKE_PAIRING_CODE"))
        val paired = DeviceApi(origin!!).pair(pairingId, pairingCode, "Android core 模拟设备")
        val api = DeviceApi(origin, paired.token)
        val session = api.start()
        try {
            val state = SharingState()
            state.started(state.begin(), session)
            val first = state.capture(Fix(30.0, 120.0, 8.0, System.currentTimeMillis() - 2_000, 80))!!
            val accepted = api.uploadResult(first)
            assertTrue(accepted.accepted)
            assertEquals(DeviceConfig(), accepted.config)
            val duplicate = api.uploadResult(first)
            assertFalse(duplicate.accepted)
            assertEquals(DeviceConfig(), duplicate.config)
            val second = state.capture(first.fix.copy(capturedAtMillis = first.fix.capturedAtMillis + 1_000, batteryPercent = null))!!
            assertTrue(api.upload(second))
            assertEquals("out_of_order", assertThrows(ApiException::class.java) { api.upload(first) }.code)
        } finally { api.stop(session) }
        val stoppedPoint = Upload(session, 3, Fix(30.0, 120.0, 8.0, System.currentTimeMillis(), 80))
        assertEquals("sharing_session_conflict", assertThrows(ApiException::class.java) { api.upload(stoppedPoint) }.code)
    }
}
