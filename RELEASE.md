# Test and release

```sh
make check
# Review and commit your changes on main.
make release VERSION=v0.7.5
```

Choose a new version. `make check` runs the isolated Go tests with race detection,
vet, formatting checks, and the release-script tests. Tests use fictional fixtures
and disposable configuration. Run them once; release does not repeat them.

Release builds the committed source, checks the binary in an isolated home,
packages it, pushes main and the tag, uploads the archive, and updates Homebrew.
No PRs, GitHub Actions, or additional account permissions are part of this process.
It uses your existing `origin` transport and `gh` login with access to the two
project repositories. Requires Apple Silicon macOS, Go, Python 3.12+, Zsh and
Gitleaks. Review changed publication content per [AGENTS.md](AGENTS.md).

Optional notes: `./scripts/release-local v0.7.5 --notes notes.md`.
Build without publishing: `./scripts/release-local v0.7.5 --prepare`.
Outputs are retained in `dist/v0.7.5`; remove an unpublished build before retrying.

Once the formula is updated, install with:

```sh
brew update && brew upgrade infurio/tap/contexthop
```

On failure, stop and inspect the completed steps. Never overwrite a published
version. Before the push, removing the unpublished output and retrying is safe.
After upload, use the retained formula and notes to finish the tap/source release
rather than rebuilding or uploading different bytes under the same tag.
