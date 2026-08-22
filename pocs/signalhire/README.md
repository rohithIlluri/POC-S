# signalhire

**Application authenticity engine.** Point it at a folder of applications and it
labels each one **genuine effort / needs review / mass-generated / high risk**,
with machine-readable reason codes and the evidence behind every flag.

This is the Phase-0 slice of [the build plan](docs/BUILD_PLAN.md): the detection
engine, a CLI that turns an inbox folder into a triage report, the signature
collector, and the evaluation harness with its release gates. No service, no
database, no dashboard — those are Phase 1, and none of them can be designed
honestly before the engine's numbers are real.

## What it is not

Not an AI-text detector. Nothing in this package scores writing style, fluency,
perplexity or "AI-ness". Single-document AI-text classification has
unacceptable false-positive rates on human writing — worst on non-native
English writers — and is defeated by light editing. Using it as a rejection
basis is indefensible, so the engine does not have the capability at all.

Not an auto-reject tool. Every output is assistive: a score, a label and the
evidence, routed to a human. Nothing here decides anything.

## What it detects instead

| Analyzer | Reason codes | What it catches |
|---|---|---|
| `forensics` | `GEN_TOOL_MATCH`, `HUMAN_TOOL_MATCH`, `NO_PRODUCER`, `FRESH_GENERATION`, `SINGLE_SHOT_PDF`, `DEFAULT_TITLE` | Wrapper/auto-apply toolchains in PDF metadata; PDFs generated seconds before submission |
| `layout` | `KNOWN_TEMPLATE`, `TEMPLATE_SWARM`, `ALLOWLISTED_TEMPLATE` | Structurally identical documents — same fonts, sizes, column geometry — regardless of text |
| `hidden` | `HIDDEN_TEXT`, `PROMPT_INJECTION` | White-on-white keyword stuffing, sub-3pt text, off-page text, instructions aimed at an LLM reader |
| `jd_mirror` | `JD_MIRROR_EXTREME`, `JD_MIRROR_HIGH`, `JD_PHRASE_LIFT` | Resumes over-fitted to the job description: rare-term overlap and verbatim phrase lifts |
| `dedupe` | `RECYCLED_IDENTITY`, `SPRAY_APPLY`, `DUP_CLUSTER` | The same resume body across the population — including under a swapped identity |
| `contact` | `CONTACT_COLLISION`, `DISPOSABLE_CONTACT` | One mailbox or phone behind several candidate names; throwaway-mail providers |
| `boilerplate` | `SHARED_BOILERPLATE` | Paraphrase farms: 8-word runs shared verbatim by many distinct applicants, below the near-duplicate threshold |
| `recurrence` | `RECURRING_IDENTITY`, `RECURRING_BODY`, `RECURRING_CONTACT`, `RECURRING_TEMPLATE`, `RECURRING_PHRASES` | The same rig across *earlier scans* — the farm that submits two applications per requisition instead of fifty |

Each analyzer is a pure function `(ParsedDoc, Context) -> list[Signal]`, so
every one of them is unit-testable without touching a filesystem.

### The population outlives the batch

Every population analyzer above `recurrence` needs a crowd inside one scan,
which is a weakness a farm can simply choose to exploit: deliver thin and the
duplicate cluster, the phrase swarm and the template swarm all have nothing to
compare against. Measured on the trickle set of the synthetic corpus — the same
manufactured documents, two per scan — batch-only detection catches **0 of 30**.

`signalhire/memory.py` makes the population cumulative. Every scanned document
leaves behind one-way keys (MinHash bands of the identity-masked body, the
layout fingerprint, hashed contact handles, a hashed phrase sample) and the
next scan asks what it has seen before. Once the memory holds a population,
recall on that same trickle is **100%**, with no cost in false flags.

The engine defines the port, never the storage: `PopulationMemory` is a
two-method protocol, `InMemoryPopulationMemory` is the reference
implementation, and the webapp binds a SQLite-backed one scoped to a billing
account. Three rules keep an account from flagging itself over time — a scan is
never part of the population it is judged against, a re-uploaded batch dedupes
to the records it already wrote, and one candidate applying to five
requisitions is one applicant, not five.

Nothing reconstructible is stored: no text, no names, no addresses. That is
what makes the cross-tenant version (§2.3 of the build plan) possible later —
"this body hit fourteen other agencies this week" without a syllable of anyone's
resume moving between tenants.

## Install

```bash
cd pocs/signalhire
python -m venv .venv && source .venv/bin/activate
pip install -e ".[dev]"
```

Python 3.11+. Dependencies: PyMuPDF, pikepdf, datasketch.

## Use

```bash
# Triage a folder of applications; open report.html in a browser.
signalhire scan ./inbox --jd req-4471.txt --html report.html

# Machine-readable output for a pipeline.
signalhire scan ./inbox --json results.json --quiet

# Widen or narrow the queue.
signalhire scan ./inbox --sensitivity conservative
```

Accepts `.pdf`, `.docx`, `.odt`, `.rtf`, `.html`/`.htm`, `.txt` and `.md`.
DOCX keeps the full forensic layer — authoring application, timestamps, and
run-level concealment (white text, sub-3pt fonts, `w:vanish`); HTML surfaces
`display:none` / white-ink stuffing; ODT and RTF contribute their generator
strings. Legacy binary `.doc` is accepted but reported as a parse failure —
export it as `.docx` or PDF. Passing `--jd` enables the JD-mirroring
analyzer; everything else works without it.

The population analyzers score the batch against itself, so results depend on
what you scan together. Scan a whole requisition's inbox at once — that is
where cross-applicant duplication becomes visible.

### Web MVP

The same engine behind a recruiter-facing site: drag-drop a batch of
applications, paste the JD, get the triage dashboard with every reason code
and its evidence.

```bash
pip install -e ".[web]"
signalhire web            # serves http://127.0.0.1:8710
```

Uploads are parsed and scored in memory as one population batch and never
written to disk; the upload time is used as the real submission timestamp,
which makes the fresh-generation signal meaningful. The UI lives in
`webapp/static/index.html` (no build step) and the API is
`POST /api/scan` (multipart `files` + `jd` + `sensitivity`).

Deploying: `docker build -t signalhire .` then
`docker run -p 8710:8710 -v signalhire-data:/data -e SIGNALHIRE_HASH_SALT=…
signalhire`. Set `SIGNALHIRE_WEBHOOK_SECRET` and `STRIPE_LINK_AGENCY` /
`STRIPE_LINK_TALENT_CLOUD` to arm real billing; until then upgrades run in
labeled dev mode.

A note on interview copilots (Cluely, Parakeet and similar): this engine reads
the *application-side* artifacts those workflows produce — generator
toolchains, JD mirroring, fresh-generation timestamps, template swarms,
recycled identities. Detecting an overlay assistant live on a candidate's
machine during a call is an interview-side surface and explicitly out of
scope for Phase 0.

### Growing the signature DB

```bash
# 5–10 resumes generated from one wrapper tool, in one folder:
signalhire collect samples/teal --tool teal_v3 \
    --human-corpus eval/corpus/human_verified --out signatures.json

signalhire scan ./inbox --signatures signatures.json
```

A proposal only activates when it matches **every** sample from its tool and
**zero** documents in the verified-human corpus. Anything else is written with
`active: false` for a human to review, and the engine never loads it. Re-running
a collection never flips an activation decision a human already made.

### Evaluation

```bash
signalhire corpus eval/corpus     # build the synthetic corpus
python eval/run.py                # run the gates; non-zero exit on failure
python eval/run.py --update-baseline
```

Gates, all of which must pass:

| Gate | Threshold |
|---|---|
| False-flag rate on `human_verified` | < 2% |
| Recall on `wrapper_generated` | > 70% |
| Recall on `wrapper_evasion` (track-covering) | ≥ 90% |
| Recall on `attack` | = 100% |
| Fairness slice (native vs non-native writers) | \|z\| < 1.96 |
| Recall on the trickle set once the memory is warm | ≥ 90% |
| False-flag rate on genuine applicants inside the trickle | < 2% |

The trickle gates are run separately from the rest: those documents are
delivered batch by batch through a population memory rather than scanned as one
population, and the harness runs the same sequence twice — once with a memory
and once without — so the recall number is always reported against its own
control.

`eval/run.py` also fails on a >2pt regression against `eval/baseline.json`, so
a change that buys recall with human false-flags cannot land quietly.

**The synthetic corpus proves the pipeline, not the accuracy.** It is generated
by `signalhire/corpus.py` and is the only thing that can live in a public repo.
Real numbers require a collected corpus in `eval/corpus.local/` (git-ignored):
consented human resumes including non-native English writers, output from real
wrapper-tool collection runs, and hand-built attack cases. `eval/run.py` picks
that directory up automatically when it exists.

## Design rules the code enforces

1. **Assist, never auto-reject.** No label means "reject". The HTML report says
   so on its face.
2. **Explainable or it doesn't ship.** Every signal carries `evidence` a
   recruiter can read aloud. A test asserts no flag is ever raised without one.
3. **Population beats document.** The edge is seeing patterns across thousands
   of applications, not judging one PDF.
4. **No protected attributes, and no proxies for them.** Nothing reads or
   infers race, gender, age, national origin or disability, and no signal is
   derived from writing style. The fairness gate is the enforcement mechanism:
   if a signal splits the slices, remove it — do not reweight it.
5. **Weak signals can never flag alone.** `mass_generated` requires at least one
   STRONG signal by construction; weak signals can only reach `needs_review`.
6. **PII is hashed at the parse boundary.** Identity becomes salted hashes in
   `parse.py`; nothing downstream sees a clear-text email or phone. Set
   `SIGNALHIRE_HASH_SALT` per deployment.

## Layout

```
signalhire/
├── types.py         Signal, ParsedDoc, Context, Identity, ScoredApplication
├── parse.py         PDF/text extraction; identity extraction + hashing
├── signatures.py    generator signature DB (seeds + collector merge)
├── analyzers/       forensics · layout · hidden · jd_mirror · dedupe
│                    contact · boilerplate · recurrence
├── memory.py        cross-scan population memory (port + reference impl)
├── evidence.py      log-odds combination with per-family correlation discount
├── scoring.py       effort/risk scores, labels, sensitivity thresholds
├── pipeline.py      stage orchestration + population context
├── report.py        HTML triage report, JSON, terminal summary
├── collector.py     signature proposals + validation gate
├── corpus.py        synthetic evaluation corpus generator
├── evaluate.py      metrics and release gates
└── cli.py           scan · collect · corpus · eval
eval/run.py          CI entry point with regression tracking
tests/               128 tests, no network, ~6s
```

## Findings from building it

Three things the plan's seed design got wrong, corrected here:

- **`RECYCLED_IDENTITY` alone scored `genuine`.** At the plan's weights the
  strongest fraud signal produces risk 56, under the 60 threshold, while the
  effort score stays at 100 because risk codes are excluded from it. The label
  rule now routes any STRONG risk-code signal to `high_risk` directly.
- **Near-white detection compared packed RGB integers.** `color >= 0xF5F5F5`
  treats pure red (`0xFF0000`) as near-white. Channels are now decomposed.
- **LSH at threshold 0.8 misses identity swaps.** A recycled resume with a
  swapped name and contact block lands around Jaccard 0.7–0.85 — the banding
  misses it roughly a third of the time, and short resumes fall below the
  threshold outright. The index is now queried wide (0.6) and verified exactly,
  and comparison runs on identity-masked body text.

### Trickle scale was a real hole

Every population analyzer is a batch analyzer, and a farm gets to choose the
batch size. Delivered two per scan, the corpus's own wrapper-evasion documents
scored `genuine` — not marginally, but with nothing to say about them at all.
The fix was not a better per-document detector; it was remembering. What that
buys is bounded and worth stating plainly: the engine still cannot call a farm
on its second sighting, and neither could a recruiter. What it can do is stop
being fooled by the twentieth, and put the earlier ones in the scan history
where they can be found once the pattern has a name.

## Not built yet (Phase 1+)

Email ingestion, async pipeline, Postgres/BigQuery, the review dashboard, ATS
writeback, semantic (embedding) clustering, behavioural signals. See
[docs/BUILD_PLAN.md](docs/BUILD_PLAN.md).
