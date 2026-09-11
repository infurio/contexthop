"""Regression tests for release publication guards; no GitHub writes or builds."""
import base64
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import tarfile
import shutil
import unittest

SCRIPTS = Path(__file__).resolve().parents[1] / "release"


class ReleaseGuards(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.log = self.root / "calls.jsonl"
        gh = self.root / "gh"
        gh.write_text(f'''#!{sys.executable}
import json, os, sys
from pathlib import Path
args = sys.argv[1:]
with open(os.environ["GH_CALLS"], "a") as log:
    log.write(json.dumps(args) + "\\n")
if args[0] == "api":
    if os.environ.get("API_FAIL"):
        raise SystemExit(1)
    if "--paginate" in args:
        print(os.environ.get("EXISTING_TAGS", ""))
    elif "--method" in args:
        Path(os.environ["PAYLOAD"]).write_text(sys.stdin.read())
    else:
        print(os.environ["CURRENT_FORMULA"])
''')
        gh.chmod(0o755)
        self.env = dict(os.environ, PATH=str(self.root) + os.pathsep + os.environ["PATH"],
                        GH_CALLS=str(self.log), PAYLOAD=str(self.root / "payload.json"),
                        GH_TOKEN="test-placeholder", TAG="v1.2.3", TAP_REPO="example/tap",
                        GITHUB_SHA="a" * 40)
        # An isolated script root lets the update script resolve its candidate.
        scripts = self.root / "scripts/release"
        scripts.mkdir(parents=True)
        self.update = scripts / "update_tap.py"
        self.update.write_text((SCRIPTS / "update_tap.py").read_text())
        out = self.root / "dist" / "v1.2.3"
        out.mkdir(parents=True)
        self.candidate = 'class Contexthop < Formula\n  version "1.2.3"\nend\n'
        (out / "contexthop.rb").write_text(self.candidate)

    def calls(self):
        return [json.loads(line) for line in self.log.read_text().splitlines()] if self.log.exists() else []

    def current(self, formula):
        self.env["CURRENT_FORMULA"] = json.dumps({"sha": "previous-sha", "content":
                                                 base64.b64encode(formula.encode()).decode()})

    def run_update(self):
        return subprocess.run([sys.executable, str(self.update), "v1.2.3"], env=self.env,
                              capture_output=True, text=True)

    def test_existing_release_is_never_overwritten(self):
        self.env["EXISTING_TAGS"] = "v1.2.3"
        result = subprocess.run(["bash", str(SCRIPTS / "publish-release.sh")], env=self.env,
                                capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("refusing to overwrite", result.stderr)
        self.assertEqual(len(self.calls()), 1)

    def test_network_failure_does_not_become_missing_release(self):
        self.env["API_FAIL"] = "1"
        result = subprocess.run(["bash", str(SCRIPTS / "publish-release.sh")], env=self.env,
                                capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(len(self.calls()), 1)

    def test_formula_cannot_downgrade(self):
        self.current('  version "2.0.0"\n')
        result = self.run_update()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("downgrade", result.stderr)
        self.assertEqual(len(self.calls()), 1)

    def test_same_version_different_content_is_rejected(self):
        self.current('  version "1.2.3"\n')
        self.assertNotEqual(self.run_update().returncode, 0)
        self.assertEqual(len(self.calls()), 1)

    def test_identical_formula_retry_does_not_write(self):
        self.current(self.candidate)
        result = self.run_update()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(any("PUT" in call for call in self.calls()))
        self.assertEqual(self.calls()[-1], ["release", "edit", "v1.2.3", "--repo", "example/tap", "--latest"])

    def test_formula_update_uses_current_sha(self):
        self.current('  version "1.2.2"\n')
        result = self.run_update()
        self.assertEqual(result.returncode, 0, result.stderr)
        payload = json.loads((self.root / "payload.json").read_text())
        self.assertEqual(payload["sha"], "previous-sha")
        self.assertEqual(base64.b64decode(payload["content"]).decode(), self.candidate)

    def run_source_release(self):
        env = dict(self.env, GITHUB_REPOSITORY="example/source", CURRENT_FORMULA="Release notes")
        return subprocess.run(["bash", str(SCRIPTS / "publish-source-release.sh")],
                              env=env, capture_output=True, text=True)

    def test_source_release_retry_preserves_existing_release(self):
        self.env["EXISTING_TAGS"] = "v1.2.3"
        self.assertEqual(self.run_source_release().returncode, 0)
        self.assertEqual(len(self.calls()), 1)

    def test_source_release_api_failure_stops_publication(self):
        self.env["API_FAIL"] = "1"
        self.assertNotEqual(self.run_source_release().returncode, 0)
        self.assertEqual(len(self.calls()), 1)

    def test_source_release_requires_existing_tag(self):
        result = self.run_source_release()
        self.assertEqual(result.returncode, 0, result.stderr)
        call = self.calls()[-1]
        self.assertEqual(call[:3], ["release", "create", "v1.2.3"])
        self.assertIn("--verify-tag", call)
        self.assertEqual(call[call.index("--repo") + 1], "example/source")

    def test_invalid_versions_fail_before_build(self):
        for version in ("v01.2.3", "v1.2.3-rc.1", "1.2.3", "v1.2", "v1.2.3;echo unsafe"):
            with self.subTest(version=version):
                result = subprocess.run([sys.executable, str(SCRIPTS / "package_release.py"), version],
                                        capture_output=True, text=True)
                self.assertEqual(result.returncode, 2)
                self.assertIn("stable", result.stderr)


class AppleSiliconBundle(unittest.TestCase):
    def test_packager_and_verifier_reject_extra_architectures(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            scripts = root / "scripts/release"
            scripts.mkdir(parents=True)
            for name in ("package_release.py", "verify_release.py"):
                shutil.copyfile(SCRIPTS / name, scripts / name)
            (root / ".go-version").write_text("1.26.7")
            for name in ("LICENSE", "THIRD_PARTY_NOTICES"):
                shutil.copyfile(SCRIPTS.parents[1] / name, root / name)
            # A fake build checks the requested architecture without compiling
            # or installing anything. Extra files must fail before smoke tests.
            go = root / "go"
            go.write_text(f'''#!{sys.executable}
import os, sys
from pathlib import Path
assert os.environ["GOOS"] == "darwin" and os.environ["GOARCH"] == "arm64"
args = sys.argv[1:]
Path(args[args.index("-o")+1]).write_text("#!/bin/sh\\ncase \\"$1\\" in version) echo 'contexthop 1.2.3';; help) echo 'Usage:';; esac\\n")
''')
            go.chmod(0o755)
            env = dict(os.environ, PATH=str(root) + os.pathsep + os.environ["PATH"])
            package = subprocess.run([sys.executable, str(scripts / "package_release.py"), "v1.2.3"], env=env, capture_output=True, text=True)
            self.assertEqual(package.returncode, 0, package.stderr)
            out = root / "dist/v1.2.3"
            self.assertEqual({p.name for p in out.iterdir()}, {"contexthop_1.2.3_darwin_arm64.tar.gz", "checksums.txt", "contexthop.rb"})
            formula = (out / "contexthop.rb").read_text()
            self.assertIn("depends_on arch: :arm64", formula)
            self.assertNotIn("intel", formula)
            self.assertNotIn("amd64", formula)
            with tarfile.open(out / "contexthop_1.2.3_darwin_arm64.tar.gz") as archive:
                self.assertEqual(archive.getnames(), ["chop", "LICENSE", "THIRD_PARTY_NOTICES"])
                for name in ("LICENSE", "THIRD_PARTY_NOTICES"):
                    self.assertEqual(archive.extractfile(name).read(), (root / name).read_bytes())
                    self.assertEqual(archive.getmember(name).mode, 0o644)
                self.assertIn('license "MIT"', formula)
                self.assertIn('doc.install "LICENSE", "THIRD_PARTY_NOTICES"', formula)
            extra = out / "contexthop_1.2.3_darwin_amd64.tar.gz"
            extra.write_bytes(b"stale Intel build")
            verify = subprocess.run([sys.executable, str(scripts / "verify_release.py"), "v1.2.3"], capture_output=True, text=True)
            self.assertNotEqual(verify.returncode, 0)
            self.assertIn("unexpected release files", verify.stderr)


if __name__ == "__main__":
    unittest.main()
