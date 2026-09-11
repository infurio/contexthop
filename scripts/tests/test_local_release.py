"""Exercise release ordering and failures without network access or publication."""
import importlib.util
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("local_release", Path(__file__).resolve().parents[1] / "release/local_release.py")
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


class LocalRelease(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.calls = []
        self.dirty = False
        self.existing = False
        self.fail_build = False
        self.fail_push = False

    def output(self, *args):
        self.calls.append(args)
        if args[:3] == ("git", "branch", "--show-current"):
            return "main"
        if args[:3] == ("git", "status", "--porcelain"):
            return " M fictional.go" if self.dirty else ""
        if args[:3] == ("git", "rev-parse", "HEAD"):
            return "a" * 40
        if args[:2] == ("git", "ls-remote"):
            return "existing-tag" if self.existing else ""
        return ""

    def fake_run(self, *args, **kwargs):
        self.calls.append(args)
        if args[:2] == ("git", "archive"):
            with tarfile.open(args[args.index("-o") + 1], "w"):
                pass
        if args[0] == "python3" and args[1].endswith("package_release.py"):
            if self.fail_build:
                raise subprocess.CalledProcessError(1, args)
            bundle = Path(kwargs["cwd"]) / "dist/v1.2.3"
            bundle.mkdir(parents=True)
            (bundle / "fictional-artifact").write_text("test fixture")
        if "push" in args and self.fail_push:
            raise subprocess.CalledProcessError(1, args)

    def invoke(self, *extra):
        cwd = Path.cwd()
        try:
            with patch.object(release, "ROOT", self.root), patch.object(release, "run", self.fake_run), patch.object(release, "output", self.output), patch.object(release.platform, "system", return_value="Darwin"), patch.object(release.platform, "machine", return_value="arm64"), patch.object(sys, "argv", ["release", "v1.2.3", *extra]):
                release.main()
        finally:
            os.chdir(cwd)

    def test_prepare_never_accesses_github_or_pushes(self):
        self.invoke("--prepare")
        self.assertFalse(any(c[0] == "gh" or "push" in c or "ls-remote" in c for c in self.calls))
        self.assertTrue((self.root / "dist/v1.2.3/fictional-artifact").exists())

    def test_dirty_checkout_stops_before_build_or_publication(self):
        self.dirty = True
        with self.assertRaises(SystemExit):
            self.invoke()
        self.assertFalse(any(c[0] in ("python3", "gh") for c in self.calls))

    def test_existing_tag_stops_before_build(self):
        self.existing = True
        with self.assertRaises(SystemExit):
            self.invoke()
        self.assertFalse(any("push" in c or any("package_release" in a for a in c) for c in self.calls))

    def test_build_failure_never_pushes(self):
        self.fail_build = True
        with self.assertRaises(subprocess.CalledProcessError):
            self.invoke()
        self.assertFalse(any("push" in c for c in self.calls))

    def test_push_failure_never_uploads(self):
        self.fail_push = True
        with self.assertRaises(subprocess.CalledProcessError):
            self.invoke()
        self.assertFalse(any(c[0] == "bash" for c in self.calls))

    def test_verified_commit_is_pushed_atomically_before_upload_and_tap(self):
        self.invoke()
        push = next(i for i, c in enumerate(self.calls) if "push" in c)
        self.assertIn("--atomic", self.calls[push])
        self.assertIn("a" * 40 + ":refs/heads/main", self.calls[push])
        verify = next(i for i, c in enumerate(self.calls) if any("verify_release.py" in a for a in c))
        publish = next(i for i, c in enumerate(self.calls) if "scripts/release/publish-release.sh" in c)
        tap = next(i for i, c in enumerate(self.calls) if "scripts/release/update_tap.py" in c)
        self.assertLess(verify, push)
        self.assertLess(push, publish)
        self.assertLess(publish, tap)
