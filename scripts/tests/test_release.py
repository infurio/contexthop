"""Exercise release ordering and failures without network access or publication."""
import importlib.util
from importlib.machinery import SourceFileLoader
import base64
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_loader("local_release", SourceFileLoader("local_release", str(Path(__file__).resolve().parents[1] / "release-local")))
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
        self.asset = self.root / "dist/v1.2.3/asset.tar.gz"

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
        if args[:2] == ("gh", "api") and "--paginate" not in args:
            return json.dumps({"sha": "fixture", "content": base64.b64encode(b'  version "1.2.2"\n').decode()})
        return ""

    def fake_run(self, *args, **kwargs):
        self.calls.append(args)
        if args[:2] == ("git", "archive"):
            with tarfile.open(args[args.index("-o") + 1], "w"):
                pass
        if "push" in args and self.fail_push:
            raise subprocess.CalledProcessError(1, args)

    def fake_build(self, source, out, version):
        self.calls.append(("build",))
        if self.fail_build:
            raise subprocess.CalledProcessError(1, ["go", "build"])
        self.asset.write_text("fictional artifact")
        (out / "contexthop.rb").write_text("fictional formula")
        return self.asset

    def invoke(self, *extra):
        cwd = Path.cwd()
        try:
            with patch.object(release, "ROOT", self.root), patch.object(release, "run", self.fake_run), patch.object(release, "output", self.output), patch.object(release, "build", self.fake_build), patch.object(release.platform, "system", return_value="Darwin"), patch.object(release.platform, "machine", return_value="arm64"), patch.object(sys, "argv", ["release", "v1.2.3", *extra]):
                release.main()
        finally:
            os.chdir(cwd)

    def test_prepare_never_accesses_github_or_pushes(self):
        self.invoke("--prepare")
        self.assertFalse(any(c[0] == "gh" or "push" in c or "ls-remote" in c for c in self.calls))
        self.assertTrue((self.asset).exists())

    def test_dirty_checkout_stops_before_build_or_publication(self):
        self.dirty = True
        with self.assertRaises((SystemExit, ValueError)):
            self.invoke()
        self.assertFalse(any(c[0] in ("python3", "gh") for c in self.calls))

    def test_existing_tag_stops_before_build(self):
        self.existing = True
        with self.assertRaises((SystemExit, ValueError)):
            self.invoke()
        self.assertFalse(any("push" in c or c == ("build",) for c in self.calls))

    def test_build_failure_never_pushes(self):
        self.fail_build = True
        with self.assertRaises(subprocess.CalledProcessError):
            self.invoke()
        self.assertFalse(any("push" in c for c in self.calls))

    def test_push_failure_never_uploads(self):
        self.fail_push = True
        with self.assertRaises(subprocess.CalledProcessError):
            self.invoke()
        self.assertFalse(any(c[:3] == ("gh", "release", "create") for c in self.calls))

    def test_verified_commit_is_pushed_atomically_before_upload_and_tap(self):
        self.invoke()
        push = next(i for i, c in enumerate(self.calls) if "push" in c)
        self.assertIn("--atomic", self.calls[push])
        self.assertIn("a" * 40 + ":refs/heads/main", self.calls[push])
        verify = self.calls.index(("build",))
        publish = next(i for i, c in enumerate(self.calls) if c[:3] == ("gh", "release", "create"))
        tap = next(i for i, c in enumerate(self.calls) if "PUT" in c)
        self.assertLess(verify, push)
        self.assertLess(push, publish)
        self.assertLess(publish, tap)


class Packaging(unittest.TestCase):
    def test_archive_and_smoke_tests_are_isolated(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            source, out = root / "source", root / "out"
            source.mkdir()
            out.mkdir()
            (source / ".go-version").write_text("1.26.7")
            for name in ("LICENSE", "THIRD_PARTY_NOTICES"):
                (source / name).write_text("Fictional notice")
            environments = []

            def execute(*args, **kwargs):
                if args[0] == "go":
                    Path(args[args.index("-o") + 1]).write_bytes(b"fictional executable")
                else:
                    environments.append(kwargs["env"])

            def read(*args, **kwargs):
                environments.append(kwargs["env"])
                return "contexthop 1.2.3" if args[1] == "version" else "Usage: chop"

            with patch.object(release, "run", execute), patch.object(release, "output", read):
                archive = release.build(source, out, "1.2.3")
            with tarfile.open(archive) as bundle:
                self.assertEqual(bundle.getnames(), ["chop", "LICENSE", "THIRD_PARTY_NOTICES"])
                for member in bundle.getmembers():
                    self.assertEqual((member.uid, member.gid, member.uname, member.gname), (0, 0, "", ""))
            self.assertEqual(len(environments), 3)
            for env in environments:
                self.assertEqual(set(env), {"HOME", "PATH", "TERM", "CONTEXTHOP_CACHE_DIR"})
            import hashlib
            digest = hashlib.sha256(archive.read_bytes()).hexdigest()
            self.assertIn(digest, (out / "contexthop.rb").read_text())
            self.assertIn(digest, (out / "checksums.txt").read_text())
