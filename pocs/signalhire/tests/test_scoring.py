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
    assert score(signals)["effort_score"] == 39

    assert score(signals, Thresholds.for_sensitivity("conservative"))["label"] \
        == "needs_review"
    assert score(signals, Thresholds.for_sensitivity("aggressive"))["label"] \
        == "mass_generated"


def test_conservative_sensitivity_clears_a_borderline_application():
    borderline = [sig("GEN_TOOL_MATCH", Severity.STRONG, 0.6),
                  sig("FRESH_GENERATION", Severity.WEAK, 0.3)]
    assert score(borderline)["label"] == "needs_review"
    assert score(borderline, Thresholds.for_sensitivity("conservative"))["label"] \
        == "genuine"
