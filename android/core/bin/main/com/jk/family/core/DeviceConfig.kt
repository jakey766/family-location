package com.jk.family.core

import com.google.gson.JsonElement
import com.google.gson.JsonObject
import com.google.gson.JsonParser

/** A complete validated snapshot; never apply individual fields from a malformed response. */
data class DeviceConfig(
    val movingIntervalSeconds: Int = 120,
    val stationaryIntervalSeconds: Int = 300,
    val movingHighAccuracy: Boolean = false,
    val quietHoursStart: String = "22:00",
    val quietHoursEnd: String = "08:00",
    val version: Long = 1,
) {
    init {
        require(movingIntervalSeconds in 60..3600 && movingIntervalSeconds % 60 == 0)
        require(stationaryIntervalSeconds in movingIntervalSeconds..3600 && stationaryIntervalSeconds % 60 == 0)
        require(TIME.matches(quietHoursStart) && TIME.matches(quietHoursEnd) && quietHoursStart != quietHoursEnd)
        require(version > 0)
    }
    fun intervalMillis(moving: Boolean) = (if (moving) movingIntervalSeconds else stationaryIntervalSeconds) * 1000L
    fun encode(): String = toJson().toString()
    fun toJson(): JsonObject = JsonObject().apply {
        addProperty("moving_interval_seconds", movingIntervalSeconds)
        addProperty("stationary_interval_seconds", stationaryIntervalSeconds)
        addProperty("moving_high_accuracy", movingHighAccuracy)
        addProperty("quiet_hours_start", quietHoursStart)
        addProperty("quiet_hours_end", quietHoursEnd)
        addProperty("version", version)
    }
    companion object {
        private val TIME = Regex("(?:[01][0-9]|2[0-3]):[0-5][0-9]")
        fun decode(value: String?): DeviceConfig? = runCatching { parse(JsonParser.parseString(value)) }.getOrNull()
        fun parse(value: JsonElement?): DeviceConfig? = runCatching {
            val json = value!!.asJsonObject
            fun integer(key: String): Long {
                val p = json[key].asJsonPrimitive
                require(p.isNumber && Regex("[0-9]+").matches(p.toString()))
                return p.asString.toLong()
            }
            fun string(key: String): String { val p = json[key].asJsonPrimitive; require(p.isString); return p.asString }
            val moving = integer("moving_interval_seconds")
            val still = integer("stationary_interval_seconds")
            require(moving in 60..3600 && still in 60..3600)
            val accuracy = json["moving_high_accuracy"].asJsonPrimitive
            require(accuracy.isBoolean)
            DeviceConfig(moving.toInt(), still.toInt(), accuracy.asBoolean, string("quiet_hours_start"), string("quiet_hours_end"), integer("version"))
        }.getOrNull()
    }
}

data class UploadResult(val accepted: Boolean, val config: DeviceConfig? = null)
