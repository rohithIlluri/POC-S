"""Stage 4 — scoring, labels and reason codes.

Two scores per application:
  * effort_score (0–100): 100 = a tailored, human-authored application;
    low = mass-generated.
  * risk_score  (0–100): identity/fraud risk — recycled bodies under swapped
    identities, hidden text, prompt injection.

Both are *assistive*. No label in this module means "reject"; `mass_generated`
and `high_risk` mean "a human should look at this first, and here is why".

The weights below are seeds. Real weights come from the eval harness
(`eval/run.py`), tuned so the false-flag rate on verified-human resumes stays
under 2% before any pilot sees a score.
"""

from __future__ import annotations

from dataclasses import dataclass
from decimal import ROUND_HALF_UP, Decimal

from .evidence import combine, independent_families
from .types import ScoredApplication, Severity, Signal

LABELS = ("genuine", "needs_review", "mass_generated", "high_risk")

# Signals that speak to identity/fraud rather than effort. They feed the risk
# score and are excluded from the effort score so a fraud signal can never be
# explained away by a strong effort showing.
RISK_CODES = {"RECYCLED_IDENTITY", "HIDDEN_TEXT", "PROMPT_INJECTION",
              "CONTACT_COLLISION"}

# Weak signals from this many distinct analyzers escalate a below-review-line
# effort score to mass_generated (see the convergence comment in score()).
CONVERGENT_FAMILIES = 4


@dataclass(frozen=True)
class Thresholds:
    """Label boundaries on the calibrated 0-100 scale.

    Derived from the evaluation corpus rather than guessed: on it, wrapper
    output lands at effort 3, deliberately track-covering wrapper output at
    17-33, human-content-through-a-wrapper hybrids at 33-47, and verified
    human documents at 72-100 (median 100). The boundaries below sit in the
    gaps between those clusters, which is what keeps the human false-flag
    rate at zero while catching every evasion.

    `effort_multiplier` and `risk_multiplier` are retained for compatibility
    with callers that construct Thresholds directly; the combiner in
    evidence.py produces a probability, so they no longer scale the score.
    """

    effort_multiplier: float = 55.0
    risk_multiplier: float = 70.0
    high_risk_at: int = 60
    mass_generated_at: int = 35
    needs_review_at: int = 60

    @classmethod
    def for_sensitivity(cls, sensitivity: str = "balanced") -> "Thresholds":
        """The dashboard's conservative ↔ aggressive slider.

        Conservative moves work *out* of the queue (fewer flags, more genuine);
        aggressive pulls more borderline applications in for review.
        """
        if sensitivity == "conservative":
            return cls(high_risk_at=70, mass_generated_at=25, needs_review_at=45)
        if sensitivity == "aggressive":
            return cls(high_risk_at=50, mass_generated_at=45, needs_review_at=65)
        return cls()


def _exact(value: float) -> Decimal:
    """A float's intended decimal value, not its binary artifact.

    `Decimal(0.7)` is 0.6999999999999999555910790149937383830547332763671875;
    `Decimal(str(0.7))` is 0.7. Going through `str` recovers the number the
    weights were written as, which is what the scoring math is supposed to be
    about.
    """
    return Decimal(str(value))


def _clamp(value: Decimal | float) -> int:
    """Round half up, then clamp to 0-100."""
    if not isinstance(value, Decimal):
        value = _exact(value)
    quantized = value.quantize(Decimal("1"), rounding=ROUND_HALF_UP)
    return max(0, min(100, int(quantized)))


def score(signals: list[Signal], thresholds: Thresholds | None = None) -> dict:
    t = thresholds or Thresholds()

    # Scores are computed in Decimal, not binary floating point. These weights
    # sum to exactly 1.1, and 100 - 1.1 * 55 is exactly 39.5 — a boundary the
    # rounding rule below is supposed to resolve. In binary the same
    # expression lands on 39.49999999999999 or 39.50000000000001 depending on
    # accumulation order, which differs across interpreter versions: the same
    # resume scored 39 on Python 3.11 and 40 on 3.12. Near a threshold that is
    # a different *label*, and a reason code that cannot be reproduced for an
    # audit. No rounding mode can repair a value already on the wrong side of
    # the boundary, so the arithmetic itself has to be exact.
    effort_signals = [s for s in signals if s.code not in RISK_CODES]
    risk_signals = [s for s in signals if s.code in RISK_CODES]

    # Evidence is combined in log-odds with a per-family correlation discount
    # rather than summed (see evidence.py): JD_MIRROR_HIGH and JD_PHRASE_LIFT
    # co-occur at Jaccard 1.00 on the corpus, so summing them counts one fact
    # twice. The posterior is a probability, which is what makes scores
    # comparable across requisitions and signature-DB versions.
    p_mass, effort_contributions = combine(effort_signals)
    p_risk, risk_contributions = combine(risk_signals) if risk_signals else (0.0, [])

    # effort_score stays "100 = a tailored human application", so it is the
    # complement of the mass-generation posterior.
    effort = _clamp(_exact(100.0 * (1.0 - p_mass)))
    risk = _clamp(_exact(100.0 * p_risk))

    has_hard = any(s.severity is Severity.DETERMINISTIC for s in signals)
    has_strong_risk = any(
        s.severity is Severity.STRONG and s.code in RISK_CODES for s in signals
    )
    has_strong_effort = any(
        s.severity is Severity.STRONG and s.code not in RISK_CODES for s in signals
    )
    # Convergence: weak evidence from many *independent* analyzer families.
    # A pile of weak signals from one analyzer stays capped at needs_review,
    # but a document that is mildly suspicious to four unrelated detectors at
    # once is what deliberate track-covering looks like — each evasion
    # (strip the metadata, launder the format) trades a strong signal for
    # several weak ones spread across families.
    convergent = independent_families(effort_signals) >= CONVERGENT_FAMILIES

    if risk >= t.high_risk_at or has_hard or has_strong_risk:
        label = "high_risk"
    elif effort <= t.mass_generated_at and has_strong_effort:
        # `mass_generated` requires at least one STRONG signal by construction:
        # a pile of WEAK signals from one family can only reach `needs_review`.
        label = "mass_generated"
    elif effort <= t.needs_review_at and convergent:
        label = "mass_generated"
    elif effort <= t.needs_review_at:
        label = "needs_review"
    else:
        label = "genuine"

    # Order reason codes by how much each actually moved the score, not by raw
    # weight: a signal discounted for corroborating its own family should rank
    # below one that contributed independent evidence.
    effect = {c.code: abs(c.log_odds_delta)
              for c in effort_contributions + risk_contributions}
    ordered = sorted(signals,
                     key=lambda s: (-effect.get(s.code, 0.0),
                                    -abs(s.score_impact)))

    return {
        "effort_score": effort,
        "risk_score": risk,
        "label": label,
        "reason_codes": [s.as_dict() for s in ordered],
        # The audit trail: what each signal was worth, what correlation
        # discount it took, and how many independent families agreed.
        "evidence": {
            "p_mass_generated": round(p_mass, 4),
            "p_risk": round(p_risk, 4),
            "independent_families": independent_families(effort_signals),
            "contributions": [c.as_dict() for c in
                              effort_contributions + risk_contributions],
        },
    }


def score_document(doc, signals: list[Signal],
                   thresholds: Thresholds | None = None) -> ScoredApplication:
    result = score(signals, thresholds)
    order = {r["code"]: i for i, r in enumerate(result["reason_codes"])}
    return ScoredApplication(
        doc=doc,
        signals=sorted(signals, key=lambda s: order.get(s.code, 999)),
        effort_score=result["effort_score"],
        risk_score=result["risk_score"],
        label=result["label"],
        evidence=result["evidence"],
    )
