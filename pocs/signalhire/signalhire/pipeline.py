"""Stage orchestration: documents in, scored applications out.

    parse  →  build population context  →  per-document analyzers
           →  population analyzers      →  score

In production stages 1–2 run per document on Cloud Run and stage 3 reads
rollups from BigQuery/Redis. Here the whole batch is the population, which is
what makes the Phase-0 CLI a real demo rather than a toy: pointed at one req's
inbox folder, it sees exactly the cross-applicant patterns the product sells.
"""

from __future__ import annotations

import math
from collections import Counter, defaultdict
from dataclasses import dataclass, field
from pathlib import Path

from . import analyzers as analyzer_registry
from .analyzers.dedupe import body_text, minhash, new_index
from .analyzers.jd_mirror import terms
from .analyzers.layout import layout_fingerprint
from .parse import discover, parse_file
from .scoring import Thresholds, score_document
from .signatures import SIGNATURE_DB_VERSION, load_signatures, template_indexes
from .types import Context, ParsedDoc, ScoredApplication

# Below this many documents, batch-derived idf is noise; jd_mirror falls back
# to its static common-terms list instead.
MIN_CORPUS_FOR_IDF = 25


@dataclass
class ScanResult:
    applications: list[ScoredApplication]
    context: Context
    stats: dict = field(default_factory=dict)

    def by_label(self) -> dict[str, list[ScoredApplication]]:
        out: dict[str, list[ScoredApplication]] = defaultdict(list)
        for app in self.applications:
            out[app.label].append(app)
        return dict(out)

    def as_dict(self) -> dict:
        return {
            "stats": self.stats,
            "applications": [a.as_dict() for a in self.applications],
        }


def build_context(docs: list[ParsedDoc], jd_text: str = "",
                  signatures_path: str | Path | None = None) -> Context:
    signatures = load_signatures(signatures_path)
    known_templates, allowlist = template_indexes(signatures)

    ctx = Context(
        signatures=signatures,
        template_index=known_templates,
        template_allowlist=allowlist,
        jd_text=jd_text,
        identity={d.doc_id: d.identity for d in docs},
    )

    # Layout population counts: distinct applicants per structural fingerprint.
    layout_applicants: dict[str, set[str]] = defaultdict(set)
    for d in docs:
        d.layout_hash = layout_fingerprint(d)
        if d.layout_hash:
            layout_applicants[d.layout_hash].add(d.identity.key() or d.doc_id)
    ctx.layout_counts = {h: len(a) for h, a in layout_applicants.items()}

    # Corpus idf for JD-mirroring rare-term selection.
    if len(docs) >= MIN_CORPUS_FOR_IDF:
        df: Counter = Counter()
        for d in docs:
            df.update(set(terms(d.text)))
        n = len(docs)
        ctx.global_idf = {t: math.log((n + 1) / (c + 1)) for t, c in df.items()}

    # Near-duplicate index: build every signature first, then insert, so the
    # result never depends on the order files were read.
    ctx.lsh = new_index()
    for d in docs:
        body = body_text(d)
        if body.strip():
            ctx.minhashes[d.doc_id] = minhash(body)
    with ctx.lsh.insertion_session() as session:
        for doc_id, m in ctx.minhashes.items():
            session.insert(doc_id, m)

    return ctx


def score_documents(docs: list[ParsedDoc], jd_text: str = "",
                    signatures_path: str | Path | None = None,
                    sensitivity: str = "balanced") -> ScanResult:
    ctx = build_context(docs, jd_text=jd_text, signatures_path=signatures_path)
    thresholds = Thresholds.for_sensitivity(sensitivity)

    applications: list[ScoredApplication] = []
    for doc in docs:
        signals = []
        for analyze in analyzer_registry.PER_DOCUMENT:
            signals.extend(analyze(doc, ctx))
        for analyze in analyzer_registry.POPULATION:
            signals.extend(analyze(doc, ctx))
        applications.append(score_document(doc, signals, thresholds))

    applications.sort(key=lambda a: (-a.risk_score, a.effort_score))

    label_counts = Counter(a.label for a in applications)
    stats = {
        "documents": len(applications),
        "parse_failures": sum(1 for a in applications if a.doc.parse_error),
        "labels": {label: label_counts.get(label, 0)
                   for label in ("genuine", "needs_review",
                                 "mass_generated", "high_risk")},
        "sensitivity": sensitivity,
        "signature_db_version": SIGNATURE_DB_VERSION,
        "signatures_active": len(ctx.signatures),
        "jd_provided": bool(jd_text.strip()),
        "idf_source": "batch" if ctx.global_idf else "static_fallback",
    }
    return ScanResult(applications=applications, context=ctx, stats=stats)


def scan(target: str | Path, jd_text: str = "",
         signatures_path: str | Path | None = None,
         sensitivity: str = "balanced",
         exclude: set[Path] | None = None) -> ScanResult:
    """Parse every supported document under `target` and score the batch."""
    paths = discover(target, exclude=exclude)
    docs = [parse_file(p) for p in paths]
    result = score_documents(docs, jd_text=jd_text,
                             signatures_path=signatures_path,
                             sensitivity=sensitivity)
    result.stats["source"] = str(target)
    return result
