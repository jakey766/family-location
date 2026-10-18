package com.jk.family.core

import com.google.gson.GsonBuilder
import com.google.gson.JsonObject
import com.google.gson.JsonParser
import okhttp3.Call
import okhttp3.CookieJar
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import java.io.ByteArrayOutputStream
import java.io.IOException
import java.net.URI
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.TimeZone
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference

class ApiException(val status: Int, val code: String) : Exception("Device API error ($status, $code)")

data class PairingResult(val deviceId: String, val deviceName: String, val token: String) {
    override fun toString() = "PairingResult(credentials omitted)"
}

/** Blocking API: call serially on a worker. cancel() may be called from any thread. */
class DeviceApi(baseUrl: String, val token: String? = null) {
    val baseUrl = normalizeOrigin(baseUrl)
    private val gson = GsonBuilder().serializeNulls().create()
    private val inFlight = AtomicReference<Call?>()
    private val client = OkHttpClient.Builder()
        .cookieJar(CookieJar.NO_COOKIES)
        .followRedirects(false)
        .followSslRedirects(false)
        .retryOnConnectionFailure(false)
        .callTimeout(10, TimeUnit.SECONDS)
        .connectTimeout(10, TimeUnit.SECONDS)
        .readTimeout(10, TimeUnit.SECONDS)
        .writeTimeout(10, TimeUnit.SECONDS)
        .build()

    fun pair(pairingId: String, code: String, name: String): PairingResult {
        val json = request("POST", "/api/v1/devices/pair", mapOf("pairing_id" to pairingId, "code" to code, "name" to name), 201, false)
        val device = json["device"]?.takeIf { it.isJsonObject }?.asJsonObject ?: throw ApiException(201, "invalid_response")
        return PairingResult(string(device, "id", 201), string(device, "name", 201), string(json, "device_token", 201))
    }

    fun start(): String = string(request("POST", "/api/v1/device/sharing/start", emptyMap<String, String>(), 201), "sharing_session_id", 201)

    fun stop(sessionId: String) {
        request("POST", "/api/v1/device/sharing/stop", mapOf("sharing_session_id" to sessionId), 204)
    }

    fun upload(upload: Upload): Boolean {
        val timestamp = SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss.SSS'Z'", Locale.ROOT).apply {
            timeZone = TimeZone.getTimeZone("UTC")
        }.format(Date(upload.fix.capturedAtMillis))
        val json = request("PUT", "/api/v1/device/location", mapOf(
            "sharing_session_id" to upload.sessionId,
            "sequence" to upload.sequence,
            "latitude" to upload.fix.latitude,
            "longitude" to upload.fix.longitude,
            "coordinate_system" to "GCJ-02",
            "accuracy_m" to upload.fix.accuracyM,
            "battery_percent" to upload.fix.batteryPercent,
            "captured_at" to timestamp,
        ), 200)
        val accepted = json["accepted"]
        if (accepted == null || !accepted.isJsonPrimitive || !accepted.asJsonPrimitive.isBoolean) throw ApiException(200, "invalid_response")
        return accepted.asBoolean
    }

    fun cancel() { inFlight.get()?.cancel() }

    private fun request(method: String, path: String, body: Any, expected: Int, authenticated: Boolean = true): JsonObject {
        val builder = Request.Builder().url(baseUrl + path)
            .header("Accept", "application/json")
            .method(method, gson.toJson(body).toRequestBody("application/json; charset=utf-8".toMediaType()))
        if (authenticated) {
            if (token.isNullOrBlank() || !token.matches(Regex("[A-Za-z0-9_-]+"))) throw ApiException(401, "unauthorized")
            builder.header("Authorization", "Bearer $token")
        }
        val call = client.newCall(builder.build())
        check(inFlight.compareAndSet(null, call)) { "Device API calls must be serialized" }
        try {
            call.execute().use { response ->
                val output = ByteArrayOutputStream()
                response.body?.byteStream()?.use { stream ->
                    val buffer = ByteArray(8192)
                    while (true) {
                        val count = stream.read(buffer)
                        if (count == -1) break
                        if (output.size() + count > MAX_RESPONSE_BYTES) throw ApiException(response.code, "response_too_large")
                        output.write(buffer, 0, count)
                    }
                }
                val json = try {
                    JsonParser.parseString(output.toString("UTF-8")).takeIf { it.isJsonObject }?.asJsonObject
                } catch (_: RuntimeException) { null }
                if (response.code != expected) {
                    val value = json?.get("error")
                    val code = if (value?.isJsonPrimitive == true && value.asJsonPrimitive.isString) value.asString else null
                    throw ApiException(response.code, code?.takeIf { it in ERROR_CODES } ?: "http_error")
                }
                if (expected == 204) return JsonObject()
                return json ?: throw ApiException(response.code, "invalid_response")
            }
        } catch (_: IOException) {
            throw ApiException(0, "network_error")
        } finally {
            inFlight.compareAndSet(call, null)
        }
    }

    private fun string(json: JsonObject, key: String, status: Int): String {
        val value = json[key]
        if (value?.isJsonPrimitive != true || !value.asJsonPrimitive.isString || value.asString.isBlank()) throw ApiException(status, "invalid_response")
        return value.asString
    }

    companion object {
        private const val MAX_RESPONSE_BYTES = 65_536
        private val ERROR_CODES = setOf("invalid_json", "invalid_input", "invalid_pairing", "invalid_credentials", "unauthorized",
            "forbidden", "not_found", "conflict", "body_too_large", "unsupported_media_type", "rate_limited", "internal_error",
            "sharing_session_conflict", "out_of_order")

        fun normalizeOrigin(input: String): String {
            val uri = try { URI(input.trim()) } catch (_: Exception) { throw IllegalArgumentException("Invalid server origin") }
            val scheme = uri.scheme?.lowercase(Locale.ROOT)
            require((scheme == "http" || scheme == "https") && !uri.host.isNullOrBlank() && uri.rawUserInfo == null &&
                (uri.rawPath.isNullOrEmpty() || uri.rawPath == "/") && uri.rawQuery == null && uri.rawFragment == null &&
                (uri.port == -1 || uri.port in 1..65535) && !uri.rawAuthority.endsWith(":")) { "Invalid server origin" }
            return "$scheme://${uri.rawAuthority.lowercase(Locale.ROOT)}"
        }
    }
}
