"""Evaluation harness and release gates (§7 of the build plan).

Four gates, all of which must pass before a design partner sees a score:

  1. false-flag rate on verified-human documents      < 2%
  2. recall on wrapper-generated documents            > 70%
  3. recall on the attack set                        == 100%
  4. fairness slice: the false-flag rate on the non-native-writer slice must
     be statistically indistinguishable from the native slice.

Gate 4 is the one that decides what the product is allowed to look at. If a
signal splits those two slices, the rule is to *remove* the signal, not to
reweight it — a reweighted signal still encodes the same correlation.
"""

from __future__ import annotations

import json
import math
from dataclasses import dataclass, field
from pathlib import Path

from .pipeline import ScanResult, scan

FLAGGED_LABELS = {"mass_generated", "high_risk"}

MAX_FALSE_FLAG_RATE = 0.02
MIN_WRAPPER_RECALL = 0.70
MIN_ATTACK_RECALL = 1.0
FAIRNESS_Z_LIMIT = 1.96          # two-sided, alpha = 0.05


@dataclass
class Gate:
    name: str
    passed: bool
    detail: str


@dataclass
class EvalReport:
    metrics: dict
    gates: list[Gate] = field(default_factory=list)

    @property
    def passed(self) -> bool:
        return all(g.passed for g in self.gates)

    def as_dict(self) -> dict:
        return {
            "passed": self.passed,
            "metrics": self.metrics,
            "gates": [{"name": g.name, "passed": g.passed, "detail": g.detail}
                      for g in self.gates],
        }

    def format(self) -> str:
        lines = ["Evaluation report", "=" * 17, ""]
        for set_name, m in sorted(self.metrics["sets"].items()):
            lines.append(f"{set_name:<20} n={m['n']:<4} flagged={m['flagged']:<4} "
                         f"rate={m['flag_rate']:.1%}  labels={m['labels']}")
        f = self.metrics["fairness"]
        lines += [
            "",
            f"fairness  native n={f['native_n']} flag_rate={f['native_rate']:.1%} · "
            f"non-native n={f['non_native_n']} flag_rate={f['non_native_rate']:.1%} · "
            f"z={f['z']:.2f}",
            "",
        ]
        for g in self.gates:
            lines.append(f"  [{'PASS' if g.passed else 'FAIL'}] {g.name}: {g.detail}")
        lines += ["", "RESULT: " + ("PASS" if self.passed else "FAIL")]
        return "\n".join(lines)


def _two_proportion_z(f1: int, n1: int, f2: int, n2: int) -> float:
    """Two-proportion z statistic. Returns 0.0 when the pooled rate is 0 —
    two slices that both flag nothing are trivially indistinguishable."""
    if n1 == 0 or n2 == 0:
        return 0.0
    p1, p2 = f1 / n1, f2 / n2
    pooled = (f1 + f2) / (n1 + n2)
    if pooled in (0.0, 1.0):
        return 0.0
    se = math.sqrt(pooled * (1 - pooled) * (1 / n1 + 1 / n2))
    return (p1 - p2) / se if se else 0.0


def evaluate(corpus_dir: str | Path, sensitivity: str = "balanced",
             signatures_path: str | Path | None = None) -> EvalReport:
    corpus_dir = Path(corpus_dir)
    manifest = json.loads((corpus_dir / "manifest.json").read_text())
    jd_path = corpus_dir / manifest["jd"] if manifest.get("jd") else None
    jd_text = jd_path.read_text() if jd_path else ""

    # The whole corpus is scanned as one population — the human set has to hold
    # up while wrapper swarms and duplicate clusters are in the batch with it.
    # The JD itself lives in the corpus folder and must not be scored as one
    # of the applications.
    result: ScanResult = scan(corpus_dir, jd_text=jd_text,
                              signatures_path=signatures_path,
                              sensitivity=sensitivity,
                              exclude={jd_path} if jd_path else None)
    by_path = {str(Path(a.doc.source_path).resolve()): a for a in result.applications}

    sets: dict[str, dict] = {}
    slices: dict[str, dict] = {}
    misses: dict[str, list[str]] = {}

    for entry in manifest["docs"]:
        path = str((corpus_dir / entry["path"]).resolve())
        app = by_path.get(path)
        if app is None:
            raise RuntimeError(f"corpus document was not scanned: {entry['path']}")
        flagged = app.label in FLAGGED_LABELS

        s = sets.setdefault(entry["set"], {"n": 0, "flagged": 0, "labels": {}})
        s["n"] += 1
        s["flagged"] += int(flagged)
        s["labels"][app.label] = s["labels"].get(app.label, 0) + 1

        if entry["set"] == "human_verified":
            sl = slices.setdefault(entry["slice"], {"n": 0, "flagged": 0})
            sl["n"] += 1
            sl["flagged"] += int(flagged)

        wrong = (flagged and entry["expect"] == "clean") or \
                (not flagged and entry["expect"] == "flagged")
        if wrong:
            misses.setdefault(entry["set"], []).append(
                f"{entry['path']} -> {app.label} "
                f"(effort {app.effort_score}, risk {app.risk_score}; "
                f"{', '.join(s.code for s in app.signals[:4]) or 'no signals'})")

    for s in sets.values():
        s["flag_rate"] = s["flagged"] / s["n"] if s["n"] else 0.0

    native = slices.get("native", {"n": 0, "flagged": 0})
    non_native = slices.get("non_native", {"n": 0, "flagged": 0})
    z = _two_proportion_z(native["flagged"], native["n"],
                          non_native["flagged"], non_native["n"])

    metrics = {
        "corpus": str(corpus_dir),
        "synthetic": bool(manifest.get("synthetic")),
        "sensitivity": sensitivity,
        "scan_stats": result.stats,
        "sets": sets,
        "fairness": {
            "native_n": native["n"],
            "native_rate": native["flagged"] / native["n"] if native["n"] else 0.0,
            "non_native_n": non_native["n"],
            "non_native_rate": (non_native["flagged"] / non_native["n"]
                                if non_native["n"] else 0.0),
            "z": z,
        },
        "misses": misses,
    }

    human = sets.get("human_verified", {"n": 0, "flag_rate": 0.0, "flagged": 0})
    wrapper = sets.get("wrapper_generated", {"n": 0, "flag_rate": 0.0})
    attack = sets.get("attack", {"n": 0, "flag_rate": 0.0})

    gates = [
        Gate("false_flag_rate",
             human["flag_rate"] < MAX_FALSE_FLAG_RATE,
             f"{human['flag_rate']:.2%} of {human['n']} human docs flagged "
             f"(limit {MAX_FALSE_FLAG_RATE:.0%})"),
        Gate("wrapper_recall",
             wrapper["flag_rate"] > MIN_WRAPPER_RECALL,
             f"{wrapper['flag_rate']:.2%} of {wrapper['n']} wrapper docs flagged "
             f"(floor {MIN_WRAPPER_RECALL:.0%})"),
        Gate("attack_recall",
             attack["flag_rate"] >= MIN_ATTACK_RECALL,
             f"{attack['flag_rate']:.2%} of {attack['n']} attack docs flagged "
             f"(required {MIN_ATTACK_RECALL:.0%})"),
        Gate("fairness_slice",
             abs(z) < FAIRNESS_Z_LIMIT,
             f"|z|={abs(z):.2f} between native and non-native slices "
             f"(limit {FAIRNESS_Z_LIMIT})"),
    ]
    return EvalReport(metrics=metrics, gates=gates)
