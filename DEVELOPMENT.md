# Development and deployment

Run commands from the repository root on an Apple Silicon Mac. You need Go,
Python 3.12+, Zsh, Gitleaks, and your existing Git/GitHub CLI access.

## 1. Make a change and try it locally

```sh
git switch main
git pull --ff-only
```

Edit the code, then launch the development shell:

```sh
./scripts/dev
chop
```

This uses the checkout with your real local configuration. It does not replace
the Homebrew installation. After further edits, run `make build`, then `chop`
again. Run `exit` to leave the development shell.

## 2. Run the isolated tests

```sh
make check
```

This runs Go tests with race detection, vet, formatting checks, and script tests.
Tests use a temporary home, a clean environment, and fictional provider fixtures.
Fix any failures before continuing. The release command does not repeat the tests.

## 3. Commit and release

Review all changes, including new files, for correctness and private information:

```sh
git status --short
git diff
```

Then commit and publish. Replace `v0.7.5` with the next unused version:

```sh
git add -A
git commit -m "Describe the change"
make release VERSION=v0.7.5
```

That final command builds once, pushes main and the version tag, uploads the
binary, updates the Homebrew formula, and creates the release pages. No separate
push, PR, CI wait, or mandatory release-notes file is needed.

## 4. Install the release

```sh
brew update && brew upgrade infurio/tap/contexthop
```

If publication fails, inspect the error before retrying. Outputs are in
`dist/VERSION`. Never overwrite a published version. Before anything is pushed,
you can remove the unpublished output and retry; after upload, finish the missing
publication step using the retained files.
