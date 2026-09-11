#!/usr/bin/env python3
"""Refresh notices from the pinned Go toolchain and linked macOS ARM64 modules."""
import json
import os
from pathlib import Path
import subprocess

root = Path(__file__).resolve().parents[1]
env = dict(os.environ, GOTOOLCHAIN="go" + (root / ".go-version").read_text().strip(),
           GOCACHE=str(root / ".gocache"), GOOS="darwin", GOARCH="arm64", CGO_ENABLED="0")
raw = subprocess.check_output(["go", "list", "-deps", "-json", "./cmd/contexthop"], cwd=root, env=env, text=True)
modules = {}
decoder = json.JSONDecoder()
while raw.strip():
    package, end = decoder.raw_decode(raw.lstrip())
    raw = raw.lstrip()[end:]
    module = package.get("Module", {})
    if module and not module.get("Main"):
        if module.get("Replace"):
            raise SystemExit("Review replacement module licensing before generating notices")
        modules[module["Path"]] = module

goroot = Path(subprocess.check_output(["go", "env", "GOROOT"], env=env, text=True).strip())
parts = ["Third-party notices for ContextHop\n\n"
         "ContextHop is licensed under MIT (see LICENSE). The following notices apply\n"
         "to the Go runtime and dependencies included in the macOS ARM64 binary.\n"]

def append_notice(label, source):
    parts.append("\n" + "=" * 72 + "\n" + label + "\n\n" + source.read_text())

append_notice("Go standard library and runtime", goroot / "LICENSE")
for name, module in sorted(modules.items()):
    notices = sorted(p for p in Path(module["Dir"]).iterdir()
                     if p.is_file() and p.name.upper().startswith(("LICENSE", "LICENCE", "COPYING", "NOTICE")))
    if not notices:
        raise SystemExit(f"No licence found for {name}; manual review required")
    for notice in notices:
        append_notice(f"{name} {module['Version']} — {notice.name}", notice)
(root / "THIRD_PARTY_NOTICES").write_text("".join(parts))
print(f"Updated notices for Go and {len(modules)} linked modules")
