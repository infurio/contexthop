# Releasing ContextHop

A release starts when you push a version tag. GitHub builds the macOS Apple Silicon
binary, tests it, updates Homebrew, and publishes the release notes in both
repositories. You do not need to build or upload the release yourself.

The normal process is: **choose a version → prepare a PR → merge → push the tag →
wait for GitHub to finish**. Run the commands below from the repository root.
You’ll need Go, Zsh, Git, an authenticated `gh` CLI, and Gitleaks.

## 1. Choose the version and prepare the PR

Check which versions already exist:

```sh
gh release list --repo infurio/contexthop
gh release list --repo infurio/homebrew-tap
git ls-remote --tags origin
```

Choose an unused `vMAJOR.MINOR.PATCH`: usually a patch for fixes or a minor version
for new features. Beta/RC tags and leading zeroes are not supported.

Add `docs/releases/MAJOR.MINOR.PATCH.md` with the changes users will notice and any
upgrade instructions. GitHub uses this file for both release pages. Update other
docs as needed. If Go or dependencies changed, also run
`python3 scripts/update_notices.py` and review `THIRD_PARTY_NOTICES`.

Run the checks and smoke-test the affected features. The
[verification checklist](#release-verification) below helps you choose what to test.

```sh
make ci
python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v
git diff --check
```

Open a PR, include the test results, and merge after **Required checks** passes.
Release workflow changes must be merged too: the tag uses the workflow in its commit.

Demo recording is separate. If a demo needs updating, use
[`scripts/record-demos`](docs/demos/README.md) before merging. CI and release jobs
do not record demos.

## 2. Check the merged commit

```sh
git switch main
git pull --ff-only
git status --short
git log -1 --format='%H %s'
```

`git status --short` should print nothing, and the last commit should be the PR you
intend to release. Stop if either is wrong or the pull failed. Confirm that commit’s
GitHub CI passed before continuing.

Review the committed changes for credentials and personal details, including
commit author information. Use a public commit identity. Scan the exact commit:

```sh
release_scan_dir=$(mktemp -d)
git archive HEAD | tar -x -C "$release_scan_dir"
gitleaks dir --redact --no-banner "$release_scan_dir"
```

Resolve any findings before releasing. Review demo frames visually as well;
a text scan cannot read them. Keep scan reports outside the repository.

Secret scanning does not detect every private context name or organizational
identifier. Before publication, verify that tests, examples, snapshots and media
were constructed from fictional fixtures, not copied from live configuration.
Review all changed text and every distinct media frame against the
[repository privacy requirements](AGENTS.md). Unknown fixture provenance blocks
publication until the content is replaced with independently constructed fictional
data. Never commit private comparison inputs, matched values, or audit reports.

## 3. Push the release tag

Replace `vX.Y.Z` with your chosen version. Recheck that it is still unused, then run:

```sh
release_tag=vX.Y.Z
git tag -a "$release_tag" -m "ContextHop $release_tag"
git push origin "refs/tags/$release_tag"
```

**The push starts publication.** The tag supplies the binary’s version, so no source
version constant needs editing. Never reuse a tag, including one from an incomplete
release. Wait for this release to finish before starting another.

## 4. Wait, then confirm

Open the [Release workflow](https://github.com/infurio/contexthop/actions/workflows/release.yml),
or watch it from the terminal:

```sh
gh run list --workflow release.yml --branch "$release_tag"
# Replace RUN_ID with the ID shown for this release.
gh run watch RUN_ID --exit-status
```

Wait for **all five jobs**, including `source-release`, to pass. Then check both pages:

```sh
gh release view "$release_tag" --repo infurio/homebrew-tap
gh release view "$release_tag" --repo infurio/contexthop
```

The binary release should contain the ARM64 archive and `checksums.txt`. Both pages
should include your notes and the right source commit; the source release should
link to the downloads. Confirm the Homebrew formula has the new version.

Users can now upgrade:

```sh
brew update && brew upgrade infurio/tap/contexthop
```

## If something fails

Read the failed job’s log before retrying. Fix setup or permission problems, then
rerun **failed jobs only** if assets have already been published:

```sh
gh run rerun RUN_ID --failed
```

Do not delete, overwrite, or move a published version. A code fix normally needs a
new patch release. If an upload failed while the release was still a draft,
inspect that draft before deleting it and retrying; the publisher will not
overwrite it automatically.

The tap update can safely retry when its contents already match. It rejects
downgrades and conflicting contents for the same version. The source-release job
leaves an existing release unchanged. There is no automatic rollback.

## Reference

These details are useful when changing the release tooling or setting it up again.

<details>
<summary><strong>What GitHub does</strong></summary>

### Packaging and retries

| Job | What it does |
| --- | --- |
| `candidate` | Validates the tag, main ancestry, and notes; checks CI evidence; builds, verifies, and tests a local Homebrew install. |
| `publish` | Rechecks the same archive, uploads it as a draft, compares downloaded assets, then publishes. |
| `update-tap` | Tests installation through public download URLs, then updates the formula. |
| `verify-install` | Installs from the public tap on a fresh Mac runner. |
| `source-release` | Copies the notes and adds the binary download link in the source repository. |

Full CI is reused only from the latest main-branch push run of `ci.yml` for the
exact tagged commit, with all three jobs successful in that attempt. Missing,
failed, unfinished, or documentation-only evidence causes the candidate job to run
full checks instead. An API error also falls back to full checks.

The archive is built once and reused throughout. It contains `chop`, `LICENSE`,
and `THIRD_PARTY_NOTICES`, with no configuration or credentials. Provider CLIs are
installed separately. New releases target macOS ARM64; older Intel assets remain
available. Release runs are serialized, but are not guaranteed FIFO ordering.

### Local packaging

Optional, for debugging packaging before publication:

```sh
python3 scripts/release/package_release.py "$release_tag"
python3 scripts/release/verify_release.py "$release_tag"
```

These commands create and check `dist/<tag>/`. They do not publish anything.
Packaging refuses a nonempty output directory; remove only disposable local output
when rebuilding an unpublished candidate. Build paths and VCS metadata are omitted.

`scripts/release/test-homebrew.sh` installs and uninstalls a temporary candidate.
Run it on a disposable runner, not over your normal ContextHop installation.

### Failures and retries

See [If something fails](#if-something-fails). Separate jobs let a failed later
stage retry without rebuilding or republishing successful earlier stages.

</details>

<details>
<summary><strong>What to smoke-test</strong></summary>

### Release verification

Choose checks relevant to the changes and record what actually ran in the PR.
CI and fictional provider responses do not establish live-account behaviour.

- **Terminal UI:** selection priorities, dependent unstaging, ADC toggle/reset,
  apply, workspace save, identity choice, empty lists, filters, Options and Help.
  Check resizing, tag colours, and readability without colour. The
  [user guide](docs/usage.md) defines the expected interactions.
- **Shells and credentials:** managed and unmanaged Zsh, separate identities and
  contexts in separate processes, explicit subshells, current-shell switching,
  and preserved child exit status. With two integrated terminals, apply a shared
  config, pin one terminal with Apply here, then publish another shared config.
  Confirm only the follower changes, the pinned header previews the shared config,
  and Ctrl+G / Join shared rejoins without publishing Selected. Check the Not set
  state after `chop shared clear`, and setup guidance without integration.
  Check CLI and ADC identities separately;
  disabled bindings must not use ambient credentials.
- **Failure paths:** cancelled or failed authentication/validation must leave the
  active context unchanged. Completed discovery results must survive cancellation;
  cleanup removes only abandoned staging. Failed edits stay recoverable, changed
  dependencies require review, and noninteractive commands must not open browsers.
- **Kubernetes changes:** missing, unreadable, multiple, and conflicting kubeconfig
  sources; namespaces; endpoint/CA fingerprints; distinct API, access, and network
  errors. Activation must not add remote reachability checks.
- **Publication content:** source, diagnostics, release notes and every distinct
  demo frame for private data. Check archive contents, owner metadata and build paths.
- **Support claims:** verify SSH, tmux, remote development, or accessibility if the
  release claims support for them.

</details>

<details>
<summary><strong>One-time GitHub setup</strong></summary>

### One-time GitHub setup

- Enable Actions, the pinned official actions, and macOS runners.
- Add `HOMEBREW_TAP_TOKEN` to the source repository’s Actions secrets: a fine-grained
  token for `infurio/homebrew-tap` with Contents read/write and permission to update
  its default branch. The source repository’s `GITHUB_TOKEN` cannot write to the tap.
- Keep `Formula/contexthop.rb` in the tap with an explicit stable version.
- Require **Required checks** on `main`, and restrict `v*` tag creation/deletion to
  maintainers. The workflow does not configure branch or tag protection.

Normal CI runs on PRs, main pushes, and merge-queue entries. Documentation-only
changes use whitespace/link checks; code, demo scripts, configuration, and unknown
changes use the macOS suite. **Required checks** gates either path. Avoid workflow
path filters that could leave this required check pending. Publishing credentials
are used only in tag-triggered jobs.

</details>

<details>
<summary><strong>Making source public</strong></summary>

### Source publication

A release does not change repository visibility. The current-commit scan above
does not audit old commits, PRs, releases, or private backups.

Before making a repository public, audit those historical records too. If history
must be excluded, publish a reviewed export in a new repository using a public
commit identity. Exclude credentials, real catalogs, local environment files,
caches, generated binaries, and Git history; ignore rules do not remove files
already tracked by Git.

Inspect the export’s files, links, permissions, owner metadata, and media. Extract
and verify the final export, and repeat the review if its contents change. Keep
sensitive scan reports outside the export.

</details>
