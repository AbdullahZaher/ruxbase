#!/usr/bin/env python3
"""Inventory Go build metadata from the binaries in release archives."""

import argparse
import hashlib
import json
import shutil
import subprocess
import tempfile
from pathlib import Path
from zipfile import ZipFile


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def inspect_archive(path):
    binary = "ruxbase-import" if path.name.startswith("ruxbase-import_") else "ruxbase"
    if "_windows_" in path.name:
        binary += ".exe"
    with ZipFile(path) as archive, tempfile.TemporaryDirectory() as directory:
        entries = [item for item in archive.infolist() if item.filename == binary]
        if len(entries) != 1:
            raise ValueError(f"{path.name}: expected exactly one {binary}")
        executable = Path(directory) / binary
        with archive.open(entries[0]) as source, executable.open("wb") as destination:
            shutil.copyfileobj(source, destination)
        binary_sha = digest(executable)
        output = subprocess.check_output(["go", "version", "-m", str(executable)], text=True)

    header, _, build_info = output.partition("\n")
    _, separator, toolchain = header.partition(": ")
    rows = build_info.splitlines()
    if not separator or not any(row.startswith("\tpath\t") for row in rows):
        raise ValueError(f"{path.name}: missing Go build information")
    return {
        "archive": path.name,
        "archive_sha256": digest(path),
        "binary": binary,
        "binary_sha256": binary_sha,
        "toolchain": toolchain.strip(),
        "build_info": rows,
    }


def git(ref):
    return subprocess.check_output(["git", "rev-parse", ref], text=True).strip()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path, help="GoReleaser snapshot output")
    args = parser.parse_args()
    archives = sorted(args.directory.glob("ruxbase*.zip"))
    if not archives:
        parser.error("no Ruxbase release archives found")
    print(json.dumps({
        "commit": git("HEAD"),
        "tree": git("HEAD^{tree}"),
        "archives": [inspect_archive(path) for path in archives],
    }, indent=2))


if __name__ == "__main__":
    main()
