package com.jk.family

import android.content.Context
import com.jk.family.core.DeviceConfig
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [25, 33])
class DeviceConfigStoreTest {
    private val context = RuntimeEnvironment.getApplication()
    private val prefs get() = context.getSharedPreferences("device", Context.MODE_PRIVATE)
    @Test fun persistedConfigSurvivesNewStoreButIsBoundToCredentialAndClearedOnUnpair() {
        prefs.edit().putString("credential", "encrypted-identity-a").commit()
        val store = CredentialStore(context)
        val binding = store.configBinding()
        val config = DeviceConfig(60, 600, true, "12:15", "13:45", 3)
        assertTrue(store.saveConfig(config, binding))
        assertEquals(config, CredentialStore(context).readConfig())
        assertFalse(store.saveConfig(DeviceConfig(version = 2), binding))
        prefs.edit().putString("credential", "encrypted-identity-b").commit()
        assertEquals(DeviceConfig(), store.readConfig())
        assertFalse(store.saveConfig(config.copy(version = 4), binding))
        store.clear()
        assertEquals(DeviceConfig(), store.readConfig())
        assertNull(store.configBinding())
        assertFalse(prefs.contains("device_config"))
    }
    @Test fun commitFailureWithUpdatedMemoryRetriesSameVersionToDisk() {
        val memory = context.getSharedPreferences("config-write-memory", Context.MODE_PRIVATE)
        val disk = context.getSharedPreferences("config-write-disk", Context.MODE_PRIVATE)
        memory.edit().putString("credential", "encrypted-identity").commit()
        disk.edit().putString("credential", "encrypted-identity").commit()
        var fail = true
        var writes = 0
        val failingPrefs = object : android.content.SharedPreferences by memory {
            override fun edit(): android.content.SharedPreferences.Editor {
                val edit = memory.edit()
                return object : android.content.SharedPreferences.Editor by edit {
                    override fun putString(key: String?, value: String?): android.content.SharedPreferences.Editor {
                        edit.putString(key, value); return this
                    }
                    override fun commit(): Boolean {
                        writes++
                        edit.commit() // Android updates its in-process values even when disk commit fails.
                        if (fail) return false
                        disk.edit().putString("device_config", memory.getString("device_config", null))
                            .putString("device_config_binding", memory.getString("device_config_binding", null)).commit()
                        return true
                    }
                }
            }
        }
        fun backed(prefs: android.content.SharedPreferences) = CredentialStore(object : android.content.ContextWrapper(context) {
            override fun getSharedPreferences(name: String?, mode: Int) = prefs
        })
        val store = backed(failingPrefs)
        val config = DeviceConfig(60, 600, false, "12:15", "13:45", 2)
        assertFalse(store.saveConfig(config, store.configBinding()))
        assertEquals(config, store.readConfig())
        assertEquals("a process restart still sees the old disk state", DeviceConfig(), backed(disk).readConfig())
        fail = false
        assertTrue("matching in-memory values do not prove a previous commit succeeded", store.saveConfig(config, store.configBinding()))
        assertEquals(2, writes)
        assertEquals(config, backed(disk).readConfig())
    }

    @Test fun malformedPersistenceUsesDefaultsAndUnpairedCannotSaveConfig() {
        val store = CredentialStore(context)
        assertFalse(store.saveConfig(DeviceConfig(version = 3), null))
        prefs.edit().putString("credential", "encrypted-identity").putString("device_config_binding", "encrypted-identity")
            .putString("device_config", "{}").commit()
        assertEquals(DeviceConfig(), store.readConfig())
    }
}
