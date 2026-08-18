"""The generator signature DB — the moat.

Two kinds of entry live here:

  * `producer_regex` — matches the PDF producer/creator toolchain string.
    Positive confidence = generator/wrapper toolchain. Negative confidence =
    human-authoring tool (Word, LaTeX, Google Docs), which *reduces* suspicion.
  * `layout_hash` — an exact structural fingerprint of a known template.
    Positive = known wrapper template. Negative = allowlisted human template
    (a popular Google Docs or Overleaf resume template), which suppresses
    the TEMPLATE_SWARM signal entirely.

Seeds are hardcoded below; the collector (§6 of the build plan) appends
validated entries to a JSON file which is merged over the seeds. Every entry
carries a `version` so a score explanation stays reproducible for an audit.
"""

from __future__ import annotations

import json
import re
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

SIGNATURE_DB_VERSION = "seed-2026.08.2"


@dataclass
class GeneratorSignature:
    kind: str            # producer_regex | layout_hash
    pattern: str
    tool_label: str
    confidence: float    # >0 suspicious, <0 human-authoring allowlist
    active: bool = True
    source: str = "seed"
    version: str = SIGNATURE_DB_VERSION
    _compiled: Any = field(default=None, repr=False, compare=False)

    def matches(self, value: str) -> bool:
        if self.kind == "producer_regex":
            if self._compiled is None:
                self._compiled = re.compile(self.pattern, re.I)
            return bool(self._compiled.search(value))
        return self.pattern == value


# (pattern, label, confidence)
_PRODUCER_SEEDS: list[tuple[str, str, float]] = [
    # --- generator / wrapper toolchains -------------------------------------
    (r"react-pdf",                     "react_pdf_builder",     0.7),
    (r"pdfmake",                       "pdfmake_builder",       0.7),
    (r"Puppeteer",                     "puppeteer_pipeline",    0.7),
    (r"wkhtmltopdf",                   "html_to_pdf_pipeline",  0.6),
    (r"WeasyPrint",                    "weasyprint_pipeline",   0.6),
    (r"(HeadlessChrome|Skia/PDF)",     "headless_browser",      0.5),
    (r"Prince",                        "princexml_pipeline",    0.5),
    (r"jsPDF",                         "jspdf_builder",         0.6),
    (r"ReportLab",                     "reportlab_pipeline",    0.4),
    (r"python-docx|docx4j",            "docx_builder",          0.6),
    (r"PhpWord|PHPWord",               "phpword_builder",       0.6),
    (r"Aspose",                        "aspose_pipeline",       0.4),
    (r"Canva",                         "canva",                 0.1),  # ambiguous
    # --- human authoring tools (negative = reduces suspicion) ---------------
    (r"Microsoft.*Word",               "ms_word",              -0.4),
    (r"LaTeX|pdfTeX|XeTeX|LuaTeX",     "latex",                -0.4),
    (r"Google( Docs)?",                "google_docs",          -0.3),
    (r"LibreOffice|OpenOffice",        "libreoffice",          -0.3),
    (r"Acrobat|Adobe PDF Library",     "acrobat",              -0.2),
    (r"(macOS|Quartz|CoreGraphics)",   "mac_print_dialog",     -0.2),
    (r"Pages",                         "apple_pages",          -0.2),
]

# Default PDF titles that wrapper exporters emit and humans rarely keep.
DEFAULT_TITLES = {
    "resume", "untitled document", "untitled", "cv-template",
    "resume-export", "document", "export", "cv", "resume.pdf",
}


def seed_signatures() -> list[GeneratorSignature]:
    return [
        GeneratorSignature("producer_regex", pattern, label, conf)
        for pattern, label, conf in _PRODUCER_SEEDS
    ]


def load_signatures(path: str | Path | None = None) -> list[GeneratorSignature]:
    """Seeds, plus any collector-produced entries from `path` (JSON list).

    Only `active` entries are returned: the collector writes new signatures
    with `active=false` until they clear the validation gate (matches every
    sample from their tool AND zero documents in the verified-human corpus).
    """
    sigs = seed_signatures()
    if path:
        p = Path(path)
        if p.exists():
            for raw in json.loads(p.read_text()):
                sigs.append(GeneratorSignature(
                    kind=raw["kind"],
                    pattern=raw["pattern"],
                    tool_label=raw["tool_label"],
                    confidence=float(raw["confidence"]),
                    active=bool(raw.get("active", False)),
                    source=raw.get("source", "collector"),
                    version=raw.get("version", "collector"),
                ))
    return [s for s in sigs if s.active]


def template_indexes(
    signatures: list[GeneratorSignature],
) -> tuple[dict[str, str], dict[str, str], dict[str, str]]:
    """Split layout signatures into (known exact, allowlist, known loose).

    Loose (font-profile) hashes only ever *flag* templates — an allowlist
    entry must match exactly, because suppressing the swarm signal on a loose
    profile would let a wrapper hide behind Helvetica-11."""
    known: dict[str, str] = {}
    allow: dict[str, str] = {}
    known_loose: dict[str, str] = {}
    for s in signatures:
        if s.kind == "layout_hash":
            (known if s.confidence > 0 else allow)[s.pattern] = s.tool_label
        elif s.kind == "layout_hash_loose" and s.confidence > 0:
            known_loose[s.pattern] = s.tool_label
    return known, allow, known_loose
