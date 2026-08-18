"""Phase-0 CLI: a folder of applications in, a triage report out.

    signalhire scan ./inbox --jd req.txt --html report.html
    signalhire corpus eval/corpus
    signalhire eval eval/corpus --json metrics.json
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

from .collector import merge_into, propose
from .corpus import build_corpus
from .evaluate import evaluate
from .pipeline import scan
from .report import render_html, render_json, render_text


def _read(path: str | None) -> str:
    return Path(path).read_text() if path else ""


def cmd_scan(args: argparse.Namespace) -> int:
    target = Path(args.target)
    if not target.exists():
        print(f"error: {target} does not exist", file=sys.stderr)
        return 2

    result = scan(target, jd_text=_read(args.jd),
                  signatures_path=args.signatures,
                  sensitivity=args.sensitivity)
    if not result.applications:
        print(f"error: no supported documents found under {target}", file=sys.stderr)
        return 2

    if args.html:
        Path(args.html).write_text(render_html(result, title=args.title))
        print(f"wrote HTML report: {args.html}")
    if args.json:
        Path(args.json).write_text(render_json(result))
        print(f"wrote JSON: {args.json}")
    if not args.quiet:
        print(render_text(result))
    return 0


def cmd_corpus(args: argparse.Namespace) -> int:
    entries = build_corpus(args.out, seed=args.seed, humans=args.humans,
                           wrappers=args.wrappers)
    print(f"wrote {len(entries)} synthetic documents to {args.out}")
    return 0


def cmd_eval(args: argparse.Namespace) -> int:
    corpus = Path(args.corpus)
    if not (corpus / "manifest.json").exists():
        print(f"error: no manifest.json in {corpus}. Run: signalhire corpus {corpus}",
              file=sys.stderr)
        return 2

    report = evaluate(corpus, sensitivity=args.sensitivity,
                      signatures_path=args.signatures)
    print(report.format())
    if report.metrics["misses"]:
        print("\nMisses:")
        for set_name, items in report.metrics["misses"].items():
            for item in items:
                print(f"  {set_name}: {item}")
    if args.json:
        Path(args.json).write_text(json.dumps(report.as_dict(), indent=2, default=str))
        print(f"\nwrote metrics: {args.json}")
    return 0 if report.passed else 1


def cmd_collect(args: argparse.Namespace) -> int:
    try:
        proposals = propose(args.samples, args.tool,
                            human_corpus=args.human_corpus,
                            confidence=args.confidence)
    except ValueError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2

    for p in proposals:
        state = "ACTIVE " if p.active else "held   "
        print(f"  [{state}] {p.kind:<15} {p.pattern:<28} {p.rationale}")
    merge_into(args.out, proposals)
    active = sum(1 for p in proposals if p.active)
    print(f"\nwrote {len(proposals)} proposals ({active} active) to {args.out}")
    if not args.human_corpus:
        print("note: no --human-corpus given, so nothing cleared the "
              "false-positive half of the validation gate.", file=sys.stderr)
    return 0


def cmd_web(args: argparse.Namespace) -> int:
    try:
        import uvicorn
    except ImportError:
        print("error: the web app needs the web extra — pip install -e '.[web]'",
              file=sys.stderr)
        return 2
    uvicorn.run("webapp.app:app", host=args.host, port=args.port)
    return 0


def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(
        prog="signalhire",
        description="Application authenticity engine — triage a batch of "
                    "applications into genuine effort / mass-generated / "
                    "needs review, with explainable reason codes.")
    sub = p.add_subparsers(dest="command", required=True)

    s = sub.add_parser("scan", help="score a folder of applications")
    s.add_argument("target", help="folder (or single file) of PDFs / text resumes")
    s.add_argument("--jd", help="path to the job description (enables JD-mirroring)")
    s.add_argument("--html", help="write a self-contained HTML triage report here")
    s.add_argument("--json", help="write machine-readable results here")
    s.add_argument("--title", default="Application triage report")
    s.add_argument("--signatures", help="collector-produced signature JSON to merge")
    s.add_argument("--sensitivity", default="balanced",
                   choices=("conservative", "balanced", "aggressive"))
    s.add_argument("--quiet", action="store_true", help="suppress the terminal summary")
    s.set_defaults(func=cmd_scan)

    c = sub.add_parser("corpus", help="generate the synthetic evaluation corpus")
    c.add_argument("out", nargs="?", default="eval/corpus")
    c.add_argument("--seed", type=int, default=7)
    c.add_argument("--humans", type=int, default=60)
    c.add_argument("--wrappers", type=int, default=40)
    c.set_defaults(func=cmd_corpus)

    e = sub.add_parser("eval", help="run the release gates against a corpus")
    e.add_argument("corpus", nargs="?", default="eval/corpus")
    e.add_argument("--json", help="write metrics.json here")
    e.add_argument("--signatures")
    e.add_argument("--sensitivity", default="balanced",
                   choices=("conservative", "balanced", "aggressive"))
    e.set_defaults(func=cmd_eval)

    col = sub.add_parser("collect",
                         help="propose signatures from a folder of samples "
                              "generated by one wrapper tool")
    col.add_argument("samples", help="folder of documents from a single tool")
    col.add_argument("--tool", required=True, help="tool label, e.g. teal_v3")
    col.add_argument("--human-corpus",
                     help="verified-human folder; required for a proposal to "
                          "clear the validation gate")
    col.add_argument("--out", default="signatures.json")
    col.add_argument("--confidence", type=float, default=0.7)
    col.set_defaults(func=cmd_collect)

    w = sub.add_parser("web", help="serve the recruiter web app (needs [web] extra)")
    w.add_argument("--host", default="127.0.0.1")
    w.add_argument("--port", type=int, default=8710)
    w.set_defaults(func=cmd_web)
    return p


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    return args.func(args)


if __name__ == "__main__":  # pragma: no cover
    raise SystemExit(main())
