"""Analyzer B — layout fingerprinting.

Wrapper templates emit structurally identical PDFs — same fonts, same sizes,
same column geometry — even when the text differs completely. We hash the
*structure only* (text content is deliberately excluded) and match that hash
both against the signature DB and across the population.
"""

from __future__ import annotations

import hashlib
import math
from collections import Counter

from ..types import Context, ParsedDoc, Severity, Signal

ANALYZER = "layout"
SWARM_THRESHOLD = 25

# Formats whose "layout" is synthesized by our own parser (fixed font, fixed
# size, fixed x) rather than authored. Fingerprinting them is meaningless and
# actively dangerous: thirty pasted ATS text bodies would all share one hash
# and fire TEMPLATE_SWARM on honest applicants.
SYNTHETIC_LAYOUT_KINDS = {"plaintext", "html", "rtf", "odt", "doc"}


def has_authored_layout(doc: ParsedDoc) -> bool:
    return doc.meta.get("source_kind") not in SYNTHETIC_LAYOUT_KINDS


def layout_fingerprint(doc: ParsedDoc) -> str:
    """Structure-only hash: font name, rounded size, x-start bucketed to 5pt,
    and how many runs share each of those triples (log2-bucketed).

    Order-independent (sorted) so a reordered section keeps its fingerprint,
    and text-independent so two candidates using the same template collide on
    purpose. The count buckets matter: without them every single-font,
    single-column document in the world hashes to the same value, which would
    make TEMPLATE_SWARM fire on everything simple.
    """
    feats: Counter = Counter(
        (b["font"], round(b["size"]), int(b["bbox"][0] // 5))
        for page in doc.pages
        for b in page["blocks"]
        if b["text"].strip()
    )
    if not feats:
        return ""
    canon = "|".join(
        f"{font}:{size}:{x}:{int(math.log2(count)) + 1}"
        for (font, size, x), count in sorted(feats.items())
    )
    canon += f"#pages={len(doc.pages)}"
    return hashlib.sha256(canon.encode()).hexdigest()[:16]


def loose_fingerprint(doc: ParsedDoc) -> str:
    """Perturbation-resistant secondary hash: fonts and rounded sizes only.

    The exact fingerprint dies to trivial evasion — nudge the margin 5pt or
    add one run and the hash changes. This one survives both, at the cost of
    being too loose for population swarm counting, so it is used *only* to
    match known wrapper templates from the signature DB, where a false match
    still needs a colliding font/size profile that cleared the collector's
    human-corpus gate."""
    feats: Counter = Counter(
        (b["font"], round(b["size"]))
        for page in doc.pages
        for b in page["blocks"]
        if b["text"].strip()
    )
    if not feats:
        return ""
    canon = "|".join(
        f"{font}:{size}:{int(math.log2(count)) + 1}"
        for (font, size), count in sorted(feats.items())
    )
    return "L" + hashlib.sha256(canon.encode()).hexdigest()[:15]


def analyze_layout(doc: ParsedDoc, ctx: Context) -> list[Signal]:
    if not has_authored_layout(doc):
        return []
    fp = doc.layout_hash or layout_fingerprint(doc)
    doc.layout_hash = fp
    if not fp:
        return []

    signals: list[Signal] = []

    tmpl = ctx.template_index.get(fp)
    if tmpl:
        signals.append(Signal(
            code="KNOWN_TEMPLATE", severity=Severity.STRONG, score_impact=0.6,
            evidence={"template": tmpl, "match": "exact_structure"},
            analyzer=ANALYZER,
        ))
    else:
        loose = ctx.template_index_loose.get(loose_fingerprint(doc))
        if loose:
            signals.append(Signal(
                code="KNOWN_TEMPLATE", severity=Severity.STRONG, score_impact=0.6,
                evidence={"template": loose, "match": "font_profile",
                          "note": "exact structure perturbed; font/size "
                                  "profile still matches the template"},
                analyzer=ANALYZER,
            ))

    allow = ctx.template_allowlist.get(fp)
    if allow:
        # A known human template (Google Docs / Overleaf). Say so, and suppress
        # the swarm signal below — these legitimately swarm.
        return signals + [Signal(
            code="ALLOWLISTED_TEMPLATE", severity=Severity.INFO, score_impact=-0.1,
            evidence={"template": allow}, analyzer=ANALYZER,
        )]

    count = ctx.layout_counts.get(fp, 0)
    if count >= SWARM_THRESHOLD:
        # WEAK by construction: popular legitimate templates swarm too, so this
        # can never produce a `mass_generated` label on its own.
        signals.append(Signal(
            code="TEMPLATE_SWARM", severity=Severity.WEAK, score_impact=0.2,
            evidence={"same_layout_applicants": count,
                      "threshold": SWARM_THRESHOLD}, analyzer=ANALYZER,
        ))
    return signals
