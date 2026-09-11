#!/usr/bin/env python3
"""Verify the complete release bundle, then smoke-test its native Mac binary."""
import argparse
import hashlib
import platform
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("version")
args = parser.parse_args()
if not re.fullmatch(r"v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)", args.version):
    parser.error("expected a stable vMAJOR.MINOR.PATCH tag")
version = args.version[1:]
root = Path(__file__).resolve().parents[2] / "dist" / args.version
names = {f"contexthop_{version}_darwin_{arch}.tar.gz" for arch in ("arm64",)}
assert {p.name for p in root.iterdir()} == names | {"checksums.txt", "contexthop.rb"}, "unexpected release files"
lines = (root / "checksums.txt").read_text().splitlines()
checksums = dict(line.split("  ", 1)[::-1] for line in lines)
assert len(lines) == 1 and set(checksums) == names, "unexpected checksum entries"
formula = (root / "contexthop.rb").read_text()
assert 'depends_on :macos' in formula and 'depends_on arch: :arm64' in formula and 'linux' not in formula and 'amd64' not in formula and 'on_intel' not in formula, "formula must target Apple Silicon macOS only"
for name in sorted(names):
    digest = hashlib.sha256((root / name).read_bytes()).hexdigest()
    assert digest == checksums[name], f"checksum mismatch: {name}"
    assert f'url "https://github.com/infurio/homebrew-tap/releases/download/{args.version}/{name}"' in formula
    assert f'sha256 "{digest}"' in formula
    with tarfile.open(root / name, "r:gz") as archive:
        members = archive.getmembers()
        assert len(members) == 3 and {m.name for m in members} == {"chop", "LICENSE", "THIRD_PARTY_NOTICES"}, "archive must contain chop and licence notices"
        for member in members:
            assert member.isfile(), "archive members must be regular files"
            assert member.uid == member.gid == 0 and not member.uname and not member.gname, "archive owner metadata must be anonymous"
            assert member.mode == (0o755 if member.name == "chop" else 0o644), "unexpected archive permissions"
            if member.name != "chop":
                expected = Path(__file__).resolve().parents[2] / member.name
                assert archive.extractfile(member).read() == expected.read_bytes(), "licence contents differ from source"
        assert 'license "MIT"' in formula, "formula must declare MIT"
        assert 'doc.install "LICENSE", "THIRD_PARTY_NOTICES"' in formula, "formula must install notices"
assert platform.system() == "Darwin", "native smoke tests require macOS"
assert platform.machine() == "arm64", "native smoke tests require Apple Silicon"
arch = "arm64"
with tempfile.TemporaryDirectory(prefix="contexthop-smoke-") as staging:
    binary = Path(staging) / "chop"
    with tarfile.open(root / f"contexthop_{version}_darwin_{arch}.tar.gz", "r:gz") as archive:
        binary.write_bytes(archive.extractfile("chop").read())
    binary.chmod(0o755)
    # Shell integration follows shared state automatically; smoke tests must use
    # a disposable home and must not inherit the caller's managed session.
    home = Path(staging) / "home"
    home.mkdir()
    smoke_env = {"HOME": str(home), "PATH": "/usr/bin:/bin", "TERM": "dumb",
                 "CONTEXTHOP_CACHE_DIR": str(Path(staging) / "cache")}
    assert subprocess.check_output([str(binary), "version"], text=True, env=smoke_env).strip() == f"contexthop {version}"
    assert "Usage:" in subprocess.check_output([str(binary), "help"], text=True, env=smoke_env)
    subprocess.run(["zsh", "-f", "-c", 'eval "$("$1" shell-init zsh --in-place)"; chop version', "_", str(binary)], check=True, env=smoke_env)
print(f"Verified {args.version} archives, formula, and native {arch} binary")
