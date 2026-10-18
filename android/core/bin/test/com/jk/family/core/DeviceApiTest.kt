package com.jk.family.core

import com.google.gson.JsonParser
import com.sun.net.httpserver.HttpExchange
import com.sun.net.httpserver.HttpServer
import org.junit.Assert.*
import org.junit.After
import org.junit.Before
import org.junit.Test
import java.net.CookieHandler
import java.net.CookieManager
import java.net.HttpCookie
import java.net.InetSocketAddress
import java.net.URI
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.CountDownLatch

class DeviceApiTest {
    private lateinit var server: HttpServer
    private lateinit var origin: String
    private val executor = Executors.newCachedThreadPool()

    @Before fun setup() {
        server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.executor = executor
        server.start()
        origin = "http://127.0.0.1:${server.address.port}"
    }

    @After fun cleanup() { server.stop(0); executor.shutdownNow() }

    private fun HttpExchange.reply(status: Int, body: String) {
        responseHeaders.set("Content-Type", "application/json")
        val bytes = body.toByteArray()
        sendResponseHeaders(status, if (status == 204) -1 else bytes.size.toLong())
        if (status != 204) responseBody.use { it.write(bytes) }
        close()
    }

    @Test fun originAcceptsOnlyHttpOrigins() {
        assertEquals("http://example.com:8080", DeviceApi.normalizeOrigin(" http://EXAMPLE.com:8080/ "))
        assertEquals("https://example.com", DeviceApi.normalizeOrigin("https://example.com"))
        for (bad in listOf("file:///tmp/a", "http://u:p@host", "http://host/a", "http://host?x", "http://host#x",
            "http://host:0", "http://host:65536", "http://host:bad", "http://host:", "http:///host", "https://")) {
            assertThrows("accepted invalid origin", IllegalArgumentException::class.java) { DeviceApi.normalizeOrigin(bad) }
        }
    }

    @Test fun pairingPreservesLeadingZeroAndNeverUsesAdminCookiesOrBearer() {
        var body = ""
        var cookie: String? = null
        var authorization: String? = null
        server.createContext("/api/v1/devices/pair") {
            body = it.requestBody.bufferedReader().readText()
            cookie = it.requestHeaders.getFirst("Cookie")
            authorization = it.requestHeaders.getFirst("Authorization")
            it.reply(201, """{"device":{"id":"id","name":"家人"},"device_token":"new-device-token"}""")
        }
        val previous = CookieHandler.getDefault()
        val manager = CookieManager()
        manager.cookieStore.add(URI(origin), HttpCookie("family_session", "admin-secret"))
        CookieHandler.setDefault(manager)
        try {
            val paired = DeviceApi(origin, "existing-device-token").pair("pairing", "00123456", "家人")
            assertEquals("new-device-token", paired.token)
            assertEquals("id", paired.deviceId)
            assertEquals("家人", paired.deviceName)
            assertEquals("00123456", JsonParser.parseString(body).asJsonObject["code"].asString)
            assertNull(cookie)
            assertNull(authorization)
        } finally { CookieHandler.setDefault(previous) }
    }

    @Test fun deviceRequestsKeepOriginalTimeSequenceAndUseOnlyDeviceBearer() {
        val requests = mutableListOf<Pair<String, String>>()
        server.createContext("/api/v1/device/") {
            assertEquals("Bearer device-token", it.requestHeaders.getFirst("Authorization"))
            assertNull(it.requestHeaders.getFirst("Cookie"))
            assertEquals("application/json; charset=utf-8", it.requestHeaders.getFirst("Content-Type"))
            requests += it.requestMethod to it.requestBody.bufferedReader().readText()
            when (it.requestURI.path) {
                "/api/v1/device/sharing/start" -> it.reply(201, """{"sharing_session_id":"session"}""")
                "/api/v1/device/location" -> it.reply(200, """{"accepted":false}""")
                else -> it.reply(204, "")
            }
        }
        val api = DeviceApi(origin, "device-token")
        assertEquals("session", api.start())
        val upload = Upload("session", 7, Fix(30.0, 120.0, 6.5, 1_700_000_000_123L, null))
        assertFalse(api.upload(upload))
        assertFalse(api.upload(upload))
        api.stop("session")
        assertEquals(listOf("POST", "PUT", "PUT", "POST"), requests.map { it.first })
        assertEquals(requests[1].second, requests[2].second)
        val json = JsonParser.parseString(requests[1].second).asJsonObject
        assertEquals(7L, json["sequence"].asLong)
        assertEquals("2023-11-14T22:13:20.123Z", json["captured_at"].asString)
        assertEquals("GCJ-02", json["coordinate_system"].asString)
        assertTrue(json["battery_percent"].isJsonNull)
    }

    @Test fun refusesRedirectAndNeverLeaksBearerToRedirectDestination() {
        var redirected = false
        server.createContext("/api/v1/device/sharing/start") {
            it.responseHeaders.set("Location", "$origin/redirected")
            it.reply(307, "")
        }
        server.createContext("/redirected") { redirected = true; it.reply(201, "{}") }
        val error = assertThrows(ApiException::class.java) { DeviceApi(origin, "secret").start() }
        assertEquals(307, error.status)
        assertFalse(redirected)
    }

    @Test fun errorsExposeOnlyAllowlistedCodesWithoutResponseOrCredential() {
        var response = """{"error":"invalid_pairing"}"""
        server.createContext("/api/v1/devices/pair") { it.reply(400, response) }
        val api = DeviceApi(origin)
        assertEquals("invalid_pairing", assertThrows(ApiException::class.java) { api.pair("id", "00000000", "n") }.code)
        response = """{"error":"secret-token-and-location"}"""
        val error = assertThrows(ApiException::class.java) { api.pair("id", "00000000", "n") }
        assertEquals("http_error", error.code)
        assertFalse(error.toString().contains("secret-token"))
        assertNull(error.cause)
        assertFalse(error.toString().contains(origin))
    }

    @Test fun malformedOrOversizedResponsesAreRejected() {
        var response = """{"sharing_session_id":null}"""
        server.createContext("/api/v1/device/sharing/start") { it.reply(201, response) }
        val api = DeviceApi(origin, "token")
        assertEquals("invalid_response", assertThrows(ApiException::class.java) { api.start() }.code)
        response = "x".repeat(65_537)
        assertEquals("response_too_large", assertThrows(ApiException::class.java) { api.start() }.code)
    }

    @Test fun requestTimesOutWithoutRetainingSensitiveCause() {
        server.createContext("/api/v1/device/sharing/start") {
            try { Thread.sleep(12_000); it.reply(201, "{}") } catch (_: Exception) { it.close() }
        }
        val started = System.nanoTime()
        val error = assertThrows(ApiException::class.java) { DeviceApi(origin, "token").start() }
        assertEquals("network_error", error.code)
        assertEquals(0, error.status)
        assertNull(error.cause)
        assertTrue(TimeUnit.NANOSECONDS.toSeconds(System.nanoTime() - started) < 12)
    }

    @Test fun cancelInterruptsInFlightRequest() {
        val entered = CountDownLatch(1)
        server.createContext("/api/v1/device/sharing/start") {
            entered.countDown()
            try { Thread.sleep(12_000) } catch (_: InterruptedException) { }
            it.close()
        }
        val api = DeviceApi(origin, "token")
        val request = executor.submit<ApiException> { assertThrows(ApiException::class.java) { api.start() } }
        assertTrue(entered.await(2, TimeUnit.SECONDS))
        api.cancel()
        assertEquals("network_error", request.get(2, TimeUnit.SECONDS).code)
    }
}
