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

## Getting your Mini's camera into the page
DJI Fly can push the live view to a custom RTMP server (Mini 3 / 3 Pro / 4 Pro, DJI Fly ≥ 1.4.12, phone on the controller):
1. `python3 -m dronelink --video rtmp` (listens on `rtmp://0.0.0.0:1935/live/dji`).
2. In DJI Fly: Settings > Transmission > Live Streaming Platforms > RTMP, enter `rtmp://<your-computer-ip>:1935/live/dji`, start the stream.
3. The feed appears in the Camera panel. Expect a few seconds of latency (RTMP), so this is for viewing, not piloting by camera. Older Minis without RTMP can use any ffmpeg input: `--video rtsp://...` or a capture device.

## Controlling the real aircraft (not built yet)
DJI only allows third-party control through its **Mobile SDK V5** (Mini 3, Mini 3 Pro, Mini 4 Pro; older Minis use the legacy V4 SDK). That SDK runs inside an Android app connected to the controller, so real takeoff/land needs a small **bridge app** exposing:
```
GET  /status            -> {"phase": "LANDED|TAKING_OFF|HOVERING|LANDING", "altitude_m": 0, "battery_pct": 87}
POST /power {"state":"on"|"off"}  -> same as status
```
`BridgeDrone` in `dronelink/drones.py` already speaks this (tested against a fake bridge). Start with `--bridge http://<phone>:<port> --allow-flight`. Without `--allow-flight` the UI can read status but every flight command is refused. The UI asks for confirmation before sending to a real aircraft. Building the bridge needs a DJI developer account/app key and an Android toolchain; that is the next milestone, along with movement controls.

## Safety and limits
- Real flight: only in an open area you control, line of sight, props-clear, and within local drone regulations. Keep the DJI controller in hand; its RTH/land buttons override anything here.
- The server binds to `127.0.0.1` and has **no authentication**; don't expose it with `--host 0.0.0.0` on an untrusted network. State-changing calls reject cross-origin requests and non-JSON bodies.
- Verified here: the simulator, HTTP API, MJPEG pipeline (ffmpeg test pattern), bridge protocol against a fake, and the UI in headless Chromium. **Not** verified: a real DJI Fly RTMP stream or a real aircraft.
