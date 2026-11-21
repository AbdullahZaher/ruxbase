import json
import tempfile
import unittest
from pathlib import Path

from upstream_inventory import (
    annotated_paths,
    classify,
    git,
    module_path_only,
    read_policy,
    source_review,
    stable_tags,
    version,
)


class InventoryTests(unittest.TestCase):
    def test_numeric_tag_order_and_only_stable_shape(self):
        raw = (
            b"a\trefs/tags/v0.40.9\n"
            b"b\trefs/tags/v0.40.10\n"
            b"c\trefs/tags/v0.41.0-rc.1\n"
            b"d\trefs/tags/v0.22.99\n"
            b"e\trefs/tags/v0.040.1\n"
        )
        self.assertEqual(stable_tags(raw), ["v0.22.99", "v0.40.9", "v0.40.10"])
        with self.assertRaises(ValueError):
            version("v0.41.0-rc.1")

    def test_divergence_inventory(self):
        rows = classify(
            {"same": "a", "edit": "a", "removed": "a", "port": "a"},
            {"same": "a", "edit": "b", "new": "c", "port": "a"},
            {"same": "a", "edit": "c", "removed": "a", "port": "b", "future": "d"},
        )
        by_path = {row["path"]: row for row in rows}
        self.assertEqual(by_path["same"]["status"], "IDENTICAL")
        self.assertEqual(by_path["new"]["status"], "RUXBASE_ADDED")
        self.assertEqual(by_path["removed"]["status"], "RUXBASE_REMOVED")
        self.assertEqual(by_path["edit"]["review"], "REVIEW_DIVERGENCE")
        self.assertEqual(by_path["port"]["review"], "REVIEW_UPSTREAM_CHANGE")
        self.assertEqual(by_path["future"]["review"], "REVIEW_UPSTREAM_CHANGE")

    def test_source_review_tracks_inherited_go_and_manifest_changes(self):
        rows = classify(
            {
                "LICENSE.md": "a", "legacy/COPYING": "a", "go.mod": "a",
                "core/annotated.go": "a", "core/plain.go": "a",
                "core/plain_test.go": "a", "core/unchanged.go": "a", "core/import.go": "a",
            },
            {
                "LICENSE.md": "a", "go.mod": "b", "ui/package-lock.json": "b",
                "core/annotated.go": "b", "core/plain.go": "b",
                "core/plain_test.go": "b", "core/unchanged.go": "a", "core/import.go": "b",
                "core/new.go": "b",
            },
        )
        review = source_review(
            rows, {"core/annotated.go", "core/unchanged.go", "core/new.go"},
            lambda row: row["path"] == "core/import.go",
        )
        self.assertEqual(review["inherited_go_with_annotation"], ["core/annotated.go"])
        self.assertEqual(review["inherited_go_without_annotation"], ["core/plain.go"])
        self.assertEqual(review["inherited_go_module_path_only"], ["core/import.go"])
        self.assertEqual(review["inherited_go_tests"], ["core/plain_test.go"])
        self.assertEqual(
            {row["path"]: row["status"] for row in review["license_and_dependency_files"]},
            {
                "LICENSE.md": "IDENTICAL",
                "go.mod": "RUXBASE_MODIFIED",
                "legacy/COPYING": "RUXBASE_REMOVED",
                "ui/package-lock.json": "RUXBASE_ADDED",
            },
        )

    def test_annotation_scan_uses_committed_go_blobs(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            git(root, "init", "-q")
            (root / "baseline.go").write_text("package example\n// ruxbase:   \n", encoding="utf-8")
            (root / "NOTICE.md").write_text("// ruxbase: ignored extension\n", encoding="utf-8")
            git(root, "add", ".")
            git(root, "-c", "user.name=Inventory Test", "-c", "user.email=test@example.invalid",
                "commit", "-qm", "baseline")
            self.assertEqual(annotated_paths(root, "HEAD"), set())
            (root / "with space.go").write_text("package example\n// ruxbase: evidence\n",
                                               encoding="utf-8")
            git(root, "add", ".")
            git(root, "-c", "user.name=Inventory Test", "-c", "user.email=test@example.invalid",
                "commit", "-qm", "annotated")
            (root / "with space.go").unlink()
            self.assertEqual(annotated_paths(root, "HEAD"), {"with space.go"})

    def test_module_path_only_requires_exact_blob_and_mode_match(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            git(root, "init", "-q")
            path = root / "entry.go"

            def store_blob(source):
                path.write_text(source, encoding="utf-8")
                return git(root, "hash-object", "-w", str(path)).decode().strip()

            original = store_blob('package example\nimport "github.com/pocketbase/pocketbase/core"\n')
            changed = store_blob('package example\nimport "github.com/AbdullahZaher/ruxbase/core"\n')
            unrelated = store_blob('package example\nimport "github.com/AbdullahZaher/ruxbase/cmd"\n')
            row = {"baseline": ("100644", "blob", original)}
            row["ruxbase"] = ("100644", "blob", changed)
            self.assertTrue(module_path_only(root, root, row))
            row["ruxbase"] = ("100644", "blob", unrelated)
            self.assertFalse(module_path_only(root, root, row))
            row["ruxbase"] = ("100755", "blob", changed)
            self.assertFalse(module_path_only(root, root, row))

    def test_policy_validation(self):
        root = Path(__file__).resolve().parents[1]
        original = json.loads((root / "UPSTREAM.lock.json").read_text())
        self.assertTrue(read_policy(root)["sole_upstream"])
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)
            lock = path / "UPSTREAM.lock.json"
            original["policy"]["automatic_merge"] = True
            lock.write_text(json.dumps(original))
            with self.assertRaises(ValueError):
                read_policy(path)
            original["policy"]["automatic_merge"] = False
            original["upstream"]["repository"] = "https://github.com/example/other"
            lock.write_text(json.dumps(original))
            with self.assertRaises(ValueError):
                read_policy(path)
            original["upstream"]["repository"] = "https://github.com/pocketbase/pocketbase"
            original["upstream"]["reference_commit"] = "main"
            lock.write_text(json.dumps(original))
            with self.assertRaises(ValueError):
                read_policy(path)


if __name__ == "__main__":
    unittest.main()
