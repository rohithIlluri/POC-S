package dev.dronelink.bridge

import android.content.Context
import android.util.Log
import dji.sdk.keyvalue.key.BatteryKey
import dji.sdk.keyvalue.key.FlightControllerKey
import dji.sdk.keyvalue.key.KeyTools
import dji.sdk.keyvalue.value.common.EmptyMsg
import dji.v5.common.callback.CommonCallbacks
import dji.v5.common.error.IDJIError
import dji.v5.common.register.DJISDKInitEvent
import dji.v5.et.action
import dji.v5.et.create
import dji.v5.manager.SDKManager
import dji.v5.manager.interfaces.SDKManagerCallback
import dji.v5.manager.datacenter.MediaDataCenter
import dji.v5.manager.datacenter.livestream.LiveStreamType
import dji.v5.manager.datacenter.livestream.settings.LiveStreamSettings
import dji.v5.manager.datacenter.livestream.settings.RtmpSettings
import dji.v5.manager.KeyManager

/**
 * The only file that touches the DJI Mobile SDK V5 (5.18.0).
 *
 * WRITTEN FROM DJI'S PUBLIC SAMPLE, NOT COMPILED: init/register and KeyStartTakeoff /
 * KeyStartAutoLanding mirror the sample verbatim. The telemetry key names and the live-stream
 * calls are from memory of the V5 API; if Android Studio flags one, check the sample
 * (Mobile-SDK-Android-V5 > android-sdk-v5-sample) and fix it here, nothing else depends on it.
 */
object DjiAdapter : DroneAdapter {
    private const val TAG = "DjiAdapter"
    @Volatile var registered = false
    @Volatile var lastEvent = "starting"
    private val phases = PhaseTracker()

    fun init(ctx: Context) {
        SDKManager.getInstance().init(ctx, object : SDKManagerCallback {
            override fun onRegisterSuccess() { registered = true; lastEvent = "registered" }
            override fun onRegisterFailure(error: IDJIError?) { lastEvent = "register failed: ${error?.description()}" }
            override fun onProductConnect(productId: Int) { lastEvent = "aircraft connected" }
            override fun onProductDisconnect(productId: Int) { lastEvent = "aircraft disconnected" }
            override fun onProductChanged(productId: Int) {}
            override fun onInitProcess(event: DJISDKInitEvent?, totalProcess: Int) {
                if (event == DJISDKInitEvent.INITIALIZE_COMPLETE) SDKManager.getInstance().registerApp()
            }
            override fun onDatabaseDownloadProgress(current: Long, total: Long) {}
        })
    }

    private fun <T> read(info: dji.sdk.keyvalue.key.DJIKeyInfo<T>): T? =
        try { KeyManager.getInstance().getValue(KeyTools.createKey(info)) } catch (e: Exception) { Log.w(TAG, "read failed", e); null }

    override fun status(): Status {
        val connected = registered && read(FlightControllerKey.KeyConnection) == true
        val flying = read(FlightControllerKey.KeyIsFlying) == true
        val motors = read(FlightControllerKey.KeyAreMotorsOn) == true
        val alt = read(FlightControllerKey.KeyAltitude) ?: 0.0
        val sats = read(FlightControllerKey.KeyGPSSatelliteCount) ?: 0
        val batt = read(BatteryKey.KeyChargeRemainingInPercent) ?: 0
        return Status(phases.phase(connected, flying, motors, alt), alt, batt, sats >= 8, connected)
    }

    override fun takeoff(onDone: (String?) -> Unit) {
        phases.command(PhaseTracker.Cmd.TAKEOFF)
        FlightControllerKey.KeyStartTakeoff.create().action(
            { _: EmptyMsg? -> onDone(null) },
            { e: IDJIError -> phases.command(PhaseTracker.Cmd.NONE); onDone(e.description()) })
    }

    override fun land(onDone: (String?) -> Unit) {
        phases.command(PhaseTracker.Cmd.LAND)
        FlightControllerKey.KeyStartAutoLanding.create().action(
            { _: EmptyMsg? -> onDone(null) },
            { e: IDJIError -> onDone(e.description()) })
    }

    override fun startVideo(rtmpUrl: String, onDone: (String?) -> Unit) {
        val lsm = MediaDataCenter.getInstance().liveStreamManager
        lsm.setLiveStreamSettings(
            LiveStreamSettings.Builder()
                .setLiveStreamType(LiveStreamType.RTMP)
                .setRtmpSettings(RtmpSettings.Builder().setUrl(rtmpUrl).build())
                .build())
        lsm.startStream(object : CommonCallbacks.CompletionCallback {
            override fun onSuccess() = onDone(null)
            override fun onFailure(error: IDJIError) = onDone(error.description())
        })
    }

    override fun stopVideo() {
        MediaDataCenter.getInstance().liveStreamManager.stopStream(object : CommonCallbacks.CompletionCallback {
            override fun onSuccess() {}
            override fun onFailure(error: IDJIError) {}
        })
    }
}
