"""Analyzer G — shared-boilerplate detection (population).

MinHash dedupe needs ~80% body overlap; a paraphrase farm that rewrites half
of every sentence sails under it. But rewriting is lazy at the phrase level:
long word runs survive verbatim across the batch. This analyzer counts 8-word
shingles that appear under many *distinct* applicants and flags documents
whose text is substantially built from them.

Still a property of the population, never of the writer: a shared 8-gram is
shared regardless of who typed it, and the thresholds (many distinct
identities, a large fraction of the document) keep ordinary resume phrasing
("responsible for the development of...") from ever flagging — common phrases
are short; verbatim 8-word runs across 4+ strangers are manufactured.
"""

from __future__ import annotations

from ..types import Context, ParsedDoc, Severity, Signal
from .dedupe import body_text

ANALYZER = "boilerplate"

SHINGLE_N = 8
MIN_OTHER_OWNERS = 3      # shared with at least this many other applicants
MIN_GRAMS = 30            # below this the fraction is noise
WEAK_FRACTION = 0.25
STRONG_FRACTION = 0.60
# Sharing a third of the document is STRONG when the sharing is industrial:
# a study group might converge on phrasing with three classmates, not with
# fifteen strangers.
INDUSTRIAL_FRACTION = 0.35
INDUSTRIAL_OWNERS = 15


def gram_sequence(text: str, n: int = SHINGLE_N) -> list[str]:
    words = text.lower().split()
    return [" ".join(words[i:i + n]) for i in range(len(words) - n + 1)]


def analyze_boilerplate(doc: ParsedDoc, ctx: Context) -> list[Signal]:
    if not ctx.shingle_owners:
        return []
    body = ctx.bodies.get(doc.doc_id)
    grams = gram_sequence(body if body is not None else body_text(doc))
    if len(grams) < MIN_GRAMS:
        return []

    # An owner count includes this document's own identity, so "shared"
    # means at least MIN_OTHER_OWNERS + 1 distinct identities in total.
    shared = [g for g in set(grams)
              if ctx.shingle_owners.get(g, 0) >= MIN_OTHER_OWNERS + 1]
    fraction = len(shared) / len(set(grams))
    if fraction < WEAK_FRACTION:
        return []

    owner_counts = sorted(ctx.shingle_owners.get(g, 0) for g in shared)
    median_owners = owner_counts[len(owner_counts) // 2]
    strong = fraction >= STRONG_FRACTION or (
        fraction >= INDUSTRIAL_FRACTION and median_owners >= INDUSTRIAL_OWNERS)
    samples = sorted(shared, key=lambda g: -ctx.shingle_owners.get(g, 0))[:3]
    return [Signal(
        code="SHARED_BOILERPLATE",
        severity=Severity.STRONG if strong else Severity.WEAK,
        score_impact=0.5 if strong else 0.3,
        evidence={
            "shared_phrase_fraction": round(fraction, 2),
            "distinct_shared_phrases": len(shared),
            "median_applicants_per_phrase": median_owners,
            "min_other_applicants": MIN_OTHER_OWNERS,
            "samples": samples,
        },
        analyzer=ANALYZER,
    )]
