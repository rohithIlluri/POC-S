"""Analyzer A — metadata / toolchain forensics.

Matches the PDF's producer/creator strings and timestamp structure against the
generator signature DB. This is the workhorse analyzer and the one that
compounds as the signature DB grows.
"""

from __future__ import annotations

from ..parse import parse_pdf_date
from ..signatures import DEFAULT_TITLES
from ..types import Context, ParsedDoc, Severity, Signal

ANALYZER = "forensics"


def analyze_forensics(doc: ParsedDoc, ctx: Context) -> list[Signal]:
    signals: list[Signal] = []
    producer = f'{doc.meta.get("producer", "")} {doc.meta.get("creator", "")}'.strip()

    if producer:
        for sig in ctx.signatures:
            if sig.kind != "producer_regex" or not sig.matches(producer):
                continue
            suspicious = sig.confidence > 0
            signals.append(Signal(
                code="GEN_TOOL_MATCH" if suspicious else "HUMAN_TOOL_MATCH",
                severity=(
                    (Severity.STRONG if sig.confidence >= 0.6 else Severity.WEAK)
                    if suspicious else Severity.INFO
                ),
                score_impact=sig.confidence,
                evidence={
                    "matched": sig.tool_label,
                    "producer_string": producer,
                    "signature_version": sig.version,
                },
                analyzer=ANALYZER,
            ))
    elif doc.meta.get("source_kind") != "plaintext":
        # A stripped producer string is itself mildly interesting: authoring
        # tools all stamp one, and hygiene-conscious generators strip it.
        # Plain-text sources (ATS text fields) never have one, so they are
        # exempt — otherwise every text submission starts out suspicious.
        signals.append(Signal(
            code="NO_PRODUCER", severity=Severity.WEAK, score_impact=0.15,
            evidence={"note": "no producer or creator string in PDF metadata"},
            analyzer=ANALYZER,
        ))

    # parse_pdf lowercases every metadata key, so only the lowercase forms exist.
    created = parse_pdf_date(doc.meta.get("creationdate"))
    modified = parse_pdf_date(doc.meta.get("moddate"))

    # Wrapper sites render the PDF seconds before submitting it. Humans attach
    # a file they exported earlier.
    if created and doc.submitted_at:
        delta = (doc.submitted_at - created).total_seconds()
        if 0 <= delta < 120:
            signals.append(Signal(
                code="FRESH_GENERATION", severity=Severity.WEAK, score_impact=0.3,
                evidence={"seconds_before_submit": int(delta)}, analyzer=ANALYZER,
            ))

    # created == modified to the second → single-shot machine export, never a
    # document that was opened, edited and re-saved.
    if created and modified and created == modified:
        signals.append(Signal(
            code="SINGLE_SHOT_PDF", severity=Severity.WEAK, score_impact=0.15,
            evidence={"created": created.isoformat()}, analyzer=ANALYZER,
        ))

    title = str(doc.meta.get("title") or "").strip().lower()
    if title and title in DEFAULT_TITLES:
        signals.append(Signal(
            code="DEFAULT_TITLE", severity=Severity.WEAK, score_impact=0.1,
            evidence={"title": title}, analyzer=ANALYZER,
        ))

    if doc.parse_error:
        signals.append(Signal(
            code="PARSE_FAILED", severity=Severity.INFO, score_impact=0.0,
            evidence={"error": doc.parse_error}, analyzer=ANALYZER,
        ))

    return signals
