"""Subscription store — accounts, API keys, tiers and usage metering.

SQLite via the stdlib so the MVP deploys anywhere Python runs. The engine
itself (signalhire/) knows nothing about any of this: tiers gate volume and
workflow, never detection quality — every tier runs the identical engine.
"""

from __future__ import annotations

import os
import re
import secrets
import sqlite3
import threading
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

_EMAIL_RE = re.compile(r"^[\w.+-]+@[\w-]+\.[\w.-]+$")

# Volume and workflow only. `scans_per_month: None` means unlimited.
TIERS: dict[str, dict[str, Any]] = {
    "scout": {
        "label": "Scout", "price_usd": 0,
        "scans_per_month": 5, "max_files": 25,
        "json_export": False, "api_access": False, "custom_signatures": False,
    },
    "agency": {
        "label": "Agency", "price_usd": 149,
        "scans_per_month": 200, "max_files": 200,
        "json_export": True, "api_access": True, "custom_signatures": False,
    },
    "talent_cloud": {
        "label": "Talent Cloud", "price_usd": 499,
        "scans_per_month": None, "max_files": 500,
        "json_export": True, "api_access": True, "custom_signatures": True,
    },
}

DEMO_MAX_FILES = 5

_SCHEMA = """
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    org TEXT NOT NULL DEFAULT '',
    api_key TEXT NOT NULL UNIQUE,
    tier TEXT NOT NULL DEFAULT 'scout',
    created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS scans (
    id INTEGER PRIMARY KEY,
    user_id INTEGER REFERENCES users(id),
    files INTEGER NOT NULL,
    flagged INTEGER NOT NULL DEFAULT 0,
    req TEXT NOT NULL DEFAULT '',
    labels TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS scans_user_month ON scans(user_id, created_at);
"""

# Columns added after the first release; applied to pre-existing dev DBs.
_MIGRATIONS = (
    "ALTER TABLE scans ADD COLUMN req TEXT NOT NULL DEFAULT ''",
    "ALTER TABLE scans ADD COLUMN labels TEXT NOT NULL DEFAULT '{}'",
)


def _now() -> str:
    return datetime.now(timezone.utc).isoformat()


def _month_prefix() -> str:
    return datetime.now(timezone.utc).strftime("%Y-%m")


class Store:
    def __init__(self, path: str | Path | None = None) -> None:
        raw = path or os.environ.get("SIGNALHIRE_DB", "data/signalhire.db")
        if raw != ":memory:":
            Path(raw).parent.mkdir(parents=True, exist_ok=True)
        self._conn = sqlite3.connect(str(raw), check_same_thread=False)
        self._conn.row_factory = sqlite3.Row
        self._lock = threading.Lock()
        with self._lock:
            self._conn.executescript(_SCHEMA)
            for migration in _MIGRATIONS:
                try:
                    self._conn.execute(migration)
                except sqlite3.OperationalError:
                    pass  # column already exists
            self._conn.commit()

    # -- accounts ----------------------------------------------------------

    def signup(self, email: str, org: str = "") -> dict:
        email = (email or "").strip().lower()
        if not _EMAIL_RE.match(email):
            raise ValueError("a valid email address is required")
        api_key = "sh_" + secrets.token_urlsafe(24)
        with self._lock:
            try:
                self._conn.execute(
                    "INSERT INTO users (email, org, api_key, created_at) "
                    "VALUES (?, ?, ?, ?)",
                    (email, (org or "").strip()[:120], api_key, _now()))
                self._conn.commit()
            except sqlite3.IntegrityError:
                raise ValueError("that email already has an account") from None
        return self.by_key(api_key)  # type: ignore[return-value]

    def by_key(self, api_key: str) -> dict | None:
        if not api_key:
            return None
        with self._lock:
            row = self._conn.execute(
                "SELECT * FROM users WHERE api_key = ?", (api_key,)).fetchone()
        return dict(row) if row else None

    def set_tier(self, api_key: str, tier: str) -> dict:
        if tier not in TIERS:
            raise ValueError(f"unknown tier: {tier}")
        user = self.by_key(api_key)
        if user is None:
            raise ValueError("unknown API key")
        with self._lock:
            self._conn.execute("UPDATE users SET tier = ? WHERE id = ?",
                               (tier, user["id"]))
            self._conn.commit()
        user["tier"] = tier
        return user

    # -- metering ----------------------------------------------------------

    def scans_this_month(self, user_id: int) -> int:
        with self._lock:
            row = self._conn.execute(
                "SELECT COUNT(*) AS n FROM scans "
                "WHERE user_id = ? AND created_at LIKE ?",
                (user_id, f"{_month_prefix()}%")).fetchone()
        return int(row["n"])

    def record_scan(self, user_id: int | None, files: int, flagged: int,
                    req: str = "", labels: dict | None = None) -> None:
        import json
        with self._lock:
            self._conn.execute(
                "INSERT INTO scans (user_id, files, flagged, req, labels, "
                "created_at) VALUES (?, ?, ?, ?, ?, ?)",
                (user_id, files, flagged, (req or "").strip()[:120],
                 json.dumps(labels or {}), _now()))
            self._conn.commit()

    def history(self, user_id: int, limit: int = 20) -> list[dict]:
        import json
        with self._lock:
            rows = self._conn.execute(
                "SELECT files, flagged, req, labels, created_at FROM scans "
                "WHERE user_id = ? ORDER BY id DESC LIMIT ?",
                (user_id, limit)).fetchall()
        return [{**dict(r), "labels": json.loads(r["labels"])} for r in rows]

    # -- entitlements ------------------------------------------------------

    def entitlements(self, user: dict | None) -> dict:
        """What this caller may do right now. `user=None` is demo mode."""
        if user is None:
            return {"tier": "demo", "label": "Demo",
                    "max_files": DEMO_MAX_FILES,
                    "scans_per_month": 1, "scans_used": 0, "scans_left": 1,
                    "json_export": False, "api_access": False,
                    "custom_signatures": False}
        tier = TIERS[user["tier"]]
        used = self.scans_this_month(user["id"])
        cap = tier["scans_per_month"]
        return {
            "tier": user["tier"], "label": tier["label"],
            "max_files": tier["max_files"],
            "scans_per_month": cap, "scans_used": used,
            "scans_left": None if cap is None else max(0, cap - used),
            "json_export": tier["json_export"],
            "api_access": tier["api_access"],
            "custom_signatures": tier["custom_signatures"],
        }

    def check_scan_allowed(self, user: dict | None, n_files: int) -> str | None:
        """None when allowed, otherwise a human-readable refusal."""
        ent = self.entitlements(user)
        if n_files > ent["max_files"]:
            return (f"{ent['label']} scans are capped at {ent['max_files']} "
                    f"files per batch (you sent {n_files}). Upgrade for "
                    "larger requisitions.")
        if user is not None and ent["scans_left"] == 0:
            return (f"{ent['label']} includes {ent['scans_per_month']} scans "
                    "per month and this month's are used. Upgrade to keep "
                    "scanning.")
        return None
