import unittest

from mindflight import dsp
from mindflight.agent import parse, run_plan
from mindflight.backends import SimBackend
from mindflight.safety import Gate, Limits
from mindflight.session import run_eeg
from mindflight.synth import SyntheticMuse

BASE = [(8, "neutral")]


def fly(script, seed=1, **kw):
    return run_eeg(SyntheticMuse(BASE + script, seed=seed), **kw)


class Dsp(unittest.TestCase):
    def test_focus_index_orders_states(self):
        idx = {}
        for s in ("relax", "neutral", "focus"):
            m = SyntheticMuse([(2, s)], seed=3)
            window = [list(c) for c in zip(*m.read(256))]
            idx[s] = dsp.window_features(window)["focus_index"]
        self.assertLess(idx["relax"], idx["neutral"])
        self.assertLess(idx["neutral"], idx["focus"])


class Eeg(unittest.TestCase):
    def test_focus_moves_drone_and_clench_lands(self):
        trace, gate = fly([(6, "focus"), (1, "clench"), (4, "neutral")])
        self.assertEqual(gate.phase, "LANDED")
        self.assertEqual(gate.land_reason, "jaw clench")
        self.assertGreater(max(r[4] for r in trace), 3.0)  # travelled north

    def test_neutral_only_hovers(self):
        trace, _ = fly([(8, "neutral")], seed=2)
        self.assertLess(max(abs(r[3]) + abs(r[4]) for r in trace), 0.5)

    def test_double_blink_turns_90(self):
        trace, _ = fly([(3, "focus"), (1.5, "double_blink"), (4, "neutral")])
        self.assertAlmostEqual(trace[-1][6] % 360, 90.0, delta=5)

    def test_signal_loss_lands(self):
        _, gate = fly([(3, "focus"), (6, "disconnect")])
        self.assertEqual(gate.land_reason, "signal lost")

    def test_geofence_blocks_forward(self):
        gate = Gate(Limits(geofence_m=5.0))
        trace, gate = fly([(30, "focus")], gate=gate)
        self.assertLess(max((r[3] ** 2 + r[4] ** 2) ** 0.5 for r in trace), 7.0)
        self.assertIn("geofence: forward blocked", gate.notes)

    def test_never_arms_without_signal(self):
        trace, gate = run_eeg(SyntheticMuse([(15, "disconnect")]), max_ticks=100)
        self.assertEqual(gate.phase, "PREFLIGHT")
        self.assertTrue(all(r[5] == 0 for r in trace))


class Agent(unittest.TestCase):
    def test_parse(self):
        self.assertEqual(parse("Take off, forward 5 m then turn right; land"),
                         [("takeoff", None), ("forward", 5.0), ("turn right", None), ("land", None)])
        with self.assertRaises(ValueError):
            parse("do a flip")

    def test_plan_flies_square_leg_and_lands(self):
        trace, gate = run_plan(parse("takeoff, forward 5, turn right, forward 5, land"))
        self.assertEqual(gate.phase, "LANDED")
        self.assertAlmostEqual(trace[-1][2], 5.0, delta=1.0)
        self.assertAlmostEqual(trace[-1][3], 5.0, delta=1.0)

    def test_plan_cannot_exceed_geofence(self):
        trace, gate = run_plan(parse("takeoff, forward 100, land"), gate=Gate(Limits(geofence_m=10)))
        self.assertLess(max(abs(r[3]) for r in trace), 12.0)
        self.assertEqual(gate.phase, "LANDED")


class Sim(unittest.TestCase):
    def test_speed_is_clamped_by_gate(self):
        g = Gate(Limits(max_speed=1.0))
        g.arm(True)
        b = SimBackend()
        for _ in range(100):
            b.send(g.step(0.1, b.telemetry(), forward=99.0))
            b.step(0.1)
        self.assertLessEqual(b.telemetry().speed, 1.0 + 1e-6)


if __name__ == "__main__":
    unittest.main()
