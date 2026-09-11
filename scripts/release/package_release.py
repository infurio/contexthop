#!/usr/bin/env python3
"""Build release archives and a Homebrew formula; does not publish anything."""
import argparse
import hashlib
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("version", help="Release tag, for example v0.1.0")
args = parser.parse_args()
if not re.fullmatch(r"v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)", args.version):
    parser.error("version must be a stable vMAJOR.MINOR.PATCH tag")
version = args.version[1:]
root = Path(__file__).resolve().parents[2]
out = root / "dist" / args.version
if out.exists() and any(out.iterdir()):
    parser.error(f"output directory is not empty: {out}; use a new version or remove local build output")
out.mkdir(parents=True, exist_ok=True)
checksums = {}
for goos, goarch in [("darwin", "arm64")]:
    name = f"contexthop_{version}_{goos}_{goarch}.tar.gz"
    env = dict(os.environ, GOOS=goos, GOARCH=goarch, CGO_ENABLED="0",
               GOTOOLCHAIN=os.environ.get("TOOLCHAIN", "go" + (root / ".go-version").read_text().strip()),
               GOCACHE=os.environ.get("GOCACHE", str(root / ".gocache")))
    with tempfile.TemporaryDirectory(prefix="contexthop-release-") as staging:
        binary = Path(staging) / "chop"
        subprocess.run(["go", "build", "-trimpath", "-buildvcs=false",
                        "-ldflags", f"-s -w -X main.version={version}",
                        "-o", str(binary), "./cmd/contexthop"], cwd=root, env=env, check=True)
        with tarfile.open(out / name, "w:gz") as archive:
            for source, member, mode in [(binary, "chop", 0o755),
                                         (root / "LICENSE", "LICENSE", 0o644),
                                         (root / "THIRD_PARTY_NOTICES", "THIRD_PARTY_NOTICES", 0o644)]:
                info = archive.gettarinfo(str(source), member)
                info.uid = info.gid = 0
                info.uname = info.gname = ""
                info.mtime = 0
                info.mode = mode
                with source.open("rb") as contents:
                    archive.addfile(info, contents)
    checksums[name] = hashlib.sha256((out / name).read_bytes()).hexdigest()
    print(name, flush=True)

base = f"https://github.com/infurio/homebrew-tap/releases/download/{args.version}"
lines = ['class Contexthop < Formula',
         '  desc "Isolated terminal contexts for Google Cloud, Kubernetes, and Docker"',
         '  homepage "https://github.com/infurio/homebrew-tap"',
         f'  version "{version}"', '  license "MIT"', '  depends_on :macos', '  depends_on arch: :arm64', '']
for platform, goos in [("macos", "darwin")]:
    lines.append(f"  on_{platform} do")
    for arch, goarch in [("arm", "arm64")]:
        name = f"contexthop_{version}_{goos}_{goarch}.tar.gz"
        lines.extend([f"    on_{arch} do", f'      url "{base}/{name}"',
                      f'      sha256 "{checksums[name]}"', '    end'])
    lines.extend(['  end', ''])
lines.extend(['  def install', '    bin.install "chop"', '    doc.install "LICENSE", "THIRD_PARTY_NOTICES"', '  end', '',
              '  test do', '    assert_equal "contexthop #{version}", shell_output("#{bin}/chop version").strip',
              '    assert_match "Usage:", shell_output("#{bin}/chop help")',
              '  end', 'end', ''])
(out / "contexthop.rb").write_text('\n'.join(lines))
(out / "checksums.txt").write_text(''.join(f"{digest}  {name}\n" for name, digest in checksums.items()))
print(f"Prepared {out}; inspect and test before publishing.")
