import hashlib
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch
from zipfile import ZipFile

from release_go_inventory import inspect_archive


class ReleaseGoInventoryTest(unittest.TestCase):
    def test_inspects_executable_in_windows_importer(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "ruxbase-import_v1_windows_amd64.zip"
            binary = b"binary fixture"
            with ZipFile(path, "w") as archive:
                archive.writestr("ruxbase-import.exe", binary)
            rows = ["\tpath\tmodule/cmd/import", "\tdep\tmodule/lib\tv1.2.3",
                    "\t=>\tmodule/fork\tv1.2.4\th1:sum"]
            with patch("release_go_inventory.subprocess.check_output") as go:
                go.return_value = "binary: go1.27.4\n" + "\n".join(rows)
                result = inspect_archive(path)
            self.assertEqual(result["binary_sha256"], hashlib.sha256(binary).hexdigest())
            self.assertEqual(result["archive_sha256"], hashlib.sha256(path.read_bytes()).hexdigest())
            self.assertEqual(result["toolchain"], "go1.27.4")
            self.assertEqual(result["build_info"], rows)

    def test_missing_executable_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "ruxbase_v1_linux_amd64.zip"
            with ZipFile(path, "w") as archive:
                archive.writestr("NOTICE.md", "fixture")
            with self.assertRaisesRegex(ValueError, "expected exactly one ruxbase"):
                inspect_archive(path)
