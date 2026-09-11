"""Verify the Linux publication check still rejects damaged release bundles."""
import hashlib
import io
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import unittest


class ReleaseVerification(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        scripts = self.root / "scripts/release"
        scripts.mkdir(parents=True)
        source = Path(__file__).resolve().parents[1] / "release/verify_release.py"
        self.script = scripts / "verify_release.py"
        shutil.copyfile(source, self.script)
        self.bundle = self.root / "dist/v1.2.3"
        self.bundle.mkdir(parents=True)
        self.name = "contexthop_1.2.3_darwin_arm64.tar.gz"
        for name in ("LICENSE", "THIRD_PARTY_NOTICES"):
            (self.root / name).write_text("Fictional verification fixture.\n")
        self.write_bundle()

    def write_bundle(self, bad_notice=False):
        archive = self.bundle / self.name
        with tarfile.open(archive, "w:gz") as output:
            for name in ("chop", "LICENSE", "THIRD_PARTY_NOTICES"):
                data = b"fictional binary" if name == "chop" else (self.root / name).read_bytes()
                if bad_notice and name == "LICENSE":
                    data = b"different notice"
                info = tarfile.TarInfo(name)
                info.mode = 0o755 if name == "chop" else 0o644
                info.size = len(data)
                output.addfile(info, io.BytesIO(data))
        digest = hashlib.sha256(archive.read_bytes()).hexdigest()
        (self.bundle / "checksums.txt").write_text(f"{digest}  {self.name}\n")
        (self.bundle / "contexthop.rb").write_text(
            'depends_on :macos\ndepends_on arch: :arm64\nlicense "MIT"\n'
            'doc.install "LICENSE", "THIRD_PARTY_NOTICES"\n'
            f'url "https://github.com/infurio/homebrew-tap/releases/download/v1.2.3/{self.name}"\n'
            f'sha256 "{digest}"\n')

    def verify(self, archives_only=True):
        # Simulate Linux on every development platform; default verification
        # must still refuse to omit its macOS-native smoke test.
        code = "import platform,runpy,sys; platform.system=lambda: 'Linux'; path=sys.argv.pop(1); runpy.run_path(path,run_name='__main__')"
        args = [sys.executable, "-c", code, str(self.script), "v1.2.3"]
        if archives_only:
            args.append("--archives-only")
        return subprocess.run(args, capture_output=True, text=True)

    def test_archives_only_does_not_execute_the_binary(self):
        result = self.verify()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("native execution skipped", result.stdout)

    def test_corrupted_archive_is_rejected(self):
        with (self.bundle / self.name).open("ab") as archive:
            archive.write(b"corruption")
        result = self.verify()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("checksum mismatch", result.stderr)

    def test_notice_mismatch_is_rejected_even_with_valid_checksums(self):
        self.write_bundle(bad_notice=True)
        result = self.verify()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("licence contents differ", result.stderr)

    def test_default_still_requires_native_verification(self):
        result = self.verify(archives_only=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("native smoke tests require macOS", result.stderr)
