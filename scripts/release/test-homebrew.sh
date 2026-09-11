#!/usr/bin/env bash
# Run on an isolated CI runner: install the candidate into a temporary local tap.
set -euo pipefail
formula=$(python3 -c 'import pathlib,sys; print(pathlib.Path(sys.argv[1]).resolve())' "$1")
mode=${2:-published}
tap=contexthop/ci
export HOMEBREW_NO_AUTO_UPDATE=1
cleanup() {
  brew uninstall --force "$tap/contexthop" >/dev/null 2>&1 || true
  brew untap "$tap" >/dev/null 2>&1 || true
}
brew tap-new --no-git "$tap"
trap cleanup EXIT
target="$(brew --repository "$tap")/Formula/contexthop.rb"
cp "$formula" "$target"
if [[ "$mode" == local ]]; then
  python3 - "$target" "$(dirname "$formula")" <<'PY'
from pathlib import Path
import re
import sys
formula = Path(sys.argv[1])
archives = Path(sys.argv[2])
formula.write_text(re.sub(r'url "https://[^\"]+/([^/\"]+\.tar\.gz)"',
                          lambda m: 'url "' + (archives / m[1]).as_uri() + '"',
                          formula.read_text()))
PY
elif [[ "$mode" != published ]]; then
  echo "Expected local or published mode" >&2
  exit 1
fi
brew install --formula "$tap/contexthop"
brew test "$tap/contexthop"
chop version
