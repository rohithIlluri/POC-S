"""Cross-scan population memory and analyzer H (recurrence).

The tests that matter most here are the negative ones. A memory that
accumulates evidence about applicants is a machine for manufacturing false
positives unless three things hold, so each is pinned:

  * re-scanning a batch must not build a population out of one batch;
  * one candidate applying to five requisitions is one applicant, not five;
  * a scan is never part of the population it is judged against.
"""

from __future__ import annotations

from datetime import datetime, timedelta, timezone

from signalhire.analyzers.recurrence import analyze_recurrence
from signalhire.memory import (InMemoryPopulationMemory, band_keys, jaccard,
                               phrase_sketch, probe, record_for)
from signalhire.pipeline import build_context, score_documents
from signalhire.types import Context, Identity

from conftest import make_block, make_doc

BODY = (
    "Senior platform engineer with nine years owning the kubernetes footprint "
    "and the terraform modules behind it, designing grpc service contracts and "
    "running an istio service mesh for a regional logistics operator. Tuned "
    "prometheus and thanos for long horizon retention across multi tenant "
    "control plane workloads and took the argo rollouts canary strategy from "
    "manual to automated with opentelemetry collectors and vault rotation. "
)


def farm_doc(i: int, *, tail_words: int = 25, identity=None):
    """One farm document: a shared body plus a little unique padding."""
    tail = " ".join(f"unique{i}word{j}" for j in range(tail_words))
    return make_doc(BODY + tail, doc_id=f"farm{i}",
                    identity=identity or Identity(email_hash=f"e{i}",
                                                  name_hash=f"n{i}"))


HUMAN_STYLES = [
    {"font": "helv", "size": 10.0, "x": 54.0},
    {"font": "tiro", "size": 11.0, "x": 66.0},
    {"font": "cour", "size": 10.5, "x": 48.0},
    {"font": "hebo", "size": 12.0, "x": 72.0},
    {"font": "tibo", "size": 9.5, "x": 60.0},
]


def human_doc(i: int, text: str | None = None):
    """A genuine applicant: own words, own layout.

    Real resumes from different people do not share an exact structural
    fingerprint, and `make_doc`'s default block would give every document in
    a test the same one — which is a property of the fixture, not of anybody's
    document.
    """
    style = HUMAN_STYLES[i % len(HUMAN_STYLES)]
    body = text or (
        f"Warehouse operations lead in the {i} district, responsible for bay "
        f"scheduling across {i + 3} docks and a team of {i + 8}. Rebuilt the "
        f"depot routing sheet after the {2010 + i} merger and cut turnaround "
        f"by {i + 4} minutes. Volunteer dispatcher at the {i} street shelter."
    )
    return make_doc(blocks=[make_block(body, **style)], doc_id=f"human{i}",
                    identity=Identity(email_hash=f"h{i}", name_hash=f"hn{i}"))


def scan_once(docs, memory, scan_id):
    """One scan through the full pipeline, as an account would run it."""
    return score_documents(docs, memory=memory, scan_id=scan_id)


# --- key derivation --------------------------------------------------------

def test_band_keys_are_per_band_and_stable():
    sig = list(range(128))
    keys = band_keys(sig)
    assert len(keys) == 16
    assert len(set(keys)) == 16                  # no self-collisions
    assert keys == band_keys(list(sig))          # deterministic
    assert keys[0].startswith("b0:") and keys[1].startswith("b1:")


def test_band_keys_collide_only_for_matching_bands():
    """A band key encodes its position: band 0 of one document can never
    match band 3 of another, which would be a match made by the encoding."""
    a = band_keys(list(range(128)))
    b = band_keys([v + 1 for v in range(128)])
    assert not set(a) & set(b)


def test_phrase_sketch_is_a_shared_sample():
    shared = [f"phrase number {i} of the shared run" for i in range(200)]
    a = phrase_sketch(shared + ["only in a"], k=32)
    b = phrase_sketch(shared + ["only in b"], k=32)
    assert len(a) == 32
    # Bottom-k over a hashed universe: two documents built from the same
    # phrases sample the *same* phrases, which is what makes the sketches
    # comparable across documents at all.
    assert len(set(a) & set(b)) >= 30


def test_jaccard_of_identical_signatures_is_one():
    assert jaccard([1, 2, 3], [1, 2, 3]) == 1.0
    assert jaccard([1, 2, 3], [1, 2, 4]) == 2 / 3
    assert jaccard([], [1]) == 0.0


# --- the memory itself -----------------------------------------------------

def test_memory_dedupes_the_same_document_from_the_same_owner():
    memory = InMemoryPopulationMemory()
    doc = farm_doc(0)
    record = record_for(doc, None, scan_id="s1")
    memory.remember([record])
    memory.remember([record_for(doc, None, scan_id="s2")])
    assert memory.size() == 1


def test_probe_excludes_the_current_scan():
    memory = InMemoryPopulationMemory()
    docs = [farm_doc(i) for i in range(3)]
    memory.remember([record_for(d, None, scan_id="s1") for d in docs])
    hits = probe(memory, record_for(docs[0], None, scan_id="s1"))
    assert hits.empty


# --- recurrence: the farm --------------------------------------------------

def test_a_trickling_farm_becomes_visible_across_scans():
    """Two documents per scan: invisible in any one batch, a population once
    the account has seen enough of them."""
    memory = InMemoryPopulationMemory()
    first = [farm_doc(i) for i in (0, 1)]
    result = scan_once(first, memory, "s0")
    assert all(not any(s.analyzer == "recurrence" for s in a.signals)
               for a in result.applications)

    for batch in range(1, 9):
        docs = [farm_doc(2 * batch), farm_doc(2 * batch + 1)]
        result = scan_once(docs, memory, f"s{batch}")

    codes = {s.code for a in result.applications for s in a.signals}
    assert "RECURRING_PHRASES" in codes
    assert memory.size() == 18


def test_recurring_phrases_escalates_when_the_sharing_is_industrial():
    """The in-batch rule, applied to the accumulated population: a third of a
    document shared with fifteen strangers is industrial, not a coincidence."""
    memory = InMemoryPopulationMemory()
    for batch in range(11):
        scan_once([farm_doc(2 * batch), farm_doc(2 * batch + 1)],
                  memory, f"s{batch}")
    result = scan_once([farm_doc(99)], memory, "final")
    hit = next(s for s in result.applications[0].signals
               if s.code == "RECURRING_PHRASES")
    assert hit.evidence["median_earlier_applicants_per_phrase"] >= 15
    assert hit.severity.value == "strong"
    assert result.applications[0].label in ("mass_generated", "high_risk")


def test_recycled_body_under_a_new_name_is_a_cross_scan_risk_signal():
    memory = InMemoryPopulationMemory()
    original = make_doc(BODY + " tail words for padding here",
                        doc_id="orig",
                        identity=Identity(email_hash="a", name_hash="alice"))
    scan_once([original], memory, "s0")

    twin = make_doc(BODY + " tail words for padding here", doc_id="twin",
                    identity=Identity(email_hash="b", name_hash="bob"))
    result = scan_once([twin], memory, "s1")
    hit = next(s for s in result.applications[0].signals
               if s.code == "RECURRING_IDENTITY")
    assert hit.severity.value == "strong"
    assert hit.evidence["max_similarity"] >= 0.8
    assert result.applications[0].label == "high_risk"


def test_contact_handle_reused_under_a_new_name_across_scans():
    memory = InMemoryPopulationMemory()
    first = make_doc("wholly distinct prose about warehouse logistics work",
                     doc_id="c1",
                     identity=Identity(email_hash="shared", name_hash="alice"))
    scan_once([first], memory, "s0")
    second = make_doc("entirely different prose about municipal water systems",
                      doc_id="c2",
                      identity=Identity(email_hash="shared", name_hash="bob"))
    result = scan_once([second], memory, "s1")
    assert "RECURRING_CONTACT" in {s.code for s in result.applications[0].signals}
    assert result.applications[0].label == "high_risk"


# --- recurrence: the false positives it must not produce -------------------

def test_rescanning_a_batch_does_not_manufacture_a_population():
    """The commonest honest re-run there is: yesterday's folder, uploaded
    again with nothing changed. It must leave the population exactly where it
    was, or the memory becomes a machine for flagging its own users — five
    re-runs of one batch would otherwise look like five weeks of a farm."""
    memory = InMemoryPopulationMemory()
    scan_once([human_doc(i) for i in range(4)], memory, "s0")
    size_after_first = memory.size()

    for run in range(5):
        result = scan_once([human_doc(i) for i in range(4)], memory, f"re{run}")
    assert memory.size() == size_after_first
    assert not any(s.analyzer == "recurrence"
                   for a in result.applications for s in a.signals)
    assert all(a.label == "genuine" for a in result.applications)


def test_one_candidate_applying_to_many_requisitions_is_one_applicant():
    """The same person, the same resume, five different reqs over five scans.
    Nothing about that is suspicious and nothing may fire."""
    memory = InMemoryPopulationMemory()
    me = Identity(email_hash="mine", name_hash="me", display_name="Wen Park")
    for req in range(5):
        doc = make_doc(BODY + " a tail of my own words here for padding",
                       doc_id=f"app{req}", identity=me)
        result = scan_once([doc], memory, f"req{req}")
    app = result.applications[0]
    assert not any(s.analyzer == "recurrence" for s in app.signals)
    assert app.label == "genuine"


def test_unidentified_reuploads_share_one_anonymous_owner():
    """A resume with no readable identity, uploaded again in another format,
    is one anonymous applicant — not two strangers with the same body."""
    memory = InMemoryPopulationMemory()
    text = BODY + " closing paragraph with a few more words to pad it out"
    scan_once([make_doc(text, doc_id="as_pdf")], memory, "s0")
    result = scan_once([make_doc(text, doc_id="as_docx")], memory, "s1")
    assert not any(s.analyzer == "recurrence"
                   for s in result.applications[0].signals)


def test_genuine_applicants_alongside_a_farm_stay_clean():
    """Ten scans of a farm trickling past, with a real applicant in every
    batch. The farm accumulates; the applicants must not."""
    memory = InMemoryPopulationMemory()
    for batch in range(10):
        result = scan_once([farm_doc(batch), human_doc(batch)],
                           memory, f"s{batch}")
    human_app = next(a for a in result.applications
                     if a.doc.doc_id.startswith("human"))
    farm_app = next(a for a in result.applications
                    if a.doc.doc_id.startswith("farm"))
    assert human_app.label == "genuine"
    assert not any(s.analyzer == "recurrence" for s in human_app.signals)
    assert any(s.analyzer == "recurrence" for s in farm_app.signals)


def test_a_shared_stock_sentence_cannot_flag_anybody_on_its_own():
    """Applicants who all copied one stock line from the same careers-advice
    page do recur against each other — and that is exactly the kind of weak,
    ambiguous evidence that must never reach a flag by itself."""
    stock = ("Proven track record of delivering results in fast paced "
             "environments while collaborating across cross functional teams ")
    memory = InMemoryPopulationMemory()
    for batch in range(12):
        result = scan_once(
            [human_doc(batch, stock + f"Depot lead in district {batch}, "
                       f"{batch + 3} docks, team of {batch + 8}, rebuilt the "
                       f"routing sheet after the {2010 + batch} merger.")],
            memory, f"s{batch}")
    app = result.applications[0]
    assert all(s.severity.value == "weak" for s in app.signals
               if s.analyzer == "recurrence")
    assert app.label != "mass_generated"


# --- wiring ----------------------------------------------------------------

def test_the_engine_without_a_memory_is_unchanged():
    docs = [farm_doc(i) for i in range(4)]
    without = score_documents(docs)
    assert without.stats["memory"]["enabled"] is False
    assert not any(s.analyzer == "recurrence"
                   for a in without.applications for s in a.signals)


def test_scan_stats_report_the_population_the_batch_was_judged_against():
    memory = InMemoryPopulationMemory()
    scan_once([farm_doc(i) for i in range(4)], memory, "s0")
    stats = scan_once([farm_doc(i) for i in range(4, 8)], memory, "s1").stats
    assert stats["memory"]["enabled"] is True
    assert stats["memory"]["documents_remembered"] == 4
    assert stats["memory"]["documents_added"] == 4


def test_a_batch_is_never_part_of_its_own_population():
    """Documents are committed after scoring, so an eight-document batch is
    judged against what came before it and nothing else."""
    memory = InMemoryPopulationMemory()
    result = scan_once([farm_doc(i) for i in range(8)], memory, "s0")
    assert result.stats["memory"]["documents_remembered"] == 0
    assert not any(s.analyzer == "recurrence"
                   for a in result.applications for s in a.signals)
    assert memory.size() == 8


def test_analyzer_probes_on_demand_when_called_directly():
    """Analyzers stay pure functions of (doc, ctx): given only a bound memory
    they do their own lookup rather than requiring the pipeline's cache."""
    memory = InMemoryPopulationMemory()
    docs = [farm_doc(i) for i in range(12)]
    memory.remember([record_for(d, None, scan_id="earlier") for d in docs])
    ctx = Context(memory=memory, scan_id="now")
    assert analyze_recurrence(farm_doc(99), ctx)


def test_records_carry_no_document_text():
    doc = make_doc("Ana Lee ana@example.com " + BODY,
                   identity=Identity(email_hash="e", name_hash="n",
                                     display_name="Ana Lee"))
    record = record_for(doc, None, scan_id="s")
    blob = repr(record.as_dict())
    assert "kubernetes" not in blob
    assert "Ana" not in blob
    assert "example.com" not in blob
