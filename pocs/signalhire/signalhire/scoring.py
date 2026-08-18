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
RISK_CODES = {"RECYCLED_IDENTITY", "HIDDEN_TEXT", "PROMPT_INJECTION"}


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


def _clamp(value: float) -> int:
    """Round half *up*, deterministically, then clamp to 0-100.

    Plain `round()` is round-half-to-even on a float that binary arithmetic
    has already nudged: summing the same weights can land on 39.5 under one
    interpreter and 39.50000000000001 under another, which then round to 39
    and 40. That is a score — and near a threshold, a label — that depends on
    the Python version a deployment happens to run. Quantizing through Decimal
    with ROUND_HALF_UP makes the boundary a property of the weights alone, so
    a reason code stays reproducible for an audit.
    """
    quantized = Decimal(value).quantize(Decimal("1"), rounding=ROUND_HALF_UP)
    return max(0, min(100, int(quantized)))


def score(signals: list[Signal], thresholds: Thresholds | None = None) -> dict:
    t = thresholds or Thresholds()

    effort_raw = sum(s.score_impact for s in signals if s.code not in RISK_CODES)
    risk_raw = sum(s.score_impact for s in signals if s.code in RISK_CODES)

    effort = _clamp(100 - effort_raw * t.effort_multiplier)
    risk = _clamp(risk_raw * t.risk_multiplier)

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
