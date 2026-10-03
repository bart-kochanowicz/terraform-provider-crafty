"""Regression checks for corrupted or incomplete release packages."""

import hashlib
import json
import os
from pathlib import Path
import tempfile
import shutil
import subprocess
import unittest
from unittest.mock import patch
import zipfile

from verify_release import PLATFORMS, PROVIDER, verify_archives, verify_signature


class ReleaseArchiveTest(unittest.TestCase):
    def packages(self, directory):
        version = "0.1.0-SNAPSHOT"
        (directory / "metadata.json").write_text(json.dumps({"version": version}))
        lines = []
        manifest = directory / f"{PROVIDER}_{version}_manifest.json"
        manifest.write_text(json.dumps({"version": 1, "metadata": {"protocol_versions": ["6.0"]}}))
        lines.append(hashlib.sha256(manifest.read_bytes()).hexdigest() + "  " + manifest.name)
        for system, arch in sorted(PLATFORMS):
            archive = directory / f"{PROVIDER}_{version}_{system}_{arch}.zip"
            binary = f"{PROVIDER}_v{version}" + (".exe" if system == "windows" else "")
            with zipfile.ZipFile(archive, "w") as package:
                package.writestr(binary, b"test binary")
                package.writestr("LICENSE", (Path(__file__).resolve().parents[1] / "LICENSE").read_bytes())
            lines.append(hashlib.sha256(archive.read_bytes()).hexdigest() + "  " + archive.name)
        checksums = directory / f"{PROVIDER}_{version}_SHA256SUMS"
        checksums.write_text("\n".join(lines) + "\n")
        return version, checksums

    def test_complete_archives_pass(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            version, _ = self.packages(directory)
            self.assertEqual(verify_archives(directory), version)

    def test_manifest_requires_supported_protocol_and_matching_checksum(self):
        for invalid in ("protocol", "checksum"):
            with self.subTest(invalid=invalid), tempfile.TemporaryDirectory() as temporary:
                directory = Path(temporary)
                version, _ = self.packages(directory)
                manifest = directory / f"{PROVIDER}_{version}_manifest.json"
                manifest.write_text(json.dumps({"version": 1, "metadata": {"protocol_versions": ["5.0" if invalid == "protocol" else "6.0"]}}, indent=2))
                with self.assertRaisesRegex(RuntimeError, "Registry manifest"):
                    verify_archives(directory)

    def test_downloaded_release_requires_manifest_asset(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            version, _ = self.packages(directory)
            (directory / f"{PROVIDER}_{version}_manifest.json").unlink()
            with self.assertRaises(FileNotFoundError):
                verify_archives(directory)

    @unittest.skipUnless(shutil.which("gpg"), "GPG is needed for detached-signature integration tests")
    def test_real_signature_rejects_wrong_key_missing_armor_and_modified_checksums(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            home = directory / "gnupg"
            home.mkdir(mode=0o700)
            with patch.dict(os.environ, {"GNUPGHOME": str(home)}):
                try:
                    subprocess.run(["gpg", "--batch", "--pinentry-mode", "loopback", "--passphrase", "", "--quick-generate-key", "Crafty test <test@example.invalid>", "rsa2048", "sign", "0"], capture_output=True, check=True, timeout=60)
                    keys = subprocess.check_output(["gpg", "--batch", "--with-colons", "--list-keys"], text=True, stderr=subprocess.DEVNULL)
                    fingerprint = next(line.split(":")[9] for line in keys.splitlines() if line.startswith("fpr:"))
                    version, checksums = self.packages(directory)
                    signature = Path(str(checksums) + ".sig")
                    subprocess.run(["gpg", "--batch", "--no-armor", "--local-user", fingerprint, "--output", str(signature), "--detach-sign", str(checksums)], capture_output=True, check=True)
                    verify_signature(directory, version, fingerprint)
                    with self.assertRaisesRegex(RuntimeError, "unexpected signing key"):
                        verify_signature(directory, version, "0" * 40)
                    checksums.write_text(checksums.read_text() + "changed\n")
                    with self.assertRaisesRegex(RuntimeError, "signature is invalid"):
                        verify_signature(directory, version, fingerprint)
                    signature.write_bytes(b"-----BEGIN PGP SIGNATURE-----")
                    with self.assertRaisesRegex(RuntimeError, "ASCII armor"):
                        verify_signature(directory, version, fingerprint)
                    signature.unlink()
                    with self.assertRaisesRegex(RuntimeError, "Missing SHASUMS"):
                        verify_signature(directory, version, fingerprint)
                finally:
                    subprocess.run(["gpgconf", "--kill", "gpg-agent"], capture_output=True)

    def test_missing_platform_or_checksum_is_rejected(self):
        for missing in ("archive", "checksum", "duplicate checksum"):
            with self.subTest(missing=missing), tempfile.TemporaryDirectory() as temporary:
                directory = Path(temporary)
                _, checksums = self.packages(directory)
                if missing == "archive":
                    next(directory.glob("*.zip")).unlink()
                else:
                    lines = checksums.read_text().splitlines()
                    checksums.write_text("\n".join(lines[1:] if missing == "checksum" else lines + [lines[0]]))
                with self.assertRaises(RuntimeError):
                    verify_archives(directory)

    def test_corrupted_archive_is_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            self.packages(directory)
            next(directory.glob("*.zip")).write_bytes(b"corrupted")
            with self.assertRaisesRegex(RuntimeError, "Checksum mismatch"):
                verify_archives(directory)

    def test_missing_or_changed_license_is_rejected_with_matching_checksum(self):
        for license_text in (None, b"different license"):
            with self.subTest(license_text=license_text), tempfile.TemporaryDirectory() as temporary:
                directory = Path(temporary)
                _, checksums = self.packages(directory)
                archive = next(directory.glob("*.zip"))
                with zipfile.ZipFile(archive) as package:
                    binary = next(name for name in package.namelist() if name != "LICENSE")
                with zipfile.ZipFile(archive, "w") as package:
                    package.writestr(binary, b"test binary")
                    if license_text is not None:
                        package.writestr("LICENSE", license_text)
                lines = [hashlib.sha256(archive.read_bytes()).hexdigest() + "  " + archive.name if line.split()[1] == archive.name else line for line in checksums.read_text().splitlines()]
                checksums.write_text("\n".join(lines))
                with self.assertRaisesRegex(RuntimeError, "Unexpected archive contents|Archive license"):
                    verify_archives(directory)

    def test_wrong_binary_name_is_rejected_even_with_matching_checksum(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            _, checksums = self.packages(directory)
            archive = next(directory.glob("*.zip"))
            with zipfile.ZipFile(archive, "w") as package:
                package.writestr("wrong-version-binary", b"test binary")
                package.writestr("LICENSE", (Path(__file__).resolve().parents[1] / "LICENSE").read_bytes())
            lines = [hashlib.sha256(archive.read_bytes()).hexdigest() + "  " + archive.name if line.split()[1] == archive.name else line for line in checksums.read_text().splitlines()]
            checksums.write_text("\n".join(lines))
            with self.assertRaisesRegex(RuntimeError, "Unexpected archive contents"):
                verify_archives(directory)
