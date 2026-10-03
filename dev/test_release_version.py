"""Guard against publishing inconsistent versions or unrelated release notes."""

from pathlib import Path
import tempfile
import subprocess
import sys
import unittest

from release_version import check_version


class ReleaseVersionTest(unittest.TestCase):
    def fixture(self, root):
        (root / "Makefile").write_text("VERSION ?= 0.1.0\n")
        (root / "CHANGELOG.md").write_text("## Unreleased\nFuture changes\n\n## 0.1.0 — 2026-10-03\nInitial notes\n\n## 0.0.1 — 2026-01-01\nOlder changes\n")

    def test_extracts_only_selected_release(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self.fixture(root)
            self.assertEqual(check_version(root, "v0.1.0"), ("0.1.0", "Initial notes\n"))

    def test_rejects_wrong_tag(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self.fixture(root)
            with self.assertRaisesRegex(RuntimeError, "Release tag"):
                check_version(root, "v0.2.0")

    def test_rejects_inconsistent_versions_or_unfinished_notes(self):
        for filename in ("Makefile", "CHANGELOG.md"):
            with self.subTest(filename=filename), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                self.fixture(root)
                path = root / filename
                path.write_text(path.read_text().replace("0.1.0", "0.2.0"))
                with self.assertRaises(RuntimeError):
                    check_version(root)

    def test_next_patch_needs_only_makefile_and_changelog(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self.fixture(root)
            (root / "Makefile").write_text("VERSION ?= 0.1.1\n")
            path = root / "CHANGELOG.md"
            path.write_text("## 0.1.1 — 2026-10-04\nPatch notes\n\n" + path.read_text())
            self.assertEqual(check_version(root), ("0.1.1", "Patch notes\n"))

    def test_rejects_empty_notes(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self.fixture(root)
            (root / "CHANGELOG.md").write_text("## 0.1.0 — 2026-10-03\n\n")
            with self.assertRaisesRegex(RuntimeError, "finalized notes"):
                check_version(root)

    def test_cli_emits_only_validated_tag_and_selected_notes(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self.fixture(root)
            script = root / "dev/release_version.py"
            script.parent.mkdir()
            script.write_text(Path(__file__).with_name("release_version.py").read_text())
            notes = root / "notes.md"
            result = subprocess.run([sys.executable, str(script), "--print-tag", "--notes-output", str(notes)], capture_output=True, text=True, check=True)
            self.assertEqual(result.stdout, "v0.1.0\n")
            self.assertEqual(notes.read_text(), "Initial notes\n")
            result = subprocess.run([sys.executable, str(script), "--print-tag", "--tag", "v0.2.0"], capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(result.stdout, "")
