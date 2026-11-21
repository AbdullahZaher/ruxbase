import hashlib
import importlib.util
import json
import pathlib
import tempfile
import unittest
import zipfile

spec = importlib.util.spec_from_file_location("recovery", pathlib.Path(__file__).resolve().parents[1] / "scripts/recover-docs-snapshot.py")
r = importlib.util.module_from_spec(spec)
spec.loader.exec_module(r)

class Recovery(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        self.id = "a" * 40
        self.data = {name: b"test" for name in ["index.html", "llms.txt", "openapi.json",
            "reference/api.md", "pagefind/pagefind.js", "sitemap-index.xml"]}
        identity = {"sourceCommit": self.id, "sourceTree": self.id, "documentationStatus": "development", "releaseVersion": None}
        self.data["version.json"] = json.dumps({**identity, "dirty": False}).encode()
        self.data["versions.json"] = json.dumps({**identity, "channels": {"stable": None}, "releases": []}).encode()

    def archive(self, extra=None, change=None):
        entries = [{"path": name, "bytes": len(data), "sha256": r.sha(data)} for name, data in sorted(self.data.items())]
        m = {"schemaVersion": 1, "product": "Ruxbase", "sourceCommit": self.id, "sourceTree": self.id,
             "documentationStatus": "development", "releaseVersion": None, "selfExcluded": r.MANIFEST,
             "fileCount": len(entries), "totalBytes": sum(x["bytes"] for x in entries), "files": entries}
        raw = json.dumps(m).encode()
        zpath = self.root / "snapshot.zip"
        with zipfile.ZipFile(zpath, "w") as z:
            for name, data in self.data.items():
                z.writestr(name, data if name != change else b"modified")
            z.writestr(r.MANIFEST, raw)
            if extra:
                z.writestr(extra, b"unknown")
        return zpath, r.sha(zpath.read_bytes()), r.sha(raw)

    def run_recover(self, archive, digest, manifest, commit=None):
        return r.recover(archive, self.root / "restored", digest, manifest, commit or self.id)

    def test_exact_restore(self):
        a, d, m = self.archive()
        result = self.run_recover(a, d, m)
        self.assertEqual(result["fileCount"], len(self.data))
        self.assertEqual((self.root / "restored/index.html").read_bytes(), b"test")
        with self.assertRaisesRegex(ValueError, "Destination already exists"):
            self.run_recover(a, d, m)

    def test_external_identity(self):
        a, d, m = self.archive()
        for digest, manifest, commit, pattern in [
            ("0"*64, m, self.id, "Archive digest"), (d, "0"*64, self.id, "Manifest digest"),
            (d, m, "b"*40, "Unexpected source")]:
            with self.assertRaisesRegex(ValueError, pattern):
                self.run_recover(a, digest, manifest, commit)
            self.assertFalse((self.root / "restored").exists())

    def test_modified_file(self):
        a, d, m = self.archive(change="index.html")
        with self.assertRaisesRegex(ValueError, "File digest mismatch"):
            self.run_recover(a, d, m)
        self.assertFalse((self.root / "restored").exists())

    def test_unexpected_member(self):
        a, d, m = self.archive(extra="unexpected.txt")
        with self.assertRaisesRegex(ValueError, "Archive file set mismatch"):
            self.run_recover(a, d, m)

    def test_excluded_viewer_even_if_sealed(self):
        self.data["api-viewer/index.html"] = b"unreviewed"
        a, d, m = self.archive()
        with self.assertRaisesRegex(ValueError, "excluded"):
            self.run_recover(a, d, m)

    def test_unsafe_path(self):
        a, d, m = self.archive(extra="../outside.txt")
        with self.assertRaisesRegex(ValueError, "Unsafe"):
            self.run_recover(a, d, m)
        self.assertFalse((self.root / "outside.txt").exists())

if __name__ == "__main__":
    unittest.main()
