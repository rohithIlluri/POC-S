"""Closed loop: source -> features -> decoder -> gate -> backend."""

import time

from . import dsp
from .backends import SimBackend
from .intent import Calibration, Decoder, signal_ok
from .safety import Gate

HOP = 32  # samples (125 ms)
DT = HOP / dsp.FS


def run_eeg(source, backend=None, gate=None, calib_windows=24, log=None, max_ticks=100_000, realtime=False):
    """Fly from an EEG source until landed or the source ends. Returns the trace."""
    backend, gate = backend or SimBackend(), gate or Gate()
    buf = [[0.0] * dsp.WINDOW for _ in range(4)]
    cal, dec, ok_for, trace, t, since_art = Calibration(calib_windows), None, 0.0, [], 0.0, 99.0
    for tick in range(max_ticks):
        if source.done and gate.phase != "LANDING":
            gate.request_land("source ended")
        hop = list(zip(*source.read(HOP)))  # per-channel
        t += DT
        for i in range(4):
            buf[i] = buf[i][HOP:] + list(hop[i])
        f = dsp.window_features(buf) if t >= 1.0 else None
        ok = f is not None and signal_ok(f["ch_std"])
        fwd, turn = 0.0, False
        if dec is None:
            if f and ok:
                cal.add(f)
            ok_for = ok_for + DT if ok else 0.0
            if cal.done:
                dec = Decoder(cal.baseline())
                gate.arm(ok_for > 2.0)
        else:
            turn = dec.blink_edge(hop, t)
            since_art = 0.0 if turn or dec._blink_on else since_art + DT
            state, clench = dec.update(f, artifact=since_art < 1.0)
            if clench:
                gate.request_land("jaw clench")
            fwd = gate.lim.max_speed if state == "focus" else 0.0
            if not ok:
                state = "no-signal"
        cmd = gate.step(DT, backend.telemetry(), fwd, turn, ok or dec is None)
        backend.send(cmd)
        tel = backend.step(DT)
        trace.append((round(t, 3), gate.phase, dec.state if dec else "calibrating",
                      round(tel.x, 2), round(tel.y, 2), round(tel.z, 2), round(tel.yaw, 1)))
        if log and tick % 8 == 0:
            log(trace[-1])
        if realtime:
            time.sleep(DT)
        if gate.phase == "LANDED":
            break
    return trace, gate
