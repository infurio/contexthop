#!/usr/bin/env python3
"""Publish a reviewed, committed version locally without rerunning development tests."""
import argparse
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import tarfile
import tempfile
import time

ROOT = Path(__file__).resolve().parents[2]
SOURCE = "infurio/contexthop"
TAP = "infurio/homebrew-tap"
URL = f"https://github.com/{SOURCE}.git"


def run(*args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def output(*args):
    return subprocess.check_output(args, text=True).strip()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version", help="unused stable version, e.g. v0.7.5")
    parser.add_argument("--prepare", action="store_true", help="build and verify only; no GitHub access or publication")
    args = parser.parse_args()
    tag = args.version
    if not re.fullmatch(r"v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)", tag):
        parser.error("expected a stable vMAJOR.MINOR.PATCH version")
    started = time.monotonic()
    os.chdir(ROOT)
    if platform.system() != "Darwin" or platform.machine() != "arm64":
        parser.error("release on an Apple Silicon Mac")
    if output("git", "branch", "--show-current") != "main":
        parser.error("switch to main before releasing")
    if output("git", "status", "--porcelain"):
        parser.error("review and commit your changes before releasing")
    sha = output("git", "rev-parse", "HEAD")
    destination = ROOT / "dist" / tag
    if destination.exists():
        parser.error(f"{destination} already exists; inspect it before retrying")
    env = dict(os.environ, TAG=tag, GITHUB_SHA=sha, GITHUB_REPOSITORY=SOURCE,
               TAP_REPO=TAP, GOCACHE=str(ROOT / ".gocache"))
    # Validate notes before doing any work. Their contents must be reviewed with
    # the commit, just like all other publication content.
    run("python3", "scripts/release/release_notes.py", tag, env=env, stdout=subprocess.DEVNULL)
    if not args.prepare:
        run("gh", "auth", "status", stdout=subprocess.DEVNULL)
        if output("git", "ls-remote", "--tags", URL, f"refs/tags/{tag}"):
            parser.error("remote tag already exists; never overwrite a published version")
        for repo in (SOURCE, TAP):
            tags = output("gh", "api", "--paginate", f"repos/{repo}/releases?per_page=100", "--jq", ".[].tag_name").splitlines()
            if tag in tags:
                parser.error(f"{tag} already exists in {repo}")
    # Build exactly the committed source, independent of subsequent edits.
    with tempfile.TemporaryDirectory(prefix="contexthop-release-") as temp:
        snapshot = Path(temp) / "source"
        snapshot.mkdir()
        archive = Path(temp) / "source.tar"
        run("git", "archive", "--format=tar", "-o", str(archive), sha)
        with tarfile.open(archive) as bundle:
            bundle.extractall(snapshot, filter="data")
        run("gitleaks", "dir", "--redact", "--no-banner", str(snapshot), stdout=subprocess.DEVNULL)
        run("python3", str(snapshot / "scripts/release/package_release.py"), tag, env=env, cwd=snapshot)
        run("python3", str(snapshot / "scripts/release/verify_release.py"), tag, env=env, cwd=snapshot)
        shutil.copytree(snapshot / "dist" / tag, destination)
        if args.prepare:
            print(f"Prepared {destination} in {time.monotonic() - started:.1f}s; nothing published.")
            return
        # An atomic, non-force push rejects divergent main or an existing tag.
        # Push the captured SHA, never an unchecked later HEAD.
        run("git", "-c", "credential.helper=", "-c", "credential.helper=!gh auth git-credential",
            "push", "--atomic", URL, f"{sha}:refs/heads/main", f"{sha}:refs/tags/{tag}")
        # Use the same committed scripts and bundle throughout publication.
        run("bash", "scripts/release/publish-release.sh", cwd=snapshot, env=env)
        run("python3", "scripts/release/update_tap.py", tag, cwd=snapshot, env=env)
        print(f"{tag} is available through Brew ({time.monotonic() - started:.1f}s).", flush=True)
        run("bash", "scripts/release/publish-source-release.sh", cwd=snapshot, env=env)
    print("brew update && brew upgrade infurio/tap/contexthop")


if __name__ == "__main__":
    try:
        main()
    except (subprocess.CalledProcessError, OSError) as error:
        raise SystemExit(f"Release stopped: {error}. Inspect the completed steps before retrying; see RELEASE.md.")
