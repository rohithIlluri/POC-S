import http.client
import json
import shutil
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer

from dronelink.drones import BridgeDrone, SimDrone
from dronelink.server import make_server
from dronelink.video import Ingest, split_jpegs


class FakeDrone(SimDrone):
    def __init__(self):  # no background thread: tests drive tick()
        self._lock = threading.Lock()
        self.phase, self.alt, self.battery, self._stop = "LANDED", 0.0, 100.0, False


def serve(drone, ingest=None):
    srv = make_server(drone, ingest, "127.0.0.1", 0)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv, srv.server_address[1]


def req(port, method, path, body=None, headers=None):
    c = http.client.HTTPConnection("127.0.0.1", port, timeout=5)
    h = {"Content-Type": "application/json", **(headers or {})}
    c.request(method, path, None if body is None else json.dumps(body), h)
    r = c.getresponse()
    out = r.status, json.loads(r.read() or b"{}")
    c.close()
    return out


class Sim(unittest.TestCase):
    def test_takeoff_hover_land_cycle(self):
        d = FakeDrone()
        d.power("on")
        for _ in range(40):
            d.tick(0.1)
        self.assertEqual((d.status()["phase"], d.status()["altitude_m"]), ("HOVERING", 3.0))
        d.power("off")
        for _ in range(60):
            d.tick(0.1)
        self.assertEqual((d.status()["phase"], d.status()["altitude_m"], d.status()["powered"]), ("LANDED", 0.0, False))

    def test_low_battery_refuses_takeoff(self):
        d = FakeDrone()
        d.battery = 10
        self.assertEqual(d.power("on")["phase"], "LANDED")


class Api(unittest.TestCase):
    def setUp(self):
        self.d = FakeDrone()
        self.srv, self.port = serve(self.d)

    def tearDown(self):
        self.srv.shutdown()
        self.srv.server_close()

    def test_power_on_off(self):
        s, j = req(self.port, "POST", "/api/power", {"state": "on"})
        self.assertEqual((s, j["phase"]), (200, "TAKING_OFF"))
        self.assertEqual(req(self.port, "GET", "/api/status")[1]["powered"], True)
        self.assertEqual(req(self.port, "POST", "/api/power", {"state": "off"})[1]["phase"], "LANDING")

    def test_rejects_bad_state_content_type_and_cross_origin(self):
        self.assertEqual(req(self.port, "POST", "/api/power", {"state": "spin"})[0], 400)
        self.assertEqual(req(self.port, "POST", "/api/power", {"state": "on"}, {"Content-Type": "text/plain"})[0], 415)
        s, _ = req(self.port, "POST", "/api/power", {"state": "on"}, {"Origin": "http://evil.example"})
        self.assertEqual(s, 403)
        self.assertEqual(self.d.status()["phase"], "LANDED")

    def test_serves_ui(self):
        c = http.client.HTTPConnection("127.0.0.1", self.port)
        c.request("GET", "/")
        r = c.getresponse()
        self.assertIn(b"dronelink", r.read())
        c.close()


class Bridge(unittest.TestCase):
    def setUp(self):
        calls = self.calls = []

        class B(BaseHTTPRequestHandler):
            def log_message(self, *a):
                pass

            def _send(self):
                b = json.dumps({"phase": "HOVERING", "altitude_m": 3.0, "battery_pct": 80}).encode()
                self.send_response(200)
                self.send_header("Content-Length", str(len(b)))
                self.end_headers()
                self.wfile.write(b)

            def do_GET(self):
                self._send()

            def do_POST(self):
                calls.append(json.loads(self.rfile.read(int(self.headers["Content-Length"]))))
                self._send()

        self.b = HTTPServer(("127.0.0.1", 0), B)
        threading.Thread(target=self.b.serve_forever, daemon=True).start()
        self.url = f"http://127.0.0.1:{self.b.server_address[1]}"

    def tearDown(self):
        self.b.shutdown()

    def test_status_readable_but_flight_blocked_by_default(self):
        d = BridgeDrone(self.url)
        self.assertTrue(d.status()["powered"])
        with self.assertRaises(PermissionError):
            d.power("off")
        self.assertEqual(self.calls, [])
        srv, port = serve(d)
        self.assertEqual(req(port, "POST", "/api/power", {"state": "off"})[0], 403)
        srv.shutdown()

    def test_allow_flight_forwards_command(self):
        BridgeDrone(self.url, allow_flight=True).power("off")
        self.assertEqual(self.calls, [{"state": "off"}])

    def test_bridge_down_reports_link(self):
        s = BridgeDrone("http://127.0.0.1:1").status()
        self.assertTrue(s["link"].startswith("down"))


class Video(unittest.TestCase):
    def test_split_jpegs_handles_partial_frames(self):
        a, b = b"\xff\xd8AAA\xff\xd9", b"\xff\xd8BB\xff\xd9"
        frames, rest = split_jpegs(a + b + b"\xff\xd8CC")
        self.assertEqual((frames, rest), ([a, b], b"\xff\xd8CC"))

    @unittest.skipUnless(shutil.which("ffmpeg"), "ffmpeg not installed")
    def test_test_pattern_streams_through_http(self):
        ing = Ingest("test")
        ing.start()
        srv, port = serve(FakeDrone(), ing)
        try:
            deadline = time.time() + 15
            while not ing.live and time.time() < deadline:
                time.sleep(0.1)
            self.assertTrue(ing.live)
            self.assertTrue(req(port, "GET", "/api/status")[1]["video_live"])
            c = http.client.HTTPConnection("127.0.0.1", port, timeout=10)
            c.request("GET", "/video")
            r = c.getresponse()
            self.assertIn("multipart/x-mixed-replace", r.getheader("Content-Type"))
            self.assertIn(b"image/jpeg", r.read(2000))
            c.close()
        finally:
            ing.close()
            srv.shutdown()


if __name__ == "__main__":
    unittest.main()
