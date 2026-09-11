# Contributing to ContextHop

Bug reports, documentation improvements, and focused pull requests are welcome.
Read the [user guide](docs/usage.md) and search existing
[issues](https://github.com/infurio/contexthop/issues) before opening a new one.
For a substantial feature or change to selection behaviour, open an issue first
so the intended behaviour can be discussed.

## Reports and examples

Include `chop version`, your macOS and terminal versions, reproduction steps,
and expected versus actual behaviour. Use fictional accounts, projects, and
clusters. Redact screenshots and logs; do not attach credentials, tokens, full
catalogs, or kubeconfigs. Report security concerns through the
[private channel](SECURITY.md).

Real local context names and identifying configuration details are private, even
when they are not credentials. Never copy them into tests, fixtures, snapshots,
documentation, media, or review text. Construct fictional examples independently
using the Acme fixtures in `internal/testenv`; do not sanitize copies of live
configuration. See the [repository privacy requirements](AGENTS.md).

## Development

The supported development and release target is macOS Apple Silicon with zsh.
Install Go and the toolchain version in [.go-version](.go-version). The Makefile
selects that version. Provider CLIs are only needed for the integrations you use;
automated tests and [demos](docs/demos/README.md) use isolated fixtures and stubs.
See [architecture and development](docs/architecture.md) for the code layout.

Before submitting code or release-tool changes, run:

```sh
make ci
python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v
git diff --check
```

Add regression coverage for behaviour changes. For TUI changes, check normal
terminal sizes, keyboard navigation, search, cancellation, and managed/unmanaged
shell behaviour. Describe manual checks and anything you could not verify.
Documentation-only changes receive lightweight CI checks when they match the
[documented path rules](RELEASE.md#one-time-github-setup).

Keep PRs focused and explain the user-visible outcome. The project uses the
[MIT licence](LICENSE); contributions are made under the same licence.
Follow the [code of conduct](CODE_OF_CONDUCT.md).
