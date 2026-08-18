"""Analyzers: pure `(ParsedDoc, Context) -> list[Signal]` functions.

Per-document analyzers run first; population analyzers need the whole batch
indexed before they can say anything, so the pipeline runs them in a second
pass.
"""

from .boilerplate import analyze_boilerplate
from .contact import analyze_contact
from .dedupe import analyze_dedupe
from .forensics import analyze_forensics
from .hidden import analyze_hidden
from .jd_mirror import analyze_jd_mirror
from .layout import analyze_layout

PER_DOCUMENT = [analyze_forensics, analyze_hidden, analyze_layout, analyze_jd_mirror]
POPULATION = [analyze_dedupe, analyze_contact, analyze_boilerplate]

__all__ = [
    "analyze_boilerplate", "analyze_contact", "analyze_dedupe",
    "analyze_forensics", "analyze_hidden", "analyze_jd_mirror",
    "analyze_layout", "PER_DOCUMENT", "POPULATION",
]
