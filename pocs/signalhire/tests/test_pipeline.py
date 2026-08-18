from __future__ import annotations

import json

from signalhire.cli import main
from signalhire.evaluate import evaluate
from signalhire.parse import parse_file, salted_hash
from signalhire.pipeline import scan
from signalhire.report import render_html, render_json, render_text


def _apps_under(result, folder: str):
    return [a for a in result.applications if f"/{folder}/" in a.doc.source_path]


def test_parse_extracts_text_metadata_and_hashed_identity(tiny_corpus):
    doc = parse_file(next((tiny_corpus / "wrapper_generated").glob("*.pdf")))
    assert doc.text.strip()
    assert doc.meta["producer"]
    assert doc.sha256 and len(doc.sha256) == 64
    # Identity is hashed, never carried in the clear.
    assert doc.identity.email_hash and "@" not in doc.identity.email_hash
    assert doc.identity.email_hash == salted_hash(
        next(w for w in doc.text.split() if "@" in w))


def test_scan_labels_the_batch(tiny_corpus):
    jd = (tiny_corpus / "jd.txt").read_text()
    result = scan(tiny_corpus, jd_text=jd, exclude={tiny_corpus / "jd.txt"})

    assert result.stats["documents"] == len(list(tiny_corpus.rglob("*.pdf")))
    assert all(a.label == "genuine" for a in _apps_under(result, "human_verified"))
    assert all(a.label in ("mass_generated", "high_risk")
               for a in _apps_under(result, "wrapper_generated"))
    assert all(a.label == "high_risk" for a in _apps_under(result, "attack"))


def test_every_flag_carries_evidence(tiny_corpus):
    result = scan(tiny_corpus, exclude={tiny_corpus / "jd.txt"})
    flagged = [a for a in result.applications
               if a.label in ("mass_generated", "high_risk")]
    assert flagged
    for app in flagged:
        assert app.signals, f"{app.doc.source_path} flagged with no reason code"
        assert any(s.evidence for s in app.signals)


def test_scan_is_order_independent(tiny_corpus):
    a = scan(tiny_corpus, exclude={tiny_corpus / "jd.txt"})
    b = scan(tiny_corpus, exclude={tiny_corpus / "jd.txt"})
    assert {x.doc.source_path: x.label for x in a.applications} == \
           {x.doc.source_path: x.label for x in b.applications}


def test_cluster_ids_are_stable_across_ring_members(tiny_corpus):
    """Every member of one near-duplicate ring reports the same cluster id."""
    result = scan(tiny_corpus, exclude={tiny_corpus / "jd.txt"})
    seen: dict[str, set[str]] = {}
    for app in result.applications:
        for s in app.signals:
            cluster = s.evidence.get("cluster")
            if cluster:
                seen.setdefault(cluster, set()).add(app.doc.doc_id)
    assert seen, "no cluster signals fired on the corpus"
    for cluster, members in seen.items():
        assert members <= set(result.context.clusters), cluster
        assert {result.context.clusters[m] for m in members} == {cluster}


def test_reports_render(tiny_corpus):
    result = scan(tiny_corpus, exclude={tiny_corpus / "jd.txt"})

    html = render_html(result)
    assert "Assistive output" in html
    assert "<script" not in html.lower()
    assert "HIDDEN_TEXT" in html

    payload = json.loads(render_json(result))
    assert payload["stats"]["documents"] == len(result.applications)
    assert payload["applications"][0]["reason_codes"] is not None

    assert "Assistive output" in render_text(result)


def test_eval_gates_pass_on_the_synthetic_corpus(tiny_corpus):
    report = evaluate(tiny_corpus)
    assert report.passed, report.format()
    assert {g.name for g in report.gates} == {
        "false_flag_rate", "wrapper_recall", "attack_recall", "fairness_slice"}
    assert report.metrics["fairness"]["non_native_n"] > 0


def test_cli_scan_and_eval(tiny_corpus, tmp_path, capsys):
    html, out_json = tmp_path / "r.html", tmp_path / "r.json"
    assert main(["scan", str(tiny_corpus), "--jd", str(tiny_corpus / "jd.txt"),
                 "--html", str(html), "--json", str(out_json)]) == 0
    assert html.exists() and json.loads(out_json.read_text())["applications"]

    assert main(["eval", str(tiny_corpus)]) == 0
    assert "RESULT: PASS" in capsys.readouterr().out


def test_cli_rejects_a_missing_target(tmp_path):
    assert main(["scan", str(tmp_path / "nope")]) == 2
