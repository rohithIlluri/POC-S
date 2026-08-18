"""Analyzer C — hidden content & prompt injection (deterministic).

Everything here is an objective property of the file, not a judgement about
the writer: text painted white-on-white, text at 1pt, text positioned off the
page, or an instruction aimed at whatever LLM reads the resume next. These are
the only signals allowed to flag an application on their own.

Two precision rules keep the deterministic signals honest:

  * Near-white text only counts as hidden on a light-themed document. A resume
    designed on a dark background renders *all* of its text near-white; text
    painted white to hide it is a minority slice of an otherwise dark-inked
    document. (Without the page's real background colour this is a heuristic —
    an all-white document on a white page reads as blank to a human, but it is
    indistinguishable here from a dark-theme design.)
  * Prompt-injection patterns are deterministic only when the instruction is
    *hidden*. Run over visible text they flag ordinary resumes — anyone who
    "designed the system prompt for a support agent" or can "act as an
    assistant to recruiters". A visible match on the high-precision patterns
    is surfaced as a WEAK, non-risk signal instead.
"""

from __future__ import annotations

import re

from ..types import Context, ParsedDoc, Severity, Signal

ANALYZER = "hidden"

HIDDEN_WORD_THRESHOLD = 5
NEAR_WHITE_CHANNEL = 0xF0
TINY_FONT_PT = 2.5
# At or above this share of near-white words, the document is a dark-theme
# design, not a light document with a hidden block appended.
DARK_THEME_FRACTION = 0.7

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

# The subset precise enough to be worth surfacing when it appears in *visible*
# text. "system prompt" or "act as an assistant" in plain sight is a normal
# resume sentence; "ignore all previous instructions" is not.
VISIBLE_PATTERN_LABELS = {
    "ignore_previous", "score_override", "disregard_criteria",
    "injection_non_english", "pseudo_tag",
}


def _channels(color: int) -> tuple[int, int, int]:
    """PyMuPDF packs span colour as an sRGB int (0xRRGGBB)."""
    color = int(color) & 0xFFFFFF
    return (color >> 16) & 0xFF, (color >> 8) & 0xFF, color & 0xFF


def is_near_white(color: int) -> bool:
    return min(_channels(color)) >= NEAR_WHITE_CHANNEL


def _off_page(bbox: list[float], width: float, height: float) -> bool:
    x0, y0, x1, y1 = bbox
    return x1 <= 0 or y1 <= 0 or x0 >= width or y0 >= height


def _is_dark_theme(doc: ParsedDoc) -> bool:
    total = near_white = 0
    for page in doc.pages:
        for b in page["blocks"]:
            words = len(b["text"].split())
            total += words
            if is_near_white(b["color"]):
                near_white += words
    return total > 0 and near_white / total >= DARK_THEME_FRACTION


def _match_labels(text_lower: str) -> set[str]:
    return {label for pattern, label in INJECTION_PATTERNS
            if re.search(pattern, text_lower)}


def analyze_hidden(doc: ParsedDoc, ctx: Context) -> list[Signal]:
    signals: list[Signal] = []
    hidden_words = 0
    samples: list[str] = []
    reasons: set[str] = set()
    hidden_texts: list[str] = []
    dark_theme = _is_dark_theme(doc)

    for page in doc.pages:
        width, height = page.get("width", 612.0), page.get("height", 792.0)
        for b in page["blocks"]:
            text = b["text"].strip()
            if not text:
                continue
            why = []
            if not dark_theme and is_near_white(b["color"]):
                why.append("near_white_text")
            if b["size"] <= TINY_FONT_PT:
                why.append("sub_3pt_font")
            if _off_page(b["bbox"], width, height):
                why.append("off_page_position")
            if why:
                hidden_words += len(text.split())
                reasons.update(why)
                hidden_texts.append(text)
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

    hidden_hits = _match_labels(" ".join(hidden_texts).lower())
    if hidden_hits:
        signals.append(Signal(
            code="PROMPT_INJECTION", severity=Severity.DETERMINISTIC, score_impact=0.95,
            evidence={"patterns_matched": sorted(hidden_hits)}, analyzer=ANALYZER,
        ))

    visible_hits = (_match_labels(doc.text.lower()) & VISIBLE_PATTERN_LABELS) - hidden_hits
    if visible_hits:
        signals.append(Signal(
            code="INJECTION_PHRASE", severity=Severity.WEAK, score_impact=0.2,
            evidence={"patterns_matched": sorted(visible_hits),
                      "note": "matched in visible text — surfaced for review, "
                              "not treated as a hidden instruction"},
            analyzer=ANALYZER,
        ))

    return signals
