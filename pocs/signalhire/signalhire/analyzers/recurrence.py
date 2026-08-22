"""Analyzer H — cross-scan recurrence (population, over time).

Every other population analyzer asks "who else is in this batch?". A farm
answers that by making the batch small: two applications per requisition and
the duplicate cluster, the phrase swarm, the template swarm and the contact
collision all have nothing to compare against. The documents did not get any
more genuine — the crowd just moved out of the window.

This analyzer restores the crowd from the population memory (`memory.py`):
one-way keys left behind by every earlier scan in the same account. Two
documents a week for ten weeks is a population of twenty, and the fact that
each individual batch was small is not evidence of anything.

Everything here is a property of the population, never of the writer, and the
signals mirror the in-batch analyzers whose evidence they extend:

  * RECURRING_IDENTITY — this body was submitted before under a *different*
    named candidate. Cross-scan RECYCLED_IDENTITY; a risk signal.
  * RECURRING_BODY     — this body was submitted before by other applicants.
  * RECURRING_TEMPLATE — this exact structure has been arriving under many
    distinct applicants across scans. WEAK for the same reason
    TEMPLATE_SWARM is: popular legitimate templates recur too.
  * RECURRING_PHRASES  — the document is substantially built from phrases
    already seen from several other applicants in earlier scans.
  * RECURRING_CONTACT  — this mailbox or phone arrived before under another
    name. Cross-scan CONTACT_COLLISION; a risk signal.

The rules that keep an honest account from flagging itself over time are in
`memory.py`: the current scan is always excluded, a re-uploaded batch dedupes
to the records it already wrote, and one candidate applying to five
requisitions is one owner — recurrence needs *other* owners, every time.
"""

from __future__ import annotations

from ..memory import MemoryHits, probe, record_for
from ..types import Context, ParsedDoc, Severity, Signal
from .boilerplate import INDUSTRIAL_FRACTION, INDUSTRIAL_OWNERS

ANALYZER = "recurrence"

# Distinct earlier applicants sharing this exact structure before it means
# anything. Higher than the in-batch swarm threshold is tempting, but the
# in-batch count is a snapshot and this one accumulates, so the bar is the
# other way round: layout evidence stays WEAK no matter how large it gets.
TEMPLATE_OWNERS = 12

# Phrase recurrence uses the same shape as the in-batch boilerplate analyzer:
# a phrase counts as shared when several *other* applicants used it, and the
# document counts as built from shared phrases at a quarter of its sketch.
# The industrial escalation is imported from that analyzer rather than
# restated, so the two rules cannot drift apart: sharing a third of a document
# with fifteen strangers is the same fact whether the strangers arrived in one
# batch or over three months.
PHRASE_MIN_OWNERS = 3
PHRASE_WEAK_FRACTION = 0.25
PHRASE_STRONG_FRACTION = 0.60
PHRASE_MIN_SKETCH = 16

# One earlier sighting of the same body is a WEAK observation; two or more
# distinct earlier applicants is a farm.
BODY_STRONG_OWNERS = 2


def _record(doc: ParsedDoc, ctx: Context):
    """This document as the memory sees it. The pipeline builds every record
    once per batch; a direct analyzer call builds one on demand."""
    cached = ctx.memory_records.get(doc.doc_id)
    if cached is not None:
        return cached
    return record_for(doc, ctx, scan_id=ctx.scan_id)


def _hits(doc: ParsedDoc, ctx: Context) -> MemoryHits | None:
    """The pipeline probes the memory once per batch and caches the result.
    A direct analyzer call (tests, embedders) probes on demand instead."""
    cached = ctx.memory_hits.get(doc.doc_id)
    if cached is not None:
        return cached
    if ctx.memory is None:
        return None
    return probe(ctx.memory, _record(doc, ctx))


def analyze_recurrence(doc: ParsedDoc, ctx: Context) -> list[Signal]:
    hits = _hits(doc, ctx)
    if hits is None or hits.empty:
        return []

    me = doc.identity
    signals: list[Signal] = []

    # --- body recurrence --------------------------------------------------
    if hits.body:
        # Sightings by this same applicant are dropped first: one candidate
        # applying to five requisitions over five scans is one applicant, and
        # every number below describes the *other* people who sent this body.
        mine = _record(doc, ctx).owner
        others = [(record, similarity) for record, similarity in hits.body
                  if record.owner != mine]
    else:
        others = []

    if others:
        owners = {record.owner for record, _ in others}
        scans = {record.scan_id for record, _ in others if record.scan_id}
        best = max(similarity for _, similarity in others)
        first_seen = min(record.seen_at for record, _ in others)

        # A different *named* candidate is the cross-scan version of
        # RECYCLED_IDENTITY: both sides must carry a real handle and the
        # handles must disagree. An unknown identity is never "someone else".
        named = [record for record, _ in others
                 if record.identified and record.name_hash
                 and me.name_hash and record.name_hash != me.name_hash]
        if named:
            signals.append(Signal(
                code="RECURRING_IDENTITY", severity=Severity.STRONG,
                score_impact=0.75,
                evidence={
                    "earlier_applicants_other_identity":
                        len({r.owner for r in named}),
                    "earlier_scans": len({r.scan_id for r in named if r.scan_id}),
                    "max_similarity": round(best, 2),
                    "first_seen": first_seen.date().isoformat(),
                },
                analyzer=ANALYZER,
            ))
        else:
            strong = len(owners) >= BODY_STRONG_OWNERS
            signals.append(Signal(
                code="RECURRING_BODY",
                severity=Severity.STRONG if strong else Severity.WEAK,
                score_impact=0.6 if strong else 0.3,
                evidence={
                    "earlier_applicants": len(owners),
                    "earlier_scans": len(scans),
                    "max_similarity": round(best, 2),
                    "first_seen": first_seen.date().isoformat(),
                },
                analyzer=ANALYZER,
            ))

    # --- template recurrence ----------------------------------------------
    if hits.layout and hits.layout.owners >= TEMPLATE_OWNERS:
        signals.append(Signal(
            code="RECURRING_TEMPLATE", severity=Severity.WEAK,
            score_impact=0.2,
            evidence={
                "earlier_applicants_same_layout": hits.layout.owners,
                "earlier_scans": hits.layout.scans,
                "threshold": TEMPLATE_OWNERS,
            },
            analyzer=ANALYZER,
        ))

    # --- phrase recurrence ------------------------------------------------
    if hits.sketch_size >= PHRASE_MIN_SKETCH:
        shared = [key for key, owners_count in hits.phrase_owners.items()
                  if owners_count >= PHRASE_MIN_OWNERS]
        fraction = len(shared) / hits.sketch_size
        if fraction >= PHRASE_WEAK_FRACTION:
            counts = sorted(hits.phrase_owners[key] for key in shared)
            median_owners = counts[len(counts) // 2]
            strong = fraction >= PHRASE_STRONG_FRACTION or (
                fraction >= INDUSTRIAL_FRACTION
                and median_owners >= INDUSTRIAL_OWNERS)
            signals.append(Signal(
                code="RECURRING_PHRASES",
                severity=Severity.STRONG if strong else Severity.WEAK,
                score_impact=0.5 if strong else 0.3,
                evidence={
                    "recurring_phrase_fraction": round(fraction, 2),
                    "sampled_phrases": hits.sketch_size,
                    "median_earlier_applicants_per_phrase": median_owners,
                    "min_earlier_applicants": PHRASE_MIN_OWNERS,
                },
                analyzer=ANALYZER,
            ))

    # --- contact recurrence -----------------------------------------------
    if hits.contact and me.name_hash:
        others = {record.owner for record in hits.contact
                  if record.name_hash and record.name_hash != me.name_hash}
        if others:
            signals.append(Signal(
                code="RECURRING_CONTACT", severity=Severity.STRONG,
                score_impact=0.7,
                evidence={
                    "earlier_applicants_other_name": len(others),
                    "earlier_scans": len({record.scan_id for record in hits.contact
                                          if record.scan_id}),
                },
                analyzer=ANALYZER,
            ))

    return signals
