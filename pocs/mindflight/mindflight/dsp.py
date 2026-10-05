"""Pure-python band-power extraction (Goertzel + Hann) for 1 s windows at 256 Hz."""

import math

FS = 256
WINDOW = 256  # 1 s -> 1 Hz bin resolution
CHANNELS = ("TP9", "AF7", "AF8", "TP10")
FRONTAL = (1, 2)

BANDS = {
    "delta": range(1, 4),
    "alpha": range(8, 13),
    "beta": range(13, 31),
    "emg": range(45, 101, 5),
}

_HANN = [0.5 - 0.5 * math.cos(2 * math.pi * i / (WINDOW - 1)) for i in range(WINDOW)]


def _bin_power(x, k):
    """Squared amplitude of DFT bin k of an already-windowed signal."""
    n = len(x)
    c = 2 * math.cos(2 * math.pi * k / n)
    s1 = s2 = 0.0
    for v in x:
        s1, s2 = v + c * s1 - s2, s1
    mag2 = s1 * s1 + s2 * s2 - c * s1 * s2
    return (2 * math.sqrt(max(mag2, 0.0)) / n) ** 2 * 4  # Hann gain correction


def band_powers(samples):
    """Mean per-bin power in each band for one channel window of WINDOW samples."""
    mean = sum(samples) / len(samples)
    x = [(v - mean) * w for v, w in zip(samples, _HANN)]
    return {b: sum(_bin_power(x, k) for k in ks) / len(ks) for b, ks in BANDS.items()}


def std(samples):
    m = sum(samples) / len(samples)
    return math.sqrt(sum((v - m) ** 2 for v in samples) / len(samples))


def window_features(window):
    """window: list of per-channel sample lists. Returns the features the decoder needs."""
    per_ch = [band_powers(ch) for ch in window]
    fr = [per_ch[i] for i in FRONTAL]
    alpha = sum(p["alpha"] for p in fr) / len(fr)
    beta = sum(p["beta"] for p in fr) / len(fr)
    return {
        "focus_index": beta / (alpha + beta + 1e-9),
        "emg": sum(p["emg"] for p in per_ch) / len(per_ch),
        "ch_std": [std(ch) for ch in window],
    }
