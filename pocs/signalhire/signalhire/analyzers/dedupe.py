"""Analyzer E — population near-duplicate clustering.

The highest-value analyzer, and the one a single-document detector cannot
have: any one resume looks fine, but the *population* exposes mass generation
and identity recycling.

MinHash over 5-word shingles, indexed in an LSH bucket at Jaccard >= 0.8. In
production the index lives in Redis and spans the tenant's whole history (and,
on hashed signatures only, other tenants). In the Phase-0 CLI the population
is the batch being scanned.
"""

from __future__ import annotations

import re

from datasketch import MinHash, MinHashLSH

from ..types import Context, Identity, ParsedDoc, Severity, Signal

ANALYZER = "dedupe"

NUM_PERM = 128
# The LSH index is tuned for *recall* and the candidates it returns are then
# verified exactly. Banding alone is probabilistic: a resume recycled with a
# swapped name and contact block lands around Jaccard 0.84, which an index
# banded at 0.8 misses roughly a third of the time. Retrieve wide, verify hard.
LSH_QUERY_THRESHOLD = 0.6
SIMILARITY_THRESHOLD = 0.8
SHINGLE_K = 5
SPRAY_THRESHOLD = 10
CLUSTER_THRESHOLD = 3


_EMAIL = re.compile(r"[\w.+-]+@[\w-]+\.[\w.-]+")
_PHONE = re.compile(r"(?:\+?\d{1,3}[\s.-]?)?(?:\(?\d{3}\)?[\s.-]?)\d{3}[\s.-]?\d{4}")
_LINK = re.compile(r"https?://\S+|(?:www|linkedin\.com|github\.com)/?\S*", re.I)


def body_text(doc: ParsedDoc) -> str:
    """The document with its identity block masked out.

    Recycled-resume fraud swaps exactly one thing: the name, email and phone
    at the top. On a 400-word resume that block is a big enough share of the
    text to drag Jaccard from ~0.95 down to ~0.70 — below any threshold that
    is safe to use for unrelated documents. Masking the identity tokens lets
    the *body* be compared on its own, which is the thing that was recycled.
    """
    text = _EMAIL.sub(" ", doc.text)
    text = _PHONE.sub(" ", text)
    text = _LINK.sub(" ", text)
    name = doc.identity.display_name.strip()
    if name:
        for token in name.split():
            if len(token) > 2:
                text = re.sub(rf"\b{re.escape(token)}\b", " ", text, flags=re.I)
    return " ".join(text.split())


def shingles(text: str, k: int = SHINGLE_K) -> set[str]:
    words = text.lower().split()
    if len(words) <= k:
        return {" ".join(words)} if words else set()
    return {" ".join(words[i:i + k]) for i in range(len(words) - k + 1)}


def minhash(text: str) -> MinHash:
    m = MinHash(num_perm=NUM_PERM)
    for sh in shingles(text):
        m.update(sh.encode("utf8"))
    return m


def new_index() -> MinHashLSH:
    return MinHashLSH(threshold=LSH_QUERY_THRESHOLD, num_perm=NUM_PERM)


def _different_person(a: Identity, b: Identity) -> bool:
    """True only when both documents carry a usable identity handle and the
    handles disagree. An unknown identity never counts as a different person —
    that would manufacture fraud signals out of missing data."""
    ka, kb = a.key(), b.key()
    return bool(ka) and bool(kb) and ka != kb


def _same_person(a: Identity, b: Identity) -> bool:
    """Both handles present and equal. Unknown identities are neither the same
    person nor a different one — they only ever count toward DUP_CLUSTER."""
    ka, kb = a.key(), b.key()
    return bool(ka) and bool(kb) and ka == kb


def analyze_dedupe(doc: ParsedDoc, ctx: Context) -> list[Signal]:
    if ctx.lsh is None or not doc.text.strip():
        return []

    m = ctx.minhashes.get(doc.doc_id)
    if m is None:
        return []
    doc.minhash_sig = [int(v) for v in m.hashvalues]

    similarity = {
        d: ctx.minhashes[d].jaccard(m)
        for d in ctx.lsh.query(m)
        if d != doc.doc_id and d in ctx.minhashes
    }
    near = [d for d, j in similarity.items() if j >= SIMILARITY_THRESHOLD]
    if not near:
        return []

    me = ctx.identity.get(doc.doc_id, Identity())
    other_person = [d for d in near
                    if _different_person(me, ctx.identity.get(d, Identity()))]
    same_person = [d for d in near
                   if _same_person(me, ctx.identity.get(d, Identity()))]

    # Stable id from the pipeline's union-find over all verified pairs, so
    # every member of a ring reports the same cluster even when similarity is
    # not transitive; the local fallback covers direct analyzer calls.
    cluster_id = ctx.clusters.get(doc.doc_id) or "cl_" + min([doc.doc_id, *near])[:8]
    signals: list[Signal] = []

    if other_person:
        # Same resume body submitted under a different identity — the strongest
        # fraud signal the engine produces.
        signals.append(Signal(
            code="RECYCLED_IDENTITY", severity=Severity.STRONG, score_impact=0.8,
            evidence={
                "cluster": cluster_id,
                "matching_docs_other_identity": len(other_person),
                "max_similarity": round(max(similarity[d] for d in other_person), 2),
                "similarity_threshold": SIMILARITY_THRESHOLD,
            },
            analyzer=ANALYZER,
        ))

    if len(same_person) >= SPRAY_THRESHOLD:
        signals.append(Signal(
            code="SPRAY_APPLY", severity=Severity.WEAK, score_impact=0.3,
            evidence={"cluster": cluster_id,
                      "same_doc_applications": len(same_person)},
            analyzer=ANALYZER,
        ))

    if len(near) >= CLUSTER_THRESHOLD and not other_person:
        signals.append(Signal(
            code="DUP_CLUSTER", severity=Severity.WEAK, score_impact=0.25,
            evidence={"cluster": cluster_id, "cluster_size": len(near) + 1},
            analyzer=ANALYZER,
        ))

    return signals
