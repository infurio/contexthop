"""Render reviewed version notes together with installation and source details."""
import argparse
import os
from pathlib import Path
import re


def render(tag, sha, root):
    if not re.fullmatch(r"v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)", tag):
        raise ValueError("expected a stable vMAJOR.MINOR.PATCH tag")
    if not re.fullmatch(r"[0-9a-f]{40}", sha):
        raise ValueError("expected a source commit SHA")
    notes = (root / "docs/releases" / f"{tag[1:]}.md").read_text().strip()
    if not notes:
        raise ValueError("release notes must not be empty")
    return (notes + f"\n\n## Installation and downloads\n\n"
            f"ContextHop {tag[1:]} for macOS (Apple Silicon), with zsh shell integration.\n\n"
            "Install: `brew install infurio/tap/contexthop`\n\n"
            "Upgrade: `brew update && brew upgrade infurio/tap/contexthop`\n\n"
            f"Source commit: {sha}\n\n"
            "SHA-256 checksums are included in checksums.txt. Provider CLIs are installed separately.\n")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("tag")
    args = parser.parse_args()
    print(render(args.tag, os.environ["GITHUB_SHA"], Path(__file__).resolve().parents[2]), end="")
