"""Drone backends. SimBackend is a self-contained kinematic quad; MavlinkBackend
talks to ArduPilot SITL (or any MAVLink autopilot) over pymavlink."""

import math
from dataclasses import dataclass


@dataclass
class Command:
    mode: str = "hover"  # hover | takeoff | velocity | land
    forward: float = 0.0  # m/s, body frame
    yaw_rate: float = 0.0  # deg/s, + = clockwise
    alt: float = 3.0


@dataclass
class Telemetry:
    x: float = 0.0  # east
    y: float = 0.0  # north
    z: float = 0.0  # up
    yaw: float = 0.0  # deg, 0 = north, clockwise
    speed: float = 0.0
    landed: bool = True


class SimBackend:
    """First-order velocity response; climbs 1.5 m/s, descends 1 m/s."""

    TAU = 0.4

    def __init__(self):
        self.t = Telemetry()
        self.cmd = Command()

    def send(self, cmd):
        self.cmd = cmd

    def step(self, dt):
        c, t = self.cmd, self.t
        want = 0.0
        if c.mode == "takeoff":
            t.landed = False
            t.z = min(c.alt, t.z + 1.5 * dt)
        elif c.mode == "land":
            t.z = max(0.0, t.z - 1.0 * dt)
            t.landed = t.z == 0.0
        elif c.mode == "velocity":
            want = c.forward
            t.yaw = (t.yaw + c.yaw_rate * dt) % 360
        t.speed += (want - t.speed) * min(1.0, dt / self.TAU)
        h = math.radians(t.yaw)
        t.x += t.speed * math.sin(h) * dt
        t.y += t.speed * math.cos(h) * dt
        return t

    def telemetry(self):
        return self.t


class MavlinkBackend:  # pragma: no cover - needs a running SITL
    """GUIDED-mode control: takeoff, body-frame velocity, LAND. Untested against
    real SITL in CI. Start SITL e.g. `sim_vehicle.py -v ArduCopter --console`."""

    def __init__(self, conn="udp:127.0.0.1:14550"):
        try:
            from pymavlink import mavutil
        except ImportError as e:
            raise SystemExit("pymavlink missing: pip install pymavlink") from e
        self.mav = mavutil.mavlink_connection(conn)
        self.mu = mavutil
        self.mav.wait_heartbeat(timeout=15)
        self.t = Telemetry()
        self._mode = None
        self._home = None

    def _set_mode(self, name):
        if self._mode != name:
            self.mav.set_mode(name)
            self._mode = name

    def send(self, cmd):
        m, mu = self.mav, self.mu
        if cmd.mode == "takeoff" and self.t.landed and self._mode != "GUIDED":
            self._set_mode("GUIDED")
            m.arducopter_arm()
            m.motors_armed_wait()
            m.mav.command_long_send(m.target_system, m.target_component,
                                    mu.mavlink.MAV_CMD_NAV_TAKEOFF, 0, 0, 0, 0, 0, 0, 0, cmd.alt)
        elif cmd.mode == "land":
            self._set_mode("LAND")
        elif cmd.mode in ("velocity", "hover"):
            fwd = cmd.forward if cmd.mode == "velocity" else 0.0
            yr = math.radians(cmd.yaw_rate) if cmd.mode == "velocity" else 0.0
            m.mav.set_position_target_local_ned_send(
                0, m.target_system, m.target_component, mu.mavlink.MAV_FRAME_BODY_OFFSET_NED,
                0b0000_0111_1100_0111, 0, 0, 0, fwd, 0, 0, 0, 0, 0, 0, yr)

    def step(self, dt):
        while (msg := self.mav.recv_match(type=["LOCAL_POSITION_NED", "ATTITUDE"], blocking=False)):
            if msg.get_type() == "LOCAL_POSITION_NED":
                self.t.x, self.t.y, self.t.z = msg.y, msg.x, -msg.z
                self.t.speed = math.hypot(msg.vx, msg.vy)
                self.t.landed = self.t.z < 0.15
            else:
                self.t.yaw = math.degrees(msg.yaw) % 360
        return self.t

    def telemetry(self):
        return self.t
