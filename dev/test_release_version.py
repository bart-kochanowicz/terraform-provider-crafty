"""Guard against publishing inconsistent versions or unrelated release notes."""

from pathlib import Path
import tempfile
import unittest

from release_version import check_version


class ReleaseVersionTest(unittest.TestCase):
    def fixture(self, root):
        (root / "Makefile").write_text("VERSION ?= 0.1.0\n")
        for example in ("local", "docker"):
            path = root / f"examples/{example}/main.tf"
            path.parent.mkdir(parents=True)
            path.write_text('crafty = {\n version = "0.1.0"\n}\n')
        (root / "docs").mkdir()
        (root / "docs/releasing.md").write_text("## Install v0.1.0 from GitHub Releases\nversion=0.1.0\n$version = '0.1.0'\n")
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
        for filename in ("Makefile", "examples/local/main.tf", "examples/docker/main.tf", "docs/releasing.md", "CHANGELOG.md"):
            with self.subTest(filename=filename), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                self.fixture(root)
                path = root / filename
                path.write_text(path.read_text().replace("0.1.0", "0.2.0"))
                with self.assertRaises(RuntimeError):
                    check_version(root)

    def test_rejects_outdated_installation_command(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self.fixture(root)
            path = root / "docs/releasing.md"
            path.write_text(path.read_text().replace("$version = '0.1.0'", "$version = '0.2.0'"))
            with self.assertRaisesRegex(RuntimeError, "Installation command"):
                check_version(root)
