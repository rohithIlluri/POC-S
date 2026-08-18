"""SignalHire web MVP — the Phase-0 engine behind a recruiter-facing site.

Everything runs in-process and in-memory: uploaded files are parsed, scored as
one population batch (which is what powers the cross-applicant signals), and
the results are returned as JSON. No uploaded document is written to disk.

Run:  uvicorn webapp.app:app --reload      (from pocs/signalhire)
  or: signalhire web
"""

from __future__ import annotations

import hashlib
import json
import uuid
from datetime import datetime, timezone
from pathlib import Path

from fastapi import FastAPI, File, Form, UploadFile
from fastapi.responses import FileResponse, JSONResponse

from signalhire import __version__ as engine_version
from signalhire.parse import SUPPORTED_SUFFIXES, parse_pdf, parse_text
from signalhire.pipeline import score_documents
from signalhire.signatures import SIGNATURE_DB_VERSION

MAX_FILES = 200
MAX_FILE_BYTES = 15 * 1024 * 1024

STATIC = Path(__file__).parent / "static"

app = FastAPI(title="SignalHire", version=engine_version)


@app.get("/")
def index() -> FileResponse:
    return FileResponse(STATIC / "index.html")


@app.get("/api/health")
def health() -> dict:
    return {"ok": True, "engine": engine_version,
            "signature_db": SIGNATURE_DB_VERSION}


@app.post("/api/scan")
async def scan_batch(
    files: list[UploadFile] = File(...),
    jd: str = Form(""),
    sensitivity: str = Form("balanced"),
) -> JSONResponse:
    if sensitivity not in ("conservative", "balanced", "aggressive"):
        return JSONResponse({"error": "unknown sensitivity"}, status_code=422)
    if len(files) > MAX_FILES:
        return JSONResponse(
            {"error": f"at most {MAX_FILES} files per scan"}, status_code=413)

    docs = []
    skipped: list[dict] = []
    submitted_at = datetime.now(timezone.utc)  # upload time IS submission time

    for f in files:
        name = Path(f.filename or "upload").name
        data = await f.read()
        if len(data) > MAX_FILE_BYTES:
            skipped.append({"file": name, "reason": "over 15 MB"})
            continue
        suffix = Path(name).suffix.lower()
        if suffix not in SUPPORTED_SUFFIXES:
            skipped.append({"file": name,
                            "reason": f"unsupported type ({suffix or 'no extension'})"})
            continue
        doc_id = hashlib.sha256(data + name.encode()).hexdigest()[:16]
        application_id = str(uuid.uuid4())
        if suffix == ".pdf":
            doc = parse_pdf(doc_id, application_id, data, source_path=name)
        else:
            doc = parse_text(doc_id, application_id, data, source_path=name)
        doc.submitted_at = submitted_at
        docs.append(doc)

    if not docs:
        return JSONResponse(
            {"error": "no supported documents in the upload "
                      "(PDF, .txt and .md are accepted)",
             "skipped": skipped},
            status_code=422)

    result = score_documents(docs, jd_text=jd, sensitivity=sensitivity)
    result.stats["source"] = "upload"
    result.stats["skipped"] = skipped
    payload = result.as_dict()
    # Round-trip through the engine's JSON encoder so datetimes serialize the
    # same way they do in the CLI report.
    return JSONResponse(json.loads(json.dumps(payload, default=str)))


def main() -> None:  # pragma: no cover - thin uvicorn launcher
    import uvicorn
    uvicorn.run("webapp.app:app", host="127.0.0.1", port=8710, reload=False)


if __name__ == "__main__":  # pragma: no cover
    main()
