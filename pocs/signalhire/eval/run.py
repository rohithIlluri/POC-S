#!/usr/bin/env python3
"""Nightly / CI evaluation entry point.

  python eval/run.py                      # synthetic corpus, built if missing
  python eval/run.py --corpus eval/corpus.local
  python eval/run.py --update-baseline    # accept the current numbers

Exits non-zero when a gate fails or when a headline metric regresses against
`eval/baseline.json` by more than the tolerance — so a change that quietly
trades human false-flags for wrapper recall cannot land unnoticed.

If `eval/corpus.local/` exists it is used by default: that is where the real
collected corpus goes (git-ignored — it holds candidate documents).
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from signalhire.corpus import build_corpus          # noqa: E402
from signalhire.evaluate import evaluate            # noqa: E402

HERE = Path(__file__).resolve().parent
DEFAULT_SYNTHETIC = HERE / "corpus"
LOCAL_CORPUS = HERE / "corpus.local"
BASELINE = HERE / "baseline.json"

# Headline metrics, and which direction counts as a regression.
TRACKED = {
    "human_verified_flag_rate": "lower_is_better",
    "wrapper_generated_flag_rate": "higher_is_better",
    "attack_flag_rate": "higher_is_better",
    # Thin-delivered farm output, once the population memory has a population.
    # Tracked separately from the single-batch numbers because it is the one
    # metric that measures what the engine *remembers* rather than what it was
    # handed in one go.
    "trickle_warm_flag_rate": "higher_is_better",
    "trickle_human_flag_rate": "lower_is_better",
}
TOLERANCE = 0.02


def headline(metrics: dict) -> dict[str, float]:
    sets = metrics["sets"]
    out = {
        f"{name}_flag_rate": round(sets.get(name, {}).get("flag_rate", 0.0), 4)
        for name in ("human_verified", "wrapper_generated", "attack")
    }
    trickle = metrics.get("trickle") or {}
    if trickle:
        with_memory = trickle["with_memory"]
        out["trickle_warm_flag_rate"] = round(with_memory["warm"]["flag_rate"], 4)
        out["trickle_human_flag_rate"] = round(with_memory["human"]["flag_rate"], 4)
    return out


def regressions(current: dict[str, float], baseline: dict[str, float]) -> list[str]:
    out = []
    for key, direction in TRACKED.items():
        if key not in baseline or key not in current:
            continue
        delta = current[key] - baseline[key]
        worse = delta > TOLERANCE if direction == "lower_is_better" \
            else delta < -TOLERANCE
        if worse:
            out.append(f"{key}: {baseline[key]:.2%} -> {current[key]:.2%}")
    return out


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--corpus", help="corpus directory (defaults to "
                                     "eval/corpus.local if present)")
    ap.add_argument("--sensitivity", default="balanced")
    ap.add_argument("--signatures")
    ap.add_argument("--metrics-out", default=str(HERE / "metrics.json"))
    ap.add_argument("--update-baseline", action="store_true")
    args = ap.parse_args(argv)

    if args.corpus:
        corpus = Path(args.corpus)
    elif LOCAL_CORPUS.exists():
        corpus = LOCAL_CORPUS
    else:
        corpus = DEFAULT_SYNTHETIC
        if not (corpus / "manifest.json").exists():
            print(f"building synthetic corpus in {corpus} ...")
            build_corpus(corpus)

    # Recorded relative to eval/ so a baseline stays comparable across
    # machines and CI runners.
    corpus_key = os.path.relpath(corpus, HERE)

    report = evaluate(corpus, sensitivity=args.sensitivity,
                      signatures_path=args.signatures)
    print(report.format())
    if report.metrics["misses"]:
        print("\nMisses:")
        for set_name, items in report.metrics["misses"].items():
            for item in items:
                print(f"  {set_name}: {item}")

    current = headline(report.metrics)
    Path(args.metrics_out).write_text(json.dumps(
        {"headline": current, **report.as_dict()}, indent=2, default=str))

    if report.metrics["synthetic"]:
        print("\nNOTE: synthetic corpus. These gates prove the pipeline is "
              "wired correctly, not that the engine is accurate on real "
              "documents. Point --corpus at a collected corpus before a pilot.")

    failed = not report.passed
    if args.update_baseline:
        BASELINE.write_text(json.dumps(
            {"corpus": corpus_key, "synthetic": report.metrics["synthetic"],
             "headline": current}, indent=2))
        print(f"\nbaseline updated: {BASELINE}")
    elif BASELINE.exists():
        baseline = json.loads(BASELINE.read_text())
        if baseline.get("corpus") == corpus_key:
            found = regressions(current, baseline.get("headline", {}))
            for line in found:
                print(f"REGRESSION  {line}")
            failed = failed or bool(found)
        else:
            print(f"\n(baseline was recorded on {baseline.get('corpus')}, "
                  "not comparing)")

    return 1 if failed else 0


if __name__ == "__main__":
    raise SystemExit(main())
