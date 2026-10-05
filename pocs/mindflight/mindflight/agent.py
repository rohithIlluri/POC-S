"""Text-agent path: a plan ("takeoff, forward 5, turn right, land") runs through the
same safety gate as EEG. parse() is rule-based/offline; swap in an LLM planner that
returns the same step tuples and the gate still bounds everything it can do."""

import re

from .backends import SimBackend
from .safety import Gate
from .session import DT

_STEP = re.compile(r"^(takeoff|take off|forward|fwd|turn (?:left|right)|land|hover)\s*(\d+(?:\.\d+)?)?\s*(?:m|s|deg)?$")


def parse(plan):
    steps = []
    for raw in re.split(r"[,;\n]|\bthen\b", plan.lower()):
        raw = raw.strip()
        if not raw:
            continue
        m = _STEP.match(raw)
        if not m:
            raise ValueError(f"cannot parse step: {raw!r}")
        verb, n = m.group(1).replace("take off", "takeoff").replace("fwd", "forward"), m.group(2)
        steps.append((verb, float(n) if n else None))
    return steps


def run_plan(steps, backend=None, gate=None, log=None, max_ticks=20_000):
    backend, gate = backend or SimBackend(), gate or Gate()
    gate.arm(True)
    trace, tick = [], 0

    def tick_once(forward=0.0, turn=False):
        nonlocal tick
        tick += 1
        cmd = gate.step(DT, backend.telemetry(), forward, turn)
        backend.send(cmd)
        tel = backend.step(DT)
        trace.append((round(tick * DT, 3), gate.phase, round(tel.x, 2), round(tel.y, 2), round(tel.z, 2), round(tel.yaw, 1)))
        if log and tick % 8 == 0:
            log(trace[-1])
        return tel

    for verb, n in steps:
        if tick > max_ticks or gate.phase in ("LANDING", "LANDED"):
            break
        if verb == "takeoff":
            while gate.phase in ("TAKEOFF",) and tick < max_ticks:
                tick_once()
        elif verb == "forward":
            start, d = backend.telemetry(), n or 1.0
            x0, y0 = start.x, start.y
            while ((backend.telemetry().x - x0) ** 2 + (backend.telemetry().y - y0) ** 2) ** 0.5 < d and tick < max_ticks:
                before = len(gate.notes)
                tick_once(forward=gate.lim.max_speed)
                if len(gate.notes) > before:  # geofence refused: abort leg
                    break
        elif verb.startswith("turn"):
            sign = 1 if verb.endswith("right") else -1
            # gate turns clockwise by 90 deg per request; left = 3 right turns
            for _ in range(1 if sign > 0 else 3):
                gate.turn_left = 0
                tick_once(turn=True)
                while gate.turn_left > 0:
                    tick_once()
        elif verb == "hover":
            for _ in range(int((n or 1) / DT)):
                tick_once()
        elif verb == "land":
            gate.request_land("plan: land")
    while gate.phase == "LANDING" and tick < max_ticks:
        tick_once()
    return trace, gate
