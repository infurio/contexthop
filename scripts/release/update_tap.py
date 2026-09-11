#!/usr/bin/env python3
"""Update the tap formula using the contents API, refusing version downgrades."""
import argparse
import base64
import json
import os
from pathlib import Path
import re
import subprocess

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("tag")
args = parser.parse_args()
pattern = r"(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)"
if not re.fullmatch("v" + pattern, args.tag):
    parser.error("expected a stable vMAJOR.MINOR.PATCH tag")
repo = os.environ["TAP_REPO"]
endpoint = f"repos/{repo}/contents/Formula/contexthop.rb"
# A missing formula is a setup error; never mistake permission failures for 404.
current = json.loads(subprocess.check_output(["gh", "api", endpoint], text=True))
old_formula = base64.b64decode(current["content"]).decode()
old_version = re.search(r'^  version "(' + pattern + r')"$', old_formula, re.MULTILINE)
if old_version is None:
    raise SystemExit("Cannot determine current tap version; refusing automatic update")
new_version = tuple(map(int, args.tag[1:].split(".")))
if tuple(map(int, old_version[1].split("."))) > new_version:
    raise SystemExit("Refusing to downgrade the Homebrew formula")
formula = Path(__file__).resolve().parents[2] / "dist" / args.tag / "contexthop.rb"
content = formula.read_bytes()
if old_formula.encode() == content:
    print("Formula already matches this release")
else:
    if tuple(map(int, old_version[1].split("."))) == new_version:
        raise SystemExit("Version already exists with different formula content; investigate before retrying")
    payload = json.dumps({"message": f"Update contexthop to {args.tag}", "sha": current["sha"],
                          "content": base64.b64encode(content).decode()})
    subprocess.run(["gh", "api", "--method", "PUT", endpoint, "--input", "-"],
                   input=payload, text=True, check=True, stdout=subprocess.DEVNULL)
    print(f"Updated {repo} to {args.tag}")
# Mark latest only after the formula has advanced successfully.
subprocess.run(["gh", "release", "edit", args.tag, "--repo", repo, "--latest"], check=True)
