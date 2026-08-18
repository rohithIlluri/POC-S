from __future__ import annotations

from signalhire.scoring import Thresholds, score
from signalhire.types import Severity, Signal


def sig(code: str, severity: Severity, impact: float,
        analyzer: str = "test") -> Signal:
    return Signal(code=code, severity=severity, score_impact=impact,
                  evidence={}, analyzer=analyzer)


def test_no_signals_is_genuine():
    assert score([])["label"] == "genuine"


def test_human_tool_signal_keeps_effort_at_ceiling():
    result = score([sig("HUMAN_TOOL_MATCH", Severity.INFO, -0.4)])
    assert result["effort_score"] == 100
    assert result["label"] == "genuine"


def test_weak_signals_alone_can_never_reach_mass_generated():
    """Six weak signals from one analyzer are one observation seen six times,
    not six independent facts: the correlation discount keeps them out of
    mass_generated no matter how many fire."""
    weak = [sig(f"WEAK_{i}", Severity.WEAK, 0.3) for i in range(6)]
    result = score(weak)
    assert result["label"] != "mass_generated"
    assert result["evidence"]["independent_families"] == 1


def test_strong_plus_weak_reaches_mass_generated():
    result = score([
        sig("GEN_TOOL_MATCH", Severity.STRONG, 0.7),
        sig("JD_MIRROR_EXTREME", Severity.STRONG, 0.5),
        sig("TEMPLATE_SWARM", Severity.WEAK, 0.2),
    ])
    assert result["label"] == "mass_generated"


def test_deterministic_signal_forces_high_risk():
    result = score([sig("HIDDEN_TEXT", Severity.DETERMINISTIC, 0.9)])
    assert result["label"] == "high_risk"
    assert result["risk_score"] >= 60


def test_recycled_identity_alone_is_high_risk():
    """A strong fraud signal must not be diluted by a clean effort score:
    RECYCLED_IDENTITY scores 56 on the risk scale, under the 60 threshold."""
    result = score([sig("RECYCLED_IDENTITY", Severity.STRONG, 0.8),
                    sig("HUMAN_TOOL_MATCH", Severity.INFO, -0.4)])
    assert result["effort_score"] == 100
    assert result["label"] == "high_risk"


def test_risk_codes_do_not_move_the_effort_score():
    with_risk = score([sig("HIDDEN_TEXT", Severity.DETERMINISTIC, 0.9)])
    assert with_risk["effort_score"] == 100


def test_reason_codes_are_ordered_by_impact():
    result = score([sig("SMALL", Severity.WEAK, 0.1),
                    sig("BIG", Severity.STRONG, 0.7)])
    assert [r["code"] for r in result["reason_codes"]] == ["BIG", "SMALL"]


def test_sensitivity_slider_moves_the_boundary():
    """The slider must move work in and out of the queue: the same evidence
    is reviewed under balanced and flagged under aggressive."""
    signals = [sig("GEN_TOOL_MATCH", Severity.STRONG, 0.7, "forensics"),
               sig("FRESH_GENERATION", Severity.WEAK, 0.3, "forensics"),
               sig("DEFAULT_TITLE", Severity.WEAK, 0.1, "forensics")]
    balanced = score(signals)
    assert balanced["label"] == "needs_review"

    assert score(signals, Thresholds.for_sensitivity("conservative"))["label"] \
        in ("genuine", "needs_review")
    assert score(signals, Thresholds.for_sensitivity("aggressive"))["label"] \
        == "mass_generated"


def test_scores_are_not_interpreter_dependent():
    """The same resume must score the same on every deployment, or a reason
    code is not reproducible for an audit.

    These weights sum to exactly 1.1, putting the raw effort score on 39.5 —
    a boundary. In binary floating point that expression evaluates to
    39.49999999999999 or 39.50000000000001 depending on accumulation order,
    which is why the same input scored 39 on Python 3.11 and 40 on 3.12.
    """
    from decimal import Decimal

    from signalhire.scoring import _clamp, _exact

    # The arithmetic is exact: no drift at the 15th decimal place.
    weights = [_exact(0.7), _exact(0.3), _exact(0.1)]
    assert sum(weights, Decimal(0)) == Decimal("1.1")
    assert Decimal(100) - Decimal("1.1") * _exact(55.0) == Decimal("39.5")

    # And the boundary resolves half-up, not banker's rounding.
    assert _clamp(Decimal("39.5")) == 40
    assert _clamp(Decimal("40.5")) == 41       # plain round() gives 40 here
    assert _clamp(39.5) == 40                  # float callers too
    assert _clamp(Decimal("-3.2")) == 0
    assert _clamp(Decimal("140")) == 100

def _weak(code: str, analyzer: str, impact: float = 0.2) -> Signal:
    return Signal(code=code, severity=Severity.WEAK, score_impact=impact,
                  evidence={}, analyzer=analyzer)


def test_weak_convergence_across_four_families_escalates():
    """Track-covering trades one strong signal for weak ones everywhere:
    weak evidence from four independent analyzers below the review line is
    mass_generated even with no STRONG signal."""
    signals = [_weak("SHARED_BOILERPLATE", "boilerplate", 0.3),
               _weak("TEMPLATE_SWARM", "layout", 0.2),
               _weak("JD_MIRROR_HIGH", "jd_mirror", 0.25),
               _weak("NO_PRODUCER", "forensics", 0.15)]
    result = score(signals)
    assert result["effort_score"] <= 55
    assert result["label"] == "mass_generated"


def test_weak_pile_from_few_families_stays_needs_review():
    signals = [_weak("SHARED_BOILERPLATE", "boilerplate", 0.3),
               _weak("JD_MIRROR_HIGH", "jd_mirror", 0.25),
               _weak("JD_PHRASE_LIFT", "jd_mirror", 0.2),
               _weak("DUP_CLUSTER", "dedupe", 0.25)]
    assert score(signals)["label"] == "needs_review"


def test_conservative_sensitivity_moves_work_out_of_the_queue():
    """Conservative must never pull *more* in than balanced does."""
    borderline = [sig("GEN_TOOL_MATCH", Severity.STRONG, 0.6, "forensics"),
                  sig("FRESH_GENERATION", Severity.WEAK, 0.3, "forensics")]
    order = {"genuine": 0, "needs_review": 1, "mass_generated": 2,
             "high_risk": 3}
    balanced = score(borderline)["label"]
    conservative = score(borderline,
                         Thresholds.for_sensitivity("conservative"))["label"]
    aggressive = score(borderline,
                       Thresholds.for_sensitivity("aggressive"))["label"]
    assert order[conservative] <= order[balanced] <= order[aggressive]


# --- evidence combination --------------------------------------------------

def test_correlated_signals_are_not_counted_twice():
    """JD_MIRROR_HIGH and JD_PHRASE_LIFT co-occur at Jaccard 1.00 on the
    corpus — two measurements of one fact. Adding the second must move the
    score far less than the first did, or one fact is counted twice."""
    first = [sig("JD_MIRROR_HIGH", Severity.WEAK, 0.25, "jd_mirror")]
    both = first + [sig("JD_PHRASE_LIFT", Severity.WEAK, 0.20, "jd_mirror")]

    drop_one = 100 - score(first)["effort_score"]
    drop_two = 100 - score(both)["effort_score"]
    assert drop_two > drop_one                      # it still adds something
    assert drop_two < drop_one * 1.6                # but heavily discounted


def test_independent_families_outweigh_one_noisy_family():
    """Three families agreeing is stronger evidence than one family firing
    three times, even at identical weights."""
    one_family = [sig(f"A{i}", Severity.WEAK, 0.25, "forensics")
                  for i in range(3)]
    three_families = [sig("A", Severity.WEAK, 0.25, "forensics"),
                      sig("B", Severity.WEAK, 0.25, "layout"),
                      sig("C", Severity.WEAK, 0.25, "jd_mirror")]
    assert score(three_families)["effort_score"] < score(one_family)["effort_score"]
    assert score(three_families)["evidence"]["independent_families"] == 3


def test_every_signal_keeps_an_audited_contribution():
    """Explainability is the product: each signal must carry what it was
    worth and what discount it took."""
    signals = [sig("GEN_TOOL_MATCH", Severity.STRONG, 0.7, "forensics"),
               sig("FRESH_GENERATION", Severity.WEAK, 0.3, "forensics"),
               sig("TEMPLATE_SWARM", Severity.WEAK, 0.2, "layout")]
    contributions = score(signals)["evidence"]["contributions"]
    assert {c["code"] for c in contributions} == {s.code for s in signals}
    for c in contributions:
        assert c["likelihood_ratio"] > 0
        assert 0.0 <= c["correlation_discount"] <= 1.0
    # The second signal in a family is discounted; the first is not.
    forensics = sorted([c for c in contributions if c["analyzer"] == "forensics"],
                       key=lambda c: c["family_rank"])
    assert forensics[0]["correlation_discount"] == 1.0
    assert forensics[1]["correlation_discount"] < 1.0


def test_a_clean_document_scores_a_perfect_hundred():
    """No evidence means no suspicion — the prior must never act as a penalty
    charged to every applicant for existing."""
    assert score([])["effort_score"] == 100
    only_human = [sig("HUMAN_TOOL_MATCH", Severity.INFO, -0.4, "forensics")]
    assert score(only_human)["effort_score"] == 100


def test_scores_are_probabilities():
    """The score is a calibrated posterior, so it must stay in range and move
    monotonically with the strength of the evidence."""
    weak = score([sig("W", Severity.WEAK, 0.3, "forensics")])
    strong = score([sig("S", Severity.STRONG, 0.7, "forensics")])
    hard = score([sig("H", Severity.DETERMINISTIC, 0.9, "forensics")])
    assert 0 <= hard["effort_score"] <= strong["effort_score"] \
        <= weak["effort_score"] <= 100
    assert 0.0 <= weak["evidence"]["p_mass_generated"] <= 1.0
