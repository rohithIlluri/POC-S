"""Full chain: mock phone bridge (+ real RTMP push via ffmpeg) -> dronelink server -> HTTP client."""

import shutil
import socket
import threading
import time
import unittest

from dronelink.drones import BridgeDrone
from dronelink.mockbridge import MockAircraft, make_bridge, push_rtmp
from dronelink.video import Ingest

from test_dronelink import req, serve


def free_port():
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    p = s.getsockname()[1]
    s.close()
    return p


def wait(fn, timeout=20):
    end = time.time() + timeout
    while time.time() < end:
        v = fn()
        if v:
            return v
        time.sleep(0.1)
    return None


@unittest.skipUnless(shutil.which("ffmpeg"), "ffmpeg not installed")
class EndToEnd(unittest.TestCase):
    def setUp(self):
        self.air = MockAircraft(remote_enabled=True)
        self.bridge = make_bridge(self.air, token="123456")
        threading.Thread(target=self.bridge.serve_forever, daemon=True).start()
        self.rtmp_port = free_port()
        self.ingest = Ingest("rtmp", self.rtmp_port)
        self.ingest.start()
        self.stop = threading.Event()
        url = f"rtmp://127.0.0.1:{self.rtmp_port}/live/dji"
        threading.Thread(target=push_rtmp, args=(url, self.stop), daemon=True).start()
        drone = BridgeDrone(f"http://127.0.0.1:{self.bridge.server_address[1]}", allow_flight=True, token="123456")
        self.srv, self.port = serve(drone, self.ingest)

    def tearDown(self):
        self.stop.set()
        self.ingest.close()
        self.srv.shutdown()
        self.srv.server_close()
        self.bridge.shutdown()
        self.bridge.server_close()
        self.air.close()

    def status(self):
        return req(self.port, "GET", "/api/status")[1]

    def test_video_and_power_cycle_through_bridge(self):
        self.assertTrue(wait(lambda: self.status()["video_live"]), "RTMP video never went live")
        self.assertEqual(self.status()["kind"], "bridge")
        self.assertEqual(req(self.port, "POST", "/api/power", {"state": "on"})[0], 200)
        self.assertTrue(wait(lambda: self.status()["phase"] == "HOVERING"), self.status())
        self.assertEqual(req(self.port, "POST", "/api/power", {"state": "off"})[0], 200)
        self.assertTrue(wait(lambda: self.status()["phase"] == "LANDED"), self.status())

    def test_phone_gate_blocks_takeoff_but_not_landing(self):
        self.air.remote_enabled = False
        s, j = req(self.port, "POST", "/api/power", {"state": "on"})
        self.assertEqual(s, 403)
        self.assertIn("disabled on the phone", j["error"])
        self.assertEqual(self.status()["phase"], "LANDED")
        self.air.remote_enabled = True
        req(self.port, "POST", "/api/power", {"state": "on"})
        self.air.remote_enabled = False  # operator flips the switch mid-flight
        self.assertEqual(req(self.port, "POST", "/api/power", {"state": "off"})[0], 200)

    def test_wrong_or_missing_token_is_rejected(self):
        url = f"http://127.0.0.1:{self.bridge.server_address[1]}"
        for tok in (None, "000000"):
            d = BridgeDrone(url, allow_flight=True, token=tok)
            self.assertTrue(d.status()["link"].startswith("down"))
            with self.assertRaisesRegex(PermissionError, "bad or missing token"):
                d.power("on")
        self.assertEqual(self.status()["phase"], "LANDED")

    def test_no_gps_blocks_takeoff(self):
        self.air.gps_ok = False
        s, j = req(self.port, "POST", "/api/power", {"state": "on"})
        self.assertEqual((s, j["error"]), (403, "bridge refused: no GPS lock"))


if __name__ == "__main__":
    unittest.main()
