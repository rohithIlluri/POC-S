"""Stand-in for the phone bridge app: same HTTP contract, simulated aircraft, and an RTMP
test-pattern push (what the real app's live-stream manager would do). Lets you exercise
`dronelink --bridge ... --video rtmp` end to end before the Android app exists.

  python3 -m dronelink.mockbridge --port 8787 --rtmp rtmp://127.0.0.1:1935/live/dji --arm-remote
"""

import argparse
import json
import subprocess
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from .drones import SimDrone


class MockAircraft(SimDrone):
    """SimDrone + the device-side gates the real app enforces."""

    kind = "mock-bridge"

    def __init__(self, remote_enabled=False, gps_ok=True, **kw):
        super().__init__(**kw)
        self.remote_enabled, self.gps_ok = remote_enabled, gps_ok

    def status(self):
        s = super().status()
        s.update(remote_enabled=self.remote_enabled, gps_ok=self.gps_ok)
        del s["kind"], s["link"], s["powered"]
        return s


def make_bridge(aircraft, host="127.0.0.1", port=0, token=None):
    class H(BaseHTTPRequestHandler):
        def log_message(self, *a):
            pass

        def _send(self, code, obj):
            b = json.dumps(obj).encode()
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(b)))
            self.end_headers()
            self.wfile.write(b)

        def _authed(self):
            ok = token is None or self.headers.get("X-Bridge-Token") == token
            if not ok:
                self._send(401, {"error": "bad or missing token"})
            return ok

        def do_GET(self):
            if not self._authed():
                return
            self._send(200, aircraft.status()) if self.path == "/status" else self._send(404, {"error": "not found"})

        def do_POST(self):
            if not self._authed():
                return
            if self.path != "/power":
                return self._send(404, {"error": "not found"})
            state = json.loads(self.rfile.read(int(self.headers.get("Content-Length", 0))) or b"{}").get("state")
            if state not in ("on", "off"):
                return self._send(400, {"error": "state must be 'on' or 'off'"})
            if state == "on":  # landing is always allowed; takeoff is gated on the device
                if not aircraft.remote_enabled:
                    return self._send(403, {"error": "remote commands are disabled on the phone"})
                if not aircraft.gps_ok:
                    return self._send(409, {"error": "no GPS lock"})
            aircraft.power(state)
            self._send(200, aircraft.status())

    srv = ThreadingHTTPServer((host, port), H)
    srv.daemon_threads = True
    return srv


def push_rtmp(url, stop):
    """Push a test pattern to url, reconnecting until stop is set."""
    cmd = ["ffmpeg", "-hide_banner", "-loglevel", "error", "-re", "-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=30",
           "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-pix_fmt", "yuv420p", "-g", "30",
           "-f", "flv", url]
    while not stop.is_set():
        p = subprocess.Popen(cmd, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        while p.poll() is None and not stop.is_set():
            time.sleep(0.2)
        if p.poll() is None:
            p.kill()
            p.wait()
        stop.wait(1.0)


def main(argv=None):
    p = argparse.ArgumentParser(prog="dronelink.mockbridge", description=__doc__)
    p.add_argument("--host", default="127.0.0.1")
    p.add_argument("--port", type=int, default=8787)
    p.add_argument("--rtmp", help="push a test pattern to this rtmp:// URL")
    p.add_argument("--token", help="require this X-Bridge-Token (the phone app always does)")
    p.add_argument("--arm-remote", action="store_true", help="the phone-side 'allow remote flight commands' switch")
    a = p.parse_args(argv)
    air = MockAircraft(remote_enabled=a.arm_remote)
    stop = threading.Event()
    if a.rtmp:
        threading.Thread(target=push_rtmp, args=(a.rtmp, stop), daemon=True).start()
    srv = make_bridge(air, a.host, a.port, a.token)
    print(f"mock bridge on http://{a.host}:{a.port} remote_enabled={a.arm_remote} rtmp={a.rtmp}")
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        stop.set()
        air.close()


if __name__ == "__main__":
    main()
