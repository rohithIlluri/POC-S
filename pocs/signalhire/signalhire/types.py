"""Core types shared by every stage of the pipeline.

The engine is a pure library: analyzers are `(ParsedDoc, Context) -> list[Signal]`
functions with no I/O, so they are trivially unit-testable and reproducible.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime
from enum import Enum
from typing import Any, Callable


class Severity(str, Enum):
    INFO = "info"           # context only, no bearing on the label
    WEAK = "weak"           # small score impact, can never flag on its own
    STRONG = "strong"       # major score impact
    DETERMINISTIC = "hard"  # objective fact (hidden text, exact duplicate)


@dataclass
class Signal:
    """One piece of evidence produced by one analyzer.

    `score_impact` is a signed weight: positive means "more suspicious",
    negative means "more likely a human-authored document" (e.g. a Word
    producer string). `evidence` is rendered verbatim in the UI, so it must
    stay human-readable and must never contain protected attributes.
    """

    code: str
    severity: Severity
    score_impact: float
    evidence: dict[str, Any]
    analyzer: str

    def as_dict(self) -> dict[str, Any]:
        return {
            "code": self.code,
            "severity": self.severity.value,
            "score_impact": round(self.score_impact, 3),
            "evidence": self.evidence,
            "analyzer": self.analyzer,
        }


@dataclass
class Identity:
    """Salted hashes only — the engine never carries clear-text candidate PII
    past the parse stage. `display_name` exists purely so the local CLI report
    is readable; it is never used as a matching key."""

    email_hash: str = ""
    phone_hash: str = ""
    name_hash: str = ""
    display_name: str = ""
    # Domain only, never the mailbox: kept clear-text for the disposable-domain
    # check. A domain identifies a provider, not a person.
    email_domain: str = ""

    def key(self) -> str:
        """The identity key used for "same person?" comparisons.

        Email is the strongest handle; fall back to phone, then name, then
        nothing (an unknown identity never matches another unknown identity).
        """
        return self.email_hash or self.phone_hash or self.name_hash or ""


@dataclass
class ParsedDoc:
    doc_id: str
    application_id: str
    source_path: str
    text: str
    pages: list[dict]              # per-page spans: bbox, font, size, color, text
    meta: dict[str, Any]           # producer, creator, dates, pdf version
    fonts: list[str]
    sha256: str = ""
    identity: Identity = field(default_factory=Identity)
    submitted_at: datetime | None = None
    layout_hash: str = ""
    minhash_sig: list[int] = field(default_factory=list)
    parse_error: str = ""

    @property
    def word_count(self) -> int:
        return len(self.text.split())


@dataclass
class Context:
    """Everything an analyzer needs that is *not* in the document itself.

    In production these come from Postgres/BigQuery rollups; in the Phase-0
    CLI they are computed over the batch being scanned, which is exactly the
    "population view" the product is built around — just scoped to one folder.
    """

    signatures: list[Any] = field(default_factory=list)      # signatures.GeneratorSignature
    template_index: dict[str, str] = field(default_factory=dict)   # layout_hash -> label
    template_index_loose: dict[str, str] = field(default_factory=dict)  # loose hash -> label
    template_allowlist: dict[str, str] = field(default_factory=dict)
    layout_counts: dict[str, int] = field(default_factory=dict)    # layout_hash -> distinct applicants
    global_idf: dict[str, float] = field(default_factory=dict)
    identity: dict[str, Identity] = field(default_factory=dict)    # doc_id -> Identity
    jd_text: str = ""
    lsh: Any = None                                                # MinHashLSH
    minhashes: dict[str, Any] = field(default_factory=dict)        # doc_id -> MinHash
    clusters: dict[str, str] = field(default_factory=dict)         # doc_id -> stable cluster id
    creation_windows: dict[str, int] = field(default_factory=dict) # window key -> distinct applicants


Analyzer = Callable[[ParsedDoc, Context], "list[Signal]"]


@dataclass
class ScoredApplication:
    doc: ParsedDoc
    signals: list[Signal]
    effort_score: int
    risk_score: int
    label: str

    def as_dict(self) -> dict[str, Any]:
        return {
            "doc_id": self.doc.doc_id,
            "application_id": self.doc.application_id,
            "source_path": self.doc.source_path,
            "candidate": self.doc.identity.display_name,
            "sha256": self.doc.sha256,
            "layout_hash": self.doc.layout_hash,
            "label": self.label,
            "effort_score": self.effort_score,
            "risk_score": self.risk_score,
            "reason_codes": [s.as_dict() for s in self.signals],
            "parse_error": self.doc.parse_error,
        }
