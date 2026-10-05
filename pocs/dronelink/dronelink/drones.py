"""Drone adapters. Same surface: status(), power('on'|'off'). Swap Sim for Bridge to go real."""

import json
import threading
import time
import urllib.error
import urllib.request

CRUISE_ALT = 3.0


class SimDrone:
    """On = take off to 3 m and hover, off = land. Battery drains while flying."""

    kind = "sim"

    def __init__(self):
        self._lock = threading.Lock()
        self.phase, self.alt, self.battery, self._stop = "LANDED", 0.0, 100.0, False
        threading.Thread(target=self._loop, daemon=True).start()

    def _loop(self, dt=0.1):
        while not self._stop:
            self.tick(dt)
            time.sleep(dt)

    def tick(self, dt):
        with self._lock:
            if self.phase == "TAKING_OFF":
                self.alt = min(CRUISE_ALT, self.alt + 1.0 * dt)
                self.phase = "HOVERING" if self.alt >= CRUISE_ALT else self.phase
            elif self.phase == "LANDING":
                self.alt = max(0.0, self.alt - 0.8 * dt)
                self.phase = "LANDED" if self.alt == 0.0 else self.phase
            if self.alt > 0:
                self.battery = max(0.0, self.battery - 0.02 * dt)
                if self.battery == 0.0 and self.phase != "LANDING":
                    self.phase = "LANDING"

    def power(self, state):
        with self._lock:
            if state == "on" and self.phase in ("LANDED", "LANDING") and self.battery > 15:
                self.phase = "TAKING_OFF"
            elif state == "off" and self.phase in ("TAKING_OFF", "HOVERING"):
                self.phase = "LANDING"
        return self.status()

    def status(self):
        with self._lock:
            return {"kind": self.kind, "phase": self.phase, "powered": self.phase in ("TAKING_OFF", "HOVERING"),
                    "altitude_m": round(self.alt, 2), "battery_pct": round(self.battery, 1), "link": "ok"}

    def close(self):
        self._stop = True


class BridgeDrone:
    """Talks to a phone-side bridge app (DJI Mobile SDK V5) over HTTP/JSON:
        GET  {url}/status           -> {"phase","altitude_m","battery_pct",...}
        POST {url}/power {"state"}  -> same as status
    Every request carries X-Bridge-Token (the PIN shown on the phone); the bridge rejects others with 401.
    Optional status fields the UI understands: remote_enabled, gps_ok. The bridge also
    enforces its own on-device gate (takeoff refused with 403 unless enabled on the phone;
    landing is always allowed). Reference implementation: bridge-android/; stand-in: mockbridge.py. `allow_flight` must be set by the
    operator; without it only status is readable."""

    kind = "bridge"

    def __init__(self, url, allow_flight=False, timeout=3.0, token=None):
        self.url, self.allow_flight, self.timeout, self.token = url.rstrip("/"), allow_flight, timeout, token

    def _call(self, path, body=None):
        try:
            return self._raw(path, body)
        except urllib.error.HTTPError as e:  # the phone refused: surface its reason
            try:
                msg = json.load(e).get("error", e.reason)
            except ValueError:
                msg = e.reason
            raise PermissionError(f"bridge refused: {msg}") from None

    def _raw(self, path, body=None):
        req = urllib.request.Request(self.url + path, data=None if body is None else json.dumps(body).encode(),
                                     headers={"Content-Type": "application/json", **({"X-Bridge-Token": self.token} if self.token else {})})
        with urllib.request.urlopen(req, timeout=self.timeout) as r:
            return json.load(r)

    def status(self):
        try:
            s = self._call("/status")
            s.update(kind=self.kind, link="ok", powered=s.get("phase") in ("TAKING_OFF", "HOVERING"))
            return s
        except (OSError, PermissionError) as e:
            return {"kind": self.kind, "phase": "UNKNOWN", "powered": False, "link": f"down: {e}"}

    def power(self, state):
        if not self.allow_flight:
            raise PermissionError("flight commands disabled; start the server with --allow-flight")
        self._call("/power", {"state": state})
        return self.status()

    def close(self):
        pass
