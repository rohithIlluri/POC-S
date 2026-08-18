"""Analyzer D — JD mirroring.

Auto-tailoring bots ingest the job description and emit a resume fitted to it.
Two measurable consequences:

  1. *Rare-term overlap* — the resume contains nearly every uncommon term in
     the JD. Real people hit some; almost nobody hits 90%.
  2. *Phrase lift* — exact multi-word runs copied out of the JD.

Both are properties of the match between two documents, never of the writer's
fluency, so neither can proxy for a protected attribute.
"""

from __future__ import annotations

import re
from collections import Counter

from ..types import Context, ParsedDoc, Severity, Signal

ANALYZER = "jd_mirror"

NGRAM_N = 5
PHRASE_LIFT_THRESHOLD = 3
RARE_IDF = 2.0
MIN_RARE_TERMS = 8       # below this the overlap ratio is statistical noise

STOP = set(
    "the a an and or of to in for with on at by is are was were be been being as "
    "this that these those from will shall can may our your their we you they it "
    "not but if then than into over under about across per via each all any more "
    "most other such have has had do does did also including include includes".split()
)

# Terms that appear in nearly every JD *and* nearly every resume. Excluded from
# the rare set so a small local corpus does not mistake them for rare.
COMMON_JOB_TERMS = set(
    "experience years work working team teams skills skill role position job company "
    "responsibilities requirements qualifications preferred required strong excellent "
    "ability able knowledge understanding development developer engineer engineering "
    "software business project projects manage management support customer client "
    "communication written verbal degree bachelor master university college "
    "environment technologies technology tools design designing build building "
    "candidate candidates applicant opportunity benefits salary remote hybrid onsite".split()
)


def terms(text: str) -> Counter:
    words = re.findall(r"[a-zA-Z][a-zA-Z0-9+#.\-]{2,}", text.lower())
    return Counter(w.strip(".-") for w in words if w not in STOP)


def word_sequence(text: str) -> list[str]:
    return re.findall(r"[a-z0-9+#.\-]+", text.lower())


def ngrams(words: list[str], n: int = NGRAM_N) -> set[tuple[str, ...]]:
    return {tuple(words[i:i + n]) for i in range(len(words) - n + 1)}


def rare_jd_terms(jd_terms: Counter, ctx: Context) -> set[str]:
    """JD terms uncommon in the surrounding corpus.

    With a real corpus this is `global_idf` (a BigQuery rollup). The Phase-0
    CLI computes idf over the batch, which is thin, so when the corpus is too
    small to be meaningful the pipeline leaves `global_idf` empty and we fall
    back to a static common-terms list.
    """
    if ctx.global_idf:
        return {t for t in jd_terms
                if ctx.global_idf.get(t, RARE_IDF + 1) >= RARE_IDF
                and t not in COMMON_JOB_TERMS and len(t) >= 4}
    return {t for t in jd_terms if t not in COMMON_JOB_TERMS and len(t) >= 4}


def analyze_jd_mirror(doc: ParsedDoc, ctx: Context) -> list[Signal]:
    if not ctx.jd_text.strip() or not doc.text.strip():
        return []

    # The pipeline caches JD artifacts per scan; direct calls compute them.
    jd_terms = ctx.jd_terms if ctx.jd_terms is not None else terms(ctx.jd_text)
    doc_terms = terms(doc.text)
    rare = rare_jd_terms(jd_terms, ctx)
    signals: list[Signal] = []

    if len(rare) >= MIN_RARE_TERMS:
        matched = rare & set(doc_terms)
        overlap = len(matched) / len(rare)
        evidence = {
            "rare_term_overlap": round(overlap, 2),
            "rare_terms_total": len(rare),
            "matched_sample": sorted(matched)[:10],
        }
        if overlap > 0.85:
            signals.append(Signal(
                code="JD_MIRROR_EXTREME", severity=Severity.STRONG, score_impact=0.5,
                evidence=evidence, analyzer=ANALYZER,
            ))
        elif overlap > 0.70:
            signals.append(Signal(
                code="JD_MIRROR_HIGH", severity=Severity.WEAK, score_impact=0.25,
                evidence=evidence, analyzer=ANALYZER,
            ))

    jd_grams = (ctx.jd_ngrams if ctx.jd_ngrams is not None
                else ngrams(word_sequence(ctx.jd_text)))
    lifted = jd_grams & ngrams(word_sequence(doc.text))
    if len(lifted) >= PHRASE_LIFT_THRESHOLD:
        signals.append(Signal(
            code="JD_PHRASE_LIFT", severity=Severity.WEAK, score_impact=0.2,
            evidence={
                f"lifted_{NGRAM_N}grams": len(lifted),
                "samples": [" ".join(g) for g in sorted(lifted)[:3]],
            },
            analyzer=ANALYZER,
        ))

    return signals
