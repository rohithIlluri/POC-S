"""Real Muse input via LSL (run `muselsl stream` first). Needs `pip install pylsl`."""

from .dsp import FS


class LslMuse:
    def __init__(self, timeout=5.0):
        try:
            from pylsl import StreamInlet, resolve_byprop
        except ImportError as e:  # pragma: no cover - needs optional dep
            raise SystemExit("pylsl missing: pip install pylsl muselsl") from e
        streams = resolve_byprop("type", "EEG", timeout=timeout)
        if not streams:
            raise SystemExit("no LSL EEG stream found; run `muselsl stream` first")
        self.inlet = StreamInlet(streams[0], max_chunklen=FS)
        self.done = False
        self._buf = []

    def read(self, n):  # pragma: no cover - needs hardware
        while len(self._buf) < n:
            chunk, _ = self.inlet.pull_chunk(timeout=1.0, max_samples=n)
            self._buf.extend(row[:4] for row in chunk)  # TP9, AF7, AF8, TP10
        out, self._buf = self._buf[:n], self._buf[n:]
        return out
