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
import struct
from datetime import datetime, timedelta, timezone
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

# How long an account's population memory keeps a document. Long enough for a
# farm's cadence to become visible, short enough that the index is not a
# permanent record of everyone who ever applied.
MEMORY_RETENTION_DAYS = int(os.environ.get("SIGNALHIRE_MEMORY_DAYS", "90"))

# SQLite's default limit is 999 bound variables per statement; a document
# contributes ~80 keys, so lookups are chunked well under it.
_KEY_CHUNK = 400

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
-- Cross-scan population memory. One row per remembered document, holding
-- one-way keys only: MinHash bands of the identity-masked body, the layout
-- fingerprint, hashed contact handles and a hashed phrase sample. No text, no
-- names, nothing a document could be reconstructed from — which is what makes
-- remembering an applicant pool defensible in the first place.
CREATE TABLE IF NOT EXISTS memory_docs (
    id INTEGER PRIMARY KEY,
    root_id INTEGER NOT NULL,
    fingerprint TEXT NOT NULL,
    scan_id TEXT NOT NULL DEFAULT '',
    owner TEXT NOT NULL,
    name_hash TEXT NOT NULL DEFAULT '',
    identified INTEGER NOT NULL DEFAULT 0,
    signature BLOB,
    seen_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS memory_docs_unique
    ON memory_docs(root_id, fingerprint);
CREATE TABLE IF NOT EXISTS memory_keys (
    root_id INTEGER NOT NULL,
    kind TEXT NOT NULL,
    key TEXT NOT NULL,
    doc_id INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS memory_keys_lookup
    ON memory_keys(root_id, kind, key);
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

    # -- population memory -------------------------------------------------

    def memory_for(self, root_id: int) -> "OrgMemory":
        """The account's cross-scan population memory.

        Scoped to the billing account, exactly like the monthly quota and the
        requisition rollups: every seat in an agency contributes to and reads
        from one memory, because the farm they are all being hit by is one
        farm. It is *not* shared across accounts here — the cross-tenant
        version described in §2.3 of the build plan needs a consent and
        contract story this MVP does not have yet, and the keys were designed
        to make it possible later without changing what is stored.

        Never gated by tier. Memory is detection quality, and detection
        quality is identical on every plan, including the free one.
        """
        return OrgMemory(self, root_id)

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


class OrgMemory:
    """`signalhire.memory.PopulationMemory` backed by the account's SQLite rows.

    The engine never imports this class and this class never imports scoring:
    the whole contract is two methods and a record of one-way keys.
    """

    def __init__(self, store: "Store", root_id: int) -> None:
        self._store = store
        self.root_id = root_id

    # The engine's MemoryRecord is imported lazily so the store stays usable
    # (and testable) without the engine loaded.
    @staticmethod
    def _record_cls():
        from signalhire.memory import MemoryRecord
        return MemoryRecord

    @staticmethod
    def _pack(signature) -> bytes:
        values = [int(v) & 0xFFFFFFFFFFFFFFFF for v in signature]
        return struct.pack(f"<{len(values)}Q", *values) if values else b""

    @staticmethod
    def _unpack(blob: bytes | None) -> tuple[int, ...]:
        if not blob:
            return ()
        return struct.unpack(f"<{len(blob) // 8}Q", blob)

    def _row_to_record(self, row):
        return self._record_cls()(
            scan_id=row["scan_id"],
            owner=row["owner"],
            seen_at=datetime.fromisoformat(row["seen_at"]),
            name_hash=row["name_hash"] or "",
            identified=bool(row["identified"]),
            keys={},
            signature=self._unpack(row["signature"]),
        )

    def lookup(self, kind: str, keys) -> dict:
        keys = list(dict.fromkeys(keys))
        if not keys:
            return {}
        out: dict[str, list] = {}
        conn, lock = self._store._conn, self._store._lock
        for start in range(0, len(keys), _KEY_CHUNK):
            chunk = keys[start:start + _KEY_CHUNK]
            marks = ",".join("?" * len(chunk))
            with lock:
                rows = conn.execute(
                    f"SELECT k.key AS lookup_key, d.* FROM memory_keys k "
                    f"JOIN memory_docs d ON d.id = k.doc_id "
                    f"WHERE k.root_id = ? AND k.kind = ? "
                    f"AND k.key IN ({marks})",
                    (self.root_id, kind, *chunk)).fetchall()
            for row in rows:
                out.setdefault(row["lookup_key"], []).append(
                    self._row_to_record(row))
        return out

    def counts(self, kind: str, keys, *, exclude_scan: str = "",
               exclude_owner: str = "") -> dict:
        """Distinct prior owners and scans per key, as one aggregate query.

        This is the query that has to stay cheap as an account's history
        grows. A phrase used by a farm that has sent five thousand
        applications is held by five thousand rows; counting them in SQL costs
        an index scan, fetching them costs five thousand objects — and the
        bigger the farm, the worse the second one gets, which is precisely
        backwards.
        """
        from signalhire.memory import KeyCounts

        keys = list(dict.fromkeys(keys))
        if not keys:
            return {}
        out: dict[str, KeyCounts] = {}
        conn, lock = self._store._conn, self._store._lock
        for start in range(0, len(keys), _KEY_CHUNK):
            chunk = keys[start:start + _KEY_CHUNK]
            marks = ",".join("?" * len(chunk))
            with lock:
                rows = conn.execute(
                    f"SELECT k.key AS lookup_key, "
                    f"       COUNT(DISTINCT d.owner) AS owners, "
                    f"       COUNT(DISTINCT d.scan_id) AS scans "
                    f"FROM memory_keys k JOIN memory_docs d ON d.id = k.doc_id "
                    f"WHERE k.root_id = ? AND k.kind = ? "
                    f"AND k.key IN ({marks}) "
                    f"AND d.scan_id IS NOT ? AND d.owner IS NOT ? "
                    f"GROUP BY k.key",
                    (self.root_id, kind, *chunk,
                     exclude_scan, exclude_owner)).fetchall()
            for row in rows:
                if row["owners"]:
                    out[row["lookup_key"]] = KeyCounts(owners=row["owners"],
                                                       scans=row["scans"])
        return out

    def remember(self, records) -> None:
        if not records:
            return
        conn, lock = self._store._conn, self._store._lock
        with lock:
            for record in records:
                cur = conn.execute(
                    "INSERT OR IGNORE INTO memory_docs "
                    "(root_id, fingerprint, scan_id, owner, name_hash, "
                    " identified, signature, seen_at) "
                    "VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
                    (self.root_id, record.dedupe_key(), record.scan_id,
                     record.owner, record.name_hash, int(record.identified),
                     self._pack(record.signature), record.seen_at.isoformat()))
                if not cur.rowcount:
                    # Already remembered under this owner: a re-uploaded batch
                    # must not inflate the population it is judged against.
                    continue
                doc_id = cur.lastrowid
                conn.executemany(
                    "INSERT INTO memory_keys (root_id, kind, key, doc_id) "
                    "VALUES (?, ?, ?, ?)",
                    [(self.root_id, kind, key, doc_id)
                     for kind, keys in record.keys.items() for key in keys])
            self._prune(conn)
            conn.commit()

    def _prune(self, conn) -> None:
        cutoff = (datetime.now(timezone.utc)
                  - timedelta(days=MEMORY_RETENTION_DAYS)).isoformat()
        stale = [r["id"] for r in conn.execute(
            "SELECT id FROM memory_docs WHERE root_id = ? AND seen_at < ?",
            (self.root_id, cutoff)).fetchall()]
        for start in range(0, len(stale), _KEY_CHUNK):
            chunk = stale[start:start + _KEY_CHUNK]
            marks = ",".join("?" * len(chunk))
            conn.execute(
                f"DELETE FROM memory_keys WHERE doc_id IN ({marks})", chunk)
            conn.execute(
                f"DELETE FROM memory_docs WHERE id IN ({marks})", chunk)

    def size(self) -> int:
        with self._store._lock:
            row = self._store._conn.execute(
                "SELECT COUNT(*) AS n FROM memory_docs WHERE root_id = ?",
                (self.root_id,)).fetchone()
        return int(row["n"])
