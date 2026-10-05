"""On/off control of the simulated drone, shared by the Muse-gadget command and the CLI.
on = arm + take off to cruise altitude and hover; off = land (latched)."""

import threading
import time

from .backends import SimBackend
from .safety import Gate
from .session import DT


class DronePower:
    def __init__(self):
        self._lock = threading.Lock()
        self.backend, self.gate = SimBackend(), Gate()
        self._thread = None

    def tick(self):
        with self._lock:
            self.backend.send(self.gate.step(DT, self.backend.telemetry()))
            self.backend.step(DT)

    def start(self):
        """Run the sim in real time in the background (idempotent)."""
        if self._thread and self._thread.is_alive():
            return
        self._stop = False

        def loop():
            while not self._stop:
                self.tick()
                time.sleep(DT)

        self._thread = threading.Thread(target=loop, daemon=True)
        self._thread.start()

    def stop(self):
        self._stop = True

    def set(self, state):
        with self._lock:
            if state == "on":
                if self.gate.phase in ("LANDING", "LANDED"):
                    if not self.backend.telemetry().landed:
                        return self._status("still landing; retry once landed")
                    self.gate = Gate()
                self.gate.arm(True)
            elif state == "off":
                self.gate.request_land("power off")
            return self._status()

    def status(self):
        with self._lock:
            return self._status()

    def _status(self, note=None):
        t = self.backend.telemetry()
        out = {"phase": self.gate.phase, "power": "off" if self.gate.phase in ("PREFLIGHT", "LANDING", "LANDED") else "on",
               "altitude_m": round(t.z, 2), "x_m": round(t.x, 2), "y_m": round(t.y, 2), "simulated": True}
        if note:
            out["note"] = note
        return out
