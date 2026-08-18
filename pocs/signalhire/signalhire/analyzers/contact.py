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
        collisions = 0
        kinds: set[str] = set()
        for other_id, other in ctx.identity.items():
            if other_id == doc.doc_id:
                continue
            # A collision needs both names present and different — otherwise
            # it is the same person applying twice, or missing data.
            if not (me.name_hash and other.name_hash
                    and me.name_hash != other.name_hash):
                continue
            if me.email_hash and me.email_hash == other.email_hash:
                collisions += 1
                kinds.add("email")
            elif me.phone_hash and me.phone_hash == other.phone_hash:
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
