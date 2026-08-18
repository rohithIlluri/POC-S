"""Evidence combination — how independent signals become one score.

The seed scoring summed `score_impact` across every signal. That treats each
signal as an independent piece of evidence, which measurably they are not: on
the evaluation corpus `JD_MIRROR_HIGH` and `JD_PHRASE_LIFT` co-occur at
Jaccard 1.00 (every document with one has the other), and
`BATCH_TIMESTAMP_CLUSTER` co-occurs with `FRESH_GENERATION` just as tightly.
Both pairs are two measurements of a single underlying fact — "this resume was
fitted to the posting", "these files came off one rig in one run" — so summing
them counts that fact twice, and a document that trips one family in five ways
outscores a document caught independently by three different families.

The combiner here fixes that with two ideas:

1. **Log-odds accumulation.** Each signal carries a likelihood ratio: how much
   more often it appears on mass-generated applications than on genuine ones.
   Independent evidence combines by adding log-likelihood-ratios (the naive
   Bayes update), which saturates gracefully — the tenth signal can no longer
   drag a score past certainty the way a tenth additive penalty could.

2. **Per-family saturation.** Signals are grouped by analyzer family. Within a
   family the strongest signal counts fully and each additional one is
   discounted geometrically, because a family's signals are correlated by
   construction. Across families evidence combines undiscounted: four families
   agreeing is four genuinely separate observations.

The result is a calibrated posterior probability rather than an arbitrary sum,
which is what makes the `effort_score` comparable across requisitions and
across signature-DB versions.

Explainability is preserved exactly: every signal keeps its own contribution,
and `explain()` returns the per-signal breakdown that the report renders.
"""

from __future__ import annotations

import math
from dataclasses import dataclass

from .types import Severity, Signal

# Prior odds that an arbitrary application is mass-generated. Deliberately
# conservative: the engine must not start out suspicious of anyone. Tuned so a
# document with no signals scores 100 effort.
PRIOR_MASS_GENERATED = 0.15

# Each additional signal within one analyzer family is discounted by this
# factor: the 1st counts fully, the 2nd at 0.45, the 3rd at 0.20, and so on.
# Correlated evidence should add something, but never as much as the first
# independent observation.
FAMILY_DECAY = 0.45

# Likelihood ratios by severity — how much more likely this evidence is on a
# mass-generated application than on a genuine one. These are the numbers the
# eval harness tunes; the severity tiers keep them interpretable and keep the
# "weak evidence can never flag alone" property provable rather than emergent.
SEVERITY_LR = {
    Severity.DETERMINISTIC: 40.0,   # objective fact (hidden text, injection)
    Severity.STRONG: 6.0,
    Severity.WEAK: 1.8,
    Severity.INFO: 1.0,             # never moves the posterior
}

# A negative-impact signal (a human authoring tool) is evidence the other way.
HUMAN_TOOL_LR = 0.35


@dataclass(frozen=True)
class Contribution:
    """One signal's audited effect on the score."""

    code: str
    analyzer: str
    likelihood_ratio: float
    family_rank: int          # 0 = strongest in its family
    discount: float           # correlation discount applied to this signal
    log_odds_delta: float     # what it actually moved the posterior by

    def as_dict(self) -> dict:
        return {
            "code": self.code,
            "analyzer": self.analyzer,
            "likelihood_ratio": round(self.likelihood_ratio, 3),
            "family_rank": self.family_rank,
            "correlation_discount": round(self.discount, 3),
            "log_odds_delta": round(self.log_odds_delta, 4),
        }


def signal_lr(signal: Signal) -> float:
    """The likelihood ratio a single signal carries.

    Severity sets the tier; `score_impact` modulates within it, so a 0.9-impact
    STRONG signal outweighs a 0.5-impact one without either crossing into the
    next tier. A negative impact is evidence of human authorship.
    """
    if signal.score_impact < 0:
        # Scale toward 1.0 (no evidence) as the impact approaches zero.
        return 1.0 - (1.0 - HUMAN_TOOL_LR) * min(1.0, abs(signal.score_impact) / 0.4)
    base = SEVERITY_LR.get(signal.severity, 1.0)
    if base <= 1.0:
        return 1.0
    # Modulate within the tier: impact 0.5 is the tier's nominal strength.
    scale = 0.5 + min(1.5, max(0.0, signal.score_impact) / 0.5) * 0.5
    return 1.0 + (base - 1.0) * scale


def combine(signals: list[Signal],
            prior: float = PRIOR_MASS_GENERATED,
            family_decay: float = FAMILY_DECAY) -> tuple[float, list[Contribution]]:
    """Combine signals into P(mass-generated) plus a per-signal audit trail.

    Returns `(posterior, contributions)`. Contributions are ordered by the
    magnitude of their effect, which is the order the report renders them in.

    A document with no evidence against it scores as fully genuine. The prior
    is the *starting point for evidence to move*, never a penalty in itself:
    charging every applicant 15% suspicion for existing would make "we found
    nothing" indistinguishable from "we found something small", and the whole
    product rests on never flagging someone without a reason we can name.
    """
    if not any(signal_lr(s) != 1.0 for s in signals):
        return 0.0, [
            Contribution(code=s.code, analyzer=s.analyzer, likelihood_ratio=1.0,
                         family_rank=0, discount=0.0, log_odds_delta=0.0)
            for s in signals
        ]

    log_odds = math.log(prior / (1.0 - prior))
    contributions: list[Contribution] = []

    # Group by analyzer family, strongest first, so the discount lands on the
    # weaker corroborating signals rather than the primary one.
    families: dict[str, list[Signal]] = {}
    for s in signals:
        families.setdefault(s.analyzer, []).append(s)

    for analyzer, group in families.items():
        ranked = sorted(group, key=lambda s: -abs(signal_lr(s) - 1.0))
        for rank, s in enumerate(ranked):
            lr = signal_lr(s)
            if lr == 1.0:
                # INFO signals are context only; record them with zero effect
                # so the report can still show they were evaluated.
                contributions.append(Contribution(
                    code=s.code, analyzer=analyzer, likelihood_ratio=1.0,
                    family_rank=rank, discount=0.0, log_odds_delta=0.0))
                continue
            discount = family_decay ** rank
            delta = math.log(lr) * discount
            log_odds += delta
            contributions.append(Contribution(
                code=s.code, analyzer=analyzer, likelihood_ratio=lr,
                family_rank=rank, discount=discount, log_odds_delta=delta))

    posterior = 1.0 / (1.0 + math.exp(-log_odds))

    # Exculpatory evidence can cancel suspicion but never manufacture a
    # *better-than-clean* score: "authored in Word" must leave an otherwise
    # unremarkable application exactly where an empty one sits, or the score
    # stops meaning "how much did we find against this" and starts rewarding
    # documents for the tool that produced them.
    if not any(c.log_odds_delta > 0 for c in contributions):
        posterior = 0.0

    contributions.sort(key=lambda c: -abs(c.log_odds_delta))
    return posterior, contributions


def independent_families(signals: list[Signal]) -> int:
    """How many distinct analyzer families produced positive evidence.

    This is the honest measure of corroboration: five signals from one family
    are one observation seen five ways; one signal each from five families is
    five observations.
    """
    return len({s.analyzer for s in signals
                if s.score_impact > 0 and s.severity is not Severity.INFO})
