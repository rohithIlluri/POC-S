"""The non-PDF formats: DOCX/ODT/RTF/HTML parse with their forensic layer
intact, legacy .doc surfaces as a parse failure, and the dispatcher routes by
suffix."""

from __future__ import annotations

import io
import zipfile

from signalhire.analyzers.forensics import analyze_forensics
from signalhire.analyzers.hidden import analyze_hidden
from signalhire.parse import parse_bytes, parse_pdf_date
from signalhire.types import Context

W_NS = 'xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"'


def _zip_bytes(entries: dict[str, str]) -> bytes:
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w") as zf:
        for name, content in entries.items():
            zf.writestr(name, content)
    return buf.getvalue()


def make_docx(runs: str, producer: str = "Microsoft Office Word",
              created: str = "2026-08-01T10:00:00Z") -> bytes:
    return _zip_bytes({
        "word/document.xml":
            f'<w:document {W_NS}><w:body><w:p>{runs}</w:p></w:body></w:document>',
        "docProps/core.xml":
            '<cp:coreProperties '
            'xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" '
            'xmlns:dc="http://purl.org/dc/elements/1.1/" '
            'xmlns:dcterms="http://purl.org/dc/terms/">'
            '<dc:creator>Ana Lee</dc:creator>'
            f'<dcterms:created>{created}</dcterms:created>'
            f'<dcterms:modified>{created}</dcterms:modified>'
            '</cp:coreProperties>',
        "docProps/app.xml":
            '<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties">'
            f'<Application>{producer}</Application></Properties>',
    })


def run_xml(text: str, rpr: str = "") -> str:
    props = f"<w:rPr>{rpr}</w:rPr>" if rpr else ""
    return f"<w:r>{props}<w:t>{text}</w:t></w:r>"


def test_docx_text_metadata_and_timestamps():
    data = make_docx(run_xml("Ana Lee, platform engineer at Harborline."))
    doc = parse_bytes("d1", "a1", data, "resume.docx")
    assert "Harborline" in doc.text
    assert doc.meta["producer"] == "Microsoft Office Word"
    assert doc.meta["creator"] == "Ana Lee"
    assert parse_pdf_date(doc.meta["creationdate"]) is not None
    assert not doc.parse_error


def test_docx_white_and_vanished_runs_are_hidden():
    data = make_docx(
        run_xml("Ordinary resume content in dark ink, several words long here")
        + run_xml("kubernetes terraform grpc istio prometheus thanos",
                  '<w:color w:val="FFFFFF"/>')
        + run_xml("secretly vanished keyword block for the parser only",
                  '<w:vanish/>'))
    doc = parse_bytes("d1", "a1", data, "resume.docx")
    hit = next(s for s in analyze_hidden(doc, Context())
               if s.code == "HIDDEN_TEXT")
    assert "near_white_text" in hit.evidence["techniques"]
    assert "markup_hidden" in hit.evidence["techniques"]


def test_docx_generator_toolchain_is_matched():
    data = make_docx(run_xml("body"), producer="python-docx 1.1.2")
    doc = parse_bytes("d1", "a1", data, "resume.docx")
    from signalhire.signatures import load_signatures
    signals = analyze_forensics(doc, Context(signatures=load_signatures()))
    match = next(s for s in signals if s.code == "GEN_TOOL_MATCH")
    assert match.evidence["matched"] == "docx_builder"


def test_odt_text_and_generator():
    data = _zip_bytes({
        "content.xml":
            '<office:document-content '
            'xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" '
            'xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0">'
            '<office:body><office:text>'
            '<text:p>Bo Ray, nine years in logistics.</text:p>'
            '</office:text></office:body></office:document-content>',
        "meta.xml":
            '<office:document-meta '
            'xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" '
            'xmlns:meta="urn:oasis:names:tc:opendocument:xmlns:meta:1.0">'
            '<office:meta><meta:generator>LibreOffice/7.6</meta:generator>'
            '</office:meta></office:document-meta>',
    })
    doc = parse_bytes("d1", "a1", data, "resume.odt")
    assert "logistics" in doc.text
    assert doc.meta["producer"] == "LibreOffice/7.6"


def test_rtf_text_and_generator():
    rtf = (rb"{\rtf1\ansi{\*\generator Riched20 10.0.19041}"
           rb"\pard Chidi Okafor, staff engineer.\par Ten years of infra.\par}")
    doc = parse_bytes("d1", "a1", rtf, "resume.rtf")
    assert "Chidi Okafor" in doc.text
    assert "Riched20" in doc.meta["producer"]


def test_html_visible_text_and_css_concealment():
    html = (b"<html><head><meta name='generator' content='resume-wrapper 2.0'>"
            b"<style>.x{color:red}</style></head><body>"
            b"<h1>Dara Petrov</h1><p>Platform engineer, Cedar Ridge Labs.</p>"
            b"<div style='display:none'>kubernetes terraform grpc istio "
            b"prometheus thanos</div>"
            b"<span style='color:#ffffff'>vault karpenter cilium ebpf spanner "
            b"dataflow</span></body></html>")
    doc = parse_bytes("d1", "a1", html, "resume.html")
    assert "Cedar Ridge Labs" in doc.text
    assert ".x{color:red}" not in doc.text
    assert doc.meta["producer"] == "resume-wrapper 2.0"
    hit = next(s for s in analyze_hidden(doc, Context())
               if s.code == "HIDDEN_TEXT")
    assert "markup_hidden" in hit.evidence["techniques"]
    assert "near_white_text" in hit.evidence["techniques"]


def test_html_without_generator_is_not_penalized():
    doc = parse_bytes("d1", "a1", b"<html><body><p>plain page</p></body></html>",
                      "resume.html")
    codes = {s.code for s in analyze_forensics(doc, Context())}
    assert "NO_PRODUCER" not in codes


def test_docx_without_application_is_penalized():
    data = _zip_bytes({
        "word/document.xml":
            f'<w:document {W_NS}><w:body><w:p>{run_xml("body")}</w:p>'
            '</w:body></w:document>'})
    doc = parse_bytes("d1", "a1", data, "resume.docx")
    codes = {s.code for s in analyze_forensics(doc, Context())}
    assert "NO_PRODUCER" in codes


def test_legacy_doc_is_a_visible_parse_failure():
    doc = parse_bytes("d1", "a1", b"\xd0\xcf\x11\xe0 binary ole junk",
                      "resume.doc")
    assert doc.parse_error
    codes = {s.code for s in analyze_forensics(doc, Context())}
    assert "PARSE_FAILED" in codes


def test_corrupt_docx_is_a_parse_failure_not_a_crash():
    doc = parse_bytes("d1", "a1", b"not a zip at all", "resume.docx")
    assert doc.parse_error
