"""Evaluation harness and release gates (§7 of the build plan).

The gates that must pass before a design partner sees a score:

  1. false-flag rate on verified-human documents      < 2%
  2. recall on wrapper-generated documents            > 70%
  3. recall on deliberately track-covering wrappers  >= 90%
  4. recall on the attack set                        == 100%
  5. fairness slice: the false-flag rate on the non-native-writer slice must
     be statistically indistinguishable from the native slice.
  6. trickle recall: a farm that submits two applications per scan instead of
     fifty must still be caught once the population memory has accumulated a
     population — and must not cost anything in false flags to do it.

The fairness gate is the one that decides what the product is allowed to look
at. If a signal splits those two slices, the rule is to *remove* the signal,
not to reweight it — a reweighted signal still encodes the same correlation.

Gates 1-5 score the corpus as one population, which is the best case for every
cross-applicant analyzer. Gate 6 is the adversarial complement: the same
manufactured documents delivered thin, batch by batch, so that the only thing
holding the population together is what the engine chose to remember.
"""

from __future__ import annotations

import json
import math
from dataclasses import dataclass, field
from pathlib import Path

from .memory import InMemoryPopulationMemory
from .parse import parse_file
from .pipeline import ScanResult, scan, score_documents

FLAGGED_LABELS = {"mass_generated", "high_risk"}

MAX_FALSE_FLAG_RATE = 0.02
MIN_WRAPPER_RECALL = 0.70
# Deliberately track-covering wrapper output. Raised from 0.50 to 0.90 once
# correlation-aware evidence combination took the measured rate to 100%: a
# floor far below the achieved rate stops being a regression guard.
MIN_EVASION_RECALL = 0.90
MIN_ATTACK_RECALL = 1.0
FAIRNESS_Z_LIMIT = 1.96          # two-sided, alpha = 0.05

# Trickle: a farm delivering two applications per scan. Recall is gated on the
# *warm* slice only — batches scanned after the account's population memory
# holds this many documents — because the engine genuinely cannot call a farm
# on its second sighting, and a gate that pretended otherwise would be asking
# for a detector that flags strangers. A farm needs roughly
# `boilerplate.INDUSTRIAL_OWNERS` of its own applications in the memory before
# its shared phrasing is industrial rather than coincidental, and half of every
# trickle batch is genuine, so the memory is warm at about thirty documents.
WARM_MEMORY_DOCS = 30
MIN_TRICKLE_RECALL = 0.90
MAX_TRICKLE_FALSE_FLAG = 0.02


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
        t = self.metrics.get("trickle") or {}
        if t:
            warm = t["with_memory"]["warm"]
            cold = t["with_memory"]["cold"]
            control = t["without_memory"]["farm"]
            lines += [
                "",
                f"trickle   {t['batches']} batches of {t['batch_size']} · "
                f"farm caught {warm['flagged'] + cold['flagged']}"
                f"/{warm['n'] + cold['n']} with memory, "
                f"{control['flagged']}/{control['n']} without",
                f"          cold (memory under {t['warm_at']} docs) "
                f"{cold['flag_rate']:.0%} of {cold['n']} · "
                f"warm {warm['flag_rate']:.0%} of {warm['n']}",
            ]
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


def trickle_evaluate(corpus_dir: Path, manifest: dict, jd_text: str,
                     sensitivity: str = "balanced",
                     signatures_path: str | Path | None = None) -> dict:
    """Deliver the trickle set batch by batch and measure what survives.

    Two runs over identical inputs. The control has no population memory, so
    every batch is judged on its own four documents — which is the situation a
    farm engineers by submitting thin. The treatment carries one memory across
    all the batches, exactly as an account does.

    Recall is reported in two slices, and only the warm one is gated: the
    engine cannot honestly call a farm on its second sighting, and neither
    could a recruiter. What it must do is stop being fooled once the same rig
    has come past enough times, and never charge a genuine applicant for the
    fact that a farm shares the queue with them.
    """
    batches: dict[int, list[dict]] = {}
    for entry in manifest["docs"]:
        if str(entry.get("set", "")).startswith("trickle"):
            batches.setdefault(int(entry.get("batch", 0)), []).append(entry)
    if not batches:
        return {}

    def deliver(memory) -> dict:
        farm = {"n": 0, "flagged": 0}
        warm = {"n": 0, "flagged": 0}
        cold = {"n": 0, "flagged": 0}
        human = {"n": 0, "flagged": 0}
        for index in sorted(batches):
            entries = batches[index]
            docs = [parse_file(corpus_dir / e["path"]) for e in entries]
            result = score_documents(docs, jd_text=jd_text,
                                     signatures_path=signatures_path,
                                     sensitivity=sensitivity,
                                     memory=memory, scan_id=f"trickle_{index}")
            known = result.stats["memory"]["documents_remembered"]
            by_path = {Path(a.doc.source_path).name: a for a in result.applications}
            for entry in entries:
                app = by_path[Path(entry["path"]).name]
                flagged = int(app.label in FLAGGED_LABELS)
                if entry["set"] == "trickle_farm":
                    farm["n"] += 1
                    farm["flagged"] += flagged
                    slot = warm if known >= WARM_MEMORY_DOCS else cold
                    slot["n"] += 1
                    slot["flagged"] += flagged
                else:
                    human["n"] += 1
                    human["flagged"] += flagged
        return {"farm": farm, "warm": warm, "cold": cold, "human": human}

    def rates(run: dict) -> dict:
        return {name: {**counts,
                       "flag_rate": counts["flagged"] / counts["n"]
                       if counts["n"] else 0.0}
                for name, counts in run.items()}

    return {
        "batches": len(batches),
        "batch_size": max(len(v) for v in batches.values()),
        "warm_at": WARM_MEMORY_DOCS,
        "with_memory": rates(deliver(InMemoryPopulationMemory())),
        "without_memory": rates(deliver(None)),
    }


def evaluate(corpus_dir: str | Path, sensitivity: str = "balanced",
             signatures_path: str | Path | None = None) -> EvalReport:
    corpus_dir = Path(corpus_dir)
    manifest = json.loads((corpus_dir / "manifest.json").read_text())
    jd_path = corpus_dir / manifest["jd"] if manifest.get("jd") else None
    jd_text = jd_path.read_text() if jd_path else ""

    # The whole corpus is scanned as one population — the human set has to hold
    # up while wrapper swarms and duplicate clusters are in the batch with it.
    # The JD itself lives in the corpus folder and must not be scored as one
    # of the applications, and neither may the trickle set: those documents
    # exist precisely to be delivered thin, and pouring them into one batch
    # would hand the engine the population the scenario is about withholding.
    trickle_docs = [e for e in manifest["docs"]
                    if str(e.get("set", "")).startswith("trickle")]
    exclude = {jd_path} if jd_path else set()
    exclude |= {corpus_dir / e["path"] for e in trickle_docs}
    result: ScanResult = scan(corpus_dir, jd_text=jd_text,
                              signatures_path=signatures_path,
                              sensitivity=sensitivity,
                              exclude=exclude)
    by_path = {str(Path(a.doc.source_path).resolve()): a for a in result.applications}

    sets: dict[str, dict] = {}
    slices: dict[str, dict] = {}
    misses: dict[str, list[str]] = {}

    for entry in manifest["docs"]:
        if str(entry.get("set", "")).startswith("trickle"):
            continue
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
        "trickle": trickle_evaluate(corpus_dir, manifest, jd_text,
                                    sensitivity=sensitivity,
                                    signatures_path=signatures_path),
    }

    human = sets.get("human_verified", {"n": 0, "flag_rate": 0.0, "flagged": 0})
    wrapper = sets.get("wrapper_generated", {"n": 0, "flag_rate": 0.0})
    evasion = sets.get("wrapper_evasion", {"n": 0, "flag_rate": 0.0})
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
        Gate("evasion_recall",
             evasion["n"] == 0 or evasion["flag_rate"] >= MIN_EVASION_RECALL,
             f"{evasion['flag_rate']:.2%} of {evasion['n']} track-covering "
             f"wrapper docs flagged (floor {MIN_EVASION_RECALL:.0%})"),
        Gate("attack_recall",
             attack["flag_rate"] >= MIN_ATTACK_RECALL,
             f"{attack['flag_rate']:.2%} of {attack['n']} attack docs flagged "
             f"(required {MIN_ATTACK_RECALL:.0%})"),
        Gate("fairness_slice",
             abs(z) < FAIRNESS_Z_LIMIT,
             f"|z|={abs(z):.2f} between native and non-native slices "
             f"(limit {FAIRNESS_Z_LIMIT})"),
    ]

    trickle = metrics["trickle"]
    if trickle:
        warm = trickle["with_memory"]["warm"]
        trickle_human = trickle["with_memory"]["human"]
        control = trickle["without_memory"]["farm"]
        gates += [
            Gate("trickle_recall",
                 warm["n"] > 0 and warm["flag_rate"] >= MIN_TRICKLE_RECALL,
                 f"{warm['flag_rate']:.2%} of {warm['n']} thin-delivered farm "
                 f"docs flagged once the memory held {WARM_MEMORY_DOCS}+ "
                 f"documents (floor {MIN_TRICKLE_RECALL:.0%}; "
                 f"{control['flag_rate']:.0%} with no memory at all)"),
            Gate("trickle_false_flag",
                 trickle_human["flag_rate"] < MAX_TRICKLE_FALSE_FLAG,
                 f"{trickle_human['flag_rate']:.2%} of {trickle_human['n']} "
                 f"genuine applicants sharing those thin batches flagged "
                 f"(limit {MAX_TRICKLE_FALSE_FLAG:.0%})"),
        ]
    return EvalReport(metrics=metrics, gates=gates)
