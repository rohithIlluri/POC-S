import argparse
import sys

from . import __version__
from .agent import parse, run_plan
from .backends import MavlinkBackend, SimBackend
from .session import run_eeg
from .synth import SyntheticMuse

DEMO = [(8, "neutral"), (6, "focus"), (1.5, "double_blink"), (6, "focus"), (3, "relax"),
        (1.5, "double_blink"), (5, "focus"), (2, "relax"), (1, "clench"), (4, "neutral")]


def _map(trace, xi, yi, w=41, h=17):
    pts = [(r[xi], r[yi]) for r in trace]
    xs, ys = [p[0] for p in pts] + [0], [p[1] for p in pts] + [0]
    span = max(max(xs) - min(xs), max(ys) - min(ys), 1.0)
    grid = [["."] * w for _ in range(h)]
    for x, y in pts:
        c, r = int((x - min(xs)) / span * (w - 1)), int((y - min(ys)) / span * (h - 1))
        grid[h - 1 - r][c] = "*"
    c, r = int((0 - min(xs)) / span * (w - 1)), int((0 - min(ys)) / span * (h - 1))
    grid[h - 1 - r][c] = "H"
    return "\n".join("".join(r) for r in grid)


def _backend(a):
    return MavlinkBackend(a.connect) if a.backend == "mavlink" else SimBackend()


def main(argv=None):
    p = argparse.ArgumentParser(prog="mindflight", description=__doc__)
    p.add_argument("--version", action="version", version=__version__)
    p.add_argument("--backend", choices=["sim", "mavlink"], default="sim")
    p.add_argument("--connect", default="udp:127.0.0.1:14550", help="MAVLink URL (SITL)")
    sub = p.add_subparsers(dest="cmd", required=True)
    d = sub.add_parser("demo", help="synthetic Muse -> simulated drone")
    d.add_argument("--seed", type=int, default=1)
    d.add_argument("--realtime", action="store_true")
    m = sub.add_parser("muse", help="real Muse over LSL (muselsl stream)")
    m.add_argument("--seconds", type=float, default=300)
    g = sub.add_parser("agent", help='run a text plan, e.g. "takeoff, forward 5, turn right, land"')
    g.add_argument("plan")
    a = p.parse_args(argv)
    log = lambda r: print("  ".join(str(v) for v in r))  # noqa: E731
    rt = a.backend == "mavlink" or getattr(a, "realtime", False)
    if a.cmd == "agent":
        trace, gate = run_plan(parse(a.plan), _backend(a), log=log)
        xi, yi = 2, 3
    else:
        if a.cmd == "demo":
            src = SyntheticMuse(DEMO, seed=a.seed)
        else:
            from .sources import LslMuse
            src = LslMuse()
        trace, gate = run_eeg(src, _backend(a), log=log, realtime=rt)
        xi, yi = 3, 4
    print(f"\nphase={gate.phase} reason={gate.land_reason} samples={len(trace)}")
    for n in dict.fromkeys(gate.notes):
        print("note:", n)
    print("\ntop-down (H = home, * = path):\n" + _map(trace, xi, yi))
    return 0


if __name__ == "__main__":
    sys.exit(main())
