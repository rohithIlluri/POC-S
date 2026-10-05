# dronelink bridge (Android)

Phone app that sits between the DJI RC-N1 (USB) and the dronelink web UI (Wi-Fi). It exposes `GET /status` and `POST /power`, and pushes the camera to RTMP. Mini 3 + RC-N1, MSDK 5.18.0.

**Status: written, not yet built against the DJI SDK.** `DroneAdapter.kt`, `BridgeServer.kt`, the manifest and Gradle files are plain Android. `DjiAdapter.kt` is the only file that touches the DJI SDK; init/register and takeoff/landing mirror DJI's sample, while the telemetry key names and live-stream calls are from memory of the V5 API and may need a small fix when Android Studio compiles them. `BridgeServer` + `PhaseTracker` *are* compiled and exercised on a JVM (see below).

## Build and install (needs you)
1. Create a DJI developer account and an app at https://developer.dji.com/user/apps (type: Mobile SDK, Android, package name **`dev.dronelink.bridge`**). Copy the App Key.
2. Put it in `~/.gradle/gradle.properties`: `DJI_API_KEY=...` (not in git).
3. Open `bridge-android/` in Android Studio (it will generate the Gradle wrapper), sync, and fix any compile errors in `DjiAdapter.kt` against DJI's sample (`Mobile-SDK-Android-V5`, `android-sdk-v5-sample`). If the app doesn't auto-launch when the RC-N1 is plugged in, copy `res/xml/accessory_filter.xml` from that sample.
4. Install on an arm64 Android phone. First launch needs internet (SDK registration with your key).

## Connect
1. Props off, aircraft on, RC-N1 on, phone plugged into the RC-N1 over USB; open the app. It shows `sdk: registered`, `aircraft: connected` and a command line with the phone's URL and a one-time **PIN**.
2. Phone and computer on the same Wi-Fi. On the computer:
   `python3 -m dronelink --video rtmp --bridge http://<phone-ip>:8787 --bridge-token <PIN> --allow-flight`
3. In the app type `rtmp://<computer-ip>:1935/live/dji` and tap **Start camera stream**. Open http://127.0.0.1:8080: video, status, phase, battery, GPS should be live.
4. **Dry run first with propellers off:** check status updates. Takeoff stays impossible until you flip the app's **Allow remote flight commands** switch (it is always OFF at launch).
5. First real flight: open area, line of sight, GPS lock, battery ≥ 30%, controller in hand. Flip the switch, press ON in the web UI and confirm. OFF lands. The RC-N1's own RTH/stick inputs always win.

## Safety layers
PIN on every request · takeoff needs the on-phone switch (reset at each launch) · needs aircraft connected, GPS ≥ 8 satellites, battery ≥ 30%, phase LANDED · landing is always allowed · `--allow-flight` on the server · confirm dialog in the web UI.

## Checking the non-DJI code without a phone
`jvmcheck/` compiles `BridgeServer.kt` + `PhaseTracker` on a plain JVM against a fake aircraft:
```sh
cd jvmcheck && gradle installDist && build/install/jvmcheck/bin/jvmcheck 8788 424242   # [port] [pin] [locked]
python3 -m dronelink --bridge http://127.0.0.1:8788 --bridge-token 424242 --allow-flight --video none
```
