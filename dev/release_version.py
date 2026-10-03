"""Check the intended stable release version and extract its changelog entry."""

import argparse
from pathlib import Path
import re


def check_version(root, tag=None):
    match = re.search(r"^VERSION \?= (\d+\.\d+\.\d+)$", (root / "Makefile").read_text(), re.M)
    if match is None:
        raise RuntimeError("Makefile must define a stable default VERSION")
    version = match[1]
    if tag is not None and tag != f"v{version}":
        raise RuntimeError(f"Release tag {tag} does not match project version v{version}")
    changelog = (root / "CHANGELOG.md").read_text()
    entry = re.search(r"^## " + re.escape(version) + r" — \d{4}-\d{2}-\d{2}\n(.*?)(?=^## |\Z)", changelog, re.M | re.S)
    if entry is None or not entry[1].strip():
        raise RuntimeError(f"Changelog lacks finalized notes for {version}")
    notes = entry[1].strip()
    return version, notes.strip() + "\n"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tag")
    parser.add_argument("--print-tag", action="store_true", help="Print only the validated tag for automation")
    parser.add_argument("--notes-output", type=Path)
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    version, notes = check_version(root, args.tag)
    if args.notes_output:
        args.notes_output.write_text(notes)
    if args.print_tag:
        print(f"v{version}")
    else:
        print(f"Stable release version {version}: Makefile and finalized changelog agree")


if __name__ == "__main__":
    main()
