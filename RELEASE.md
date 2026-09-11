# Releasing ContextHop

Make changes, test locally, commit, then publish from your Apple Silicon Mac:

```sh
./scripts/release-local v0.7.5
```

Use an unused version. No PR, CI wait, repeated test suite, or Homebrew installation
is required to publish. `gh` must be logged in with write access to both
`infurio/contexthop` and `infurio/homebrew-tap`. Go, Python 3.12+, Zsh, and Gitleaks
must be installed.

## Normal development

1. Make the change and run relevant local tests. `make check` runs tests, vet,
   and a build; `make ci` adds formatting and race checks when needed. Reuse
   successful checks while the code and relevant inputs are unchanged.
2. Add short user-facing notes in `docs/releases/VERSION.md`.
3. Review the diff and fixture provenance, run `git diff --check`, and commit on
   `main`. Review all changed text and distinct changed media frames according to
   [AGENTS.md](AGENTS.md). Use a public commit identity. Never derive examples from
   real configuration. Update notices if dependencies changed.
4. Run `./scripts/release-local vVERSION`.

The command captures the clean commit, scans it for secrets, builds one versioned
archive using the warm local Go cache, and verifies its checksum, contents, and
native binary. It atomically pushes that commit to main and creates the version
tag, uploads the archive, and updates the tap formula. It refuses existing remote
tags/releases and never force-pushes. A divergent remote main must be reconciled
before publication.

When it prints **available through Brew**, users can run:

```sh
brew update && brew upgrade infurio/tap/contexthop
```

`brew update` refreshes the formula; `brew upgrade` installs the binary.
Source release notes are published afterward. That starts one background
Homebrew installation check on a fresh Mac. Publication does not wait for it;
GitHub reports failures through Actions. A failure discovered after publication
may require a new patch release. Do not wait for that job to report Brew availability.

GitHub CI runs only for optional PRs or manual dispatch. Direct pushes and tags
never start the old build-and-publish pipeline. Main should retain protection
against deletion and force pushes, without mandatory PRs or required checks.

## Build without publishing

```sh
./scripts/release-local v0.7.5 --prepare
```

This performs the local build, source scan, and verification without GitHub access.
The bundle is retained in `dist/v0.7.5`. Before running publication for the same
version, remove only that disposable, unpublished directory; publication builds
again from the clean commit. Preparation is for debugging, not a routine extra step.

## Interrupted publication

The command stops on errors and keeps the bundle in `dist/VERSION`. Inspect what
succeeded before retrying. Do not move tags or replace published assets.

- Before the atomic push: fix the problem, remove the unpublished local bundle,
  and run the command again. No remote release has been created.
- After the push but before upload: the tag exists. Use the retained bundle and
  `scripts/release/publish-release.sh` to finish upload. If a draft exists, inspect
  it first; the publisher intentionally refuses to overwrite it.
- After assets are published: rerun `scripts/release/update_tap.py vVERSION`, then
  `scripts/release/publish-source-release.sh`. Formula updates accept identical
  content on retry and reject downgrades or conflicting same-version contents.
- After the formula update: Brew is already ready; only source notes or background
  verification may remain.

For these recovery scripts, set `TAG=vVERSION`, `TAP_REPO=infurio/homebrew-tap`,
`GITHUB_REPOSITORY=infurio/contexthop`, and `GITHUB_SHA` to the released source
commit. Run them from that source checkout. They use your existing `gh` login.
Do not introduce a new version solely to retry a network failure.
