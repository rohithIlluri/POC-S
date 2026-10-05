package dev.dronelink.bridge

import fi.iki.elonen.NanoHTTPD
import org.json.JSONObject
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * HTTP contract (same as dronelink/mockbridge.py):
 *   GET  /status  -> {"phase","altitude_m","battery_pct","gps_ok","remote_enabled"}
 *   POST /power   {"state":"on"|"off"}
 * Every request needs X-Bridge-Token == the PIN on screen. Takeoff additionally needs the on-screen
 * "Allow remote flight" switch, a GPS lock and battery >= 30%. Landing is always allowed.
 */
class BridgeServer(
    port: Int,
    private val drone: DroneAdapter,
    private val token: String,
    private val remoteEnabled: () -> Boolean,
) : NanoHTTPD(port) {

    private fun json(code: Response.Status, o: JSONObject) =
        newFixedLengthResponse(code, "application/json", o.toString())

    private fun err(code: Response.Status, msg: String) = json(code, JSONObject().put("error", msg))

    private fun statusJson(): JSONObject = drone.status().let {
        JSONObject().put("phase", it.phase).put("altitude_m", it.altitudeM).put("battery_pct", it.batteryPct)
            .put("gps_ok", it.gpsOk).put("remote_enabled", remoteEnabled()).put("connected", it.connected)
    }

    override fun serve(session: IHTTPSession): Response {
        if (session.headers["x-bridge-token"] != token) return err(Response.Status.UNAUTHORIZED, "bad or missing token")
        return when {
            session.method == Method.GET && session.uri == "/status" -> json(Response.Status.OK, statusJson())
            session.method == Method.POST && session.uri == "/power" -> power(session)
            else -> err(Response.Status.NOT_FOUND, "not found")
        }
    }

    private fun power(session: IHTTPSession): Response {
        val files = HashMap<String, String>()
        session.parseBody(files)
        val state = try { JSONObject(files["postData"] ?: "{}").optString("state") } catch (e: Exception) { "" }
        val latch = CountDownLatch(1)
        var failure: String? = null
        when (state) {
            "on" -> {
                val s = drone.status()
                if (!remoteEnabled()) return err(Response.Status.FORBIDDEN, "remote commands are disabled on the phone")
                if (!s.connected) return err(Response.Status.CONFLICT, "aircraft not connected")
                if (!s.gpsOk) return err(Response.Status.CONFLICT, "no GPS lock")
                if (s.batteryPct < 30) return err(Response.Status.CONFLICT, "battery below 30%")
                if (s.phase != "LANDED") return err(Response.Status.CONFLICT, "already ${s.phase}")
                drone.takeoff { failure = it; latch.countDown() }
            }
            "off" -> drone.land { failure = it; latch.countDown() }
            else -> return err(Response.Status.BAD_REQUEST, "state must be 'on' or 'off'")
        }
        if (!latch.await(6, TimeUnit.SECONDS)) return err(Response.Status.INTERNAL_ERROR, "aircraft did not answer in time")
        return failure?.let { err(Response.Status.INTERNAL_ERROR, it) } ?: json(Response.Status.OK, statusJson())
    }
}
