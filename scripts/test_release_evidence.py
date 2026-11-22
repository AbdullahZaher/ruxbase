"""Regression tests for release evidence and checklist reconciliation."""

import unittest

from check_release_evidence import summarize, verify

SAMPLE_CHECKLIST = """
- [x] **W00-01 / P0** — one
- [ ] **W00-08 / P1** — matched benchmark
- [ ] **W11-06 / P1** — matched benchmark
- [ ] **JOURNEY-11** — matched benchmark
"""
MARKER = (
    "<!-- release-checklist-counts: waves=1/3 journeys=0/1 "
    "combined=1/4 open=3 gate-categories=1 -->"
)
SAMPLE_MATRIX = f"""
**1 / 4** total, **1 / 3** waves, **0 / 1** journeys;
**3** unchecked IDs, **1** gate categories.
{MARKER}
| Requirement ID | Description |
| --- | --- |
| **W00-08** | Pending |
| **W11-06** | Pending |
| **JOURNEY-11** | Pending |
"""
SAMPLE_CERTIFICATION = f"""
### Dimension 1: Requirement Completion
**1 of 4** total, **1 of 3** waves, **0 of 1** journeys;
**3** unchecked IDs, **1** gate categories.
(`W00-08`, `W11-06`, `JOURNEY-11`).
{MARKER}
### Dimension 2: Architecture
"""


class ReleaseEvidenceTests(unittest.TestCase):
    def test_shared_gate_counts_as_three_open_ids(self):
        summary, open_ids = summarize(SAMPLE_CHECKLIST)
        self.assertEqual(summary["combined_done"], 1)
        self.assertEqual(summary["combined_total"], 4)
        self.assertEqual(summary["open"], 3)
        self.assertEqual(summary["gate_categories"], 1)
        self.assertEqual(open_ids, {"W00-08", "W11-06", "JOURNEY-11"})

    def test_valid_reconciliation(self):
        _, errors = verify(SAMPLE_CHECKLIST, SAMPLE_MATRIX, SAMPLE_CERTIFICATION)
        self.assertEqual(errors, [])

    def test_stale_report_count_fails(self):
        stale = SAMPLE_MATRIX.replace("combined=1/4", "combined=2/4")
        _, errors = verify(SAMPLE_CHECKLIST, stale, SAMPLE_CERTIFICATION)
        self.assertTrue(any("stale checklist accounting" in error for error in errors))

    def test_missing_id_table_row_fails(self):
        missing = SAMPLE_MATRIX.replace("| **JOURNEY-11** | Pending |\n", "")
        _, errors = verify(SAMPLE_CHECKLIST, missing, SAMPLE_CERTIFICATION)
        self.assertTrue(any("outstanding item table" in error for error in errors))

    def test_duplicate_id_fails_closed(self):
        with self.assertRaisesRegex(ValueError, "duplicate checklist requirement ID"):
            summarize(SAMPLE_CHECKLIST + "\n- [x] **W00-01** — duplicate\n")


if __name__ == "__main__":
    unittest.main()
