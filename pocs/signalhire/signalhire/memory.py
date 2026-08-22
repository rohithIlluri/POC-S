"""Cross-scan population memory — the population is not just this batch.

Every population analyzer in the engine (dedupe, boilerplate, layout swarms,
contact collisions) needs a crowd inside one scan before it can say anything.
That is the gap a farm walks through: submit two documents per requisition
instead of fifty and every population signal goes quiet, while the documents
themselves stay exactly as manufactured as before. Trickle scale was the
honest miss recorded in it5–it7.

This module closes it by making the population *cumulative*. Each scanned
document leaves behind a small set of one-way keys — MinHash bands of the
identity-masked body, the structural layout hash, hashed contact handles, and
a bottom-k sketch of its 8-word phrase set — and the next scan asks what it
has seen before. Two documents a week for ten weeks is a population of twenty;
it is only invisible if you refuse to remember.

What is deliberately *not* stored: text, names, addresses, or anything a
document could be reconstructed from. A band key is a hash of a hash, the
phrase sketch is a hash sample, the owner is the same salted identity key the
rest of the engine uses. That is what makes the cross-tenant version of this
index (§2.3 of the build plan) legitimate — "this body hit fourteen other
agencies this week" without a syllable of anyone's resume moving between
tenants.

The engine defines the *port*, never the storage: `PopulationMemory` is a
protocol with two methods, `InMemoryPopulationMemory` is the reference
implementation used by tests and the eval harness, and the webapp binds a
SQLite-backed one scoped to a billing org. The analyzers stay pure functions
of `(ParsedDoc, Context)`.
"""

from __future__ import annotations

import hashlib
import struct
from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Iterable, Protocol, Sequence, runtime_checkable

# MinHash bands: 128 permutations in 16 bands of 8 rows. A band collision is a
# *candidate*, never a verdict — candidates are verified against the stored
# signature at the same Jaccard threshold the in-batch deduper uses, which is
# the "retrieve wide, verify hard" rule analyzer E already follows.
BAND_ROWS = 8

# How many phrase hashes to keep per document. The bottom-k sketch of a
# document's 8-gram set is a uniform sample of it, so the shared *fraction*
# measured on sketches estimates the shared fraction of the whole document —
# at a fixed 64 keys per document instead of one per phrase.
PHRASE_SKETCH = 64

KIND_BODY = "body"
KIND_LAYOUT = "layout"
KIND_CONTACT = "contact"
KIND_PHRASE = "phrase"
KINDS = (KIND_BODY, KIND_LAYOUT, KIND_CONTACT, KIND_PHRASE)


def _digest(*parts: str) -> str:
    return hashlib.sha256("\x1f".join(parts).encode()).hexdigest()[:24]


def band_keys(signature: Sequence[int], rows: int = BAND_ROWS) -> tuple[str, ...]:
    """LSH band keys for a MinHash signature.

    The band index is part of the key so band 0 of one document can never
    collide with band 3 of another — that would be a match manufactured by the
    encoding rather than by the text.
    """
    if not signature:
        return ()
    packed = [int(v) & 0xFFFFFFFFFFFFFFFF for v in signature]
    out = []
    for i in range(0, len(packed) - rows + 1, rows):
        blob = struct.pack(f"<{rows}Q", *packed[i:i + rows])
        out.append(f"b{i // rows}:" + hashlib.sha256(blob).hexdigest()[:16])
    return tuple(out)


def phrase_sketch(grams: Iterable[str], k: int = PHRASE_SKETCH) -> tuple[str, ...]:
    """Bottom-k hash sample of a document's phrase set.

    Hashing first makes the sample content-addressed rather than
    position-addressed: two documents that share a phrase share its key, so
    the sketches of different documents are directly comparable.
    """
    hashes = {hashlib.sha256(g.encode()).hexdigest()[:16] for g in grams}
    return tuple(sorted(hashes)[:k])


def jaccard(a: Sequence[int], b: Sequence[int]) -> float:
    """MinHash similarity estimate between two stored signatures."""
    if not a or not b or len(a) != len(b):
        return 0.0
    return sum(1 for x, y in zip(a, b) if x == y) / len(a)


@dataclass(frozen=True)
class MemoryRecord:
    """One document as the memory remembers it: keys, not content."""

    scan_id: str
    owner: str                       # salted identity key, or content surrogate
    seen_at: datetime
    name_hash: str = ""
    identified: bool = False         # owner is a real identity handle, not a surrogate
    keys: dict[str, tuple[str, ...]] = field(default_factory=dict)
    signature: tuple[int, ...] = ()

    def keys_of(self, kind: str) -> tuple[str, ...]:
        return tuple(self.keys.get(kind, ()))

    def dedupe_key(self) -> str:
        """Identifies "this owner, this document" so that re-scanning a folder
        does not manufacture a population out of one batch scanned twice.

        The most common honest re-run — a recruiter re-uploading yesterday's
        batch with three files added — must leave the counts exactly where
        they were, or the memory becomes a machine for flagging its own users.
        """
        body = "|".join(self.keys_of(KIND_BODY))
        layout = "|".join(self.keys_of(KIND_LAYOUT))
        return _digest(self.owner, body or layout or "empty", layout)

    def as_dict(self) -> dict:
        return {
            "scan_id": self.scan_id,
            "owner": self.owner,
            "seen_at": self.seen_at.isoformat(),
            "name_hash": self.name_hash,
            "identified": self.identified,
            "keys": {k: list(v) for k, v in self.keys.items()},
            "signature": list(self.signature),
        }

    @classmethod
    def from_dict(cls, raw: dict) -> "MemoryRecord":
        seen = raw.get("seen_at")
        return cls(
            scan_id=raw.get("scan_id", ""),
            owner=raw.get("owner", ""),
            seen_at=(datetime.fromisoformat(seen) if isinstance(seen, str)
                     else seen or datetime.now(timezone.utc)),
            name_hash=raw.get("name_hash", ""),
            identified=bool(raw.get("identified")),
            keys={k: tuple(v) for k, v in (raw.get("keys") or {}).items()},
            signature=tuple(raw.get("signature") or ()),
        )


@dataclass(frozen=True)
class KeyCounts:
    """How much of the past a key touches, without naming any of it."""

    owners: int
    scans: int


@runtime_checkable
class PopulationMemory(Protocol):
    """The port. Storage lives outside the engine; this is all it needs."""

    def lookup(self, kind: str,
               keys: Sequence[str]) -> dict[str, list[MemoryRecord]]:
        """Prior records indexed under each of `keys`, keyed by key."""

    def counts(self, kind: str, keys: Sequence[str], *,
               exclude_scan: str = "",
               exclude_owner: str = "") -> dict[str, KeyCounts]:
        """Distinct prior owners and scans per key, without materialising them.

        Phrase and layout evidence is a *count* — how many other applicants,
        how many other scans — and counts are the part that grows without
        bound. A phrase from a farm that has sent five thousand applications
        is held by five thousand records, and fetching all of them to take the
        size of a set would make the engine slowest against exactly the
        adversary it exists for. A storage backend answers this with one
        aggregate; `count_via_lookup` covers backends that cannot.
        """

    def remember(self, records: Sequence[MemoryRecord]) -> None:
        """Record this scan's documents for future scans to find."""

    def size(self) -> int:
        """How many documents are remembered (for the audit line in the UI)."""


def count_via_lookup(memory, kind: str, keys: Sequence[str], *,
                     exclude_scan: str = "",
                     exclude_owner: str = "") -> dict[str, KeyCounts]:
    """`counts` for a memory that only implements `lookup`."""
    out: dict[str, KeyCounts] = {}
    for key, records in memory.lookup(kind, keys).items():
        kept = [r for r in records
                if r.scan_id != exclude_scan and r.owner != exclude_owner]
        if kept:
            out[key] = KeyCounts(owners=len({r.owner for r in kept}),
                                 scans=len({r.scan_id for r in kept if r.scan_id}))
    return out


class InMemoryPopulationMemory:
    """Reference implementation: a dict. Used by tests and the eval harness,
    and by anyone embedding the engine who only needs one process's history."""

    def __init__(self) -> None:
        self._index: dict[tuple[str, str], list[MemoryRecord]] = {}
        self._seen: set[str] = set()

    def lookup(self, kind: str,
               keys: Sequence[str]) -> dict[str, list[MemoryRecord]]:
        out: dict[str, list[MemoryRecord]] = {}
        for key in keys:
            hits = self._index.get((kind, key))
            if hits:
                out[key] = list(hits)
        return out

    def counts(self, kind: str, keys: Sequence[str], *,
               exclude_scan: str = "",
               exclude_owner: str = "") -> dict[str, KeyCounts]:
        return count_via_lookup(self, kind, keys, exclude_scan=exclude_scan,
                                exclude_owner=exclude_owner)

    def remember(self, records: Sequence[MemoryRecord]) -> None:
        for record in records:
            fingerprint = record.dedupe_key()
            if fingerprint in self._seen:
                continue
            self._seen.add(fingerprint)
            for kind, keys in record.keys.items():
                for key in keys:
                    self._index.setdefault((kind, key), []).append(record)

    def size(self) -> int:
        return len(self._seen)


@dataclass
class MemoryHits:
    """What the memory had to say about one document, before it is judged.

    Body and contact hits carry records because they have to be verified — a
    band collision is checked against the stored signature, a contact handle
    against the name it arrived under. Layout and phrase hits carry counts
    only: there is nothing to verify and a great deal to fetch.
    """

    body: list[tuple[MemoryRecord, float]] = field(default_factory=list)
    contact: list[MemoryRecord] = field(default_factory=list)
    layout: KeyCounts | None = None
    phrase_owners: dict[str, int] = field(default_factory=dict)
    sketch_size: int = 0

    @property
    def empty(self) -> bool:
        return not (self.body or self.contact or self.layout
                    or self.phrase_owners)


def probe(memory: PopulationMemory, record: MemoryRecord,
          verify_at: float = 0.8) -> MemoryHits:
    """Ask the memory what it has seen of this document before.

    Records from `record.scan_id` are excluded throughout: the current batch
    is the in-batch analyzers' job, and counting it twice would double every
    population signal the moment memory was switched on.
    """
    hits = MemoryHits()

    def prior(records: Iterable[MemoryRecord]) -> list[MemoryRecord]:
        return [r for r in records if r.scan_id != record.scan_id]

    seen_body: set[str] = set()
    for candidates in memory.lookup(KIND_BODY,
                                    record.keys_of(KIND_BODY)).values():
        for candidate in prior(candidates):
            token = candidate.dedupe_key()
            if token in seen_body:
                continue
            similarity = jaccard(record.signature, candidate.signature)
            if similarity >= verify_at:
                seen_body.add(token)
                hits.body.append((candidate, similarity))

    # Same-owner sightings are excluded in the query rather than after it: a
    # template count is "how many *other* applicants", and one candidate
    # reusing their own resume layout is not a swarm of one.
    layout = counts_for(memory, KIND_LAYOUT, record.keys_of(KIND_LAYOUT),
                        record)
    if layout:
        hits.layout = max(layout.values(), key=lambda c: c.owners)

    for candidates in memory.lookup(KIND_CONTACT,
                                    record.keys_of(KIND_CONTACT)).values():
        hits.contact.extend(prior(candidates))

    sketch = record.keys_of(KIND_PHRASE)
    hits.sketch_size = len(sketch)
    hits.phrase_owners = {
        key: counted.owners
        for key, counted in counts_for(memory, KIND_PHRASE, sketch,
                                       record).items()}

    return hits


def counts_for(memory, kind: str, keys: Sequence[str],
               record: MemoryRecord) -> dict[str, KeyCounts]:
    """Prior owner counts for `keys`, excluding this scan and this applicant."""
    if not keys:
        return {}
    counter = getattr(memory, "counts", None)
    if counter is None:
        return count_via_lookup(memory, kind, keys,
                                exclude_scan=record.scan_id,
                                exclude_owner=record.owner)
    return counter(kind, keys, exclude_scan=record.scan_id,
                   exclude_owner=record.owner)


# Below this many 8-word runs a phrase sketch is noise, exactly as in the
# in-batch boilerplate analyzer.
MIN_GRAMS_FOR_SKETCH = 30


def record_for(doc, ctx=None, scan_id: str = "",
               seen_at: datetime | None = None) -> MemoryRecord:
    """Derive the memory record for a parsed document.

    Both the pipeline (writing) and the recurrence analyzer (reading) go
    through this one function, so a document is always looked up under exactly
    the keys it would be stored under.

    Documents with no readable identity get a *content* surrogate owner rather
    than a per-file one: the same resume re-uploaded as a PDF and then as a
    DOCX is one anonymous owner, not two, and cannot recur against itself.
    Manufactured documents are near-duplicates of each other, not byte
    twins — they keep separate surrogates and still count as separate owners.
    """
    from .analyzers.boilerplate import gram_sequence
    from .analyzers.dedupe import body_text, minhash
    from .analyzers.layout import has_authored_layout, layout_fingerprint

    body = None if ctx is None else ctx.bodies.get(doc.doc_id)
    if body is None:
        body = body_text(doc)

    sketch_source = None if ctx is None else ctx.minhashes.get(doc.doc_id)
    if sketch_source is None and body.strip():
        sketch_source = minhash(body)
    signature = (tuple(int(v) for v in sketch_source.hashvalues)
                 if sketch_source is not None else ())

    ident = doc.identity
    owner, identified = ident.key(), True
    if not owner:
        identified = False
        owner = "anon:" + _digest(body or doc.sha256 or doc.doc_id)

    keys: dict[str, tuple[str, ...]] = {}
    if signature:
        keys[KIND_BODY] = band_keys(signature)
    layout_hash = doc.layout_hash or layout_fingerprint(doc)
    if layout_hash and has_authored_layout(doc):
        keys[KIND_LAYOUT] = (layout_hash,)
    contact = tuple(h for h in (ident.email_hash, ident.phone_hash) if h)
    if contact:
        keys[KIND_CONTACT] = contact
    grams = gram_sequence(body)
    if len(grams) >= MIN_GRAMS_FOR_SKETCH:
        keys[KIND_PHRASE] = phrase_sketch(grams)

    return MemoryRecord(
        scan_id=scan_id,
        owner=owner,
        seen_at=seen_at or doc.submitted_at or datetime.now(timezone.utc),
        name_hash=ident.name_hash,
        identified=identified,
        keys=keys,
        signature=signature,
    )
