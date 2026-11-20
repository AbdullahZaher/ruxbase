"""Exercise the current sealed public build through archive recovery in a disposable directory."""
import hashlib
import importlib.util
import pathlib
import tempfile
import zipfile

root = pathlib.Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("recovery", root / "scripts/recover-docs-snapshot.py")
r = importlib.util.module_from_spec(spec)
spec.loader.exec_module(r)
dist = root / "dist"
raw = (dist / r.MANIFEST).read_bytes()
import json
identity = json.loads(raw)
with tempfile.TemporaryDirectory(prefix="docs-real-rehearsal-") as td:
    tmp = pathlib.Path(td)
    archive = tmp / "snapshot.zip"
    with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED) as z:
        for path in sorted(dist.rglob("*")):
            if path.is_file():
                z.write(path, path.relative_to(dist).as_posix())
    result = r.recover(archive, tmp / "restored", r.sha(archive.read_bytes()), r.sha(raw), identity["sourceCommit"])
    for e in identity["files"]:
        assert (dist / e["path"]).read_bytes() == (tmp / "restored" / e["path"]).read_bytes()
    assert (tmp / "restored" / r.MANIFEST).read_bytes() == raw
    print("Actual static artifact recovery rehearsal passed:", result["fileCount"], "files; exact sealed bytes.")
