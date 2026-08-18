"""Signature collection (§6 of the build plan) — how the moat compounds.

Weekly loop: generate 5–10 resumes from a wrapper tool with obviously-fake
personas, drop them in a folder, and run

    signalhire collect samples/teal --tool teal_v3 \\
        --human-corpus eval/corpus/human_verified --out signatures.json

The command proposes two kinds of signature — a producer regex and any layout
fingerprint shared across the samples — and then applies the validation gate:

    a signature activates only if it matches *every* sample from its tool and
    *zero* documents in the verified-human corpus.

Anything that fails the gate is still written out, with `active: false`, so a
human can look at it. Inactive signatures are never loaded by the engine, so a
bad proposal cannot poison scoring.
"""

from __future__ import annotations

import json
import re
from collections import Counter
from dataclasses import dataclass
from pathlib import Path

from .analyzers.layout import layout_fingerprint
from .parse import discover, parse_file
from .signatures import SIGNATURE_DB_VERSION

# A version suffix is the part of a producer string that changes between
# releases; the stable prefix is what identifies the toolchain.
_VERSIONISH = re.compile(r"[\d._/-]*\d[\d._/-]*$")


@dataclass
class Proposal:
    kind: str
    pattern: str
    tool_label: str
    confidence: float
    active: bool
    source: str
    version: str
    rationale: str

    def as_dict(self) -> dict:
        return {
            "kind": self.kind, "pattern": self.pattern,
            "tool_label": self.tool_label, "confidence": self.confidence,
            "active": self.active, "source": self.source,
            "version": self.version, "rationale": self.rationale,
        }


def producer_stem(producer: str) -> str:
    """'react-pdf 3.1.14' -> 'react-pdf'; 'wkhtmltopdf0.12.6' -> 'wkhtmltopdf'."""
    head = producer.strip().split()[0] if producer.strip() else ""
    return _VERSIONISH.sub("", head).strip("-_./")


def propose(sample_dir: str | Path, tool_label: str,
            human_corpus: str | Path | None = None,
            confidence: float = 0.7) -> list[Proposal]:
    samples = [parse_file(p) for p in discover(sample_dir)]
    if not samples:
        raise ValueError(f"no documents found in {sample_dir}")

    human_docs = [parse_file(p) for p in discover(human_corpus)] if human_corpus else []
    human_producers = [
        f'{d.meta.get("producer", "")} {d.meta.get("creator", "")}' for d in human_docs
    ]
    human_layouts = {layout_fingerprint(d) for d in human_docs}
    source = f"collect:{Path(sample_dir).name}:n={len(samples)}"

    proposals: list[Proposal] = []

    # --- producer regex ----------------------------------------------------
    stems = Counter(
        stem for d in samples
        if (stem := producer_stem(str(d.meta.get("producer", ""))))
    )
    for stem, count in stems.items():
        pattern = re.escape(stem)
        matcher = re.compile(pattern, re.I)
        covers_all = count == len(samples)
        human_hits = sum(1 for p in human_producers if matcher.search(p))
        proposals.append(Proposal(
            kind="producer_regex", pattern=pattern, tool_label=tool_label,
            confidence=confidence,
            active=covers_all and human_hits == 0,
            source=source, version=SIGNATURE_DB_VERSION,
            rationale=(f"matched {count}/{len(samples)} samples, "
                       f"{human_hits} human-corpus collisions"
                       f"{'' if human_docs else ' (no human corpus supplied)'}"),
        ))

    # --- layout fingerprints ----------------------------------------------
    layouts = Counter(fp for d in samples if (fp := layout_fingerprint(d)))
    for fp, count in layouts.items():
        if count < 2:
            continue  # a one-off layout is a document, not a template
        collides = fp in human_layouts
        proposals.append(Proposal(
            kind="layout_hash", pattern=fp, tool_label=f"{tool_label}_template",
            confidence=0.6,
            active=count == len(samples) and not collides,
            source=source, version=SIGNATURE_DB_VERSION,
            rationale=(f"shared by {count}/{len(samples)} samples, "
                       f"{'collides with' if collides else 'no collision in'} "
                       "the human corpus"),
        ))

    return proposals


def merge_into(path: str | Path, proposals: list[Proposal]) -> list[dict]:
    """Append proposals to a signature JSON file, keyed on (kind, pattern).

    An existing entry is never silently overwritten: a re-collection updates
    its rationale and source but keeps whatever `active` value a human set.
    """
    path = Path(path)
    existing: list[dict] = json.loads(path.read_text()) if path.exists() else []
    index = {(e["kind"], e["pattern"]): e for e in existing}

    for proposal in proposals:
        key = (proposal.kind, proposal.pattern)
        if key in index:
            index[key].update({"source": proposal.source,
                               "rationale": proposal.rationale})
        else:
            existing.append(proposal.as_dict())
            index[key] = existing[-1]

    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(existing, indent=2))
    return existing
