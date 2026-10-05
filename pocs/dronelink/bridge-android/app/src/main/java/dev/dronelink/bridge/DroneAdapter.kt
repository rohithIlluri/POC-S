package dev.dronelink.bridge

/** Everything the HTTP layer needs from an aircraft. DJI specifics live in DjiAdapter only. */
data class Status(
    val phase: String,          // LANDED | TAKING_OFF | HOVERING | LANDING | UNKNOWN
    val altitudeM: Double,
    val batteryPct: Int,
    val gpsOk: Boolean,
    val connected: Boolean,
)

interface DroneAdapter {
    fun status(): Status
    /** Callbacks fire on an arbitrary thread; onDone(null) = success, otherwise the error text. */
    fun takeoff(onDone: (String?) -> Unit)
    fun land(onDone: (String?) -> Unit)
    fun startVideo(rtmpUrl: String, onDone: (String?) -> Unit)
    fun stopVideo()
}

/** Pure state machine: derives the phase the web UI shows from raw telemetry + last command. */
class PhaseTracker {
    enum class Cmd { NONE, TAKEOFF, LAND }
    @Volatile var last = Cmd.NONE
    @Volatile var lastAt = 0L

    fun command(c: Cmd) { last = c; lastAt = System.currentTimeMillis() }

    fun phase(connected: Boolean, flying: Boolean, motorsOn: Boolean, altM: Double): String {
        if (!connected) return "UNKNOWN"
        val sinceCmd = System.currentTimeMillis() - lastAt
        return when {
            last == Cmd.LAND && (flying || motorsOn) -> "LANDING"
            !flying && !motorsOn -> if (last == Cmd.TAKEOFF && sinceCmd < 8_000) "TAKING_OFF" else "LANDED"
            last == Cmd.TAKEOFF && altM < 1.0 -> "TAKING_OFF"
            flying -> "HOVERING"   // POC: no movement commands yet, so "flying" means hovering
            else -> "TAKING_OFF"
        }
    }
}
