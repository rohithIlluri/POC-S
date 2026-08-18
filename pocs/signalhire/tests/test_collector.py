from __future__ import annotations

import json

from signalhire.cli import main
from signalhire.collector import merge_into, propose, producer_stem
from signalhire.pipeline import scan
from signalhire.signatures import load_signatures


def test_producer_stem_strips_versions():
    assert producer_stem("react-pdf 3.1.14") == "react-pdf"
    assert producer_stem("wkhtmltopdf0.12.6") == "wkhtmltopdf"
    assert producer_stem("") == ""


def test_proposal_activates_only_without_human_collisions(tiny_corpus):
    proposals = propose(tiny_corpus / "wrapper_generated", "acme_builder",
                        human_corpus=tiny_corpus / "human_verified")
    assert proposals
    producers = [p for p in proposals if p.kind == "producer_regex"]
    # The synthetic wrapper set rotates four producers, so no single regex
    # covers every sample and none may activate.
    assert all(not p.active for p in producers)
    assert all("human-corpus collisions" in p.rationale for p in producers)


def test_single_tool_samples_activate(tmp_path, tiny_corpus):
    single = tmp_path / "one_tool"
    single.mkdir()
    for i, src in enumerate(sorted((tiny_corpus / "wrapper_generated").glob("*.pdf"))):
        # Keep only the samples sharing one producer (they rotate by index).
        if i % 4 == 0:
            (single / src.name).write_bytes(src.read_bytes())

    proposals = propose(single, "acme_builder",
                        human_corpus=tiny_corpus / "human_verified")
    active = [p for p in proposals if p.active]
    assert any(p.kind == "producer_regex" for p in active)


def test_held_proposals_are_written_but_never_loaded(tmp_path, tiny_corpus):
    out = tmp_path / "signatures.json"
    merge_into(out, propose(tiny_corpus / "wrapper_generated", "acme_builder",
                            human_corpus=tiny_corpus / "human_verified"))
    written = json.loads(out.read_text())
    assert any(not e["active"] for e in written), "nothing was held for review"

    out.write_text(json.dumps([
        {"kind": "producer_regex", "pattern": "heldtool", "tool_label": "held",
         "confidence": 0.7, "active": False},
        {"kind": "producer_regex", "pattern": "livetool", "tool_label": "live",
         "confidence": 0.7, "active": True},
    ]))
    patterns = {s.pattern for s in load_signatures(out)}
    assert "livetool" in patterns
    assert "heldtool" not in patterns


def test_merge_preserves_a_human_activation_decision(tmp_path, tiny_corpus):
    out = tmp_path / "signatures.json"
    proposals = propose(tiny_corpus / "wrapper_generated", "acme_builder",
                        human_corpus=tiny_corpus / "human_verified")
    merge_into(out, proposals)

    entries = json.loads(out.read_text())
    entries[0]["active"] = True          # a human reviewed and approved it
    out.write_text(json.dumps(entries))

    merge_into(out, proposals)           # next week's collection run
    assert json.loads(out.read_text())[0]["active"] is True


def test_collected_signature_feeds_the_engine(tmp_path, tiny_corpus):
    out = tmp_path / "signatures.json"
    entries = [{
        "kind": "layout_hash", "pattern": "", "tool_label": "acme_template",
        "confidence": 0.6, "active": True,
    }]
    sample = scan(tiny_corpus / "wrapper_generated").applications[0]
    entries[0]["pattern"] = sample.doc.layout_hash
    out.write_text(json.dumps(entries))

    rescored = scan(tiny_corpus / "wrapper_generated", signatures_path=out)
    assert any("KNOWN_TEMPLATE" in {s.code for s in a.signals}
               for a in rescored.applications)


def test_cli_collect(tmp_path, tiny_corpus, capsys):
    out = tmp_path / "signatures.json"
    assert main(["collect", str(tiny_corpus / "wrapper_generated"),
                 "--tool", "acme_builder",
                 "--human-corpus", str(tiny_corpus / "human_verified"),
                 "--out", str(out)]) == 0
    assert "proposals" in capsys.readouterr().out
    assert out.exists()
