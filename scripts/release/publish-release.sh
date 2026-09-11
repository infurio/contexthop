#!/usr/bin/env bash
# Publish the locally verified bundle using the existing gh login. Never overwrite.
set -euo pipefail
: "${TAG:?Missing release tag}"
: "${TAP_REPO:?Missing tap repository}"
: "${GITHUB_SHA:?Missing source commit}"
[[ "$TAG" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# Authentication/network failures stop here; they must not be treated as absence.
gh api --paginate "repos/$TAP_REPO/releases?per_page=100" --jq '.[].tag_name' > "$work/tags"
if grep -Fxq "$TAG" "$work/tags"; then
  echo "Release $TAG already exists; refusing to overwrite it (including drafts)." >&2
  exit 1
fi
python3 "$(dirname "$0")/release_notes.py" "$TAG" > "$work/notes.md"
gh release create "$TAG" --repo "$TAP_REPO" --draft --title "ContextHop $TAG" \
  --notes-file "$work/notes.md" "dist/$TAG/"*.tar.gz "dist/$TAG/checksums.txt"
# Compare downloaded draft assets byte for byte before exposing them publicly.
gh release download "$TAG" --repo "$TAP_REPO" --dir "$work/download"
for asset in "dist/$TAG/"*.tar.gz "dist/$TAG/checksums.txt"; do
  cmp "$asset" "$work/download/$(basename "$asset")"
done
gh release edit "$TAG" --repo "$TAP_REPO" --draft=false --latest=false
