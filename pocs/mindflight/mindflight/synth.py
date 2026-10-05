"""Synthetic Muse (4ch, 256 Hz) driven by a script of mental/physical states."""

import math
import random

from .dsp import FS

# amplitudes in microvolts: (alpha 10 Hz, beta 20 Hz)
_STATES = {"neutral": (10, 6), "focus": (6, 14), "relax": (16, 4)}
_BLINK_LEN = 0.2


class SyntheticMuse:
    """script: list of (seconds, state). States: neutral, focus, relax,
    double_blink, clench, disconnect."""

    def __init__(self, script, seed=0):
        self.script = list(script)
        self.rng = random.Random(seed)
        self.phase = [self.rng.uniform(0, 2 * math.pi) for _ in range(8)]
        self.t = 0.0
        self.total = sum(d for d, _ in self.script)

    @property
    def done(self):
        return self.t >= self.total

    def _state_at(self, t):
        acc = 0.0
        for d, s in self.script:
            if t < acc + d:
                return s, t - acc
            acc += d
        return "neutral", 0.0

    def read(self, n):
        return [self._sample() for _ in range(n)]

    def _sample(self):
        s, local = self._state_at(self.t)
        t, rng = self.t, self.rng
        self.t += 1.0 / FS
        if s == "disconnect":
            return [rng.gauss(0, 0.1) for _ in range(4)]
        a, b = _STATES.get(s, _STATES["neutral"])
        out = []
        for ch in range(4):
            v = (a * math.sin(2 * math.pi * 10 * t + self.phase[ch])
                 + b * math.sin(2 * math.pi * 20 * t + self.phase[4 + ch])
                 + rng.gauss(0, 3))
            if s == "clench":
                v += rng.gauss(0, 60)
            if s == "double_blink" and ch in (1, 2):
                for start in (0.1, 0.5):
                    if start <= local < start + _BLINK_LEN:
                        v += 120 * math.sin(math.pi * (local - start) / _BLINK_LEN)
            out.append(v)
        return out
