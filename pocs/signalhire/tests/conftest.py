from __future__ import annotations

import pytest

from signalhire.corpus import build_corpus
from signalhire.types import Context, Identity, ParsedDoc


@pytest.fixture(scope="session")
def tiny_corpus(tmp_path_factory):
    """A small synthetic corpus, built once for the whole test session."""
    out = tmp_path_factory.mktemp("corpus")
    build_corpus(out, seed=11, humans=8, wrappers=6, hybrids=2, attack_pairs=1,
                 evasions=0, trickle_batches=0)
    return out


def make_block(text: str, *, size: float = 11.0, color: int = 0x101010,
               x: float = 60.0, y: float = 100.0, font: str = "helv") -> dict:
    return {"text": text, "font": font, "size": size, "color": color,
            "bbox": [x, y, x + 300.0, y + size]}


def make_doc(text: str = "", *, blocks: list[dict] | None = None,
             meta: dict | None = None, doc_id: str = "d1",
             identity: Identity | None = None,
             width: float = 612.0, height: float = 792.0) -> ParsedDoc:
    blocks = blocks if blocks is not None else [make_block(text)]
    doc = ParsedDoc(
        doc_id=doc_id, application_id="a1", source_path=f"/tmp/{doc_id}.pdf",
        text=text or " ".join(b["text"] for b in blocks),
        pages=[{"num": 0, "blocks": blocks, "width": width, "height": height}],
        meta=meta or {}, fonts=sorted({b["font"] for b in blocks}),
    )
    if identity:
        doc.identity = identity
    return doc


@pytest.fixture
def ctx() -> Context:
    from signalhire.signatures import load_signatures
    return Context(signatures=load_signatures())
