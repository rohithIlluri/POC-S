# dronelink

A web ground station / simulator for a **DJI Mini**: **ON/OFF** (take off / land) from the browser, a **live camera feed** in the page, and a drone adapter you can later extend to flight control. Runs today with a built-in simulated drone and a synthetic camera; the real-aircraft path needs two pieces described below.

```
DJI Fly app ──RTMP──► ffmpeg ──MJPEG──► dronelink server ──► browser (camera + ON/OFF + side view)
Phone bridge app (DJI MSDK V5) ◄──HTTP/JSON──► dronelink server   (real control; not built yet)
SimDrone ◄───────────────────────────────────► dronelink server   (works now)
```

## Run it (no drone, no dependencies except ffmpeg)
```sh
cd pocs/dronelink
python3 -m dronelink                 # http://127.0.0.1:8080  (sim drone + test-pattern camera)
python3 -m unittest discover -s tests -v
```
Click **ON** to take off to 3 m, **OFF** to land. `--video none` disables the camera.

## Your aircraft: DJI Mini 3 (non-Pro)
Findings from DJI's docs and developer reports (not yet tried on a real Mini 3):
- **Controller:** third-party control needs the basic **RC-N1 + an Android phone**. The DJI RC with built-in screen is not on the Mini 3's supported list for the SDK. Mobile SDK V5 lists Mini 3 + RC-N1 as supported from **MSDK 5.11.0**.
- **ON/OFF maps to SDK calls:** takeoff = `FlightControllerKey.KeyStartTakeoff`, land = `KeyStartAutoLanding`. Movement later = `VirtualStickManager` (pitch/roll/yaw/throttle). No waypoint missions on the Mini 3.
- **One app owns the aircraft link.** Once the bridge app is connected through the SDK, DJI Fly can't be too, so the camera must come from the bridge app (MSDK has its own RTMP live-stream manager; confirm its V5 API when building) rather than from DJI Fly.

## Getting the camera into the page
**Today (view only, before the bridge exists):** use DJI Fly's built-in RTMP (Mini 3 + RC-N1, DJI Fly ≥ 1.4.12):
1. `python3 -m dronelink --video rtmp` (listens on `rtmp://0.0.0.0:1935/live/dji`).
2. In DJI Fly: Go Fly > ⋯ > Transmission > Live Streaming Platforms > RTMP; enter `rtmp://<your-computer-ip>:1935/live/dji` (address/stream-key with a slash between), start streaming.
3. Expect a few seconds of latency, so it's for viewing, not piloting by camera. The phone and computer must be on the same network.

**Later (with the bridge):** the bridge app pushes the same RTMP to `--video rtmp`, so nothing changes on the page.

## Controlling the real aircraft
The Android bridge app lives in [`bridge-android/`](bridge-android) (build, connect and safety checklist in its README). It speaks:
```
GET  /status   -> {"phase": "LANDED|TAKING_OFF|HOVERING|LANDING", "altitude_m": 0, "battery_pct": 87, "gps_ok": true, "remote_enabled": true}
POST /power {"state":"on"|"off"}  -> same as status
```
with an `X-Bridge-Token` PIN on every request. Start the server with `--bridge http://<phone>:8787 --bridge-token <PIN> --allow-flight`. Without `--allow-flight` the UI can read status but refuses every flight command; the UI confirms before sending to a real aircraft; the phone has its own on-device takeoff switch.

**Rehearse without hardware:** `python3 -m dronelink.mockbridge --port 8790 --token 123456 --arm-remote --rtmp rtmp://127.0.0.1:1935/live/dji` plus `python3 -m dronelink --video rtmp --bridge http://127.0.0.1:8790 --bridge-token 123456 --allow-flight` runs the whole chain with a simulated aircraft and a real RTMP stream.

## Safety and limits
- Real flight: only in an open area you control, line of sight, props-clear, and within local drone regulations. Keep the DJI controller in hand; its RTH/land buttons override anything here.
- The server binds to `127.0.0.1` and has **no authentication**; don't expose it with `--host 0.0.0.0` on an untrusted network. State-changing calls reject cross-origin requests and non-JSON bodies.
- Verified here: simulator, HTTP API, RTMP→MJPEG (a real ffmpeg RTMP push), bridge protocol with token and phone-side gates (Python mock *and* the Kotlin `BridgeServer` compiled on a JVM), and the full UI in headless Chromium. **Not** verified: the DJI SDK parts of the Android app (`DjiAdapter.kt`, never compiled), the phone's own RTMP push, DJI Fly's RTMP, or any real aircraft.
