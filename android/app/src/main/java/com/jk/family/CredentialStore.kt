package com.jk.family

import com.jk.family.core.DeviceConfig
import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec
import org.json.JSONObject

internal data class Credentials(val origin: String, val token: String, val name: String) {
    override fun toString() = "Credentials(omitted)"
}

internal class CredentialStore(context: Context) {
    private val prefs = context.getSharedPreferences("device", Context.MODE_PRIVATE)
    var consent: Boolean
        get() = prefs.getBoolean("privacy_v1", false)
        set(value) { check(prefs.edit().putBoolean("privacy_v1", value).commit()) }

    var sharingRequested: Boolean
        get() = prefs.getBoolean("sharing_requested", false)
        set(value) { check(prefs.edit().putBoolean("sharing_requested", value).commit()) }

    fun read(): Credentials? {
        val encoded = prefs.getString("credential", null) ?: return null
        val bytes = Base64.decode(encoded, Base64.NO_WRAP)
        require(bytes.size > 12)
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, bytes.copyOfRange(0, 12)))
        val json = JSONObject(String(cipher.doFinal(bytes.copyOfRange(12, bytes.size)), Charsets.UTF_8))
        return Credentials(json.getString("origin"), json.getString("token"), json.getString("name"))
    }

    fun save(credentials: Credentials) {
        val plain = JSONObject().put("origin", credentials.origin).put("token", credentials.token).put("name", credentials.name)
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, key())
        val bytes = cipher.iv + cipher.doFinal(plain.toString().toByteArray(Charsets.UTF_8))
        check(prefs.edit().putString("credential", Base64.encodeToString(bytes, Base64.NO_WRAP)).remove("device_config").remove("device_config_binding").commit())
    }

    fun clear() { check(prefs.edit().remove("credential").remove("sharing_requested").remove("device_config").remove("device_config_binding").commit()) }

    // Bind to the opaque encrypted credential, never persist the bearer token outside Keystore encryption.
    fun configBinding(): String? = prefs.getString("credential", null)

    fun readConfig(): DeviceConfig {
        val binding = configBinding() ?: return DeviceConfig()
        if (prefs.getString("device_config_binding", null) != binding) return DeviceConfig()
        return DeviceConfig.decode(prefs.getString("device_config", null)) ?: DeviceConfig()
    }

    fun saveConfig(config: DeviceConfig, expectedBinding: String?): Boolean {
        if (expectedBinding == null || configBinding() != expectedBinding) return false
        val current = readConfig()
        if (config.version < current.version || (config.version == current.version && config != current)) return false
        // commit(false) can still update SharedPreferences memory. Equal values must perform
        // another commit when the controller has no confirmed durable write for this version.
        return prefs.edit().putString("device_config", config.encode())
            .putString("device_config_binding", expectedBinding).commit()
    }

    private fun key(): SecretKey {
        val store = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        (store.getKey("family-device-v1", null) as? SecretKey)?.let { return it }
        return KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore").apply {
            init(KeyGenParameterSpec.Builder("family-device-v1", KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE).build())
        }.generateKey()
    }
}
