# Application Authenticity Engine — Detailed Build Plan
**Working name:** SignalHire (placeholder — rename later)
**Target customer:** Staffing / recruiting agencies (IT staffing first) → expansion: job portals, enterprise TA
**Owner:** Rohith · **Version:** 1.0 · **Date:** Aug 2026

---

## 1. Product Definition

### 1.1 One-liner
A triage and trust layer for agency recruiting pipelines that labels every incoming application as **Genuine effort / Mass-generated / Needs review**, with explainable reason codes — so recruiters spend time on real candidates and submit to clients with confidence.

### 1.2 What it is NOT
- ❌ An "AI writing detector." Single-document AI-text classification has unacceptable false-positive rates (5–61% on human text depending on writer demographics) and is trivially defeated by light editing. It is legally and ethically radioactive as a rejection basis.
- ❌ An auto-reject tool. All outputs are **assistive**: scores + reason codes routed to a human review queue. This is both the ethical position and the compliance position (keeps us on the safe side of NYC LL144 / EU AI Act AEDT obligations early on).

### 1.3 What we detect instead (the defensible signals)
| Signal class | What it catches | Why it's robust |
|---|---|---|
| Document forensics | Wrapper-site / auto-apply generator fingerprints in PDF metadata, fonts, layout | Generators can't easily hide their toolchain; signature DB compounds |
| Cross-applicant clustering | Same resume/cover-letter body recycled across many applicants or many reqs | Individual docs look fine; the *population* view exposes mass generation |
| JD-mirroring | Resume keyword distribution suspiciously fitted to the job description | Mathematically measurable; humans don't hit 95% rare-term overlap |
| Hidden-content attacks | White-text keyword stuffing, prompt-injection strings aimed at ATS AI | Deterministic detection, zero false positives |
| Submission behavior (Phase 2) | Bot-speed form fills, velocity, device/IP reuse across identities | Behavioral, independent of text content |
| Identity consistency (Phase 3) | Same phone/email across identities, recycled resumes with swapped names | Graph analysis; critical for IT-staffing proxy/fake-experience fraud |

### 1.4 Two scores per candidate
1. **Effort Score (0–100):** genuine, tailored application ↔ mass-generated slop.
2. **Risk Score (0–100):** identity/fraud risk — duplicates, recycled content with swapped identities, hidden-text attacks.

Both come with machine-readable **reason codes** (e.g. `GEN_TOOL_MATCH:teal_v3`, `DUP_CLUSTER:cl_8841 (37 applicants)`, `HIDDEN_TEXT:white_font_412_words`).

### 1.5 Design principles
1. **Assist, never auto-reject.** Human owns the decision; we own the evidence.
2. **Explainable or it doesn't ship.** Every flag has a reason code a recruiter can read aloud to a client.
3. **Population > document.** Our edge is seeing patterns across thousands of applications, not judging one PDF.
4. **No protected attributes.** The engine never ingests or infers race, gender, age, national origin, disability. Text-style signals known to correlate with non-native English are explicitly excluded from scoring.
5. **Signature DB is the moat.** Continuous collection from wrapper sites/auto-apply tools; treat it like a threat-intel feed.

---

## 2. System Architecture

### 2.1 High-level flow
```
                            ┌─────────────────────────────────────┐
   Sources                  │           INGESTION LAYER           │
   ────────                 │                                     │
   Email inbox (IMAP/       │  • Email parser (attachments)       │
   forwarding address) ───▶ │  • ATS webhooks (Ceipal/JobDiva/    │
   ATS webhook ───────────▶ │    Bullhorn) — Phase 2              │
   Manual upload (UI) ────▶ │  • Drag-drop bulk upload (MVP)      │
   Job-board feeds ───────▶ │  • Normalizer → Application record  │
                            └───────────────┬─────────────────────┘
                                            │ Pub/Sub: application.received
                            ┌───────────────▼─────────────────────┐
                            │        DETECTION PIPELINE           │
                            │  (Cloud Run workers, Python)        │
                            │                                     │
                            │  Stage 1  Parse & extract           │
                            │           (text, layout, metadata)  │
                            │  Stage 2  Per-document analyzers    │
                            │           forensics · hidden text · │
                            │           injection · JD-mirror     │
                            │  Stage 3  Population analyzers      │
                            │           MinHash LSH dedupe ·      │
                            │           embedding clusters ·      │
                            │           identity graph            │
                            │  Stage 4  Scoring & reason codes    │
                            └───────────────┬─────────────────────┘
                                            │ Pub/Sub: application.scored
              ┌─────────────────────────────┼──────────────────────────┐
              ▼                             ▼                          ▼
   ┌────────────────────┐      ┌────────────────────┐      ┌────────────────────┐
   │  REVIEW DASHBOARD  │      │   ATS WRITEBACK    │      │   SCORING API      │
   │  (Next.js)         │      │  (tags + notes)    │      │  (portals, Phase 3)│
   │  triage queue,     │      │  Phase 2           │      │  POST /v1/score    │
   │  reason codes,     │      └────────────────────┘      └────────────────────┘
   │  bulk actions,     │
   │  audit log         │
   └────────────────────┘

   Storage: GCS (raw docs) · Postgres (app state, Cloud SQL) · BigQuery (signals,
   clusters, analytics) · Redis/Memorystore (LSH index hot cache)
```

### 2.2 Tech stack (chosen for your existing skills + cost)
| Layer | Choice | Rationale |
|---|---|---|
| Language | Python 3.12 | Your core skill; rich PDF/NLP ecosystem |
| API | FastAPI + Pydantic v2 | Async, typed, OpenAPI for free |
| Workers | Cloud Run jobs + Pub/Sub | Scale-to-zero, pay-per-use; you know GCP |
| Doc parsing | PyMuPDF (fitz) + pikepdf | Layout + low-level metadata access |
| Dataframes | Polars | Your stack; fast batch processing |
| Dedupe | datasketch (MinHash LSH) | Proven near-dupe at scale |
| Embeddings | sentence-transformers `all-MiniLM-L6-v2` locally → Vertex AI later | Free to start, swap later |
| App DB | Postgres (Cloud SQL) | Transactional state, review queue |
| Analytics | BigQuery | Cross-tenant cluster analytics; your home turf |
| Dashboard | Next.js + Tailwind on Cloud Run | Boring, fast to build |
| Auth | Firebase Auth or Clerk | Multi-tenant orgs quickly |
| Infra as code | Terraform | Reproducible from day 1 |

### 2.3 Multi-tenancy model
- `org` (agency) → `workspace` (client/account) → `req` (job) → `application`.
- Cross-tenant clustering runs on **hashed content signatures only** (MinHash signatures, embedding vectors, layout hashes) — never raw resume text across tenants. This lets us say "this exact resume body hit 14 other agencies this week" without sharing candidate PII across customers. This cross-tenant network effect is moat #2.

---

## 3. Detection Engine — Design + Code

Repo module: `engine/`. Each analyzer is a pure function: `(ParsedDoc, Context) -> list[Signal]`. Signals aggregate in the scorer. All code below is working starting-point code, not pseudocode.

### 3.0 Core types
```python
# engine/types.py
from dataclasses import dataclass, field
from enum import Enum
from typing import Any

class Severity(str, Enum):
    INFO = "info"          # context, no score impact
    WEAK = "weak"          # small score impact, never flags alone
    STRONG = "strong"      # major score impact
    DETERMINISTIC = "hard" # objective fact (hidden text, exact dupe)

@dataclass
class Signal:
    code: str                    # e.g. "GEN_TOOL_MATCH"
    severity: Severity
    score_impact: float          # 0..1 contribution weight
    evidence: dict[str, Any]     # human-readable proof for the UI
    analyzer: str

@dataclass
class ParsedDoc:
    doc_id: str
    application_id: str
    raw_bytes: bytes
    text: str
    pages: list[dict]            # per-page blocks: bbox, font, size, color, text
    meta: dict[str, Any]         # producer, creator, dates, xmp
    fonts: list[str]
    layout_hash: str = ""
    minhash_sig: list[int] = field(default_factory=list)
    embedding: list[float] = field(default_factory=list)
```

### 3.1 Stage 1 — Parse & extract
```python
# engine/parse.py
import fitz  # PyMuPDF
import pikepdf
import hashlib
from datetime import datetime, timezone
from engine.types import ParsedDoc

def parse_pdf(doc_id: str, application_id: str, data: bytes) -> ParsedDoc:
    pdf = fitz.open(stream=data, filetype="pdf")
    pages, fonts, full_text = [], set(), []
    for page in pdf:
        d = page.get_text("dict")
        blocks = []
        for b in d.get("blocks", []):
            for line in b.get("lines", []):
                for span in line["spans"]:
                    fonts.add(span["font"])
                    blocks.append({
                        "text": span["text"],
                        "font": span["font"],
                        "size": round(span["size"], 1),
                        "color": span["color"],      # int RGB
                        "bbox": [round(v, 1) for v in span["bbox"]],
                    })
                    full_text.append(span["text"])
        pages.append({"num": page.number, "blocks": blocks,
                      "width": page.rect.width, "height": page.rect.height})

    meta = dict(pdf.metadata or {})
    # Low-level metadata pikepdf sees that fitz sometimes misses
    with pikepdf.open(io.BytesIO(data)) as p:
        info = p.docinfo
        for k in info.keys():
            meta.setdefault(str(k).lstrip("/"), str(info[k]))
        meta["pdf_version"] = str(p.pdf_version)
        meta["ingested_at"] = datetime.now(timezone.utc).isoformat()

    return ParsedDoc(
        doc_id=doc_id, application_id=application_id, raw_bytes=data,
        text=" ".join(full_text), pages=pages, meta=meta,
        fonts=sorted(fonts),
    )
```

### 3.2 Analyzer A — Metadata / toolchain forensics
The workhorse. Matches producer/creator strings and structural traits against the **generator signature DB**.

```python
# engine/analyzers/forensics.py
import re
from datetime import datetime, timezone
from engine.types import ParsedDoc, Signal, Severity

# Seed signatures — grows via the collection playbook (§6).
# Stored in Postgres table `generator_signatures`; hardcoded seed below.
GENERATOR_SIGNATURES = [
    # (regex on producer|creator, tool label, confidence)
    (r"react-pdf",                    "react_pdf_builder",   0.7),
    (r"(HeadlessChrome|Skia/PDF)",    "headless_browser",    0.5),
    (r"wkhtmltopdf",                  "html_to_pdf_pipeline",0.6),
    (r"Puppeteer",                    "puppeteer_pipeline",  0.7),
    (r"WeasyPrint",                   "weasyprint_pipeline", 0.6),
    (r"pdfmake",                      "pdfmake_builder",     0.7),
    (r"Prince",                       "princexml_pipeline",  0.5),
    # Human-authored producers (NEGATIVE signals — reduce suspicion):
    (r"Microsoft.*Word",              "ms_word",            -0.4),
    (r"Acrobat",                      "acrobat",            -0.2),
    (r"(macOS|Quartz).*",             "mac_print_dialog",   -0.2),
    (r"Google( Docs)?",               "google_docs",        -0.3),
    (r"LibreOffice|OpenOffice",       "libreoffice",        -0.3),
    (r"LaTeX|pdfTeX|XeTeX",           "latex",              -0.4),
    (r"Canva",                        "canva",               0.1),  # ambiguous
]

def _parse_pdf_date(s: str) -> datetime | None:
    m = re.match(r"D:(\d{4})(\d{2})(\d{2})(\d{2})(\d{2})(\d{2})", s or "")
    if not m: return None
    try:
        return datetime(*map(int, m.groups()), tzinfo=timezone.utc)
    except ValueError:
        return None

def analyze_forensics(doc: ParsedDoc, ctx) -> list[Signal]:
    signals = []
    producer = f'{doc.meta.get("producer","")} {doc.meta.get("creator","")}'

    for pattern, label, conf in ctx.signatures or GENERATOR_SIGNATURES:
        if re.search(pattern, producer, re.I):
            sev = Severity.STRONG if conf >= 0.6 else Severity.WEAK
            signals.append(Signal(
                code="GEN_TOOL_MATCH" if conf > 0 else "HUMAN_TOOL_MATCH",
                severity=sev if conf > 0 else Severity.INFO,
                score_impact=conf,
                evidence={"matched": label, "producer_string": producer.strip()},
                analyzer="forensics"))

    # Creation timestamp vs submission timestamp: wrapper sites generate
    # the PDF seconds before submission. Humans rarely do.
    created = _parse_pdf_date(doc.meta.get("creationDate", ""))
    submitted = ctx.submitted_at
    if created and submitted:
        delta = (submitted - created).total_seconds()
        if 0 <= delta < 120:
            signals.append(Signal(
                code="FRESH_GENERATION", severity=Severity.WEAK,
                score_impact=0.3,
                evidence={"seconds_before_submit": int(delta)},
                analyzer="forensics"))

    # creationDate == modDate to the second → single-shot generation
    mod = _parse_pdf_date(doc.meta.get("modDate", ""))
    if created and mod and created == mod:
        signals.append(Signal(
            code="SINGLE_SHOT_PDF", severity=Severity.WEAK, score_impact=0.15,
            evidence={"created": created.isoformat()}, analyzer="forensics"))

    # Empty author + title matching known wrapper defaults
    title = (doc.meta.get("title") or "").lower()
    for t in ("resume", "untitled document", "cv-template", "resume-export"):
        if title == t:
            signals.append(Signal(
                code="DEFAULT_TITLE", severity=Severity.WEAK, score_impact=0.1,
                evidence={"title": title}, analyzer="forensics"))
    return signals
```

### 3.3 Analyzer B — Layout fingerprinting
Wrapper templates produce structurally identical PDFs (same fonts, sizes, column geometry) even with different text. We hash the *structure* and match across the population + against the signature DB.

```python
# engine/analyzers/layout.py
import hashlib
from engine.types import ParsedDoc, Signal, Severity

def layout_fingerprint(doc: ParsedDoc) -> str:
    """Structure-only hash: fonts, sizes, x-positions bucketed to 5pt,
    per-page block counts. Text content excluded on purpose."""
    feats = []
    for page in doc.pages:
        for b in page["blocks"]:
            feats.append((
                b["font"],
                round(b["size"]),
                int(b["bbox"][0] // 5),   # x-start bucket
            ))
    # Order-independent: sort then hash
    canon = "|".join(f"{f}:{s}:{x}" for f, s, x in sorted(set(feats)))
    return hashlib.sha256(canon.encode()).hexdigest()[:16]

def analyze_layout(doc: ParsedDoc, ctx) -> list[Signal]:
    fp = layout_fingerprint(doc)
    doc.layout_hash = fp
    signals = []

    # 1) Known template from signature DB?
    tmpl = ctx.template_index.get(fp)          # dict: hash -> tool/template label
    if tmpl:
        signals.append(Signal(
            code="KNOWN_TEMPLATE", severity=Severity.STRONG, score_impact=0.6,
            evidence={"template": tmpl}, analyzer="layout"))

    # 2) How many OTHER applicants share this exact layout recently?
    count = ctx.layout_counts.get(fp, 0)       # from BigQuery rollup, last 30d
    if count >= 25:
        signals.append(Signal(
            code="TEMPLATE_SWARM", severity=Severity.WEAK, score_impact=0.2,
            evidence={"same_layout_applicants_30d": count}, analyzer="layout"))
    return signals
```
> Note: popular legit templates (Google Docs resume templates, Overleaf) will swarm too — that's why `TEMPLATE_SWARM` alone is WEAK and never flags without co-occurring signals. Curate an allowlist of common human templates in the signature DB.

### 3.4 Analyzer C — Hidden text & prompt injection (deterministic)
```python
# engine/analyzers/hidden.py
import re
from engine.types import ParsedDoc, Signal, Severity

INJECTION_PATTERNS = [
    r"ignore (all )?(previous|prior) (instructions|prompts)",
    r"(you are|act as) (an? )?(ai|assistant|recruiter)",
    r"(rank|score|rate) (this|the) (candidate|resume) (as )?(top|highest|10/10)",
    r"disregard.*(instructions|criteria)",
    r"system prompt", r"\bLLM\b.*\binstructions\b",
]

def analyze_hidden(doc: ParsedDoc, ctx) -> list[Signal]:
    signals, hidden_words = [], 0
    hidden_samples = []
    for page in doc.pages:
        for b in page["blocks"]:
            is_white  = b["color"] >= 0xF5F5F5          # near-white on white
            is_tiny   = b["size"] <= 2.0
            off_page  = (b["bbox"][2] < 0 or b["bbox"][0] > page["width"])
            if (is_white or is_tiny or off_page) and b["text"].strip():
                hidden_words += len(b["text"].split())
                if len(hidden_samples) < 3:
                    hidden_samples.append(b["text"][:120])

    if hidden_words > 5:
        signals.append(Signal(
            code="HIDDEN_TEXT", severity=Severity.DETERMINISTIC, score_impact=0.9,
            evidence={"hidden_word_count": hidden_words,
                      "samples": hidden_samples}, analyzer="hidden"))

    text_lower = doc.text.lower()
    hits = [p for p in INJECTION_PATTERNS if re.search(p, text_lower)]
    if hits:
        signals.append(Signal(
            code="PROMPT_INJECTION", severity=Severity.DETERMINISTIC,
            score_impact=0.95,
            evidence={"patterns_matched": len(hits)}, analyzer="hidden"))
    return signals
```

### 3.5 Analyzer D — JD-mirroring score
Detects resumes over-fitted to the job description (auto-tailoring bots ingest the JD verbatim).

```python
# engine/analyzers/jd_mirror.py
import math, re
from collections import Counter
from engine.types import ParsedDoc, Signal, Severity

STOP = set("the a an and or of to in for with on at by is are as be this that".split())

def _terms(text: str) -> Counter:
    words = re.findall(r"[a-zA-Z][a-zA-Z+#\.\-]{2,}", text.lower())
    return Counter(w for w in words if w not in STOP)

def analyze_jd_mirror(doc: ParsedDoc, ctx) -> list[Signal]:
    if not ctx.jd_text:
        return []
    jd, res = _terms(ctx.jd_text), _terms(doc.text)

    # Rare-term overlap: JD terms that are uncommon in our global corpus
    # (idf from BigQuery rollup) but present in the resume.
    rare_jd = {t for t in jd if ctx.global_idf.get(t, 0) > 4.0}
    if not rare_jd:
        return []
    overlap = len(rare_jd & set(res)) / len(rare_jd)

    # Phrase-level: exact 4-gram lifts from the JD
    jd_grams  = set(zip(*[list(jd.elements())[i:] for i in range(4)]))
    doc_words = [w for w in re.findall(r"[a-z+#\.\-]+", doc.text.lower())]
    doc_grams = set(zip(*[doc_words[i:] for i in range(4)]))
    lifted = len(jd_grams & doc_grams)

    signals = []
    if overlap > 0.85:
        signals.append(Signal(
            code="JD_MIRROR_EXTREME", severity=Severity.STRONG, score_impact=0.5,
            evidence={"rare_term_overlap": round(overlap, 2),
                      "rare_terms_total": len(rare_jd)}, analyzer="jd_mirror"))
    elif overlap > 0.70:
        signals.append(Signal(
            code="JD_MIRROR_HIGH", severity=Severity.WEAK, score_impact=0.25,
            evidence={"rare_term_overlap": round(overlap, 2)}, analyzer="jd_mirror"))
    if lifted >= 3:
        signals.append(Signal(
            code="JD_PHRASE_LIFT", severity=Severity.WEAK, score_impact=0.2,
            evidence={"lifted_4grams": lifted}, analyzer="jd_mirror"))
    return signals
```

### 3.6 Analyzer E — Population near-duplicate clustering
The highest-value analyzer. Runs as a batch stage against the LSH index.

```python
# engine/analyzers/dedupe.py
from datasketch import MinHash, MinHashLSH
from engine.types import ParsedDoc, Signal, Severity

NUM_PERM = 128

def shingles(text: str, k: int = 5):
    words = text.lower().split()
    return {" ".join(words[i:i+k]) for i in range(max(len(words)-k+1, 1))}

def minhash_sig(text: str) -> MinHash:
    m = MinHash(num_perm=NUM_PERM)
    for sh in shingles(text):
        m.update(sh.encode("utf8"))
    return m

def analyze_dedupe(doc: ParsedDoc, ctx) -> list[Signal]:
    """ctx.lsh: MinHashLSH(threshold=0.8) persisted in Redis, keyed doc_id.
    ctx.identity: dict doc_id -> (email_hash, phone_hash, name_hash)."""
    m = minhash_sig(doc.text)
    doc.minhash_sig = list(m.hashvalues)
    near = ctx.lsh.query(m)          # doc_ids with Jaccard >= 0.8
    ctx.lsh.insert(doc.doc_id, m)

    if not near:
        return []
    me = ctx.identity[doc.doc_id]
    same_person  = [d for d in near if ctx.identity[d][0] == me[0]]
    other_person = [d for d in near if ctx.identity[d][0] != me[0]]

    signals = []
    if len(other_person) >= 1:
        # Same resume body under DIFFERENT identity → strongest fraud signal
        signals.append(Signal(
            code="RECYCLED_IDENTITY", severity=Severity.STRONG, score_impact=0.8,
            evidence={"matching_docs_other_identity": len(other_person)},
            analyzer="dedupe"))
    if len(same_person) >= 10:
        # Same person spraying near-identical doc across many reqs
        signals.append(Signal(
            code="SPRAY_APPLY", severity=Severity.WEAK, score_impact=0.3,
            evidence={"same_doc_applications": len(same_person)},
            analyzer="dedupe"))
    return signals
```
> Semantic layer (Phase 1.5): add sentence-transformer embeddings per section, HDBSCAN clustering nightly in BigQuery/Python to catch paraphrased mass-generation that MinHash misses. Keep MinHash as the real-time path.

### 3.7 Stage 4 — Scoring & labels
```python
# engine/scoring.py
from engine.types import Signal, Severity

LABELS = ("genuine", "needs_review", "mass_generated", "high_risk")

RISK_CODES = {"RECYCLED_IDENTITY", "HIDDEN_TEXT", "PROMPT_INJECTION"}

def score(signals: list[Signal]) -> dict:
    effort_raw = sum(s.score_impact for s in signals
                     if s.code not in RISK_CODES)
    risk_raw   = sum(s.score_impact for s in signals
                     if s.code in RISK_CODES)

    effort = max(0, min(100, round(100 - effort_raw * 55)))   # 100 = genuine
    risk   = max(0, min(100, round(risk_raw * 70)))

    has_hard   = any(s.severity == Severity.DETERMINISTIC for s in signals)
    has_strong = any(s.severity == Severity.STRONG for s in signals)

    if risk >= 60 or has_hard:
        label = "high_risk"
    elif effort <= 35 and has_strong:
        label = "mass_generated"
    elif effort <= 55:
        label = "needs_review"          # never bury on weak signals alone
    else:
        label = "genuine"

    return {
        "effort_score": effort,
        "risk_score": risk,
        "label": label,
        "reason_codes": [
            {"code": s.code, "severity": s.severity, "evidence": s.evidence}
            for s in sorted(signals, key=lambda s: -s.score_impact)
        ],
    }
```
**Calibration rule:** weights above are seeds. Real weights come from the eval set (§7) — tune so that **false-flag rate on verified-human resumes < 2%** before any pilot. `mass_generated` requires ≥1 STRONG signal by construction; WEAK signals alone can only produce `needs_review`.

---

## 4. Data Model

### 4.1 Postgres (transactional)
```sql
CREATE TABLE orgs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL,
  plan TEXT DEFAULT 'pilot',
  created_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE workspaces (            -- an agency's client/account
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id UUID REFERENCES orgs NOT NULL,
  name TEXT NOT NULL
);

CREATE TABLE reqs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID REFERENCES workspaces NOT NULL,
  title TEXT NOT NULL,
  jd_text TEXT,
  external_ref TEXT,                 -- ATS req id
  created_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE applications (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  req_id UUID REFERENCES reqs NOT NULL,
  source TEXT NOT NULL,              -- email|upload|webhook|api
  candidate_name TEXT,
  email_hash TEXT,                   -- sha256, salted per-deployment
  phone_hash TEXT,
  submitted_at TIMESTAMPTZ NOT NULL,
  status TEXT DEFAULT 'pending',     -- pending|scored|reviewed
  label TEXT,                        -- genuine|needs_review|mass_generated|high_risk
  effort_score INT, risk_score INT,
  reviewed_by UUID, reviewed_at TIMESTAMPTZ,
  review_decision TEXT               -- advanced|rejected|cleared_flag
);

CREATE TABLE documents (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  application_id UUID REFERENCES applications NOT NULL,
  kind TEXT NOT NULL,                -- resume|cover_letter|other
  gcs_uri TEXT NOT NULL,
  sha256 TEXT NOT NULL,
  layout_hash TEXT,
  meta JSONB
);

CREATE TABLE signals (
  id BIGSERIAL PRIMARY KEY,
  application_id UUID REFERENCES applications NOT NULL,
  code TEXT NOT NULL,
  severity TEXT NOT NULL,
  score_impact REAL,
  evidence JSONB,
  created_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE generator_signatures (  -- the moat
  id BIGSERIAL PRIMARY KEY,
  kind TEXT NOT NULL,                -- producer_regex|layout_hash|phrase_set
  pattern TEXT NOT NULL,
  tool_label TEXT NOT NULL,
  confidence REAL NOT NULL,          -- negative = human-tool allowlist
  source TEXT,                       -- collection run reference
  active BOOLEAN DEFAULT true,
  created_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE audit_log (             -- compliance requirement, day 1
  id BIGSERIAL PRIMARY KEY,
  org_id UUID, actor UUID,
  action TEXT NOT NULL,              -- viewed|advanced|rejected|cleared_flag|exported
  application_id UUID,
  detail JSONB,
  at TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX ON applications (req_id, label);
CREATE INDEX ON signals (application_id);
CREATE INDEX ON documents (layout_hash);
```

### 4.2 BigQuery (analytics / population signals)
```sql
-- dataset: signalhire_analytics
CREATE TABLE doc_signatures (
  doc_id STRING, org_id STRING,
  layout_hash STRING,
  minhash_sig ARRAY<INT64>,
  embedding ARRAY<FLOAT64>,
  email_hash STRING, phone_hash STRING, name_hash STRING,
  submitted_at TIMESTAMP
) PARTITION BY DATE(submitted_at);

-- Nightly rollups feeding the runtime Context:
--  layout_counts_30d:   layout_hash -> distinct applicants
--  global_idf:          term -> idf across corpus
--  identity_graph_edges: email/phone/name collisions across docs
```
**Privacy rule:** BigQuery holds signatures and hashes only — no raw resume text, no clear-text PII. Raw docs live in per-org GCS buckets with CMEK, 18-month default retention.

---

## 5. API & Integration Design

### 5.1 Public scoring API (also used by the dashboard)
```python
# api/main.py
from fastapi import FastAPI, UploadFile, BackgroundTasks, Depends
from pydantic import BaseModel

app = FastAPI(title="SignalHire API", version="0.1")

class ScoreResult(BaseModel):
    application_id: str
    label: str
    effort_score: int
    risk_score: int
    reason_codes: list[dict]

@app.post("/v1/applications", status_code=202)
async def submit_application(
    req_id: str,
    resume: UploadFile,
    background: BackgroundTasks,
    org=Depends(auth_org),
):
    """Accept a document, enqueue the pipeline, return application_id.
    Scoring is async; poll GET /v1/applications/{id} or receive webhook."""
    app_id = await store_and_enqueue(org.id, req_id, resume)
    return {"application_id": app_id, "status": "pending"}

@app.get("/v1/applications/{app_id}", response_model=ScoreResult)
async def get_score(app_id: str, org=Depends(auth_org)):
    return await load_result(org.id, app_id)

@app.post("/v1/webhooks/outbound")   # org registers their callback URL
async def register_webhook(url: str, org=Depends(auth_org)):
    ...
```

### 5.2 Email ingestion (MVP's killer feature for agencies)
Each workspace gets a forwarding address: `acme-java-dev@in.signalhire.app`.
Recruiter sets an inbox rule; every inbound resume auto-scores with zero workflow change.
- Implementation: Cloud Run service receiving from an inbound-parse provider (SendGrid Inbound Parse or Postmark), extracts attachments, maps address → req.

### 5.3 ATS integrations (Phase 2 order)
1. **Ceipal** — IT-staffing native, open API, marketplace program. Write back a custom field + note.
2. **JobDiva** — huge in IT staffing; API partner program.
3. **Bullhorn** — largest general staffing; SOAP/REST, marketplace requires partnership review (start application early — it's slow).

Writeback contract per candidate: `SH_LABEL`, `SH_EFFORT`, `SH_RISK`, note with top-3 reason codes + deep link to dashboard evidence page.

---

## 6. Signature DB Collection Playbook (the moat)

**Cadence:** weekly, 2–3 hrs, semi-automated. Target: 30 tools covered by end of month 2.

1. Maintain `tools.yaml`: wrapper sites + auto-apply tools (Teal, Rezi, Kickresume, Enhancv, Zety, Novoresume, Careerflow, LazyApply-class bots, etc.), each with account creds and last-collected date.
2. For each tool, generate 5–10 resumes from varied fake personas (persona generator script, obviously-fake data: "Test Persona QA-{n}").
3. Run `collector/ingest.py` → extracts producer strings, layout hashes, boilerplate phrase sets → writes candidate signatures to `generator_signatures` with `active=false`.
4. **Validation gate:** a signature activates only after it (a) matches all samples from its tool, and (b) matches **zero** docs in the verified-human corpus (§7). Prevents allowlist poisoning.
5. Version everything; signatures are timestamped so score explanations remain reproducible for audits.

Also collect the **human allowlist**: Word/Google Docs/LaTeX/Canva outputs, top Google Docs & Overleaf templates → negative signatures + template allowlist.

---

## 7. Evaluation Framework (build BEFORE the pilot)

### 7.1 Test corpus
| Set | Size target | Source |
|---|---|---|
| `human_verified` | 300+ | Resumes from people you know personally + public real resumes with consent; include non-native English writers deliberately |
| `wrapper_generated` | 500+ | Output of §6 collection runs |
| `hybrid` | 100+ | Human resumes lightly run through wrapper "improve" features |
| `attack` | 50+ | Hand-built hidden-text, injection, identity-swap cases |

### 7.2 Gates (must pass before any design partner sees a score)
- False-flag rate (`mass_generated` or `high_risk` on `human_verified`) **< 2%**
- Recall on `wrapper_generated` **> 70%** (will climb as signature DB grows)
- Recall on `attack` set **= 100%** (they're deterministic)
- **Fairness slice:** false-flag rate on non-native-writer subset must be statistically indistinguishable from native subset. If it isn't, remove the offending signal — don't reweight it.

### 7.3 Harness
```python
# eval/run.py — nightly CI job
# loads corpus manifests, runs full pipeline, emits metrics.json + regression
# diff vs last run; fails CI if any gate regresses.
```

---

## 8. Dashboard (MVP scope only)

Pages:
1. **Triage queue** — per-req list, label chips, effort/risk bars, sort/filter, bulk select → advance/archive. Keyboard-first (j/k/a/x).
2. **Evidence page** — per application: PDF preview (left), reason codes with evidence (right), cluster view ("this resume body ↔ 14 other applicants"), one-click `clear_flag` (feeds calibration).
3. **Req settings** — JD paste box (enables JD-mirror), forwarding address, threshold slider (conservative ↔ aggressive).
4. **Audit log** — filterable, exportable CSV (compliance selling point).

Non-goals for MVP: analytics dashboards, user management beyond invite, mobile.

---

## 9. Compliance & Ethics Guardrails (day-1 requirements)

1. **Assistive only.** No auto-reject anywhere in the product. Contractually prohibit customers from using labels as sole rejection criteria (ToS clause).
2. **No protected attributes**; no signals derived from writing fluency/style that proxy for national origin. The fairness gate in §7.2 is enforced in CI.
3. **Audit log** of every human decision — this is what makes us *defensible* under NYC LL144 / EU AI Act rather than threatened by them: we strengthen the human-oversight story.
4. **Candidate data:** hash PII for cross-tenant signals, CMEK on raw docs, 18-month retention default, DPA template ready before first pilot, delete-on-request endpoint.
5. **Positioning language:** we detect *mass-generation and fraud patterns*, never "AI use." Marketing must never claim to detect whether a candidate used AI. (True + defensible + avoids the false-positive trap.)
6. Track AEDT posture per jurisdiction: as long as output "substantially assists" decisions it can qualify as an AEDT for NYC roles — offer customers a bias-audit support pack (our fairness eval results + methodology doc) as an enterprise feature.

---

## 10. Repo Structure

```
signalhire/
├── engine/                  # pure detection library (no I/O) — unit-testable
│   ├── types.py
│   ├── parse.py
│   ├── scoring.py
│   └── analyzers/
│       ├── forensics.py
│       ├── layout.py
│       ├── hidden.py
│       ├── jd_mirror.py
│       └── dedupe.py
├── api/                     # FastAPI service
│   ├── main.py
│   ├── auth.py
│   └── routers/
├── workers/                 # Pub/Sub consumers, batch jobs
│   ├── pipeline.py          # stage orchestration
│   └── nightly_rollups.py   # BigQuery rollups, embedding clusters
├── collector/               # signature DB collection tooling (§6)
│   ├── tools.yaml
│   ├── personas.py
│   └── ingest.py
├── eval/                    # §7 harness + corpus manifests
├── dashboard/               # Next.js app
├── infra/                   # Terraform: Cloud Run, SQL, Pub/Sub, BQ, GCS
├── cli.py                   # Phase-0 demo: folder of PDFs → triage report
└── pyproject.toml
```

---

## 11. Roadmap & Milestones

### Phase 0 — Prototype + validation (Weeks 1–4)
- [ ] W1: Repo scaffold, `engine/` with forensics + hidden + layout analyzers, `cli.py` (folder in → HTML triage report out)
- [ ] W1–2: First collection run — 10 wrapper tools, seed signature DB
- [ ] W2: MinHash dedupe (local LSH), scoring v0, eval harness with starter corpus
- [ ] W2–4: **10 agency discovery calls** (your network first). Script: current volume per req, hours on triage, what a bad submittal costs them, would they forward a real req's resumes for a live demo
- [ ] W4: Live demo on ≥2 agencies' real pipelines via CLI report
- **Gate:** ≥3 agencies say "leave it running" → proceed. Otherwise revisit wedge.

### Phase 1 — Pilot product (Weeks 5–10)
- [ ] Email ingestion + async pipeline on Cloud Run/Pub/Sub
- [ ] Postgres schema, auth, minimal dashboard (queue + evidence pages)
- [ ] Eval gates green incl. fairness slice; calibration from `clear_flag` feedback
- [ ] 3–5 design partners live, free 30-day pilots
- **Success metric:** recruiter-reported ≥40% reduction in review time; <2% cleared-flag rate on `mass_generated`

### Phase 2 — First revenue + ATS (Weeks 11–20)
- [ ] Pricing: $79–99/recruiter-seat/mo or $149/active-req/mo (test both in pilots)
- [ ] Ceipal integration + writeback; start Bullhorn marketplace application
- [ ] Cross-tenant signature network (hashed) once ≥5 orgs
- [ ] Convert ≥3 pilots to paid
- **Gate:** $2–3k MRR + retention through month 2 → this is a business.

### Phase 3 — Portal/API expansion (Month 6+)
- [ ] `/v1/score` as standalone metered API for job boards (per-application pricing)
- [ ] Behavioral SDK (form-fill telemetry) for portals' native apply flows
- [ ] Interview-stage: partner (not build) for deepfake/liveness detection
- [ ] Enterprise: bias-audit support pack, SSO, SOC 2 roadmap

---

## 12. Risks & Mitigations
| Risk | Mitigation |
|---|---|
| Wrapper sites randomize metadata once detection spreads | Layered signals — population clustering & JD-mirror survive metadata hygiene; treat it as an arms race you're structurally set up to win (weekly collection cadence) |
| Legit template swarms cause false flags | Allowlist + swarm signals are WEAK-only; can never flag alone |
| Incumbents (Tofu/Endorsed) move down-market | They anchor on identity fraud/security for enterprise; stay focused on triage-for-agencies speed + Ceipal/JobDiva ecosystem they ignore |
| Bullhorn marketplace approval is slow | Start with email ingestion (no integration needed) + Ceipal; apply to Bullhorn early in parallel |
| A pilot uses labels to auto-reject | ToS prohibition + no bulk-reject on flagged-only filters in UI + audit log |
| Solo-founder bandwidth | Phase 0 is deliberately CLI-only; nothing in weeks 1–4 requires frontend or infra |

---

## 13. Immediate Next Actions (this week)
1. Scaffold repo, implement `parse.py` + `forensics.py` + `hidden.py`, run against any 20 PDFs you can gather.
2. Create accounts on 5 wrapper sites, run first collection pass, inspect producer strings by hand.
3. Draft the discovery-call script + list 15 agency contacts from your network.
4. Register the domain / pick the real name.
