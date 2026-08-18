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

SUPPORTED_SUFFIXES = {".pdf", ".docx", ".odt", ".rtf", ".html", ".htm",
                      ".txt", ".md", ".doc"}

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


def _lines_to_blocks(lines: list[str], font: str = "PlainText") -> list[dict]:
    return [
        {"text": ln, "font": font, "size": 11.0, "color": 0,
         "bbox": [0.0, float(i * 12), 500.0, float(i * 12 + 11)]}
        for i, ln in enumerate(lines)
    ]


def _assemble(doc_id: str, application_id: str, data: bytes, source_path: str,
              text: str, pages: list[dict], meta: dict, fonts: list[str],
              parse_error: str = "") -> ParsedDoc:
    doc = ParsedDoc(
        doc_id=doc_id, application_id=application_id, source_path=source_path,
        text=text, pages=pages, meta=meta, fonts=fonts,
        sha256=hashlib.sha256(data).hexdigest(), parse_error=parse_error,
    )
    doc.identity = extract_identity(text, pages)
    return doc


def parse_text(doc_id: str, application_id: str, data: bytes, source_path: str = "") -> ParsedDoc:
    """Plain-text fallback so the population analyzers can be exercised on
    non-PDF sources (pasted resume bodies, ATS text fields)."""
    text = data.decode("utf-8", errors="replace")
    lines = [ln for ln in text.splitlines() if ln.strip()]
    pages = [{"num": 0, "blocks": _lines_to_blocks(lines),
              "width": 612.0, "height": 792.0}]
    return _assemble(doc_id, application_id, data, source_path, text, pages,
                     {"producer": "", "creator": "", "source_kind": "plaintext"},
                     ["PlainText"])


# --------------------------------------------------------------------------
# DOCX — a zip of XML. Parsed with the stdlib so the forensic layer survives:
# run-level colour/size/vanish (the DOCX versions of white-on-white and 1pt
# text), the authoring Application from docProps, and creation/modification
# timestamps mapped onto the same signals the PDF path feeds.
# --------------------------------------------------------------------------

_W = "{http://schemas.openxmlformats.org/wordprocessingml/2006/main}"
_DC = "{http://purl.org/dc/elements/1.1/}"
_DCTERMS = "{http://purl.org/dc/terms/}"
_CP = ("{http://schemas.openxmlformats.org/package/2006/metadata/"
       "core-properties}")
_EP = ("{http://schemas.openxmlformats.org/officeDocument/2006/"
       "extended-properties}")
_ISO_DATE_RE = re.compile(
    r"(\d{4})-(\d{2})-(\d{2})[T ](\d{2}):(\d{2}):(\d{2})")


def _iso_to_pdf_date(value: str) -> str:
    """'2026-08-01T10:00:00Z' -> 'D:20260801100000' so parse_pdf_date and the
    timestamp signals (FRESH_GENERATION, SINGLE_SHOT_PDF) work unchanged."""
    m = _ISO_DATE_RE.match(value or "")
    return "D:" + "".join(m.groups()) if m else ""


def _zip_xml(zf: "zipfile.ZipFile", name: str):
    import xml.etree.ElementTree as ET
    try:
        return ET.fromstring(zf.read(name))
    except KeyError:
        return None


def parse_docx(doc_id: str, application_id: str, data: bytes,
               source_path: str = "") -> ParsedDoc:
    import zipfile
    try:
        zf = zipfile.ZipFile(io.BytesIO(data))
        body = _zip_xml(zf, "word/document.xml")
        if body is None:
            raise ValueError("no word/document.xml")
    except Exception as exc:
        return _assemble(doc_id, application_id, data, source_path, "", [],
                         {"source_kind": "docx"}, [],
                         parse_error=f"{type(exc).__name__}: {exc}")

    meta: dict[str, object] = {"source_kind": "docx"}
    core = _zip_xml(zf, "docProps/core.xml")
    if core is not None:
        for tag, key in ((f"{_DC}creator", "creator"),
                         (f"{_CP}lastModifiedBy", "last_modified_by"),
                         (f"{_DC}title", "title")):
            el = core.find(tag)
            if el is not None and el.text:
                meta[key] = el.text
        for tag, key in ((f"{_DCTERMS}created", "creationdate"),
                         (f"{_DCTERMS}modified", "moddate")):
            el = core.find(tag)
            if el is not None and el.text:
                stamped = _iso_to_pdf_date(el.text)
                if stamped:
                    meta[key] = stamped
    app_xml = _zip_xml(zf, "docProps/app.xml")
    if app_xml is not None:
        el = app_xml.find(f"{_EP}Application")
        if el is not None and el.text:
            version = app_xml.find(f"{_EP}AppVersion")
            meta["producer"] = el.text + (
                f" {version.text}" if version is not None and version.text else "")

    blocks: list[dict] = []
    fonts: set[str] = set()
    texts: list[str] = []
    y = 0.0
    for para in body.iter(f"{_W}p"):
        for run in para.iter(f"{_W}r"):
            text = "".join(t.text or "" for t in run.iter(f"{_W}t"))
            if not text.strip():
                continue
            rpr = run.find(f"{_W}rPr")
            color, size, font, vanished = 0, 11.0, "docx-default", False
            if rpr is not None:
                c = rpr.find(f"{_W}color")
                if c is not None:
                    raw = c.get(f"{_W}val", "")
                    if re.fullmatch(r"[0-9a-fA-F]{6}", raw):
                        color = int(raw, 16)
                sz = rpr.find(f"{_W}sz")
                if sz is not None and (sz.get(f"{_W}val") or "").isdigit():
                    size = int(sz.get(f"{_W}val")) / 2.0   # half-points
                rf = rpr.find(f"{_W}rFonts")
                if rf is not None and rf.get(f"{_W}ascii"):
                    font = rf.get(f"{_W}ascii")
                v = rpr.find(f"{_W}vanish")
                vanished = v is not None and v.get(f"{_W}val") not in ("0", "false")
            fonts.add(font)
            blocks.append({
                "text": text, "font": font, "size": round(size, 1),
                "color": color, "markup_hidden": vanished,
                "bbox": [0.0, y, 500.0, y + size],
            })
            texts.append(text)
        y += 14.0
    pages = [{"num": 0, "blocks": blocks, "width": 612.0, "height": 792.0}]
    return _assemble(doc_id, application_id, data, source_path,
                     " ".join(texts), pages, meta, sorted(fonts))


# --------------------------------------------------------------------------
# ODT — also a zip of XML: text from content.xml, the generator string and
# timestamps from meta.xml.
# --------------------------------------------------------------------------

_ODF_TEXT = "{urn:oasis:names:tc:opendocument:xmlns:text:1.0}"
_ODF_META = "{urn:oasis:names:tc:opendocument:xmlns:meta:1.0}"


def parse_odt(doc_id: str, application_id: str, data: bytes,
              source_path: str = "") -> ParsedDoc:
    import zipfile
    try:
        zf = zipfile.ZipFile(io.BytesIO(data))
        content = _zip_xml(zf, "content.xml")
        if content is None:
            raise ValueError("no content.xml")
    except Exception as exc:
        return _assemble(doc_id, application_id, data, source_path, "", [],
                         {"source_kind": "odt"}, [],
                         parse_error=f"{type(exc).__name__}: {exc}")

    meta: dict[str, object] = {"source_kind": "odt"}
    meta_xml = _zip_xml(zf, "meta.xml")
    if meta_xml is not None:
        gen = meta_xml.find(f".//{_ODF_META}generator")
        if gen is not None and gen.text:
            meta["producer"] = gen.text
        created = meta_xml.find(f".//{_ODF_META}creation-date")
        if created is not None and created.text:
            stamped = _iso_to_pdf_date(created.text)
            if stamped:
                meta["creationdate"] = stamped

    lines = []
    for p in content.iter(f"{_ODF_TEXT}p"):
        line = "".join(p.itertext()).strip()
        if line:
            lines.append(line)
    pages = [{"num": 0, "blocks": _lines_to_blocks(lines, "odt-default"),
              "width": 612.0, "height": 792.0}]
    return _assemble(doc_id, application_id, data, source_path,
                     "\n".join(lines), pages, meta, ["odt-default"])


# --------------------------------------------------------------------------
# RTF — control-word stripping good enough for body text, plus the generator
# string when one is stamped.
# --------------------------------------------------------------------------

_RTF_GENERATOR = re.compile(r"\\\*\\generator\s+([^;{}\\]+)")


def parse_rtf(doc_id: str, application_id: str, data: bytes,
              source_path: str = "") -> ParsedDoc:
    raw = data.decode("latin-1", errors="replace")
    meta: dict[str, object] = {"source_kind": "rtf"}
    gen = _RTF_GENERATOR.search(raw)
    if gen:
        meta["producer"] = gen.group(1).strip()

    text = re.sub(r"\{\\\*[^{}]*\}", " ", raw)                  # meta groups
    text = re.sub(r"\\'([0-9a-fA-F]{2})",
                  lambda m: chr(int(m.group(1), 16)), text)     # hex escapes
    text = re.sub(r"\\(par|line|sect|page)\b", "\n", text)
    text = re.sub(r"\\[a-zA-Z]+-?\d* ?", " ", text)             # control words
    text = re.sub(r"[{}]|\\[^a-zA-Z]", " ", text)
    lines = [" ".join(ln.split()) for ln in text.splitlines() if ln.strip()]
    pages = [{"num": 0, "blocks": _lines_to_blocks(lines, "rtf-default"),
              "width": 612.0, "height": 792.0}]
    return _assemble(doc_id, application_id, data, source_path,
                     "\n".join(lines), pages, meta, ["rtf-default"])


# --------------------------------------------------------------------------
# HTML — visible text plus the classic CSS concealment tricks (display:none,
# visibility:hidden, zero/one-px fonts, white ink) surfaced to the hidden-text
# analyzer via the same block fields the PDF path uses.
# --------------------------------------------------------------------------

_CSS_INVISIBLE = re.compile(
    r"display\s*:\s*none|visibility\s*:\s*hidden|font-size\s*:\s*0")
_CSS_WHITE = re.compile(r"(?<![-\w])color\s*:\s*(#fff\b|#ffffff\b|white\b)")


def parse_html(doc_id: str, application_id: str, data: bytes,
               source_path: str = "") -> ParsedDoc:
    from html.parser import HTMLParser

    class Collector(HTMLParser):
        SKIP = {"script", "style", "head", "title", "noscript"}

        def __init__(self) -> None:
            super().__init__()
            self.chunks: list[tuple[str, bool, bool]] = []  # text, hidden, white
            self.stack: list[tuple[str, bool, bool]] = []
            self.generator = ""

        def handle_starttag(self, tag, attrs):
            a = dict(attrs)
            if tag == "meta" and a.get("name", "").lower() == "generator":
                self.generator = a.get("content", "")
            if tag in ("br", "img", "hr", "input", "meta", "link"):
                return
            style = (a.get("style") or "").lower()
            parent = self.stack[-1] if self.stack else ("", False, False)
            hidden = parent[1] or bool(_CSS_INVISIBLE.search(style)) \
                or tag in self.SKIP or a.get("hidden") is not None
            white = parent[2] or bool(_CSS_WHITE.search(style))
            self.stack.append((tag, hidden, white))

        def handle_endtag(self, tag):
            for i in range(len(self.stack) - 1, -1, -1):
                if self.stack[i][0] == tag:
                    del self.stack[i:]
                    break

        def handle_data(self, text):
            text = " ".join(text.split())
            if not text:
                return
            state = self.stack[-1] if self.stack else ("", False, False)
            if state[0] in self.SKIP:
                return
            self.chunks.append((text, state[1], state[2]))

    parser = Collector()
    parser.feed(data.decode("utf-8", errors="replace"))
    blocks = []
    y = 0.0
    for text, hidden, white in parser.chunks:
        blocks.append({
            "text": text, "font": "html-default", "size": 11.0,
            "color": 0xFFFFFF if white else 0, "markup_hidden": hidden,
            "bbox": [0.0, y, 500.0, y + 11.0],
        })
        y += 12.0
    meta: dict[str, object] = {"source_kind": "html"}
    if parser.generator:
        meta["producer"] = parser.generator
    pages = [{"num": 0, "blocks": blocks, "width": 612.0, "height": 792.0}]
    return _assemble(doc_id, application_id, data, source_path,
                     " ".join(c[0] for c in parser.chunks), pages, meta,
                     ["html-default"])


def parse_doc_legacy(doc_id: str, application_id: str, data: bytes,
                     source_path: str = "") -> ParsedDoc:
    """Binary OLE .doc: accepted so the file shows up in the report as a parse
    failure instead of silently vanishing, but not text-extracted — guessing at
    the OLE stream produces mojibake that would poison the population signals."""
    return _assemble(doc_id, application_id, data, source_path, "", [],
                     {"source_kind": "doc"}, [],
                     parse_error="legacy binary .doc — export as .docx or PDF")


_PARSERS = {
    ".pdf": parse_pdf,
    ".docx": parse_docx,
    ".odt": parse_odt,
    ".rtf": parse_rtf,
    ".html": parse_html,
    ".htm": parse_html,
    ".doc": parse_doc_legacy,
}


def parse_bytes(doc_id: str, application_id: str, data: bytes,
                source_path: str = "") -> ParsedDoc:
    """Single dispatch point for every supported format; anything unrecognized
    falls back to the plain-text parser."""
    suffix = Path(source_path).suffix.lower()
    parser = _PARSERS.get(suffix, parse_text)
    return parser(doc_id, application_id, data, source_path)


def parse_file(path: str | Path, application_id: str | None = None) -> ParsedDoc:
    path = Path(path)
    data = path.read_bytes()
    doc_id = hashlib.sha256(f"{path.resolve()}".encode()).hexdigest()[:16]
    application_id = application_id or str(uuid.uuid5(uuid.NAMESPACE_URL, str(path.resolve())))
    doc = parse_bytes(doc_id, application_id, data, str(path))
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
