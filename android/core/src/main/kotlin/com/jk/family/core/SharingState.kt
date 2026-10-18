package com.jk.family.core

data class Fix(
    val latitude: Double,
    val longitude: Double,
    val accuracyM: Double,
    val capturedAtMillis: Long,
    val batteryPercent: Int?,
    val speedMps: Float = 0f,
) {
    fun isValid(): Boolean = latitude.isFinite() && latitude in -90.0..90.0 &&
        longitude.isFinite() && longitude in -180.0..180.0 &&
        accuracyM.isFinite() && accuracyM >= 0 && capturedAtMillis > 0 &&
        (batteryPercent == null || batteryPercent in 0..100) && speedMps.isFinite() && speedMps >= 0
}

data class Upload(val sessionId: String, val sequence: Long, val fix: Fix)

/** Owned by one thread (the Android main thread); network workers receive immutable Upload values. */
class SharingState {
    var active: Boolean = false
        private set
    var generation: Long = 0
        private set
    var sessionId: String? = null
        private set
    var pending: Upload? = null
        private set
    private var sequence = 0L
    private var acceptedCapturedAt = Long.MIN_VALUE

    fun begin(): Long {
        clear()
        generation++
        active = true
        return generation
    }

    fun started(generation: Long, sessionId: String): Boolean {
        if (!active || generation != this.generation || this.sessionId != null || sessionId.isBlank()) return false
        this.sessionId = sessionId
        return true
    }

    fun stop(): String? {
        val previous = sessionId
        generation++
        active = false
        clear()
        return previous
    }

    fun capture(fix: Fix): Upload? {
        val session = sessionId ?: return null
        val timeWatermark = maxOf(acceptedCapturedAt, pending?.fix?.capturedAtMillis ?: Long.MIN_VALUE)
        if (!active || !fix.isValid() || fix.capturedAtMillis <= timeWatermark || sequence >= 9_007_199_254_740_991L) return null
        return Upload(session, ++sequence, fix).also { pending = it }
    }

    fun acknowledged(upload: Upload) {
        if (upload.sessionId != sessionId) return
        acceptedCapturedAt = maxOf(acceptedCapturedAt, upload.fix.capturedAtMillis)
        if (pending?.sessionId == upload.sessionId && pending?.sequence == upload.sequence) pending = null
    }

    /** Only for an explicit server rejection, never an uncertain network failure. */
    fun rejected(upload: Upload) {
        if (pending?.sessionId == upload.sessionId && pending?.sequence == upload.sequence) pending = null
    }

    private fun clear() {
        sessionId = null
        pending = null
        sequence = 0
        acceptedCapturedAt = Long.MIN_VALUE
    }
}
