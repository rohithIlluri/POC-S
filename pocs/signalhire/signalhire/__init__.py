"""SignalHire — an application authenticity engine.

Labels every incoming application as genuine effort / mass-generated /
needs review, with machine-readable reason codes a recruiter can read aloud.

Explicitly *not* an AI-text detector: nothing in this package scores writing
style, fluency or "AI-ness". It scores document forensics, layout structure,
hidden content, job-description mirroring and cross-applicant duplication —
signals that are objective, explainable, and blind to who wrote the text.

Every output is assistive. No label in this library means "reject".
"""

from .pipeline import ScanResult, scan, score_documents
from .scoring import LABELS, Thresholds, score
from .types import ParsedDoc, ScoredApplication, Severity, Signal

__version__ = "0.1.0"

__all__ = [
    "LABELS", "ParsedDoc", "ScanResult", "ScoredApplication", "Severity",
    "Signal", "Thresholds", "scan", "score", "score_documents", "__version__",
]
