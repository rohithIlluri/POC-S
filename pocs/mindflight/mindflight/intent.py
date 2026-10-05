"""EEG window features -> discrete intents (focus/neutral/relax, blink x2, clench)."""

import statistics

BLINK_UV = 60.0  # frontal deviation that counts as a blink
DOUBLE_BLINK_S = 1.5
CLENCH_X = 8.0  # emg power vs. baseline
DEAD_STD = 0.5  # uV: flat channel = electrode off
SAT_STD = 400.0


class Calibration:
    """Collects neutral-state windows to learn the user's focus baseline."""

    def __init__(self, windows=24):
        self.need = windows
        self.focus, self.emg = [], []

    @property
    def done(self):
        return len(self.focus) >= self.need

    def add(self, f):
        self.focus.append(f["focus_index"])
        self.emg.append(f["emg"])

    def baseline(self):
        return (statistics.fmean(self.focus), max(statistics.pstdev(self.focus), 0.05),
                max(statistics.median(self.emg), 1e-6))


class Decoder:
    ENTER, EXIT, ALPHA = 1.0, 0.4, 0.3

    def __init__(self, base):
        self.mu, self.sd, self.emg0 = base
        self.ema = 0.0
        self.state = "neutral"
        self.blinks = []
        self._blink_on = False

    def blink_edge(self, hop, t):
        """hop: newest samples per channel. Returns True on a double blink."""
        fr = [hop[1], hop[2]]
        dev = max(abs(v - statistics.fmean(ch)) for ch in fr for v in ch)
        hit = dev > BLINK_UV
        edge = hit and not self._blink_on
        self._blink_on = hit
        if edge:
            self.blinks = [b for b in self.blinks if t - b < DOUBLE_BLINK_S] + [t]
            if len(self.blinks) >= 2:
                self.blinks = []
                return True
        return False

    def update(self, f, artifact):
        """Returns (state, clench). Focus is frozen while an artifact is in the window."""
        clench = f["emg"] > self.emg0 * CLENCH_X
        if artifact or clench:
            return self.state, clench
        z = (f["focus_index"] - self.mu) / self.sd
        self.ema += self.ALPHA * (z - self.ema)
        if self.state == "focus" and self.ema < self.EXIT:
            self.state = "neutral"
        elif self.state == "relax" and self.ema > -self.EXIT:
            self.state = "neutral"
        elif self.state == "neutral":
            self.state = "focus" if self.ema > self.ENTER else "relax" if self.ema < -self.ENTER else "neutral"
        return self.state, False


def signal_ok(ch_std):
    return all(DEAD_STD < s < SAT_STD for s in ch_std)
