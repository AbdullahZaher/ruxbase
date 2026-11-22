#!/usr/bin/env python3
"""Fail closed if release reports disagree with the master checklist."""

from __future__ import annotations

import re
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
CHECKLIST = "MASTER-CHECKLIST.md"
MATRIX = "docs/architecture/FINAL-RELEASE-REQUIREMENTS-MATRIX.md"
CERTIFICATION = "docs/architecture/FINAL-INDEPENDENT-CERTIFICATION.md"
ITEM = re.compile(
    r"^\s*-\s*\[([ xX])\]\s+\*\*((?:W\d{2}-(?:M\d{2}-)?\d{2}|JOURNEY-\d{2}))\b",
    re.MULTILINE,
)
MARKER = re.compile(
    r"<!-- release-checklist-counts: waves=(\d+)/(\d+) "
    r"journeys=(\d+)/(\d+) combined=(\d+)/(\d+) "
    r"open=(\d+) gate-categories=(\d+) -->"
)
BENCHMARK_GATE = frozenset({"W00-08", "W11-06", "JOURNEY-11"})


def summarize(checklist: str) -> tuple[dict[str, int], set[str]]:
    entries = ITEM.findall(checklist)
    if not entries:
        raise ValueError("no checklist requirement entries found")
    seen: set[str] = set()
    wave_done = wave_total = journey_done = journey_total = 0
    open_ids: set[str] = set()
    for flag, identifier in entries:
        if identifier in seen:
            raise ValueError(f"duplicate checklist requirement ID: {identifier}")
        seen.add(identifier)
        done = flag.lower() == "x"
        if identifier.startswith("JOURNEY-"):
            journey_total += 1
            journey_done += done
        else:
            wave_total += 1
            wave_done += done
        if not done:
            open_ids.add(identifier)

    # A dependency shared by three requirements remains three checkbox entries.
    # Grouping by release blocker is only for the separately reported gate count.
    shared = open_ids & BENCHMARK_GATE
    gate_categories = len(open_ids - BENCHMARK_GATE) + bool(shared)
    numbers = {
        "waves_done": wave_done,
        "waves_total": wave_total,
        "journeys_done": journey_done,
        "journeys_total": journey_total,
        "combined_done": wave_done + journey_done,
        "combined_total": wave_total + journey_total,
        "open": len(open_ids),
        "gate_categories": gate_categories,
    }
    return numbers, open_ids


def verify(checklist: str, matrix: str, certification: str) -> tuple[dict[str, int], list[str]]:
    numbers, open_ids = summarize(checklist)
    expected = (
        numbers["waves_done"], numbers["waves_total"],
        numbers["journeys_done"], numbers["journeys_total"],
        numbers["combined_done"], numbers["combined_total"],
        numbers["open"], numbers["gate_categories"],
    )
    errors: list[str] = []
    for name, doc in ((MATRIX, matrix), (CERTIFICATION, certification)):
        matches = MARKER.findall(doc)
        if len(matches) != 1 or tuple(int(part) for part in matches[0]) != expected:
            errors.append(f"{name}: missing/duplicate/stale checklist accounting marker")
        for identifier in sorted(open_ids):
            if f"`{identifier}`" not in doc and f"**{identifier}**" not in doc:
                errors.append(f"{name}: omitted open requirement {identifier}")

    combined_done, combined_total = numbers["combined_done"], numbers["combined_total"]
    if f"**{combined_done} / {combined_total}**" not in matrix:
        errors.append(f"{MATRIX}: displayed combined tally differs from checklist")
    if f"**{combined_done} of {combined_total}**" not in certification:
        errors.append(f"{CERTIFICATION}: displayed combined tally differs from checklist")
    for name, doc, joiner in (
        (MATRIX, matrix, " / "),
        (CERTIFICATION, certification, " of "),
    ):
        for subset in ("waves", "journeys"):
            actual = f"**{numbers[subset + '_done']}{joiner}{numbers[subset + '_total']}**"
            if actual not in doc:
                errors.append(f"{name}: missing or incorrect {subset} tally")
        if f"**{numbers['open']}**" not in doc:
            errors.append(f"{name}: missing or incorrect open entry count")
        if f"**{numbers['gate_categories']}**" not in doc:
            errors.append(f"{name}: missing or incorrect gate category count")

    listed = re.findall(
        r"^\|\s*\*\*((?:W\d{2}-(?:M\d{2}-)?\d{2}|JOURNEY-\d{2}))\*\*\s*\|",
        matrix,
        re.MULTILINE,
    )
    if len(listed) != len(set(listed)) or set(listed) != open_ids:
        errors.append(f"{MATRIX}: outstanding item table must list each open ID once")

    dimension = certification.partition("### Dimension 1:")[2].partition("### Dimension 2:")[0]
    certified_open_ids = re.findall(
        r"`(W\d{2}-(?:M\d{2}-)?\d{2}|JOURNEY-\d{2})`", dimension
    )
    if set(certified_open_ids) != open_ids or len(certified_open_ids) != len(open_ids):
        errors.append(f"{CERTIFICATION}: Dimension 1 must list open IDs exactly once")
    return numbers, errors


def main() -> int:
    checklist = (ROOT / CHECKLIST).read_text(encoding="utf-8")
    matrix = (ROOT / MATRIX).read_text(encoding="utf-8")
    certification = (ROOT / CERTIFICATION).read_text(encoding="utf-8")
    try:
        numbers, errors = verify(checklist, matrix, certification)
    except ValueError as exc:
        print(f"Release evidence check FAILED: {exc}")
        return 1
    if errors:
        for error in errors:
            print(f"Release evidence check FAILED: {error}")
        return 1
    print(
        "Release evidence OK: "
        f"{numbers['combined_done']}/{numbers['combined_total']} complete; "
        f"{numbers['open']} unchecked IDs; "
        f"{numbers['gate_categories']} gate categories"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
