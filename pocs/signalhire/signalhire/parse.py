"""Stage 1 — parse & extract.

Pulls three things out of every document:
  1. text            — for JD-mirroring and near-duplicate clustering
  2. layout spans    — font/size/colour/bbox per text run, for layout
                       fingerprinting and hidden-text detection
  3. metadata        — producer/creator/dates, for toolchain forensics

Identity is extracted here and immediately hashed: nothing downstream of this
module sees a clear-text email or phone number.
"""

from __future__ import annotations

import hashlib
import io
import os
import re
import uuid
from datetime import datetime, timezone
from pathlib import Path

try:  # PyMuPDF renamed its import in 1.24; support both.
    import pymupdf  # type: ignore
except ImportError:  # pragma: no cover - old PyMuPDF
    import fitz as pymupdf  # type: ignore

import pikepdf

from .types import Identity, ParsedDoc

SUPPORTED_SUFFIXES = {".pdf", ".txt", ".md"}

_EMAIL_RE = re.compile(r"[\w.+-]+@[\w-]+\.[\w.-]+")
# Deliberately permissive: US-style and +country formats, 10-15 digits.
_PHONE_RE = re.compile(r"(?:\+?\d{1,3}[\s.-]?)?(?:\(?\d{3}\)?[\s.-]?)\d{3}[\s.-]?\d{4}")
# The PDF spec (§7.9.4) allows truncated dates: everything after the year is
# optional, defaulting to Jan 1 / midnight.
_PDF_DATE_RE = re.compile(r"D:(\d{4})(\d{2})?(\d{2})?(\d{2})?(\d{2})?(\d{2})?")
_PDF_DATE_DEFAULTS = (None, 1, 1, 0, 0, 0)


def hash_salt() -> str:
    """Per-deployment salt for identity hashes.

    Set `SIGNALHIRE_HASH_SALT` in every real deployment; the default exists so
    the CLI and tests run out of the box on a local folder of PDFs.
    """
    return os.environ.get("SIGNALHIRE_HASH_SALT", "signalhire-local-dev-salt")


def salted_hash(value: str) -> str:
    value = (value or "").strip().lower()
    if not value:
        return ""
    return hashlib.sha256(f"{hash_salt()}:{value}".encode()).hexdigest()[:32]


def parse_pdf_date(s: str | None) -> datetime | None:
    m = _PDF_DATE_RE.match(s or "")
    if not m:
        return None
    parts = [int(g) if g is not None else d
             for g, d in zip(m.groups(), _PDF_DATE_DEFAULTS)]
    try:
        return datetime(*parts, tzinfo=timezone.utc)
    except ValueError:
        return None


def _normalize_phone(raw: str) -> str:
    digits = re.sub(r"\D", "", raw)
    # Drop a leading US country code so +1-555… and (555)… collapse together.
    if len(digits) == 11 and digits.startswith("1"):
        digits = digits[1:]
    return digits if 7 <= len(digits) <= 15 else ""


def extract_identity(text: str, pages: list[dict]) -> Identity:
    """Best-effort candidate identity from the document itself.

    The name heuristic is the largest-font text run in the top eighth of page
    one, falling back to the first non-empty line — good enough for the CLI
    report and for identity-collision clustering. It is never used to infer
    anything about the person beyond "is this the same handle as that one".
    """
    email = ""
    m = _EMAIL_RE.search(text)
    if m:
        email = m.group(0)

    phone = ""
    for pm in _PHONE_RE.finditer(text):
        phone = _normalize_phone(pm.group(0))
        if phone:
            break

    name = ""
    if pages:
        first = pages[0]
        header_cutoff = first.get("height", 792) / 8.0
        header = [
            b for b in first["blocks"]
            if b["text"].strip() and b["bbox"][1] <= header_cutoff
        ]
        if header:
            name = max(header, key=lambda b: b["size"])["text"].strip()
    if not name:
        for line in text.splitlines():
            if line.strip():
                name = line.strip()
                break
    name = name[:80]

    return Identity(
        email_hash=salted_hash(email),
        phone_hash=salted_hash(phone),
        name_hash=salted_hash(re.sub(r"\s+", " ", name)),
        display_name=name,
    )


def _pdf_low_level_meta(data: bytes) -> dict[str, str]:
    """Metadata pikepdf sees that PyMuPDF's `metadata` dict sometimes drops."""
    meta: dict[str, str] = {}
    try:
        with pikepdf.open(io.BytesIO(data)) as pdf:
            info = pdf.docinfo
            for key in info.keys():
                meta.setdefault(str(key).lstrip("/").lower(), str(info[key]))
            meta["pdf_version"] = str(pdf.pdf_version)
    except Exception as exc:  # a malformed PDF is itself a weak signal, not a crash
        meta["low_level_error"] = f"{type(exc).__name__}: {exc}"
    return meta


def parse_pdf(doc_id: str, application_id: str, data: bytes, source_path: str = "") -> ParsedDoc:
    pages: list[dict] = []
    fonts: set[str] = set()
    full_text: list[str] = []
    meta: dict[str, object] = {}
    parse_error = ""

    try:
        pdf = pymupdf.open(stream=data, filetype="pdf")
    except Exception as exc:
        return ParsedDoc(
            doc_id=doc_id, application_id=application_id, source_path=source_path,
            text="", pages=[], meta={}, fonts=[],
            sha256=hashlib.sha256(data).hexdigest(),
            parse_error=f"{type(exc).__name__}: {exc}",
        )

    with pdf:
        for page in pdf:
            blocks = []
            for b in page.get_text("dict").get("blocks", []):
                for line in b.get("lines", []):
                    for span in line.get("spans", []):
                        fonts.add(span["font"])
                        blocks.append({
                            "text": span["text"],
                            "font": span["font"],
                            "size": round(span["size"], 1),
                            "color": span["color"],          # packed int RGB
                            "bbox": [round(v, 1) for v in span["bbox"]],
                        })
                        full_text.append(span["text"])
            pages.append({
                "num": page.number,
                "blocks": blocks,
                "width": page.rect.width,
                "height": page.rect.height,
            })
        meta = {k.lower(): v for k, v in (pdf.metadata or {}).items() if v}
        meta["page_count"] = pdf.page_count

    for k, v in _pdf_low_level_meta(data).items():
        meta.setdefault(k, v)
    meta["parsed_at"] = datetime.now(timezone.utc).isoformat()

    text = " ".join(full_text)
    doc = ParsedDoc(
        doc_id=doc_id,
        application_id=application_id,
        source_path=source_path,
        text=text,
        pages=pages,
        meta=meta,
        fonts=sorted(fonts),
        sha256=hashlib.sha256(data).hexdigest(),
        parse_error=parse_error,
    )
    doc.identity = extract_identity(text, pages)
    return doc


def parse_text(doc_id: str, application_id: str, data: bytes, source_path: str = "") -> ParsedDoc:
    """Plain-text fallback so the population analyzers can be exercised on
    non-PDF sources (pasted resume bodies, ATS text fields)."""
    text = data.decode("utf-8", errors="replace")
    lines = [ln for ln in text.splitlines() if ln.strip()]
    blocks = [
        {"text": ln, "font": "PlainText", "size": 11.0, "color": 0,
         "bbox": [0.0, float(i * 12), 500.0, float(i * 12 + 11)]}
        for i, ln in enumerate(lines)
    ]
    pages = [{"num": 0, "blocks": blocks, "width": 612.0, "height": 792.0}]
    doc = ParsedDoc(
        doc_id=doc_id, application_id=application_id, source_path=source_path,
        text=text, pages=pages,
        meta={"producer": "", "creator": "", "source_kind": "plaintext"},
        fonts=["PlainText"], sha256=hashlib.sha256(data).hexdigest(),
    )
    doc.identity = extract_identity(text, pages)
    return doc


def parse_file(path: str | Path, application_id: str | None = None) -> ParsedDoc:
    path = Path(path)
    data = path.read_bytes()
    doc_id = hashlib.sha256(f"{path.resolve()}".encode()).hexdigest()[:16]
    application_id = application_id or str(uuid.uuid5(uuid.NAMESPACE_URL, str(path.resolve())))
    if path.suffix.lower() == ".pdf":
        doc = parse_pdf(doc_id, application_id, data, str(path))
    else:
        doc = parse_text(doc_id, application_id, data, str(path))
    # Submission time: the PDF's own creation date is *not* a submission time —
    # use file mtime as the local stand-in. In production this is the real
    # ingestion timestamp from the email/webhook/upload.
    doc.submitted_at = datetime.fromtimestamp(path.stat().st_mtime, tz=timezone.utc)
    return doc


def discover(folder: str | Path, exclude: set[Path] | None = None) -> list[Path]:
    folder = Path(folder)
    skip = {p.resolve() for p in (exclude or set())}
    if folder.is_file():
        supported = folder.suffix.lower() in SUPPORTED_SUFFIXES
        return [folder] if supported and folder.resolve() not in skip else []
    return sorted(
        p for p in folder.rglob("*")
        if p.is_file()
        and p.suffix.lower() in SUPPORTED_SUFFIXES
        and p.resolve() not in skip
    )
