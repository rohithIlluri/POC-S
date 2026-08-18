"""Analyzer F — contact-handle forensics (population).

Two signals, both objective properties of the batch, never of the writer:

  * CONTACT_COLLISION — the same email or phone handle appears under a
    *different* candidate name. Recycled bodies get caught by dedupe; recycled
    contact infrastructure (one operator's mailbox behind many personas) gets
    caught here, even when every resume body is unique.
  * DISPOSABLE_CONTACT — the contact email lives on a throwaway-mail provider.
    Real applicants occasionally use these too, so it is WEAK by construction
    and can never flag alone.
"""

from __future__ import annotations

from ..types import Context, ParsedDoc, Severity, Signal

ANALYZER = "contact"

# Throwaway-mail providers. A provider list identifies infrastructure, not a
# person, and carries no correlation with who the applicant is.
DISPOSABLE_DOMAINS = {
    "mailinator.com", "guerrillamail.com", "10minutemail.com", "tempmail.com",
    "temp-mail.org", "yopmail.com", "sharklasers.com", "getnada.com",
    "dispostable.com", "trashmail.com", "fakeinbox.com", "mintemail.com",
    "maildrop.cc", "throwawaymail.com", "mohmal.com",
}


def analyze_contact(doc: ParsedDoc, ctx: Context) -> list[Signal]:
    signals: list[Signal] = []
    me = doc.identity

    if me.email_hash or me.phone_hash:
        # The pipeline pre-indexes every handle; direct analyzer calls build
        # the same index from the identity map on first use. Self stays in
        # the index — the query below filters it by doc_id.
        if not ctx.contact_email and not ctx.contact_phone and ctx.identity:
            for other_id, other in ctx.identity.items():
                if other.email_hash:
                    ctx.contact_email.setdefault(other.email_hash, []).append(
                        (other_id, other.name_hash))
                if other.phone_hash:
                    ctx.contact_phone.setdefault(other.phone_hash, []).append(
                        (other_id, other.name_hash))

        # A collision needs both names present and different — otherwise it
        # is the same person applying twice, or missing data.
        collisions = 0
        kinds: set[str] = set()
        seen: set[str] = set()
        if me.name_hash:
            for other_id, other_name in ctx.contact_email.get(me.email_hash, []):
                if other_id != doc.doc_id and other_name \
                        and other_name != me.name_hash:
                    collisions += 1
                    seen.add(other_id)
                    kinds.add("email")
            for other_id, other_name in ctx.contact_phone.get(me.phone_hash, []):
                if other_id != doc.doc_id and other_id not in seen \
                        and other_name and other_name != me.name_hash:
                    collisions += 1
                    kinds.add("phone")
        if collisions:
            signals.append(Signal(
                code="CONTACT_COLLISION", severity=Severity.STRONG,
                score_impact=0.7,
                evidence={"matching_docs_other_name": collisions,
                          "handles": sorted(kinds)},
                analyzer=ANALYZER,
            ))

    if me.email_domain in DISPOSABLE_DOMAINS:
        signals.append(Signal(
            code="DISPOSABLE_CONTACT", severity=Severity.WEAK, score_impact=0.25,
            evidence={"domain": me.email_domain}, analyzer=ANALYZER,
        ))

    return signals
