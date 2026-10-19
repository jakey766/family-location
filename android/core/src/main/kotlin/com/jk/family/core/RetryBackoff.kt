package com.jk.family.core

/** Monotonic clock only; new location callbacks must not reset a failed attempt. */
class RetryBackoff {
    private var delay = 15_000L
    private var due = 0L
    fun failed(now: Long) {
        due = now + delay
        delay = (delay * 2).coerceAtMost(300_000L)
    }
    fun remaining(now: Long) = (due - now).coerceAtLeast(0)
    fun reset() { delay = 15_000L; due = 0 }
}
