"""Analyzer A — metadata / toolchain forensics.

Matches the PDF's producer/creator strings and timestamp structure against the
generator signature DB. This is the workhorse analyzer and the one that
compounds as the signature DB grows.
"""

from __future__ import annotations

from datetime import datetime

from ..parse import parse_pdf_date
from ..signatures import DEFAULT_TITLES
from ..types import Context, ParsedDoc, Severity, Signal

ANALYZER = "forensics"

# 10-minute buckets for the population creation-time clustering below.
BATCH_WINDOW_MINUTES = 10
BATCH_TIMESTAMP_THRESHOLD = 5


def creation_window(created: datetime) -> str:
    return created.replace(minute=created.minute - created.minute % BATCH_WINDOW_MINUTES,
                           second=0, microsecond=0).isoformat()


def _tool_match(sig, producer: str, others: list) -> Signal:
    suspicious = sig.confidence > 0
    evidence = {
        "matched": sig.tool_label,
        "producer_string": producer,
        "signature_version": sig.version,
    }
    if others:
        evidence["also_matched"] = sorted(s.tool_label for s in others)
    return Signal(
        code="GEN_TOOL_MATCH" if suspicious else "HUMAN_TOOL_MATCH",
        severity=(
            (Severity.STRONG if sig.confidence >= 0.6 else Severity.WEAK)
            if suspicious else Severity.INFO
        ),
        score_impact=sig.confidence,
        evidence=evidence,
        analyzer=ANALYZER,
    )


def analyze_forensics(doc: ParsedDoc, ctx: Context) -> list[Signal]:
    signals: list[Signal] = []
    producer = f'{doc.meta.get("producer", "")} {doc.meta.get("creator", "")}'.strip()

    if producer:
        matches = [sig for sig in ctx.signatures
                   if sig.kind == "producer_regex" and sig.matches(producer)]
        # One producer string is one fact: score only the strongest match in
        # each direction instead of double-counting overlapping signatures
        # (e.g. 'Puppeteer 22 / HeadlessChrome' matching both patterns).
        suspicious = [s for s in matches if s.confidence > 0]
        human = [s for s in matches if s.confidence <= 0]
        if suspicious:
            best = max(suspicious, key=lambda s: s.confidence)
            signals.append(_tool_match(best, producer,
                                       [s for s in suspicious if s is not best]))
        if human:
            best = min(human, key=lambda s: s.confidence)
            signals.append(_tool_match(best, producer,
                                       [s for s in human if s is not best]))
    elif doc.meta.get("source_kind") not in ("plaintext", "html", "rtf", "doc"):
        # A stripped producer string is itself mildly interesting: authoring
        # tools all stamp one, and hygiene-conscious generators strip it.
        # Formats that routinely carry no producer (pasted text, HTML, RTF,
        # legacy .doc) are exempt — otherwise those submissions start out
        # suspicious. PDF, DOCX and ODT keep the signal: their authoring
        # tools always stamp one.
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

    # Many distinct applicants whose documents were generated inside the same
    # ten-minute window is a batch off one rig, not a coincidence of humans.
    if created:
        window = creation_window(created)
        distinct = ctx.creation_windows.get(window, 0)
        if distinct >= BATCH_TIMESTAMP_THRESHOLD:
            signals.append(Signal(
                code="BATCH_TIMESTAMP_CLUSTER", severity=Severity.WEAK,
                score_impact=0.2,
                evidence={"window_start": window,
                          "distinct_applicants_in_window": distinct,
                          "threshold": BATCH_TIMESTAMP_THRESHOLD},
                analyzer=ANALYZER,
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
