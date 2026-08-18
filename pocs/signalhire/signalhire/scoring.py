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

from .types import ScoredApplication, Severity, Signal

LABELS = ("genuine", "needs_review", "mass_generated", "high_risk")

# Signals that speak to identity/fraud rather than effort. They feed the risk
# score and are excluded from the effort score so a fraud signal can never be
# explained away by a strong effort showing.
RISK_CODES = {"RECYCLED_IDENTITY", "HIDDEN_TEXT", "PROMPT_INJECTION",
              "CONTACT_COLLISION"}


@dataclass(frozen=True)
class Thresholds:
    effort_multiplier: float = 55.0
    risk_multiplier: float = 70.0
    high_risk_at: int = 60
    mass_generated_at: int = 35
    needs_review_at: int = 55

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
    effort_raw = sum((_exact(s.score_impact) for s in signals
                      if s.code not in RISK_CODES), Decimal(0))
    risk_raw = sum((_exact(s.score_impact) for s in signals
                    if s.code in RISK_CODES), Decimal(0))

    effort = _clamp(Decimal(100) - effort_raw * _exact(t.effort_multiplier))
    risk = _clamp(risk_raw * _exact(t.risk_multiplier))

    has_hard = any(s.severity is Severity.DETERMINISTIC for s in signals)
    has_strong_risk = any(
        s.severity is Severity.STRONG and s.code in RISK_CODES for s in signals
    )
    has_strong_effort = any(
        s.severity is Severity.STRONG and s.code not in RISK_CODES for s in signals
    )

    if risk >= t.high_risk_at or has_hard or has_strong_risk:
        label = "high_risk"
    elif effort <= t.mass_generated_at and has_strong_effort:
        # `mass_generated` requires at least one STRONG signal by construction:
        # a pile of WEAK signals can only ever reach `needs_review`.
        label = "mass_generated"
    elif effort <= t.needs_review_at:
        label = "needs_review"
    else:
        label = "genuine"

    return {
        "effort_score": effort,
        "risk_score": risk,
        "label": label,
        "reason_codes": [
            s.as_dict() for s in sorted(signals, key=lambda s: -abs(s.score_impact))
        ],
    }


def score_document(doc, signals: list[Signal],
                   thresholds: Thresholds | None = None) -> ScoredApplication:
    result = score(signals, thresholds)
    return ScoredApplication(
        doc=doc,
        signals=sorted(signals, key=lambda s: -abs(s.score_impact)),
        effort_score=result["effort_score"],
        risk_score=result["risk_score"],
        label=result["label"],
    )
