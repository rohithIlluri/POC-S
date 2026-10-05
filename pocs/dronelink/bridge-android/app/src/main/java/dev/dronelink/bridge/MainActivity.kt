package dev.dronelink.bridge

import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.Gravity
import android.view.WindowManager
import android.widget.Button
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.Switch
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import java.net.Inet4Address
import java.net.NetworkInterface
import java.security.SecureRandom

/** One screen: shows the URL + PIN to give the web UI, the remote-flight switch (always OFF at launch), RTMP controls. */
class MainActivity : AppCompatActivity() {
    private val ui = Handler(Looper.getMainLooper())
    private var server: BridgeServer? = null
    private lateinit var info: TextView
    private lateinit var gate: Switch

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        val token = "%06d".format(SecureRandom().nextInt(1_000_000))
        val port = 8787

        info = TextView(this).apply { textSize = 16f; typeface = android.graphics.Typeface.MONOSPACE }
        gate = Switch(this).apply { text = "Allow remote flight commands (takeoff)"; isChecked = false }
        val rtmp = EditText(this).apply { hint = "rtmp://<computer-ip>:1935/live/dji"; setSingleLine() }
        val start = Button(this).apply { text = "Start camera stream" }
        val stop = Button(this).apply { text = "Stop camera stream" }
        val msg = TextView(this)
        start.setOnClickListener { DjiAdapter.startVideo(rtmp.text.toString().trim()) { e -> ui.post { msg.text = e ?: "streaming" } } }
        stop.setOnClickListener { DjiAdapter.stopVideo(); msg.text = "stopped" }

        setContentView(LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL; gravity = Gravity.TOP; setPadding(40, 60, 40, 40)
            listOf(info, gate, rtmp, start, stop, msg).forEach { addView(it) }
        })

        server = BridgeServer(port, DjiAdapter, token) { gate.isChecked }.also { it.start() }
        val ip = localIp()
        val tick = object : Runnable {
            override fun run() {
                val s = DjiAdapter.status()
                info.text = "dronelink --bridge http://$ip:$port --bridge-token $token --allow-flight\n\n" +
                    "sdk: ${DjiAdapter.lastEvent}\naircraft: ${if (s.connected) "connected" else "not connected"}\n" +
                    "phase: ${s.phase}  alt ${"%.1f".format(s.altitudeM)} m  batt ${s.batteryPct}%  gps ${if (s.gpsOk) "ok" else "NO LOCK"}"
                ui.postDelayed(this, 500)
            }
        }
        ui.post(tick)
    }

    override fun onDestroy() { server?.stop(); ui.removeCallbacksAndMessages(null); super.onDestroy() }

    private fun localIp(): String =
        NetworkInterface.getNetworkInterfaces().toList().flatMap { it.inetAddresses.toList() }
            .firstOrNull { it is Inet4Address && !it.isLoopbackAddress }?.hostAddress ?: "<phone-ip>"
}
