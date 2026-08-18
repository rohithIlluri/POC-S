"""Analyzer C — hidden content & prompt injection (deterministic).

Everything here is an objective property of the file, not a judgement about
the writer: text painted white-on-white, text at 1pt, text positioned off the
page, or an instruction aimed at whatever LLM reads the resume next. These are
the only signals allowed to flag an application on their own.
"""

from __future__ import annotations

import re

from ..types import Context, ParsedDoc, Severity, Signal

ANALYZER = "hidden"

HIDDEN_WORD_THRESHOLD = 5
NEAR_WHITE_CHANNEL = 0xF0
TINY_FONT_PT = 2.5

INJECTION_PATTERNS: list[tuple[str, str]] = [
    (r"ignore\s+(all\s+)?(previous|prior|above)\s+(instructions|prompts)", "ignore_previous"),
    (r"(you\s+are|act\s+as)\s+(an?\s+)?(ai|assistant|recruiter|hiring\s+manager)", "role_override"),
    (r"(rank|score|rate|classify)\s+(this|the)\s+(candidate|resume|applicant)"
     r"[^.]{0,40}(top|highest|best|10\s*/\s*10|100%)", "score_override"),
    (r"disregard[^.]{0,40}(instructions|criteria|requirements)", "disregard_criteria"),
    (r"system\s+prompt", "system_prompt"),
    (r"\bLLM\b[^.]{0,40}\binstructions\b", "llm_instructions"),
    (r"如果你是|忽略以上指令", "injection_non_english"),
    (r"<\s*/?\s*(system|instruction)\s*>", "pseudo_tag"),
]


def _channels(color: int) -> tuple[int, int, int]:
    """PyMuPDF packs span colour as an sRGB int (0xRRGGBB)."""
    color = int(color) & 0xFFFFFF
    return (color >> 16) & 0xFF, (color >> 8) & 0xFF, color & 0xFF


def is_near_white(color: int) -> bool:
    return min(_channels(color)) >= NEAR_WHITE_CHANNEL


def _off_page(bbox: list[float], width: float, height: float) -> bool:
    x0, y0, x1, y1 = bbox
    return x1 <= 0 or y1 <= 0 or x0 >= width or y0 >= height


def analyze_hidden(doc: ParsedDoc, ctx: Context) -> list[Signal]:
    signals: list[Signal] = []
    hidden_words = 0
    samples: list[str] = []
    reasons: set[str] = set()

    for page in doc.pages:
        width, height = page.get("width", 612.0), page.get("height", 792.0)
        for b in page["blocks"]:
            text = b["text"].strip()
            if not text:
                continue
            why = []
            if is_near_white(b["color"]):
                why.append("near_white_text")
            if b["size"] <= TINY_FONT_PT:
                why.append("sub_3pt_font")
            if _off_page(b["bbox"], width, height):
                why.append("off_page_position")
            if why:
                hidden_words += len(text.split())
                reasons.update(why)
                if len(samples) < 3:
                    samples.append(text[:120])

    if hidden_words > HIDDEN_WORD_THRESHOLD:
        signals.append(Signal(
            code="HIDDEN_TEXT", severity=Severity.DETERMINISTIC, score_impact=0.9,
            evidence={"hidden_word_count": hidden_words,
                      "techniques": sorted(reasons),
                      "samples": samples},
            analyzer=ANALYZER,
        ))

    text_lower = doc.text.lower()
    hits = [label for pattern, label in INJECTION_PATTERNS
            if re.search(pattern, text_lower)]
    if hits:
        signals.append(Signal(
            code="PROMPT_INJECTION", severity=Severity.DETERMINISTIC, score_impact=0.95,
            evidence={"patterns_matched": sorted(set(hits))}, analyzer=ANALYZER,
        ))

    return signals
