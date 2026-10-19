package com.jk.family

import com.jk.family.core.DeviceApi
import org.json.JSONObject

internal data class PairingInfo(val origin: String, val id: String, val code: String) {
    companion object {
        // Parse all fields before updating the form; never stringify numeric codes and lose zeros.
        fun parse(text: CharSequence?): PairingInfo? = runCatching {
            require(text != null && text.length in 1..4096)
            val json = JSONObject(text.toString())
            val origin = DeviceApi.normalizeOrigin(json.get("server_url") as String)
            val id = (json.get("pairing_id") as String).trim()
            val code = (json.get("code") as String).trim()
            require(id.matches(Regex("[A-Za-z0-9_-]{1,128}")))
            require(code.matches(Regex("[0-9]{8}")))
            PairingInfo(origin, id, code)
        }.getOrNull()
    }
}
