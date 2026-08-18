"""SignalHire web MVP — the Phase-0 engine behind a recruiter-facing site.

Everything runs in-process and in-memory: uploaded files are parsed, scored as
one population batch (which is what powers the cross-applicant signals), and
the results are returned as JSON. No uploaded document is written to disk.

Subscription model: three tiers (Scout / Agency / Talent Cloud) that gate
volume and workflow only — detection quality is identical everywhere, because
selling "better detection" to richer customers would poison trust in every
label. Anonymous callers get a small demo scan.

Run:  uvicorn webapp.app:app --reload      (from pocs/signalhire)
  or: signalhire web
"""

from __future__ import annotations

import hashlib
import hmac
import json
import os
import uuid
from datetime import datetime, timezone
from pathlib import Path

import time
from collections import defaultdict, deque

from fastapi import FastAPI, File, Form, Header, Request, UploadFile
from fastapi.responses import FileResponse, JSONResponse

from signalhire import __version__ as engine_version
from signalhire.parse import SUPPORTED_SUFFIXES, parse_bytes
from signalhire.pipeline import score_documents
from signalhire.report import render_html
from signalhire.signatures import SIGNATURE_DB_VERSION

from .store import DEMO_MAX_FILES, TIERS, Store

MAX_FILE_BYTES = 15 * 1024 * 1024
MAX_BATCH_BYTES = int(os.environ.get("SIGNALHIRE_MAX_BATCH_MB", "250")) * 1024 * 1024
FLAGGED = {"mass_generated", "high_risk"}

STATIC = Path(__file__).parent / "static"

app = FastAPI(title="SignalHire", version=engine_version)


@app.middleware("http")
async def security_headers(request, call_next):
    response = await call_next(request)
    response.headers.setdefault("X-Content-Type-Options", "nosniff")
    response.headers.setdefault("X-Frame-Options", "DENY")
    response.headers.setdefault("Referrer-Policy", "no-referrer")
    return response


@app.on_event("startup")
def _init_store() -> None:
    if getattr(app.state, "store", None) is None:
        app.state.store = Store()


app.state.store = None


def _store() -> Store:
    if app.state.store is None:  # direct TestClient use before startup
        app.state.store = Store()
    return app.state.store


def _user(api_key: str | None) -> dict | None:
    return _store().by_key(api_key or "")


@app.get("/")
def index() -> FileResponse:
    return FileResponse(STATIC / "index.html")


@app.get("/api/health")
def health() -> dict:
    return {"ok": True, "engine": engine_version,
            "signature_db": SIGNATURE_DB_VERSION}


@app.get("/api/pricing")
def pricing() -> dict:
    return {"tiers": [
        {"id": tier_id, **{k: v for k, v in cfg.items()}}
        for tier_id, cfg in TIERS.items()
    ], "demo_max_files": DEMO_MAX_FILES}


# Per-IP signup throttle: free-tier accounts are the only unauthenticated
# write path, so they get a simple sliding-hour cap.
_signup_hits: dict[str, deque] = defaultdict(deque)


def _signup_limited(ip: str) -> bool:
    cap = int(os.environ.get("SIGNALHIRE_SIGNUPS_PER_HOUR", "20"))
    hits = _signup_hits[ip]
    now = time.monotonic()
    while hits and now - hits[0] > 3600:
        hits.popleft()
    if len(hits) >= cap:
        return True
    hits.append(now)
    return False


@app.post("/api/signup")
async def signup(payload: dict, request: Request) -> JSONResponse:
    if _signup_limited(request.client.host if request.client else "?"):
        return JSONResponse({"error": "too many signups from this address — "
                                      "try again later"}, status_code=429)
    try:
        user = _store().signup(payload.get("email", ""), payload.get("org", ""))
    except ValueError as exc:
        return JSONResponse({"error": str(exc)}, status_code=422)
    return JSONResponse({
        "email": user["email"], "org": user["org"], "tier": user["tier"],
        "api_key": user["api_key"],
        "entitlements": _store().entitlements(user),
    }, status_code=201)


@app.get("/api/me")
def me(x_api_key: str | None = Header(default=None)) -> JSONResponse:
    user = _user(x_api_key)
    if user is None:
        return JSONResponse({"error": "unknown API key"}, status_code=401)
    return JSONResponse({
        "email": user["email"], "org": user["org"], "tier": user["tier"],
        "entitlements": _store().entitlements(user),
    })


@app.post("/api/upgrade")
async def upgrade(payload: dict,
                  x_api_key: str | None = Header(default=None)) -> JSONResponse:
    user = _user(x_api_key)
    if user is None:
        return JSONResponse({"error": "sign up before upgrading"}, status_code=401)
    tier = payload.get("tier", "")
    if tier not in TIERS:
        return JSONResponse({"error": f"unknown tier: {tier}"}, status_code=422)

    # Real billing: configure Stripe payment links and flip the tier from the
    # webhook. Until those exist, dev mode upgrades directly and says so.
    link = os.environ.get(f"STRIPE_LINK_{tier.upper()}")
    if link:
        return JSONResponse({"checkout_url": link, "dev_mode": False})
    user = _store().set_tier(x_api_key or "", tier)
    return JSONResponse({
        "upgraded": True, "tier": tier, "dev_mode": True,
        "note": "no Stripe payment link configured — tier switched directly",
        "entitlements": _store().entitlements(user),
    })


@app.post("/api/rotate-key")
def rotate_key(x_api_key: str | None = Header(default=None)) -> JSONResponse:
    try:
        user = _store().rotate_key(x_api_key or "")
    except ValueError:
        return JSONResponse({"error": "unknown API key"}, status_code=401)
    return JSONResponse({"api_key": user["api_key"]})


@app.get("/api/team")
def team(x_api_key: str | None = Header(default=None)) -> JSONResponse:
    user = _user(x_api_key)
    if user is None:
        return JSONResponse({"error": "unknown API key"}, status_code=401)
    root = _store().root_of(user)
    return JSONResponse({
        "owner": root["email"],
        "members": _store().members(root["id"]),
        "entitlements": _store().entitlements(user),
    })


@app.post("/api/team/invite")
async def team_invite(payload: dict,
                      x_api_key: str | None = Header(default=None)) -> JSONResponse:
    user = _user(x_api_key)
    if user is None:
        return JSONResponse({"error": "unknown API key"}, status_code=401)
    try:
        member = _store().invite(x_api_key or "", payload.get("email", ""))
    except ValueError as exc:
        return JSONResponse({"error": str(exc)}, status_code=422)
    # The owner hands the key to the teammate; the MVP sends no email.
    return JSONResponse({"email": member["email"],
                         "api_key": member["api_key"]}, status_code=201)


@app.get("/api/history")
def history(x_api_key: str | None = Header(default=None)) -> JSONResponse:
    user = _user(x_api_key)
    if user is None:
        return JSONResponse({"error": "unknown API key"}, status_code=401)
    return JSONResponse({"scans": _store().history(user["id"])})


@app.get("/api/requisitions")
def requisitions(x_api_key: str | None = Header(default=None)) -> JSONResponse:
    user = _user(x_api_key)
    if user is None:
        return JSONResponse({"error": "unknown API key"}, status_code=401)
    root = _store().root_of(user)
    return JSONResponse({"requisitions": _store().requisitions(root["id"])})


@app.post("/api/billing/webhook")
async def billing_webhook(
    payload: dict,
    x_webhook_secret: str | None = Header(default=None),
) -> JSONResponse:
    """Payment-provider callback (Stripe-shaped). Flips the payer's tier.

    Enabled only when SIGNALHIRE_WEBHOOK_SECRET is set; the provider is
    configured to send that value in X-Webhook-Secret. The API key travels as
    the checkout session's client_reference_id and the tier in its metadata.
    """
    secret = os.environ.get("SIGNALHIRE_WEBHOOK_SECRET")
    if not secret:
        return JSONResponse({"error": "billing webhook not configured"},
                            status_code=503)
    if not hmac.compare_digest(x_webhook_secret or "", secret):
        return JSONResponse({"error": "bad webhook secret"}, status_code=401)
    if payload.get("type") != "checkout.session.completed":
        return JSONResponse({"ignored": payload.get("type", "unknown")})
    session = payload.get("data", {}).get("object", {})
    api_key = session.get("client_reference_id", "")
    tier = session.get("metadata", {}).get("tier", "")
    try:
        user = _store().set_tier(api_key, tier)
    except ValueError as exc:
        return JSONResponse({"error": str(exc)}, status_code=422)
    return JSONResponse({"ok": True, "email": user["email"], "tier": tier})


@app.post("/api/scan")
async def scan_batch(
    files: list[UploadFile] = File(...),
    jd: str = Form(""),
    sensitivity: str = Form("balanced"),
    req: str = Form(""),
    x_api_key: str | None = Header(default=None),
) -> JSONResponse:
    if sensitivity not in ("conservative", "balanced", "aggressive"):
        return JSONResponse({"error": "unknown sensitivity"}, status_code=422)

    store = _store()
    user = _user(x_api_key)
    if x_api_key and user is None:
        return JSONResponse({"error": "unknown API key"}, status_code=401)

    refusal = store.check_scan_allowed(user, len(files))
    if refusal:
        return JSONResponse({"error": refusal, "upgrade_required": True},
                            status_code=402)

    docs = []
    skipped: list[dict] = []
    submitted_at = datetime.now(timezone.utc)  # upload time IS submission time
    batch_bytes = 0

    for f in files:
        name = Path(f.filename or "upload").name
        data = await f.read()
        batch_bytes += len(data)
        if batch_bytes > MAX_BATCH_BYTES:
            return JSONResponse(
                {"error": f"batch exceeds "
                          f"{MAX_BATCH_BYTES // (1024 * 1024)} MB total"},
                status_code=413)
        if len(data) > MAX_FILE_BYTES:
            skipped.append({"file": name, "reason": "over 15 MB"})
            continue
        suffix = Path(name).suffix.lower()
        if suffix not in SUPPORTED_SUFFIXES:
            skipped.append({"file": name,
                            "reason": f"unsupported type ({suffix or 'no extension'})"})
            continue
        doc_id = hashlib.sha256(data + name.encode()).hexdigest()[:16]
        application_id = str(uuid.uuid4())
        doc = parse_bytes(doc_id, application_id, data, source_path=name)
        doc.submitted_at = submitted_at
        docs.append(doc)

    if not docs:
        return JSONResponse(
            {"error": "no supported documents in the upload "
                      "(PDF, DOCX, DOC, ODT, RTF, HTML, TXT and MD are accepted)",
             "skipped": skipped},
            status_code=422)

    result = score_documents(docs, jd_text=jd, sensitivity=sensitivity)
    flagged = sum(1 for a in result.applications if a.label in FLAGGED)
    if user is not None:
        store.record_scan(user["id"], files=len(docs), flagged=flagged,
                          req=req, labels=result.stats["labels"])
        store.upsert_requisition(store.root_of(user)["id"], req=req, jd=jd,
                                 files=len(docs), labels=result.stats["labels"])

    ent = store.entitlements(user)
    result.stats["source"] = "upload"
    result.stats["skipped"] = skipped
    result.stats["plan"] = {"tier": ent["tier"], "label": ent["label"],
                            "scans_left": ent["scans_left"],
                            "json_export": ent["json_export"],
                            "demo": user is None}
    payload = result.as_dict()
    if ent["json_export"]:
        # The self-contained HTML triage report — the artifact an agency
        # forwards to a client. Same export right as the JSON.
        payload["report_html"] = render_html(
            result, title=req or "Application triage report")
    # Round-trip through the engine's JSON encoder so datetimes serialize the
    # same way they do in the CLI report.
    return JSONResponse(json.loads(json.dumps(payload, default=str)))


def main() -> None:  # pragma: no cover - thin uvicorn launcher
    import uvicorn
    uvicorn.run("webapp.app:app", host="127.0.0.1", port=8710, reload=False)


if __name__ == "__main__":  # pragma: no cover
    main()
