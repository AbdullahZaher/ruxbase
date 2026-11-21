"""Prepare an authenticated-by-external-digests static documentation snapshot for review.

No network access, GitHub mutations, or live activation. Destination must not exist.
"""
import argparse
import hashlib
import json
import pathlib
import re
import shutil
import stat
import tempfile
import zipfile

MAX_ARCHIVE = 32 * 1024 * 1024
MAX_TOTAL = 64 * 1024 * 1024
MAX_FILE = 16 * 1024 * 1024
MAX_FILES = 4096
MANIFEST = "artifact-manifest.json"

def require(ok, message):
    if not ok:
        raise ValueError(message)

def sha(data):
    return hashlib.sha256(data).hexdigest()

def path_ok(name):
    return isinstance(name, str) and len(name) <= 512 and all(
        re.fullmatch(r"[A-Za-z0-9_@+.,-][A-Za-z0-9_@+.,-]*", segment) and not segment.startswith(".")
        for segment in name.split("/")
    )

def static_path(name):
    return path_ok(name) and not any(
        part.lower() in ("api-viewer", "scalar-candidate", "@scalar") for part in name.split("/")
    )

def parse_manifest(raw, expected_digest, expected_commit):
    require(re.fullmatch(r"[0-9a-f]{64}", expected_digest) is not None
            and sha(raw) == expected_digest, "Manifest digest mismatch")
    m = json.loads(raw)
    require(m["schemaVersion"] == 1 and m["product"] == "Ruxbase"
            and m["documentationStatus"] == "development" and m["releaseVersion"] is None
            and m["selfExcluded"] == MANIFEST, "Unsupported snapshot identity")
    require(re.fullmatch(r"[0-9a-f]{40}", expected_commit) is not None
            and m["sourceCommit"] == expected_commit
            and re.fullmatch(r"[0-9a-f]{40}", m["sourceTree"]) is not None, "Unexpected source identity")
    entries = m["files"]
    require(isinstance(entries, list) and 0 < len(entries) <= MAX_FILES
            and m["fileCount"] == len(entries), "Unexpected manifest file count")
    files = {}
    for e in entries:
        name = e["path"]
        require(static_path(name) and name != MANIFEST and name not in files,
                "Unsafe, duplicate, or excluded static path: " + str(name))
        require(type(e["bytes"]) is int and 0 <= e["bytes"] <= MAX_FILE
                and re.fullmatch(r"[0-9a-f]{64}", e["sha256"]) is not None, "Invalid file metadata")
        files[name] = e
    total = sum(e["bytes"] for e in entries)
    require(m["totalBytes"] == total and total <= MAX_TOTAL, "Artifact size mismatch")
    require(list(files) == sorted(files), "Manifest paths not sorted")
    require({"index.html", "version.json", "versions.json", "llms.txt", "openapi.json",
             "reference/api.md", "pagefind/pagefind.js", "sitemap-index.xml"}.issubset(files),
            "Required static files missing")
    return m, files

def verify_data(name, data, entry):
    require(len(data) == entry["bytes"] and sha(data) == entry["sha256"],
            "File digest mismatch: " + name)

def recover(archive, destination, archive_digest, manifest_digest, expected_commit):
    archive = pathlib.Path(archive)
    destination = pathlib.Path(destination)
    require(not destination.exists() and not destination.is_symlink(), "Destination already exists")
    require(archive.is_file() and archive.stat().st_size <= MAX_ARCHIVE, "Archive missing or too large")
    require(re.fullmatch(r"[0-9a-f]{64}", archive_digest) is not None
            and sha(archive.read_bytes()) == archive_digest, "Archive digest mismatch")
    with zipfile.ZipFile(archive) as z:
        infos = z.infolist()
        require(0 < len(infos) <= MAX_FILES + 1, "Unexpected ZIP entry count")
        names = set()
        for item in infos:
            mode = item.external_attr >> 16
            require(static_path(item.filename) and not item.is_dir()
                    and stat.S_IFMT(mode) in (0, stat.S_IFREG)
                    and item.filename not in names and not item.flag_bits & 1
                    and item.file_size <= MAX_FILE, "Unsafe, duplicate, or excluded ZIP member: " + item.filename)
            names.add(item.filename)
        require(sum(x.file_size for x in infos) <= MAX_TOTAL + MAX_FILE, "ZIP expands beyond limit")
        raw = z.read(MANIFEST)
        m, files = parse_manifest(raw, manifest_digest, expected_commit)
        require(names == set(files) | {MANIFEST}, "Archive file set mismatch")
        # Only create a private temporary staging directory after validating the entire inventory.
        with tempfile.TemporaryDirectory(prefix=".docs-recovery-", dir=destination.parent) as temp:
            staged = pathlib.Path(temp) / "snapshot"
            staged.mkdir()
            for name, e in files.items():
                data = z.read(name)
                verify_data(name, data, e)
                target = staged / name
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(data)
            v = json.loads((staged / "version.json").read_text())
            require(v["sourceCommit"] == expected_commit and v["sourceTree"] == m["sourceTree"]
                    and v["documentationStatus"] == "development" and v["releaseVersion"] is None
                    and v["dirty"] is False, "Version identity mismatch")
            catalog = json.loads((staged / "versions.json").read_text())
            require(catalog["sourceCommit"] == expected_commit and catalog["sourceTree"] == m["sourceTree"]
                    and catalog["channels"]["stable"] is None and catalog["releases"] == [],
                    "Unexpected release catalog")
            for name in ("reference/api.md", "reference/api/index.html"):
                if name in files:
                    require(b"/ruxbase/api-viewer/" not in (staged / name).read_bytes(),
                            "Public reference links to excluded viewer")
            (staged / MANIFEST).write_bytes(raw)
            require(not destination.exists() and not destination.is_symlink(), "Destination appeared during verification")
            staged.rename(destination)
    return {"sourceCommit": expected_commit, "sourceTree": m["sourceTree"],
            "fileCount": len(files), "archiveSha256": archive_digest,
            "manifestSha256": manifest_digest, "status": "verified-local-staging-only"}

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("archive")
    parser.add_argument("destination")
    parser.add_argument("--archive-sha256", required=True)
    parser.add_argument("--manifest-sha256", required=True)
    parser.add_argument("--source-commit", required=True)
    a = parser.parse_args()
    print(json.dumps(recover(a.archive, a.destination, a.archive_sha256, a.manifest_sha256,
                             a.source_commit), indent=2))
