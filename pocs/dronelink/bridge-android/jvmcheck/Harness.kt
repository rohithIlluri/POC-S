package dev.dronelink.bridge

/** JVM harness: the real BridgeServer over a fake aircraft, so the Python client can talk to it. */
class FakeAdapter : DroneAdapter {
    private val ph = PhaseTracker()
    @Volatile var flying = false
    @Volatile var alt = 0.0
    @Volatile var gps = true
    init { Thread { while (true) { Thread.sleep(100)
        if (ph.last == PhaseTracker.Cmd.TAKEOFF && flying) alt = minOf(1.2, alt + 0.12)
        if (ph.last == PhaseTracker.Cmd.LAND && flying) { alt = maxOf(0.0, alt - 0.1); if (alt == 0.0) flying = false } } }.apply { isDaemon = true }.start() }
    override fun status() = Status(ph.phase(true, flying, flying, alt), alt, 90, gps, true)
    override fun takeoff(onDone: (String?) -> Unit) { ph.command(PhaseTracker.Cmd.TAKEOFF); flying = true; onDone(null) }
    override fun land(onDone: (String?) -> Unit) { ph.command(PhaseTracker.Cmd.LAND); onDone(null) }
    override fun startVideo(rtmpUrl: String, onDone: (String?) -> Unit) = onDone(null)
    override fun stopVideo() {}
}

fun main(args: Array<String>) {
    val port = args.getOrNull(0)?.toInt() ?: 8787
    val remote = args.getOrNull(2) != "locked"
    BridgeServer(port, FakeAdapter(), args.getOrNull(1) ?: "123456") { remote }.start()
    println("kotlin bridge on $port"); Thread.sleep(Long.MAX_VALUE)
}
