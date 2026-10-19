package com.jk.family.core

import org.junit.Assert.*
import org.junit.Test

class RetryBackoffTest {
    @Test fun failuresBackOffAndCapAtFiveMinutes() {
        val retry = RetryBackoff()
        var now = 1000L
        for (delay in listOf(15000L, 30000L, 60000L, 120000L, 240000L, 300000L, 300000L)) {
            retry.failed(now)
            assertEquals(delay, retry.remaining(now))
            assertEquals(1L, retry.remaining(now + delay - 1))
            now += delay
            assertEquals(0L, retry.remaining(now))
        }
        retry.reset()
        retry.failed(now)
        assertEquals(15000L, retry.remaining(now))
    }
}
