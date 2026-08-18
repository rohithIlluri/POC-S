"""Tiers gate volume and workflow, never detection: signup, quotas, demo mode
and the dev-mode upgrade path."""

from __future__ import annotations

import pytest

pytest.importorskip("fastapi")
from fastapi.testclient import TestClient  # noqa: E402

from webapp.app import app  # noqa: E402
from webapp.store import Store  # noqa: E402


@pytest.fixture()
def client():
    app.state.store = Store(":memory:")
    with TestClient(app) as c:
        yield c
    app.state.store = None


def _resume(i: int = 0) -> tuple:
    body = (f"Person {i}\nperson{i}@example.com\n"
            f"Engineer number {i} with unique experience in domain {i}.")
    return ("files", (f"resume_{i}.txt", body.encode(), "text/plain"))


def _signup(client, email="ana@example.com") -> str:
    r = client.post("/api/signup", json={"email": email, "org": "Acme Search"})
    assert r.status_code == 201
    return r.json()["api_key"]


def test_signup_and_me(client):
    key = _signup(client)
    me = client.get("/api/me", headers={"X-API-Key": key}).json()
    assert me["tier"] == "scout"
    assert me["entitlements"]["scans_per_month"] == 5
    assert me["entitlements"]["max_files"] == 25


def test_signup_rejects_bad_email_and_duplicates(client):
    assert client.post("/api/signup", json={"email": "nope"}).status_code == 422
    _signup(client)
    r = client.post("/api/signup", json={"email": "ana@example.com"})
    assert r.status_code == 422


def test_demo_scan_is_small_and_watermarked(client):
    r = client.post("/api/scan", files=[_resume(i) for i in range(2)])
    assert r.status_code == 200
    assert r.json()["stats"]["plan"]["demo"] is True

    r = client.post("/api/scan", files=[_resume(i) for i in range(6)])
    assert r.status_code == 402
    assert "Demo" in r.json()["error"]


def test_unknown_key_is_rejected_not_demoted(client):
    r = client.post("/api/scan", files=[_resume()],
                    headers={"X-API-Key": "sh_bogus"})
    assert r.status_code == 401


def test_scout_batch_cap_and_monthly_quota(client):
    key = _signup(client)
    h = {"X-API-Key": key}

    r = client.post("/api/scan", files=[_resume(i) for i in range(26)], headers=h)
    assert r.status_code == 402
    assert "25" in r.json()["error"]

    for _ in range(5):
        assert client.post("/api/scan", files=[_resume()], headers=h).status_code == 200

    r = client.post("/api/scan", files=[_resume()], headers=h)
    assert r.status_code == 402
    assert r.json()["upgrade_required"] is True

    me = client.get("/api/me", headers=h).json()
    assert me["entitlements"]["scans_used"] == 5
    assert me["entitlements"]["scans_left"] == 0


def test_dev_mode_upgrade_lifts_the_caps(client):
    key = _signup(client)
    h = {"X-API-Key": key}
    r = client.post("/api/upgrade", json={"tier": "agency"}, headers=h)
    assert r.json()["dev_mode"] is True

    me = client.get("/api/me", headers=h).json()
    assert me["tier"] == "agency"
    assert me["entitlements"]["max_files"] == 200

    r = client.post("/api/upgrade", json={"tier": "talent_cloud"}, headers=h)
    assert r.json()["upgraded"] is True
    me = client.get("/api/me", headers=h).json()
    assert me["entitlements"]["scans_per_month"] is None
    assert me["entitlements"]["scans_left"] is None


def test_pricing_lists_three_tiers_with_identical_detection(client):
    tiers = client.get("/api/pricing").json()["tiers"]
    assert [t["id"] for t in tiers] == ["scout", "agency", "talent_cloud"]
    # No tier config may ever mention detection quality — volume/workflow only.
    for t in tiers:
        assert set(t) <= {"id", "label", "price_usd", "scans_per_month",
                          "max_files", "seats", "json_export", "api_access",
                          "custom_signatures"}


def test_history_records_requisition_and_label_counts(client):
    key = _signup(client)
    h = {"X-API-Key": key}
    client.post("/api/scan", files=[_resume(i) for i in range(3)],
                data={"req": "PLT-4471 Senior Platform Engineer"}, headers=h)
    scans = client.get("/api/history", headers=h).json()["scans"]
    assert len(scans) == 1
    assert scans[0]["req"] == "PLT-4471 Senior Platform Engineer"
    assert scans[0]["files"] == 3
    assert sum(scans[0]["labels"].values()) == 3

    assert client.get("/api/history").status_code == 401


def test_billing_webhook_flips_tier_only_with_the_secret(client, monkeypatch):
    key = _signup(client)
    event = {"type": "checkout.session.completed",
             "data": {"object": {"client_reference_id": key,
                                 "metadata": {"tier": "talent_cloud"}}}}

    # Unconfigured → disabled outright.
    monkeypatch.delenv("SIGNALHIRE_WEBHOOK_SECRET", raising=False)
    assert client.post("/api/billing/webhook", json=event).status_code == 503

    monkeypatch.setenv("SIGNALHIRE_WEBHOOK_SECRET", "whsec_test")
    r = client.post("/api/billing/webhook", json=event,
                    headers={"X-Webhook-Secret": "wrong"})
    assert r.status_code == 401

    r = client.post("/api/billing/webhook", json=event,
                    headers={"X-Webhook-Secret": "whsec_test"})
    assert r.json()["ok"] is True
    me = client.get("/api/me", headers={"X-API-Key": key}).json()
    assert me["tier"] == "talent_cloud"

    # Non-checkout events are acknowledged and ignored.
    r = client.post("/api/billing/webhook",
                    json={"type": "invoice.paid"},
                    headers={"X-Webhook-Secret": "whsec_test"})
    assert r.json() == {"ignored": "invoice.paid"}


def test_seats_are_gated_by_tier(client):
    key = _signup(client)
    h = {"X-API-Key": key}

    # Scout is a single seat.
    r = client.post("/api/team/invite", json={"email": "b@example.com"}, headers=h)
    assert r.status_code == 422 and "1 seat" in r.json()["error"]

    client.post("/api/upgrade", json={"tier": "agency"}, headers=h)
    member_keys = []
    for i in range(4):
        r = client.post("/api/team/invite",
                        json={"email": f"m{i}@example.com"}, headers=h)
        assert r.status_code == 201
        member_keys.append(r.json()["api_key"])

    # Five seats total on Agency: owner + 4 members, the next one refuses.
    r = client.post("/api/team/invite", json={"email": "m5@example.com"}, headers=h)
    assert r.status_code == 422 and "Upgrade" in r.json()["error"]

    team = client.get("/api/team", headers=h).json()
    assert team["entitlements"]["seats_used"] == 5
    assert len(team["members"]) == 4

    # Members inherit the org tier and cannot invite.
    mh = {"X-API-Key": member_keys[0]}
    me = client.get("/api/me", headers=mh).json()
    assert me["entitlements"]["tier"] == "agency"
    assert me["entitlements"]["role"] == "member"
    r = client.post("/api/team/invite", json={"email": "x@example.com"}, headers=mh)
    assert r.status_code == 422


def test_member_scans_draw_from_the_org_quota(client):
    key = _signup(client)
    h = {"X-API-Key": key}
    client.post("/api/upgrade", json={"tier": "agency"}, headers=h)
    mk = client.post("/api/team/invite", json={"email": "m@example.com"},
                     headers=h).json()["api_key"]

    client.post("/api/scan", files=[_resume()], headers={"X-API-Key": mk})
    client.post("/api/scan", files=[_resume()], headers=h)

    for who in (h, {"X-API-Key": mk}):
        e = client.get("/api/me", headers=who).json()["entitlements"]
        assert e["scans_used"] == 2
        assert e["scans_left"] == 198


def test_scan_response_carries_plan_state(client):
    key = _signup(client)
    r = client.post("/api/scan", files=[_resume()], headers={"X-API-Key": key})
    plan = r.json()["stats"]["plan"]
    assert plan == {"tier": "scout", "label": "Scout", "scans_left": 4,
                    "json_export": False, "demo": False}
