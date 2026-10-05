"""HTTP server: static UI, /api/status, /api/power, /events (SSE), /video (MJPEG).
Binds to localhost by default. State-changing calls reject cross-origin requests."""

import json
import pathlib
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse

STATIC = pathlib.Path(__file__).parent / "static"


def make_server(drone, ingest, host="127.0.0.1", port=8080):
    class H(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def log_message(self, *a):
            pass

        def _json(self, code, obj):
            body = json.dumps(obj).encode()
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.send_header("Cache-Control", "no-store")
            self.end_headers()
            self.wfile.write(body)

        def _state(self):
            s = drone.status()
            s["video_live"] = bool(ingest and ingest.live)
            s["video_source"] = ingest.source if ingest else None
            return s

        def do_GET(self):
            path = urlparse(self.path).path
            if path in ("/", "/index.html"):
                body = (STATIC / "index.html").read_bytes()
                self.send_response(200)
                self.send_header("Content-Type", "text/html; charset=utf-8")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)
            elif path == "/favicon.ico":
                self.send_response(204)
                self.send_header("Content-Length", "0")
                self.end_headers()
            elif path == "/api/status":
                self._json(200, self._state())
            elif path == "/events":
                self.send_response(200)
                self.send_header("Content-Type", "text/event-stream")
                self.send_header("Cache-Control", "no-store")
                self.end_headers()
                try:
                    while True:
                        self.wfile.write(f"data: {json.dumps(self._state())}\n\n".encode())
                        self.wfile.flush()
                        time.sleep(0.5)
                except OSError:
                    pass
                self.close_connection = True
            elif path == "/video" and ingest:
                self.send_response(200)
                self.send_header("Content-Type", "multipart/x-mixed-replace; boundary=frame")
                self.send_header("Cache-Control", "no-store")
                self.end_headers()
                seq = -1
                try:
                    while True:
                        seq, frame = ingest.wait_frame(seq)
                        if frame:
                            self.wfile.write(b"--frame\r\nContent-Type: image/jpeg\r\nContent-Length: "
                                             + str(len(frame)).encode() + b"\r\n\r\n" + frame + b"\r\n")
                            self.wfile.flush()
                except OSError:
                    pass
                self.close_connection = True
            else:
                self._json(404, {"error": "not found"})

        def do_POST(self):
            if urlparse(self.path).path != "/api/power":
                return self._json(404, {"error": "not found"})
            origin = self.headers.get("Origin")
            if origin and origin != f"http://{self.headers.get('Host')}":
                return self._json(403, {"error": "cross-origin request refused"})
            if self.headers.get("Content-Type", "").split(";")[0] != "application/json":
                return self._json(415, {"error": "application/json required"})
            try:
                n = int(self.headers.get("Content-Length", 0))
                state = json.loads(self.rfile.read(n) or b"{}").get("state")
            except (ValueError, AttributeError):
                return self._json(400, {"error": "bad json"})
            if state not in ("on", "off"):
                return self._json(400, {"error": "state must be 'on' or 'off'"})
            try:
                drone.power(state)
            except PermissionError as e:
                return self._json(403, {"error": str(e)})
            except OSError as e:
                return self._json(502, {"error": f"drone link: {e}"})
            self._json(200, self._state())

    class Srv(ThreadingHTTPServer):
        daemon_threads = True

    return Srv((host, port), H)
