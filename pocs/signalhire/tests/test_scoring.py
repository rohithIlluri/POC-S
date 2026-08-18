from __future__ import annotations

from signalhire.scoring import Thresholds, score
from signalhire.types import Severity, Signal


def sig(code: str, severity: Severity, impact: float) -> Signal:
    return Signal(code=code, severity=severity, score_impact=impact,
                  evidence={}, analyzer="test")


def test_no_signals_is_genuine():
    assert score([])["label"] == "genuine"


def test_human_tool_signal_keeps_effort_at_ceiling():
    result = score([sig("HUMAN_TOOL_MATCH", Severity.INFO, -0.4)])
    assert result["effort_score"] == 100
    assert result["label"] == "genuine"


def test_weak_signals_alone_can_never_reach_mass_generated():
    weak = [sig(f"WEAK_{i}", Severity.WEAK, 0.3) for i in range(6)]
    result = score(weak)
    assert result["effort_score"] <= 35
    assert result["label"] == "needs_review"


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
    signals = [sig("GEN_TOOL_MATCH", Severity.STRONG, 0.7),
               sig("FRESH_GENERATION", Severity.WEAK, 0.3),
               sig("DEFAULT_TITLE", Severity.WEAK, 0.1)]
    # These weights put the raw score exactly on .5, where binary summation
    # differs between interpreters (39.5 vs 39.50000000000001). _clamp rounds
    # half up through Decimal so the answer is 40 on every Python version.
    assert score(signals)["effort_score"] == 40

    assert score(signals, Thresholds.for_sensitivity("conservative"))["label"] \
        == "needs_review"
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


def test_conservative_sensitivity_clears_a_borderline_application():
    borderline = [sig("GEN_TOOL_MATCH", Severity.STRONG, 0.6),
                  sig("FRESH_GENERATION", Severity.WEAK, 0.3)]
    assert score(borderline)["label"] == "needs_review"
    assert score(borderline, Thresholds.for_sensitivity("conservative"))["label"] \
        == "genuine"
