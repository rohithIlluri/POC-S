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


def analyze_layout(doc: ParsedDoc, ctx: Context) -> list[Signal]:
    fp = layout_fingerprint(doc)
    doc.layout_hash = fp
    if not fp:
        return []

    signals: list[Signal] = []

    tmpl = ctx.template_index.get(fp)
    if tmpl:
        signals.append(Signal(
            code="KNOWN_TEMPLATE", severity=Severity.STRONG, score_impact=0.6,
            evidence={"template": tmpl}, analyzer=ANALYZER,
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
