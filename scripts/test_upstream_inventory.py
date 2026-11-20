import json
import tempfile
import unittest
from pathlib import Path

from upstream_inventory import classify, read_policy, stable_tags, version


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
