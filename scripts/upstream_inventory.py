#!/usr/bin/env python3
"""Read-only inventory of the sole PocketBase upstream; never applies patches."""

import argparse
import json
import re
import subprocess
import tempfile
from pathlib import Path, PurePosixPath

UPSTREAM = "https://github.com/pocketbase/pocketbase"
UPSTREAM_MODULE = b"github.com/pocketbase/pocketbase"
RUXBASE_MODULE = b"github.com/AbdullahZaher/ruxbase"
VERSION = re.compile(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)")
SHA = re.compile(r"[0-9a-f]{40}")
NOTICE_NAME = re.compile(r"(?:licen[cs]e|notice|copying|copyright|third[-_]party)(?:$|[._-])",
                         re.IGNORECASE)
DEPENDENCY_FILES = {"go.mod", "go.sum", "package.json", "package-lock.json"}


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


def annotated_paths(directory, ref):
    result = subprocess.run(
        ["git", "-C", str(directory), "grep", "--full-name", "-z", "-l", "-E",
         "//[[:blank:]]*ruxbase:[[:blank:]]*[^[:space:]]", ref, "--", "*.go"],
        stdout=subprocess.PIPE, timeout=180,
    )
    if result.returncode == 1:  # git grep found no matching paths.
        return set()
    result.check_returncode()
    prefix = ref.encode("ascii") + b":"
    return {
        path.removeprefix(prefix).decode("utf-8", errors="surrogateescape")
        for path in result.stdout.split(b"\0") if path
    }


def module_path_only(baseline, root, row):
    before_mode, before_kind, before_sha = row["baseline"]
    after_mode, after_kind, after_sha = row["ruxbase"]
    if before_mode != after_mode or before_kind != "blob" or after_kind != "blob":
        return False
    original = git(baseline, "cat-file", "blob", before_sha)
    current = git(root, "cat-file", "blob", after_sha)
    return original.replace(UPSTREAM_MODULE, RUXBASE_MODULE) == current


def source_review(rows, annotations, is_module_path_only):
    review = {
        "inherited_go_with_annotation": [],
        "inherited_go_without_annotation": [],
        "inherited_go_module_path_only": [],
        "inherited_go_tests": [],
        "license_and_dependency_files": [],
    }
    for row in rows:
        path = row["path"]
        name = PurePosixPath(path).name
        if name in DEPENDENCY_FILES or NOTICE_NAME.match(name):
            review["license_and_dependency_files"].append(row)
        if row["status"] != "RUXBASE_MODIFIED" or not name.endswith(".go"):
            continue
        if name.endswith("_test.go"):
            review["inherited_go_tests"].append(path)
        elif path in annotations:
            review["inherited_go_with_annotation"].append(path)
        elif is_module_path_only(row):
            review["inherited_go_module_path_only"].append(path)
        else:
            review["inherited_go_without_annotation"].append(path)
    return review


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
            "ruxbase_tree": git(root, "rev-parse", head + "^{tree}").decode().strip(),
            "upstream": UPSTREAM,
            "reference_version": reference,
            "reference_commit": verified,
            "reference_tree": git(tmp, "rev-parse", verified + "^{tree}").decode().strip(),
            "newer_stable_shaped_tags": newer,
            "candidate_tag": candidate,
            "candidate_commit": candidate_commit,
            "counts": counts,
            "source_review": source_review(
                rows, annotated_paths(root, head), lambda row: module_path_only(tmp, root, row)
            ),
            "files": rows,
            "limitations": [
                "Path and tree-entry comparison only; renames are not inferred.",
                "Candidate tags require release, provenance and compatibility review.",
                "A marker is text evidence; its presence does not establish that all changes are annotated.",
                "Module-path-only classification requires byte equality after the exact module substitution.",
                "License and dependency entries identify tracked files for review, not their dependency license obligations.",
                "No ancestry verification, license acceptance, merge readiness or runtime certification implied.",
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
    summary = {key: value for key, value in evidence.items() if key not in {"files", "source_review"}}
    summary["source_review_counts"] = {
        key: len(paths) for key, paths in evidence["source_review"].items()
    }
    print(json.dumps(summary, indent=2))


if __name__ == "__main__":
    main()
