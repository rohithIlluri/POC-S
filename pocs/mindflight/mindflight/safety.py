"""The safety gate: every command, from any source, passes through here."""

from dataclasses import dataclass

from .backends import Command


@dataclass
class Limits:
    max_speed: float = 1.5  # m/s
    cruise_alt: float = 3.0
    geofence_m: float = 25.0
    max_alt: float = 10.0
    max_flight_s: float = 120.0
    bad_signal_hover_s: float = 0.5
    bad_signal_land_s: float = 3.0
    turn_rate: float = 45.0  # deg/s
    turn_s: float = 2.0  # 90 deg per double-blink


class Gate:
    """Phase machine PREFLIGHT -> TAKEOFF -> FLIGHT -> LANDING -> LANDED.
    Fail-safe order: land latch > geofence > bad-signal hover > requested motion."""

    def __init__(self, limits=None):
        self.lim = limits or Limits()
        self.phase = "PREFLIGHT"
        self.land_reason = None
        self.bad_for = 0.0
        self.turn_left = 0.0
        self.flight_t = 0.0
        self.notes = []

    def request_land(self, why):
        if self.phase not in ("LANDING", "LANDED"):
            self.phase, self.land_reason = "LANDING", why
            self.notes.append(f"LAND: {why}")

    def arm(self, signal_good):
        if self.phase == "PREFLIGHT" and signal_good:
            self.phase = "TAKEOFF"

    def step(self, dt, tel, forward=0.0, turn=False, signal_ok=True):
        lim = self.lim
        if self.phase == "PREFLIGHT":
            return Command("hover")
        if self.phase == "LANDED" or (self.phase == "LANDING" and tel.landed):
            self.phase = "LANDED"
            return Command("land")
        self.flight_t += dt
        if self.flight_t > lim.max_flight_s:
            self.request_land("max flight time")
        if self.phase == "LANDING":
            return Command("land")
        if self.phase == "TAKEOFF":
            if tel.z >= lim.cruise_alt - 0.1:
                self.phase = "FLIGHT"
            return Command("takeoff", alt=lim.cruise_alt)
        # FLIGHT
        self.bad_for = 0.0 if signal_ok else self.bad_for + dt
        if self.bad_for >= lim.bad_signal_land_s:
            self.request_land("signal lost")
            return Command("land")
        if turn and self.turn_left <= 0:
            self.turn_left = lim.turn_s
        yaw = 0.0
        if self.turn_left > 0:
            self.turn_left -= dt
            yaw = lim.turn_rate
        fwd = min(max(forward, 0.0), lim.max_speed)
        if self.bad_for >= lim.bad_signal_hover_s:
            fwd = 0.0
        if (tel.x ** 2 + tel.y ** 2) ** 0.5 >= lim.geofence_m and fwd > 0:
            fwd = 0.0
            self.notes.append("geofence: forward blocked")
        return Command("velocity", forward=fwd, yaw_rate=yaw, alt=lim.cruise_alt)
