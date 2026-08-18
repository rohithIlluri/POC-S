"""Subscription store — accounts, API keys, tiers and usage metering.

SQLite via the stdlib so the MVP deploys anywhere Python runs. The engine
itself (signalhire/) knows nothing about any of this: tiers gate volume and
workflow, never detection quality — every tier runs the identical engine.
"""

from __future__ import annotations

import hashlib
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
        "scans_per_month": 5, "max_files": 25, "seats": 1,
        "json_export": False, "api_access": False, "custom_signatures": False,
    },
    "agency": {
        "label": "Agency", "price_usd": 149,
        "scans_per_month": 200, "max_files": 200, "seats": 5,
        "json_export": True, "api_access": True, "custom_signatures": False,
    },
    "talent_cloud": {
        "label": "Talent Cloud", "price_usd": 499,
        "scans_per_month": None, "max_files": 500, "seats": None,
        "json_export": True, "api_access": True, "custom_signatures": True,
    },
}

DEMO_MAX_FILES = 5

_SCHEMA = """
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    org TEXT NOT NULL DEFAULT '',
    key_hash TEXT UNIQUE,
    tier TEXT NOT NULL DEFAULT 'scout',
    owner_id INTEGER,
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
CREATE TABLE IF NOT EXISTS requisitions (
    root_id INTEGER NOT NULL,
    req TEXT NOT NULL,
    jd TEXT NOT NULL DEFAULT '',
    scans INTEGER NOT NULL DEFAULT 0,
    files INTEGER NOT NULL DEFAULT 0,
    labels TEXT NOT NULL DEFAULT '{}',
    updated_at TEXT NOT NULL,
    PRIMARY KEY (root_id, req)
);
"""

# Columns added after the first release; applied to pre-existing dev DBs.
_MIGRATIONS = (
    "ALTER TABLE scans ADD COLUMN req TEXT NOT NULL DEFAULT ''",
    "ALTER TABLE scans ADD COLUMN labels TEXT NOT NULL DEFAULT '{}'",
    "ALTER TABLE users ADD COLUMN owner_id INTEGER",
    "ALTER TABLE users ADD COLUMN key_hash TEXT",
)


def _key_hash(api_key: str) -> str:
    """Keys are 24 random url-safe bytes, so an unsalted digest is fine: at
    rest the DB holds only hashes, and a leaked dump cannot be replayed."""
    return hashlib.sha256(api_key.encode()).hexdigest()


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
            # Pre-hashing installs stored the clear API key; hash it in place
            # and overwrite the old column. That column is both NOT NULL and
            # UNIQUE in the legacy schema, so the overwrite value can be
            # neither NULL nor a shared '' — it has to stay distinct per row.
            # Fresh databases have no api_key column at all.
            try:
                rows = self._conn.execute(
                    "SELECT id, api_key FROM users WHERE key_hash IS NULL"
                ).fetchall()
                for row in rows:
                    if not row["api_key"]:
                        continue
                    self._conn.execute(
                        "UPDATE users SET key_hash = ?, api_key = ? "
                        "WHERE id = ?",
                        (_key_hash(row["api_key"]),
                         f"migrated:{row['id']}", row["id"]))
            except sqlite3.OperationalError:
                pass  # no legacy api_key column
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
                    "INSERT INTO users (email, org, key_hash, created_at) "
                    "VALUES (?, ?, ?, ?)",
                    (email, (org or "").strip()[:120], _key_hash(api_key),
                     _now()))
                self._conn.commit()
            except sqlite3.IntegrityError:
                raise ValueError("that email already has an account") from None
        user = self.by_key(api_key)
        assert user is not None
        # The clear key exists only in this return value — show-once.
        user["api_key"] = api_key
        return user

    def by_key(self, api_key: str) -> dict | None:
        if not api_key:
            return None
        with self._lock:
            row = self._conn.execute(
                "SELECT * FROM users WHERE key_hash = ?",
                (_key_hash(api_key),)).fetchone()
        return dict(row) if row else None

    def rotate_key(self, api_key: str) -> dict:
        """Issue a fresh key and invalidate the old one immediately."""
        user = self.by_key(api_key)
        if user is None:
            raise ValueError("unknown API key")
        new_key = "sh_" + secrets.token_urlsafe(24)
        with self._lock:
            self._conn.execute("UPDATE users SET key_hash = ? WHERE id = ?",
                               (_key_hash(new_key), user["id"]))
            self._conn.commit()
        user["api_key"] = new_key
        return user

    def root_of(self, user: dict) -> dict:
        """The billing account: the org owner for a seat, the user themself
        otherwise. Seats are one level deep by construction."""
        if not user.get("owner_id"):
            return user
        with self._lock:
            row = self._conn.execute(
                "SELECT * FROM users WHERE id = ?", (user["owner_id"],)).fetchone()
        return dict(row) if row else user

    def _org_ids(self, root_id: int) -> list[int]:
        with self._lock:
            rows = self._conn.execute(
                "SELECT id FROM users WHERE id = ? OR owner_id = ?",
                (root_id, root_id)).fetchall()
        return [r["id"] for r in rows]

    def members(self, root_id: int) -> list[dict]:
        with self._lock:
            rows = self._conn.execute(
                "SELECT email, created_at FROM users WHERE owner_id = ? "
                "ORDER BY id", (root_id,)).fetchall()
        return [dict(r) for r in rows]

    def invite(self, api_key: str, email: str) -> dict:
        """Add a seat to the caller's org. Owner-only; capped by tier."""
        user = self.by_key(api_key)
        if user is None:
            raise ValueError("unknown API key")
        if user.get("owner_id"):
            raise ValueError("only the account owner can invite teammates")
        cap = TIERS[user["tier"]]["seats"]
        seats_used = 1 + len(self.members(user["id"]))
        if cap is not None and seats_used >= cap:
            raise ValueError(
                f"{TIERS[user['tier']]['label']} includes {cap} "
                f"seat{'s' if cap != 1 else ''} and all are in use. "
                "Upgrade to add teammates.")
        member = self.signup(email, org=user["org"])
        with self._lock:
            self._conn.execute("UPDATE users SET owner_id = ? WHERE id = ?",
                               (user["id"], member["id"]))
            self._conn.commit()
        member["owner_id"] = user["id"]
        return member

    def set_tier(self, api_key: str, tier: str) -> dict:
        if tier not in TIERS:
            raise ValueError(f"unknown tier: {tier}")
        user = self.by_key(api_key)
        if user is None:
            raise ValueError("unknown API key")
        root = self.root_of(user)  # a seat's upgrade upgrades the org
        with self._lock:
            self._conn.execute("UPDATE users SET tier = ? WHERE id = ?",
                               (tier, root["id"]))
            self._conn.commit()
        root["tier"] = tier
        return root

    # -- metering ----------------------------------------------------------

    def scans_this_month(self, root_id: int) -> int:
        """Org-wide: every seat draws from the owner's monthly quota."""
        org = self._org_ids(root_id)
        marks = ",".join("?" * len(org))
        with self._lock:
            row = self._conn.execute(
                f"SELECT COUNT(*) AS n FROM scans "
                f"WHERE user_id IN ({marks}) AND created_at LIKE ?",
                (*org, f"{_month_prefix()}%")).fetchone()
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

    def upsert_requisition(self, root_id: int, req: str, jd: str,
                           files: int, labels: dict) -> None:
        """Per-requisition rollup: totals accumulate, the JD is remembered so
        the next scan of the same req can prefill it."""
        import json
        req = (req or "").strip()[:120]
        if not req:
            return
        with self._lock:
            row = self._conn.execute(
                "SELECT scans, files, labels, jd FROM requisitions "
                "WHERE root_id = ? AND req = ?", (root_id, req)).fetchone()
            if row:
                merged = json.loads(row["labels"])
                for k, v in labels.items():
                    merged[k] = merged.get(k, 0) + v
                self._conn.execute(
                    "UPDATE requisitions SET scans = ?, files = ?, labels = ?, "
                    "jd = ?, updated_at = ? WHERE root_id = ? AND req = ?",
                    (row["scans"] + 1, row["files"] + files,
                     json.dumps(merged), jd or row["jd"], _now(), root_id, req))
            else:
                self._conn.execute(
                    "INSERT INTO requisitions (root_id, req, jd, scans, files, "
                    "labels, updated_at) VALUES (?, ?, ?, 1, ?, ?, ?)",
                    (root_id, req, jd or "", files, json.dumps(labels), _now()))
            self._conn.commit()

    def requisitions(self, root_id: int) -> list[dict]:
        import json
        with self._lock:
            rows = self._conn.execute(
                "SELECT req, jd, scans, files, labels, updated_at "
                "FROM requisitions WHERE root_id = ? ORDER BY updated_at DESC",
                (root_id,)).fetchall()
        return [{**dict(r), "labels": json.loads(r["labels"])} for r in rows]

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
                    "seats": 1, "seats_used": 1, "role": "demo",
                    "json_export": False, "api_access": False,
                    "custom_signatures": False}
        root = self.root_of(user)
        tier = TIERS[root["tier"]]
        used = self.scans_this_month(root["id"])
        cap = tier["scans_per_month"]
        return {
            "tier": root["tier"], "label": tier["label"],
            "max_files": tier["max_files"],
            "scans_per_month": cap, "scans_used": used,
            "scans_left": None if cap is None else max(0, cap - used),
            "seats": tier["seats"],
            "seats_used": 1 + len(self.members(root["id"])),
            "role": "member" if user.get("owner_id") else "owner",
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
