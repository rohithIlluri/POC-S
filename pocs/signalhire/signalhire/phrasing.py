"""Plain-language rendering of signals and labels.

Every surface — the HTML report, the terminal summary, the web dashboard —
reads from here, so a finding is worded identically wherever it appears. That
consistency is not cosmetic: a recruiter may have to repeat one of these
sentences to a candidate who asks why they were flagged, and a job seeker
running their own resume through the free tier should read the same words the
recruiter reads about them.

Three rules the wording follows:

  1. Describe the document, never the person. "This file was created 4 seconds
     before it was submitted", not "this applicant rushed".
  2. State the observation, then its limits. Every explanation carries a
     `caveat` naming the innocent reason the signal fires, because most of
     them have one and a reviewer needs it in front of them.
  3. No jargon in the headline. `JD_MIRROR_EXTREME` becomes "Reads like it was
     written from your job posting"; the code stays available underneath for
     anyone who wants to cite it.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Callable

from .types import Severity


@dataclass(frozen=True)
class Explanation:
    """One signal, in words a person can act on."""

    headline: str       # what was observed, plain language
    detail: str         # the specific measurement, in a sentence
    caveat: str         # the innocent explanation, always named
    advice: str = ""    # what a job seeker can do about it (free tier)

    def as_dict(self) -> dict[str, str]:
        return {"headline": self.headline, "detail": self.detail,
                "caveat": self.caveat, "advice": self.advice}


def _pct(value: Any) -> str:
    try:
        return f"{float(value) * 100:.0f}%"
    except (TypeError, ValueError):
        return str(value)


def _count(n: Any, singular: str, plural: str | None = None) -> str:
    plural = plural or singular + "s"
    try:
        n = int(n)
    except (TypeError, ValueError):
        return f"{n} {plural}"
    return f"{n} {singular if n == 1 else plural}"


# Each entry turns a signal's evidence dict into an Explanation. Keeping them
# as functions (rather than format strings) lets the wording adapt to the
# numbers — "4 seconds before" reads differently from "in the same minute as".
EXPLAIN: dict[str, Callable[[dict], Explanation]] = {}


def _explains(code: str):
    def register(fn: Callable[[dict], Explanation]):
        EXPLAIN[code] = fn
        return fn
    return register


# --- toolchain forensics ---------------------------------------------------

@_explains("GEN_TOOL_MATCH")
def _gen_tool(ev: dict) -> Explanation:
    tool = str(ev.get("matched", "a resume-builder tool")).replace("_", " ")
    return Explanation(
        headline="Exported by a resume-generator tool",
        detail=f"The file's metadata names {tool}, a toolchain used by "
               "automated resume builders rather than by desktop word processors.",
        caveat="Some legitimate resume-builder websites use the same libraries. "
               "This says how the file was produced, not who wrote it.",
        advice="If you wrote this yourself, exporting from Word, Google Docs or "
               "LaTeX carries none of this signal.",
    )


@_explains("HUMAN_TOOL_MATCH")
def _human_tool(ev: dict) -> Explanation:
    tool = str(ev.get("matched", "a desktop editor")).replace("_", " ")
    return Explanation(
        headline="Authored in a normal desktop tool",
        detail=f"The file was produced by {tool}, which is what a person "
               "writing their own resume typically uses.",
        caveat="This counts in the applicant's favour and never on its own "
               "clears a document that has other findings.",
    )


@_explains("NO_PRODUCER")
def _no_producer(ev: dict) -> Explanation:
    return Explanation(
        headline="Metadata stripped from the file",
        detail="Authoring tools all stamp their name into a PDF. This file "
               "carries none, which usually means it was removed.",
        caveat="Some privacy tools and print-to-PDF paths strip metadata "
               "routinely, and a few exporters never write it.",
        advice="Harmless on its own — most reviewers will never notice it.",
    )


@_explains("FRESH_GENERATION")
def _fresh(ev: dict) -> Explanation:
    secs = ev.get("seconds_before_submit", 0)
    when = (f"{_count(secs, 'second')} before" if isinstance(secs, int) and secs > 1
            else "moments before")
    return Explanation(
        headline="Created seconds before it was submitted",
        detail=f"The file was generated {when} it arrived, which is the "
               "pattern of a site rendering a resume at the moment of applying.",
        caveat="Someone who exported a fresh PDF and applied immediately looks "
               "identical.",
        advice="Nothing to fix — this only matters alongside other findings.",
    )


@_explains("SINGLE_SHOT_PDF")
def _single_shot(ev: dict) -> Explanation:
    return Explanation(
        headline="Never opened or edited after it was created",
        detail="The file's created and last-modified timestamps are identical "
               "to the second, so it was written once by a machine and never "
               "re-saved.",
        caveat="A document exported once and attached straight away shows the "
               "same thing.",
    )


@_explains("DEFAULT_TITLE")
def _default_title(ev: dict) -> Explanation:
    title = ev.get("title", "a generic name")
    return Explanation(
        headline="Exporter's default filename left in place",
        detail=f"The document's internal title is “{title}”, the placeholder "
               "an export tool writes when nobody sets one.",
        caveat="Plenty of people never change it. Weak on its own.",
    )


@_explains("BATCH_TIMESTAMP_CLUSTER")
def _batch_time(ev: dict) -> Explanation:
    n = ev.get("distinct_applicants_in_window", 0)
    return Explanation(
        headline="Generated in the same few minutes as other applicants",
        detail=f"{_count(n, 'different applicant')} in this batch had their "
               "documents created inside the same ten-minute window.",
        caveat="A job-board deadline or a careers-fair push can cluster honest "
               "applications the same way.",
    )


# --- layout ----------------------------------------------------------------

@_explains("KNOWN_TEMPLATE")
def _known_template(ev: dict) -> Explanation:
    tool = str(ev.get("template", "a known template")).replace("_", " ")
    how = ("its exact structure" if ev.get("match") == "exact_structure"
           else "its font and spacing profile")
    return Explanation(
        headline="Matches a known auto-generated template",
        detail=f"The page structure matches {tool} by {how}. The match uses "
               "layout only — fonts, sizes and column positions — never the "
               "words on the page.",
        caveat="Templates get shared and copied; a person can pick up the same "
               "one honestly.",
        advice="Using your own layout, or a common word-processor template, "
               "avoids this entirely.",
    )


@_explains("ALLOWLISTED_TEMPLATE")
def _allowlisted(ev: dict) -> Explanation:
    tool = str(ev.get("template", "a common template")).replace("_", " ")
    return Explanation(
        headline="Uses a common human-authored template",
        detail=f"The layout matches {tool}, a template widely used by people "
               "writing their own resumes.",
        caveat="Recorded so that the shared-layout finding below is suppressed "
               "rather than counted against the applicant.",
    )


@_explains("TEMPLATE_SWARM")
def _swarm(ev: dict) -> Explanation:
    n = ev.get("same_layout_applicants", 0)
    return Explanation(
        headline="Many applicants share this exact layout",
        detail=f"{_count(n, 'applicant')} in this batch submitted documents "
               "with the same structural fingerprint.",
        caveat="Popular free templates legitimately produce large clusters, "
               "which is why this never flags an application on its own.",
    )


# --- hidden content --------------------------------------------------------

_TECHNIQUE_WORDS = {
    "near_white_text": "text coloured to match the page background",
    "sub_3pt_font": "text too small to read",
    "off_page_position": "text placed outside the page margins",
    "markup_hidden": "text marked hidden in the document's own markup",
}


@_explains("HIDDEN_TEXT")
def _hidden(ev: dict) -> Explanation:
    techniques = [_TECHNIQUE_WORDS.get(t, t)
                  for t in ev.get("techniques", [])]
    how = ", ".join(techniques) if techniques else "text hidden from view"
    words = ev.get("hidden_word_count", 0)
    return Explanation(
        headline="Contains text a human reader cannot see",
        detail=f"{_count(words, 'word')} are concealed using {how}. Hidden "
               "text is invisible on screen and in print, but automated "
               "screening software still reads it.",
        caveat="This is an objective property of the file. Occasionally it is "
               "left over from a template rather than added deliberately — the "
               "hidden words themselves are shown below so you can judge.",
        advice="Check the sample text below. If you did not put it there, your "
               "resume builder may have.",
    )


@_explains("PROMPT_INJECTION")
def _injection(ev: dict) -> Explanation:
    return Explanation(
        headline="Hidden instructions aimed at AI screening software",
        detail="Concealed text in this document gives instructions to whatever "
               "automated system reads it next — for example telling it to rank "
               "the candidate highly.",
        caveat="Because the text is both hidden and instruction-shaped, this is "
               "the engine's strongest single finding.",
        advice="If your resume tool added this, stop using it — this is the "
               "finding most likely to end an application.",
    )


@_explains("INJECTION_PHRASE")
def _injection_phrase(ev: dict) -> Explanation:
    return Explanation(
        headline="Wording that resembles an instruction to an AI reader",
        detail="Visible text in this document reads like an instruction aimed "
               "at automated screening software.",
        caveat="It is in plain sight, not hidden, so it is far more likely to "
               "be an ordinary sentence — someone describing prompt-engineering "
               "work, for instance. Noted for a human to glance at, nothing more.",
    )


# --- job-description mirroring ---------------------------------------------

@_explains("JD_MIRROR_EXTREME")
def _mirror_extreme(ev: dict) -> Explanation:
    overlap = _pct(ev.get("rare_term_overlap", 0))
    total = ev.get("rare_terms_total", 0)
    return Explanation(
        headline="Reads as though it was written from your job posting",
        detail=f"The resume contains {overlap} of the {total} uncommon terms "
               "in your posting. Genuine applicants typically match some; "
               "matching nearly all of them is the signature of a tool that "
               "was given the posting to work from.",
        caveat="A candidate who is a genuinely precise fit, or who tailored "
               "carefully by hand, will also score high here.",
        advice="Tailoring your resume is good practice. Describing your actual "
               "experience in your own words — rather than echoing the posting's "
               "phrasing — reads better to both software and people.",
    )


@_explains("JD_MIRROR_HIGH")
def _mirror_high(ev: dict) -> Explanation:
    overlap = _pct(ev.get("rare_term_overlap", 0))
    return Explanation(
        headline="Closely mirrors your posting's vocabulary",
        detail=f"The resume matches {overlap} of the uncommon terms in your "
               "job posting.",
        caveat="Well-targeted applications naturally overlap with the posting. "
               "Weak on its own.",
        advice="Normal for a tailored application; only notable in combination "
               "with other findings.",
    )


@_explains("JD_PHRASE_LIFT")
def _phrase_lift(ev: dict) -> Explanation:
    samples = ev.get("samples") or []
    lifted = next((v for k, v in ev.items() if k.startswith("lifted_")), 0)
    example = f' For example: “{samples[0]}”.' if samples else ""
    return Explanation(
        headline="Contains phrases copied word-for-word from your posting",
        detail=f"{_count(lifted, 'multi-word phrase')} appear verbatim in both "
               f"the posting and the resume.{example}",
        caveat="Standard industry phrasing appears in both places innocently.",
        advice="Rewrite borrowed phrases in your own words — copied lines rarely "
               "tell a recruiter anything about you.",
    )


# --- population ------------------------------------------------------------

@_explains("RECYCLED_IDENTITY")
def _recycled(ev: dict) -> Explanation:
    n = ev.get("matching_docs_other_identity", 0)
    sim = _pct(ev.get("max_similarity", 0))
    return Explanation(
        headline="The same resume body submitted under a different name",
        detail=f"This document is {sim} identical to {_count(n, 'other application')} "
               "in the batch that carry a different candidate's name and contact "
               "details. The comparison ignores the contact block entirely, so "
               "the match is in the body of the resume.",
        caveat="Shared work history at the same employer, or a resume written "
               "from a widely-circulated example, can produce genuine overlap.",
    )


@_explains("CONTACT_COLLISION")
def _collision(ev: dict) -> Explanation:
    n = ev.get("matching_docs_other_name", 0)
    handles = " and ".join(ev.get("handles", ["contact details"]))
    return Explanation(
        headline="Contact details shared with a different candidate name",
        detail=f"The {handles} on this application also appear on "
               f"{_count(n, 'application')} submitted under a different name.",
        caveat="Family members and partners do share phone numbers, and "
               "agencies sometimes submit on a candidate's behalf.",
    )


@_explains("SPRAY_APPLY")
def _spray(ev: dict) -> Explanation:
    n = ev.get("same_doc_applications", 0)
    return Explanation(
        headline="The same document sent to many roles",
        detail=f"This identical resume appears on {_count(n, 'application')} "
               "in this batch.",
        caveat="Applying widely with one resume is common and entirely "
               "legitimate.",
        advice="Sending the same document everywhere is normal; tailoring for "
               "roles you care about tends to work better.",
    )


@_explains("DUP_CLUSTER")
def _dup_cluster(ev: dict) -> Explanation:
    size = ev.get("cluster_size", 0)
    return Explanation(
        headline="Near-identical to other applications in this batch",
        detail=f"This document belongs to a group of {size} closely matching "
               "applications.",
        caveat="Colleagues from one employer, or a bootcamp cohort, often "
               "produce genuinely similar resumes.",
    )


@_explains("SHARED_BOILERPLATE")
def _boilerplate(ev: dict) -> Explanation:
    frac = _pct(ev.get("shared_phrase_fraction", 0))
    median = ev.get("median_applicants_per_phrase", 0)
    samples = ev.get("samples") or []
    example = f' One shared line: “{samples[0][:90]}”.' if samples else ""
    return Explanation(
        headline="Built largely from phrasing shared across many applicants",
        detail=f"{frac} of this resume's distinctive phrasing also appears in "
               f"other applications — typically shared with {_count(median, 'other applicant')} "
               f"each.{example}",
        caveat="Common industry phrasing and popular resume advice circulate "
               "widely; the threshold is set high enough that ordinary clichés "
               "do not trigger it.",
        advice="Phrasing that appears on dozens of other resumes tells a "
               "recruiter nothing. Replace it with specifics only you can claim.",
    )


@_explains("DISPOSABLE_CONTACT")
def _disposable(ev: dict) -> Explanation:
    domain = ev.get("domain", "a throwaway provider")
    return Explanation(
        headline="Contact email uses a throwaway-mail provider",
        detail=f"The address is on {domain}, a service that issues temporary "
               "mailboxes.",
        caveat="Privacy-conscious applicants use these legitimately.",
        advice="A permanent address makes it easier for employers to reach you.",
    )


@_explains("PARSE_FAILED")
def _parse_failed(ev: dict) -> Explanation:
    return Explanation(
        headline="This file could not be read",
        detail="The document could not be opened for analysis, so no findings "
               "were produced for it.",
        caveat="It has not been assessed either way and needs a human to open "
               "it manually.",
        advice="Re-export as a PDF or DOCX; older .doc files often cannot be "
               "read by automated systems, including applicant tracking software.",
    )


_FALLBACK = Explanation(
    headline="Finding recorded",
    detail="See the technical evidence for details.",
    caveat="Review the underlying data before acting on this.",
)


def explain(code: str, evidence: dict | None = None) -> Explanation:
    """The plain-language reading of one signal."""
    builder = EXPLAIN.get(code)
    if builder is None:
        return _FALLBACK
    try:
        return builder(evidence or {})
    except Exception:
        # Wording must never be able to break a scan.
        return _FALLBACK


# --- labels ----------------------------------------------------------------

LABEL_HEADLINE = {
    "genuine": "Nothing found",
    "needs_review": "Worth a look",
    "mass_generated": "Signs of automated generation",
    "high_risk": "Needs attention first",
}

LABEL_MEANING = {
    "genuine": "Nothing in this application stood out. That is not a judgement "
               "of quality — only that the document itself raised nothing.",
    "needs_review": "A few things stood out, none of them conclusive. Read the "
                    "findings and decide for yourself.",
    "mass_generated": "Several independent checks agree this document was "
                      "produced by an automated tool rather than written for "
                      "this role. That says nothing about whether the candidate "
                      "is a good fit.",
    "high_risk": "Something objective and serious was found — hidden text, "
                 "instructions aimed at screening software, or the same resume "
                 "under another identity. Open this one first.",
}

# What the score means, in words, for whoever is reading it.
def effort_sentence(score: int) -> str:
    if score >= 85:
        return "Reads as an application written for this role."
    if score >= 60:
        return "Mostly reads as original work, with a few generic markers."
    if score >= 35:
        return "Substantially built from reusable or generated material."
    return "Almost entirely generated or reused material."


def risk_sentence(score: int) -> str:
    if score == 0:
        return "No identity or manipulation findings."
    if score < 40:
        return "Minor identity or manipulation findings."
    if score < 70:
        return "Notable identity or manipulation findings."
    return "Serious identity or manipulation findings."


SEVERITY_WORDS = {
    Severity.DETERMINISTIC: "Objective fact",
    Severity.STRONG: "Strong indicator",
    Severity.WEAK: "Weak indicator",
    Severity.INFO: "Context",
}
