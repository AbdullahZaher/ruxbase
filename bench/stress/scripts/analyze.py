#!/usr/bin/env python3
"""Aggregates Ruxbase stress JSONL telemetry, Docker cgroup resource samples,
and generates SHA-256 artifact manifests for independent certification review."""

import hashlib
import json
import math
import os
import statistics
import sys
from collections import defaultdict
from pathlib import Path


def parse_pct(val: str) -> float:
    return float(val.strip().rstrip("%") or 0.0)


def parse_mem_mib(val: str) -> float:
    part = val.split("/")[0].strip()
    for suffix, mult in [("GiB", 1024.0), ("MiB", 1.0), ("KiB", 1.0 / 1024.0), ("B", 1.0 / (1024.0 * 1024.0))]:
        if part.endswith(suffix):
            return float(part[: -len(suffix)].strip()) * mult
    return 0.0


def load_jsonl(path: Path):
    rows = []
    if not path.exists():
        return rows
    for line in path.read_text().splitlines():
        line = line.strip()
        if line.startswith("{"):
            try:
                rows.append(json.loads(line))
            except json.JSONDecodeError:
                pass
    return rows


def summarize_results(results_dir: Path):
    metrics = []
    resources = []
    for p in sorted(results_dir.glob("*.jsonl")):
        if p.name.endswith(".resources.jsonl"):
            resources.extend(load_jsonl(p))
        else:
            metrics.extend(load_jsonl(p))

    # Group multi-trial stages by (testId, scenario, rlsStrict, poolMax, nodes, concurrency, offeredRps)
    grouped = defaultdict(list)
    for r in metrics:
        key = (
            r.get("testId", ""),
            r.get("scenario", ""),
            r.get("rlsStrict", False),
            r.get("poolMax", 0),
            r.get("nodes", 1),
            r.get("concurrency", 0),
            r.get("offeredRps", 0),
        )
        grouped[key].append(r)

    summary = {
        "totalMetricRecords": len(metrics),
        "totalResourceSamples": len(resources),
        "authViolationsTotal": sum(r.get("authViolations", 0) for r in metrics),
        "integrityErrorsTotal": sum(r.get("integrityErrors", 0) for r in metrics),
        "stages": [],
        "resourcesByContainer": {},
    }

    for key, runs in grouped.items():
        if not key[0]:
            continue
        rps_vals = [r.get("achievedRps", 0.0) for r in runs if "achievedRps" in r]
        p50_vals = [r.get("p50ms", 0.0) for r in runs if "p50ms" in r]
        p95_vals = [r.get("p95ms", 0.0) for r in runs if "p95ms" in r]
        p99_vals = [r.get("p99ms", 0.0) for r in runs if "p99ms" in r]
        err_vals = [r.get("errorRatePct", 0.0) for r in runs if "errorRatePct" in r]
        entry = {
            "testId": key[0],
            "scenario": key[1],
            "rlsStrict": key[2],
            "poolMax": key[3],
            "nodes": key[4],
            "concurrency": key[5],
            "offeredRps": key[6],
            "trials": len(runs),
            "medianRps": round(statistics.median(rps_vals), 2) if rps_vals else 0.0,
            "meanRps": round(statistics.mean(rps_vals), 2) if rps_vals else 0.0,
            "minRps": round(min(rps_vals), 2) if rps_vals else 0.0,
            "maxRps": round(max(rps_vals), 2) if rps_vals else 0.0,
            "medianP50ms": round(statistics.median(p50_vals), 3) if p50_vals else 0.0,
            "medianP95ms": round(statistics.median(p95_vals), 3) if p95_vals else 0.0,
            "medianP99ms": round(statistics.median(p99_vals), 3) if p99_vals else 0.0,
            "maxErrorRatePct": round(max(err_vals), 3) if err_vals else 0.0,
            "authViolations": sum(r.get("authViolations", 0) for r in runs),
            "integrityErrors": sum(r.get("integrityErrors", 0) for r in runs),
        }
        summary["stages"].append(entry)

    # Aggregate resource stats per container name
    by_container = defaultdict(lambda: {"cpu": [], "mem": [], "fds": []})
    for sample in resources:
        st = sample.get("stats", {})
        name = st.get("Name", "unknown")
        try:
            by_container[name]["cpu"].append(parse_pct(st.get("CPUPerc", "0%")))
            by_container[name]["mem"].append(parse_mem_mib(st.get("MemUsage", "0MiB / 0MiB")))
            fds = sample.get("fds", -1)
            if isinstance(fds, int) and fds >= 0:
                by_container[name]["fds"].append(fds)
        except ValueError:
            pass

    for cname, vals in by_container.items():
        summary["resourcesByContainer"][cname] = {
            "samples": len(vals["cpu"]),
            "cpuMeanPct": round(statistics.mean(vals["cpu"]), 2) if vals["cpu"] else 0.0,
            "cpuMaxPct": round(max(vals["cpu"]), 2) if vals["cpu"] else 0.0,
            "memMeanMiB": round(statistics.mean(vals["mem"]), 2) if vals["mem"] else 0.0,
            "memMaxMiB": round(max(vals["mem"]), 2) if vals["mem"] else 0.0,
            "fdsMin": min(vals["fds"]) if vals["fds"] else -1,
            "fdsMax": max(vals["fds"]) if vals["fds"] else -1,
        }

    # Generate SHA-256 checksums for all evidence files in results_dir
    checksums = {}
    for p in sorted(results_dir.rglob("*")):
        if p.is_file() and p.name != "SHA256SUMS.txt" and p.name != "summary.json":
            rel = str(p.relative_to(results_dir))
            checksums[rel] = hashlib.sha256(p.read_bytes()).hexdigest()

    (results_dir / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    (results_dir / "SHA256SUMS.txt").write_text(
        "\n".join(f"{h}  {rel}" for rel, h in sorted(checksums.items())) + "\n"
    )
    print(json.dumps({
        "summaryPath": str(results_dir / "summary.json"),
        "checksumFiles": len(checksums),
        "totalMetricRecords": summary["totalMetricRecords"],
        "totalResourceSamples": summary["totalResourceSamples"],
        "authViolationsTotal": summary["authViolationsTotal"],
        "integrityErrorsTotal": summary["integrityErrorsTotal"],
    }, indent=2))


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("usage: analyze.py <results_dir>", file=sys.stderr)
        sys.exit(2)
    summarize_results(Path(sys.argv[1]))
