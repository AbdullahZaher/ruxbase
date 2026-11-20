#!/usr/bin/env python3
"""Read-only inventory of the sole PocketBase upstream; never applies patches."""

import argparse
import json
import re
import subprocess
import tempfile
from pathlib import Path

UPSTREAM = "https://github.com/pocketbase/pocketbase"
VERSION = re.compile(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)")
SHA = re.compile(r"[0-9a-f]{40}")


def version(tag):
    match = VERSION.fullmatch(tag)
    if match is None:
        raise ValueError("expected a stable, three-component version tag")
    return tuple(map(int, match.groups()))


def git(directory, *args):
    return subprocess.check_output(
        ["git", "-C", str(directory), *args], timeout=180
    )


def read_policy(root):
    data = json.loads((root / "UPSTREAM.lock.json").read_text(encoding="utf-8"))
    upstream = data["upstream"]
    policy = data["policy"]
    if (
        data["schema_version"] != 1
        or upstream["repository"] != UPSTREAM
        or upstream["sole_upstream"] is not True
        or policy["automatic_merge"] is not False
        or policy["third_party_fork_sync"] is not False
        or policy["maintainer_review_required"] is not True
        or policy["compatibility_tests_required"] is not True
        or policy["postgresql_integration_tests_required"] is not True
    ):
        raise ValueError("upstream policy mismatch")
    version(upstream["reference_version"])
    if not SHA.fullmatch(upstream["reference_commit"]):
        raise ValueError("reference commit is not an immutable SHA")
    return upstream


def stable_tags(output):
    tags = set()
    for line in output.decode("ascii").splitlines():
        fields = line.split("\t")
        if len(fields) != 2 or not fields[1].startswith("refs/tags/"):
            continue
        tag = fields[1][len("refs/tags/"):]
        if VERSION.fullmatch(tag):
            tags.add(tag)
    return sorted(tags, key=version)


def tree(directory, ref):
    entries = {}
    for row in git(directory, "ls-tree", "-r", "-z", ref).split(b"\0"):
        if not row:
            continue
        meta, raw_path = row.split(b"\t", 1)
        mode, kind, sha = meta.decode("ascii").split()
        path = raw_path.decode("utf-8", errors="surrogateescape")
        entries[path] = (mode, kind, sha)
    return entries


def classify(base, local, future=None):
    rows = []
    for path in sorted(base.keys() | local.keys() | (future or {}).keys()):
        before, current = base.get(path), local.get(path)
        if before == current:
            status = "IDENTICAL" if before else "ABSENT_BOTH"
        elif before is None:
            status = "RUXBASE_ADDED"
        elif current is None:
            status = "RUXBASE_REMOVED"
        else:
            status = "RUXBASE_MODIFIED"
        row = {"path": path, "baseline": before, "ruxbase": current, "status": status}
        if future is not None:
            proposed = future.get(path)
            row["upstream_candidate"] = proposed
            if before == proposed:
                row["review"] = "NO_UPSTREAM_CHANGE"
            elif current == proposed:
                row["review"] = "ALREADY_MATCHES"
            elif current == before:
                row["review"] = "REVIEW_UPSTREAM_CHANGE"
            else:
                row["review"] = "REVIEW_DIVERGENCE"
        rows.append(row)
    return rows


def fetch_tag(directory, tag, ref):
    version(tag)
    git(directory, "fetch", "--no-tags", "--depth=1", UPSTREAM + ".git",
        "refs/tags/" + tag)
    commit = git(directory, "rev-parse", "FETCH_HEAD^{commit}").decode().strip()
    git(directory, "update-ref", ref, commit)
    return commit


def report(root):
    policy = read_policy(root)
    head = git(root, "rev-parse", "HEAD").decode().strip()
    reference = policy["reference_version"]
    tags = stable_tags(git(root, "ls-remote", "--tags", "--refs", UPSTREAM + ".git"))
    # These are stable-shaped Git tags, not a claim about published releases.
    newer = [tag for tag in tags if version(tag) > version(reference)]
    candidate = newer[-1] if newer else None
    with tempfile.TemporaryDirectory(prefix="ruxbase-upstream-") as tmp:
        git(tmp, "init", "-q")
        verified = fetch_tag(tmp, reference, "refs/audit/baseline")
        if verified != policy["reference_commit"]:
            raise ValueError("official tag does not match the lockfile")
        candidate_commit = None
        future = None
        if candidate:
            candidate_commit = fetch_tag(tmp, candidate, "refs/audit/candidate")
            future = tree(tmp, candidate_commit)
        rows = classify(tree(tmp, verified), tree(root, head), future)
        counts = {}
        for row in rows:
            counts[row["status"]] = counts.get(row["status"], 0) + 1
        return {
            "schema_version": 1,
            "ruxbase_head": head,
            "upstream": UPSTREAM,
            "reference_version": reference,
            "reference_commit": verified,
            "reference_tree": git(tmp, "rev-parse", verified + "^{tree}").decode().strip(),
            "newer_stable_shaped_tags": newer,
            "candidate_tag": candidate,
            "candidate_commit": candidate_commit,
            "counts": counts,
            "files": rows,
            "limitations": [
                "Path and tree-entry comparison only; renames are not inferred.",
                "Candidate tags require release, provenance and compatibility review.",
                "No ancestry, license, merge readiness or runtime certification implied.",
                "No upstream code was checked out, executed, pushed or merged.",
            ],
        }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    options = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    evidence = report(root)
    options.output.parent.mkdir(parents=True, exist_ok=True)
    options.output.write_text(json.dumps(evidence, indent=2, sort_keys=True) + "\n",
                              encoding="utf-8")
    print(json.dumps({k: v for k, v in evidence.items() if k != "files"}, indent=2))


if __name__ == "__main__":
    main()
