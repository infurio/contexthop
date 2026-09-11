#!/usr/bin/env bash
# Add source release notes after binary publication and the tap update.
set -euo pipefail
: "${GITHUB_REPOSITORY:?Missing source repository}"
: "${TAG:?Missing release tag}"
: "${TAP_REPO:?Missing tap repository}"
[[ "$TAG" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# API failures must stop publication; an existing release is left untouched.
gh api --paginate "repos/$GITHUB_REPOSITORY/releases?per_page=100" --jq '.[].tag_name' > "$work/tags"
if grep -Fxq "$TAG" "$work/tags"; then
  echo "Source release $TAG already exists; leaving it unchanged."
  exit 0
fi
gh api "repos/$TAP_REPO/releases/tags/$TAG" --jq '.body' > "$work/notes.md"
printf '\n[Download binaries and SHA-256 checksums](https://github.com/%s/releases/tag/%s).\n' "$TAP_REPO" "$TAG" >> "$work/notes.md"
gh release create "$TAG" --repo "$GITHUB_REPOSITORY" --verify-tag \
  --title "ContextHop $TAG" --notes-file "$work/notes.md" --latest
